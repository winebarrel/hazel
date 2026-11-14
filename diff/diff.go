package diff

import (
	"slices"

	"github.com/winebarrel/hazel/model"
	"github.com/winebarrel/hazel/parser"
)

const (
	removed           = "drop: removed from schema"
	definitionChanged = "drop and create: definition changed"
)

// Diff returns the plan that takes current to desired.
//
// Objects are matched by name. A table missing from desired is dropped and a
// new one is created; a column is added or dropped with ALTER TABLE. An
// index, a view or a trigger whose definition changed is dropped and
// created again. A virtual table whose definition changed is dropped and
// created again, which loses its data.
//
// DROP COLUMN, RENAME TO and RENAME COLUMN check every view and trigger in
// the schema, so when the plan runs one, a view or a trigger that names a
// table dropping a column, or a view that is dropped before the ALTER, is
// dropped and created again too, and so on for the views that name those.
//
// A table or a column whose desired definition has -- hazel:renamed-from is
// renamed first, when the old name is in current and the new one is not.
// SQLite rewrites what refers to the old name (foreign keys, views and
// triggers) as it renames, so current is compared as it is after the
// renames: the renames are run on a copy of it in memory. An object marked
// with -- hazel:ignore is not created, changed or dropped, and so is one
// that WithExclude leaves out. Nothing is planned for them, not even to make
// way for an ALTER that they stand in the way of, and not to create again
// what a DROP VIEW or DROP TABLE takes with it: the user is assumed to know
// SQLite, and the apply fails or the object is gone.
//
// NOT NULL is set and dropped with ALTER COLUMN, a CHECK is added with ADD
// CHECK, and a named CHECK or FOREIGN KEY is dropped with DROP CONSTRAINT.
// The plan does not look at the rows: SET NOT NULL and ADD CHECK fail at
// apply time when existing rows break them, since SQLite checks the rows
// as it runs them.
//
// Expressions (CHECK, DEFAULT, generated columns, index expressions and the
// WHERE of a partial index) and views are compared by the bytecode SQLite
// compiles them to (see explainer). Type names, collation names, triggers
// and virtual tables are compared with parser.TokensEqual. Constraints are
// compared by their content, not their names. A table that needs a change ALTER TABLE cannot
// make is rebuilt (see rebuild), and Plan.Rebuilds lists it: a column's type,
// DEFAULT, COLLATE or generated expression, the PRIMARY KEY, a UNIQUE
// constraint, adding or changing a FOREIGN KEY, dropping an unnamed CHECK or
// FOREIGN KEY, STRICT, WITHOUT ROWID, AUTOINCREMENT, and adding a STORED
// column or a column with a non-constant DEFAULT. The rebuild makes the
// table's other changes too, so it gets no ALTER.
func Diff(current, desired *model.Schema, opts ...Option) (*Plan, error) {
	ex, err := newExplainer(desired)
	if err != nil {
		return nil, err
	}
	defer ex.close()
	d := &differ{
		ex:      ex,
		exclude: func(string) bool { return false },
		cur:     current, des: desired,
		dependencies: map[string]string{}, recreatedViews: map[string]bool{}, rebuilt: map[string]bool{},
	}
	for _, o := range opts {
		o(d)
	}
	if err := d.renames(); err != nil {
		return nil, err
	}
	d.tables()
	d.indexes()
	d.views()
	d.triggers()
	return d.b.plan(), nil
}

// Option changes how Diff plans.
type Option func(*differ)

// WithExclude leaves out of the plan the tables, views and virtual tables
// whose name exclude reports, and the indexes and triggers that belong to
// them. They stay in the schemas Diff compiles and replays, so that what
// reads them still compiles, but no statement is planned for them, not even
// to make way for a DROP COLUMN or a RENAME: if one of them stands in the
// way, the apply fails.
func WithExclude(exclude func(name string) bool) Option {
	return func(d *differ) { d.exclude = exclude }
}

type differ struct {
	ex       *explainer
	exclude  func(name string) bool
	cur, des *model.Schema
	b        builder
	// rechecks reports whether the plan runs an ALTER TABLE that checks every
	// view and trigger in the schema: DROP COLUMN, RENAME TO and RENAME
	// COLUMN do.
	rechecks bool
	// dependencies maps the key of each table that drops a column, and of
	// each view that is dropped and created again, to the reason a view or a
	// trigger naming it gives: "users, which drops a column". It is used only
	// when the plan rechecks the schema. A renamed table is not there: SQLite
	// rewrites what names it.
	dependencies map[string]string
	// rebuilt holds the keys of the tables that are rebuilt. DROP TABLE drops
	// their indexes and triggers.
	rebuilt map[string]bool
	// recreatedViews holds the keys of the views that are dropped and created
	// again. DROP VIEW drops the triggers on the view as well.
	recreatedViews map[string]bool
}

func q(name string) string {
	return parser.QuoteIdent(name)
}

func (d *differ) tables() {
	for key, cur := range d.cur.Tables.All() {
		des, ok := d.des.Tables.GetOk(key)
		switch {
		case d.exclude(cur.Name):
		case ok && des.Ignored:
		case !ok:
			d.b.add(dropTables, removed, "DROP TABLE "+q(cur.Name))
			d.b.dataLoss = append(d.b.dataLoss, "table  "+cur.Name)
		case cur.Virtual || des.Virtual:
			if cur.Virtual != des.Virtual || !sqlEqual(cur.SQL, des.SQL) {
				d.b.add(dropTables, definitionChanged, "DROP TABLE "+q(cur.Name))
				d.b.add(createTables, "", des.SQL)
				d.b.dataLoss = append(d.b.dataLoss, "table  "+cur.Name)
			}
		default:
			d.alterTable(cur, des)
		}
	}
	for key, des := range d.des.Tables.All() {
		if !d.cur.Tables.Has(key) && !des.Ignored && !d.exclude(des.Name) {
			d.b.add(createTables, "", des.SQL)
		}
	}
}

func (d *differ) alterTable(cur, des *model.Table) {
	var added []string
	for key := range des.Columns.Keys() {
		if !cur.Columns.Has(key) {
			added = append(added, key)
		}
	}
	// ADD COLUMN copies the column definition, so the CHECK and REFERENCES
	// constraints written in the definition of a column being added come
	// with it and are left out of the comparison. Table constraints on the
	// new columns do not come with it, and are compared like any other.
	inAdded := func(column string) bool {
		return column != "" && slices.Contains(added, model.Key(column))
	}
	desFKs := slices.DeleteFunc(slices.Clone(des.ForeignKeys), func(fk *model.ForeignKey) bool { return inAdded(fk.Column) })
	desChecks := slices.DeleteFunc(slices.Clone(des.Checks), func(c *model.Check) bool { return inAdded(c.Column) })

	// needs lists the changes that ALTER TABLE cannot make, and changes all
	// the changes, for the comment of a rebuild.
	var needs, changes []string
	need := func(what string) {
		needs = append(needs, what)
		changes = append(changes, what)
	}
	if cur.Strict != des.Strict {
		need("STRICT")
	}
	if cur.WithoutRowid != des.WithoutRowid {
		need("WITHOUT ROWID")
	}
	if cur.Autoincrement != des.Autoincrement {
		need("AUTOINCREMENT")
	}
	if !indexColumnsEqual(cur.PrimaryKey, des.PrimaryKey) {
		need("PRIMARY KEY")
	}
	if !uniquesEqual(cur.Uniques, des.Uniques) {
		need("UNIQUE constraint")
	}
	// A named FOREIGN KEY or CHECK that is gone is dropped with DROP
	// CONSTRAINT, and a new CHECK is added with ADD CHECK. An unnamed one
	// cannot be dropped that way, and a FOREIGN KEY cannot be added.
	dropFKs, addFKs := unmatched(cur.ForeignKeys, desFKs, foreignKeyEqual)
	// CHECK constraints are compared by their expressions, not their names,
	// and a CHECK of a column equals the same CHECK of the table.
	dropChecks, addChecks := unmatched(cur.Checks, desChecks, func(a, b *model.Check) bool {
		return d.ex.sameExpr(a.Expr, b.Expr, des.Name)
	})
	switch {
	case len(addFKs) > 0 || slices.ContainsFunc(dropFKs, func(fk *model.ForeignKey) bool { return fk.Name == "" }):
		need("FOREIGN KEY constraint")
	case len(dropFKs) > 0:
		changes = append(changes, "FOREIGN KEY constraint")
	}
	switch {
	case slices.ContainsFunc(dropChecks, func(c *model.Check) bool { return c.Name == "" }):
		need("CHECK constraint")
	case len(dropChecks) > 0 || len(addChecks) > 0:
		changes = append(changes, "CHECK constraint")
	}

	var dropColumns, notNull []string
	for key, c := range cur.Columns.All() {
		dc, ok := des.Columns.GetOk(key)
		if !ok {
			dropColumns = append(dropColumns, c.Name)
			changes = append(changes, "drops column "+c.Name)
			continue
		}
		for _, what := range d.columnChanges(c, dc, des.Name) {
			need(what + " of " + dc.Name)
		}
		if c.NotNull != dc.NotNull {
			change := "DROP NOT NULL"
			if dc.NotNull {
				change = "SET NOT NULL"
			}
			notNull = append(notNull, "ALTER COLUMN "+q(c.Name)+" "+change)
			changes = append(changes, "NOT NULL of "+dc.Name)
		}
	}
	for _, key := range added {
		c := des.Columns.Get(key)
		switch {
		case c.Generated == model.GeneratedStored:
			need("adds STORED column " + c.Name)
		case c.Default != nil && !isConstant(*c.Default):
			need("adds column " + c.Name + " with a non-constant DEFAULT")
		default:
			changes = append(changes, "adds column "+c.Name)
		}
	}

	if len(needs) > 0 {
		d.rebuild(cur, des, changes)
		return
	}

	// A CHECK or a FOREIGN KEY that uses a column must be dropped before the
	// column, and a new CHECK may use a new column.
	alter := "ALTER TABLE " + q(cur.Name) + " "
	for _, fk := range dropFKs {
		d.b.add(alterTables, removed, alter+"DROP CONSTRAINT "+q(fk.Name))
	}
	for _, c := range dropChecks {
		d.b.add(alterTables, removed, alter+"DROP CONSTRAINT "+q(c.Name))
	}
	for _, name := range dropColumns {
		d.b.add(alterTables, removed, alter+"DROP COLUMN "+q(name))
		d.b.dataLoss = append(d.b.dataLoss, "column "+cur.Name+"."+name)
		d.rechecks = true
		d.dependencies[model.Key(cur.Name)] = cur.Name + ", which drops a column"
	}
	if len(added) > 0 {
		defs := columnDefinitions(des.SQL)
		for _, key := range added {
			d.b.add(alterTables, "", alter+"ADD COLUMN "+defs[key].SQL())
		}
	}
	for _, change := range notNull {
		d.b.add(alterTables, "", alter+change)
	}
	for _, c := range addChecks {
		constraint := ""
		if c.Name != "" {
			constraint = "CONSTRAINT " + q(c.Name) + " "
		}
		d.b.add(alterTables, "", alter+"ADD "+constraint+"CHECK ("+c.Expr+")")
	}
}

// columnChanges lists what differs between two columns of table that ALTER
// TABLE cannot change. NOT NULL is not among them: ALTER COLUMN changes it.
// The DEFAULT and the generated expression are compared by their bytecode.
func (d *differ) columnChanges(a, b *model.Column, table string) []string {
	var changes []string
	if !sqlEqual(a.Type, b.Type) {
		changes = append(changes, "type")
	}
	if (a.Default == nil) != (b.Default == nil) || a.Default != nil && !d.ex.sameExpr(*a.Default, *b.Default, "") {
		changes = append(changes, "DEFAULT")
	}
	if !parser.EqualFold(collation(a), collation(b)) {
		changes = append(changes, "COLLATE")
	}
	if a.Generated != b.Generated || a.Generated != model.NotGenerated && !d.ex.sameExpr(a.GeneratedExpr, b.GeneratedExpr, table) {
		changes = append(changes, "generated expression")
	}
	return changes
}

// isConstant reports whether ADD COLUMN accepts the DEFAULT on a table with
// rows: a literal, a signed number, NULL, TRUE, FALSE or a word, bare or in
// double quotes (SQLite takes it for a string), in any number of
// parentheses. sqlite3ValueFromExpr, which ADD COLUMN uses, can
// give a value for those and not for an expression, a function or
// CURRENT_TIME, CURRENT_DATE and CURRENT_TIMESTAMP.
func isConstant(dflt string) bool {
	tokens := statement(dflt).Significant()
	for len(tokens) >= 2 && tokens[0].IsPunct("(") && tokens[len(tokens)-1].IsPunct(")") {
		tokens = tokens[1 : len(tokens)-1]
	}
	if len(tokens) == 2 && (tokens[0].IsPunct("+") || tokens[0].IsPunct("-")) && tokens[1].Kind == parser.Number {
		return true
	}
	if len(tokens) != 1 {
		return false
	}
	switch t := tokens[0]; t.Kind {
	case parser.Number, parser.String, parser.Blob:
		return true
	case parser.QuotedIdent:
		return true
	case parser.Ident:
		return !t.IsKeyword("CURRENT_TIME") && !t.IsKeyword("CURRENT_DATE") && !t.IsKeyword("CURRENT_TIMESTAMP")
	}
	return false
}

// columnDefinitions returns each column definition of a CREATE TABLE, keyed
// by the column's key. SQLite stores a CREATE TABLE ... AS SELECT as a CREATE
// TABLE with a list of columns, so every table has one.
func columnDefinitions(sql string) map[string]parser.TableElement {
	defs := map[string]parser.TableElement{}
	for _, e := range parser.TableElements(statement(sql)) {
		if e.Column != "" {
			defs[model.Key(e.Column)] = e
		}
	}
	return defs
}

func (d *differ) indexes() {
	for key, cur := range d.cur.Indexes.All() {
		des, ok := d.des.Indexes.GetOk(key)
		switch {
		case d.exclude(cur.Table) || ok && d.exclude(des.Table):
		case ok && des.Ignored:
		case d.rebuilt[model.Key(cur.Table)]:
			// The index goes with the table.
		case !ok:
			// An index goes with its table when the table is dropped.
			if d.des.Tables.Has(model.Key(cur.Table)) {
				d.b.add(dropIndexes, removed, "DROP INDEX "+q(cur.Name))
			}
		case !d.ex.sameIndex(cur, des):
			d.b.add(dropIndexes, definitionChanged, "DROP INDEX "+q(cur.Name))
			d.b.add(createIndexes, "", des.SQL)
		}
	}
	for key, des := range d.des.Indexes.All() {
		if des.Ignored || d.exclude(des.Table) {
			continue
		}
		if !d.cur.Indexes.Has(key) || d.rebuilt[model.Key(des.Table)] {
			d.b.add(createIndexes, "", des.SQL)
		}
	}
}

func (d *differ) views() {
	var kept []string
	for key, cur := range d.cur.Views.All() {
		des, ok := d.des.Views.GetOk(key)
		switch {
		case d.exclude(cur.Name):
		case ok && des.Ignored:
		case !ok:
			d.b.add(dropViewsAndTriggers, removed, "DROP VIEW "+q(cur.Name))
		case !d.ex.sameView(cur, des):
			d.recreateView(key, definitionChanged)
		default:
			kept = append(kept, key)
		}
	}
	// A kept view that names a table dropping a column, or a view created
	// again, is created again too, until no more views are added.
	for d.rechecks {
		before := len(kept)
		kept = slices.DeleteFunc(kept, func(key string) bool {
			reason := d.dependency(d.cur.Views.Get(key).SQL)
			if reason != "" {
				d.recreateView(key, dependsOn(reason))
			}
			return reason != ""
		})
		if len(kept) == before {
			break
		}
	}
	for key, des := range d.des.Views.All() {
		if !d.cur.Views.Has(key) && !des.Ignored && !d.exclude(des.Name) {
			d.b.add(createViews, "", des.SQL)
		}
	}
}

func (d *differ) recreateView(key, comment string) {
	cur, des := d.cur.Views.Get(key), d.des.Views.Get(key)
	d.b.add(dropViewsAndTriggers, comment, "DROP VIEW "+q(cur.Name))
	d.b.add(createViews, "", des.SQL)
	d.recreatedViews[key] = true
	d.dependencies[key] = cur.Name + ", which is created again"
}

func (d *differ) triggers() {
	for key, cur := range d.cur.Triggers.All() {
		des, ok := d.des.Triggers.GetOk(key)
		if d.exclude(cur.Table) || ok && d.exclude(des.Table) || ok && des.Ignored {
			continue
		}
		table := model.Key(cur.Table)
		// The DROP VIEW of a view created again drops its triggers, and so
		// does the DROP TABLE of a table that is dropped or rebuilt.
		dropped := d.recreatedViews[table] || d.rebuilt[table] || !d.des.Tables.Has(table) && !d.des.Views.Has(table)
		switch {
		case !ok:
			if !dropped {
				d.b.add(dropViewsAndTriggers, removed, "DROP TRIGGER "+q(cur.Name))
			}
		case !sqlEqual(cur.SQL, des.SQL):
			if !dropped {
				d.b.add(dropViewsAndTriggers, definitionChanged, "DROP TRIGGER "+q(cur.Name))
			}
			d.b.add(createTriggers, "", des.SQL)
		case dropped:
			d.b.add(createTriggers, "", des.SQL)
		case d.rechecks:
			if reason := d.dependency(cur.SQL); reason != "" {
				d.b.add(dropViewsAndTriggers, dependsOn(reason), "DROP TRIGGER "+q(cur.Name))
				d.b.add(createTriggers, "", des.SQL)
			}
		}
	}
	for key, des := range d.des.Triggers.All() {
		if !d.cur.Triggers.Has(key) && !des.Ignored && !d.exclude(des.Table) {
			d.b.add(createTriggers, "", des.SQL)
		}
	}
}

func dependsOn(reason string) string {
	return "drop and create: depends on " + reason
}

// dependency returns the reason in dependencies for the first name in sql
// that is there, or "". The check is conservative: a name that stands for
// something else, such as a column, counts too, and costs only a view or a
// trigger created again.
func (d *differ) dependency(sql string) string {
	for _, t := range statement(sql).Significant() {
		if reason, ok := d.dependencies[model.Key(t.Name())]; ok && t.Name() != "" {
			return reason
		}
	}
	return ""
}

// statement tokenizes SQL that SQLite has already accepted, so it does not
// fail.
func statement(sql string) parser.Statement {
	tokens, _ := parser.Tokenize(sql)
	return parser.Statement{Tokens: tokens}
}

func sqlEqual(a, b string) bool {
	return parser.TokensEqual(statement(a).Tokens, statement(b).Tokens)
}

// collation returns the collation of a column. A column without COLLATE uses
// BINARY, so writing COLLATE BINARY changes nothing.
func collation(c *model.Column) string {
	if c.Collation == "" {
		return "BINARY"
	}
	return c.Collation
}

func indexColumnsEqual(a, b []*model.IndexColumn) bool {
	return slices.EqualFunc(a, b, func(x, y *model.IndexColumn) bool {
		return parser.EqualFold(x.Name, y.Name) && x.Desc == y.Desc && parser.EqualFold(x.Collation, y.Collation)
	})
}

// uniquesEqual compares the UNIQUE constraints regardless of their order.
func uniquesEqual(a, b []*model.Unique) bool {
	return sameElements(a, b, func(x, y *model.Unique) bool { return indexColumnsEqual(x.Columns, y.Columns) })
}

// sameElements reports whether a and b hold the same elements, each as many
// times, in any order.
func sameElements[T any](a, b []T, eq func(T, T) bool) bool {
	aOnly, bOnly := unmatched(a, b, eq)
	return len(aOnly) == 0 && len(bOnly) == 0
}

// unmatched pairs up the elements of a and b that are equal, in any order,
// and returns those of each that are left.
func unmatched[T any](a, b []T, eq func(T, T) bool) (aOnly, bOnly []T) {
	bOnly = slices.Clone(b)
	for _, x := range a {
		i := slices.IndexFunc(bOnly, func(y T) bool { return eq(x, y) })
		if i < 0 {
			aOnly = append(aOnly, x)
			continue
		}
		bOnly = slices.Delete(bOnly, i, i+1)
	}
	return aOnly, bOnly
}

func foreignKeyEqual(a, b *model.ForeignKey) bool {
	return namesEqual(a.Columns, b.Columns) &&
		parser.EqualFold(a.RefTable, b.RefTable) &&
		namesEqual(a.RefColumns, b.RefColumns) &&
		a.OnUpdate == b.OnUpdate &&
		a.OnDelete == b.OnDelete &&
		a.Deferred == b.Deferred
}

func namesEqual(a, b []string) bool {
	return slices.EqualFunc(a, b, parser.EqualFold)
}
