package diff

import (
	"fmt"
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
// with -- hazel:ignore is not created, changed or dropped.
//
// NOT NULL is set and dropped with ALTER COLUMN, a CHECK is added with ADD
// CHECK, and a named CHECK or FOREIGN KEY is dropped with DROP CONSTRAINT.
// The plan does not look at the rows: SET NOT NULL and ADD CHECK fail at
// apply time when existing rows break them, since SQLite checks the rows
// as it runs them.
//
// Definitions are compared with parser.TokensEqual, and constraints by their
// content, not their names. The changes that need a table rebuild are not
// supported yet and return an error: a column's type, DEFAULT, COLLATE or
// generated expression, the PRIMARY KEY, a UNIQUE constraint, adding or
// changing a FOREIGN KEY, dropping an unnamed CHECK or FOREIGN KEY, STRICT,
// WITHOUT ROWID and AUTOINCREMENT.
func Diff(current, desired *model.Schema) (*Plan, error) {
	d := &differ{cur: current, des: desired, dependencies: map[string]string{}, recreatedViews: map[string]bool{}}
	if err := d.renames(); err != nil {
		return nil, err
	}
	if err := d.tables(); err != nil {
		return nil, err
	}
	d.indexes()
	d.views()
	d.triggers()
	return d.b.plan(), nil
}

type differ struct {
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
	// recreatedViews holds the keys of the views that are dropped and created
	// again. DROP VIEW drops the triggers on the view as well.
	recreatedViews map[string]bool
}

func q(name string) string {
	return parser.QuoteIdent(name)
}

func (d *differ) tables() error {
	for key, cur := range d.cur.Tables.All() {
		des, ok := d.des.Tables.GetOk(key)
		switch {
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
			if err := d.alterTable(cur, des); err != nil {
				return err
			}
		}
	}
	for key, des := range d.des.Tables.All() {
		if !d.cur.Tables.Has(key) && !des.Ignored {
			d.b.add(createTables, "", des.SQL)
		}
	}
	return nil
}

func (d *differ) alterTable(cur, des *model.Table) error {
	unsupported := func(what string) error {
		return fmt.Errorf("table %s: changing %s is not supported yet", des.Name, what)
	}
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
	switch {
	case cur.Strict != des.Strict:
		return unsupported("STRICT")
	case cur.WithoutRowid != des.WithoutRowid:
		return unsupported("WITHOUT ROWID")
	case cur.Autoincrement != des.Autoincrement:
		return unsupported("AUTOINCREMENT")
	case !indexColumnsEqual(cur.PrimaryKey, des.PrimaryKey):
		return unsupported("the PRIMARY KEY")
	case !uniquesEqual(cur.Uniques, des.Uniques):
		return unsupported("a UNIQUE constraint")
	}

	// A named FOREIGN KEY or CHECK that is gone is dropped with DROP
	// CONSTRAINT, and a new CHECK is added with ADD CHECK. An unnamed one
	// cannot be dropped that way, and a FOREIGN KEY cannot be added.
	dropFKs, addFKs := unmatched(cur.ForeignKeys, desFKs, foreignKeyEqual)
	dropChecks, addChecks := unmatched(cur.Checks, desChecks, checkEqual)
	if len(addFKs) > 0 || slices.ContainsFunc(dropFKs, func(fk *model.ForeignKey) bool { return fk.Name == "" }) {
		return unsupported("a FOREIGN KEY constraint")
	}
	if slices.ContainsFunc(dropChecks, func(c *model.Check) bool { return c.Name == "" }) {
		return unsupported("a CHECK constraint")
	}

	var dropColumns, notNull []string
	for key, c := range cur.Columns.All() {
		dc, ok := des.Columns.GetOk(key)
		switch {
		case !ok:
			dropColumns = append(dropColumns, c.Name)
		case !columnEqual(c, dc):
			return unsupported("column " + dc.Name)
		case c.NotNull != dc.NotNull:
			change := "DROP NOT NULL"
			if dc.NotNull {
				change = "SET NOT NULL"
			}
			notNull = append(notNull, "ALTER COLUMN "+q(c.Name)+" "+change)
		}
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
	return nil
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
		case ok && des.Ignored:
		case !ok:
			// An index goes with its table when the table is dropped.
			if d.des.Tables.Has(model.Key(cur.Table)) {
				d.b.add(dropIndexes, removed, "DROP INDEX "+q(cur.Name))
			}
		case !sqlEqual(cur.SQL, des.SQL):
			d.b.add(dropIndexes, definitionChanged, "DROP INDEX "+q(cur.Name))
			d.b.add(createIndexes, "", des.SQL)
		}
	}
	for key, des := range d.des.Indexes.All() {
		if !d.cur.Indexes.Has(key) && !des.Ignored {
			d.b.add(createIndexes, "", des.SQL)
		}
	}
}

func (d *differ) views() {
	var kept []string
	for key, cur := range d.cur.Views.All() {
		des, ok := d.des.Views.GetOk(key)
		switch {
		case ok && des.Ignored:
			// An ignored view is left as it is, unless the schema check of
			// an ALTER makes it be created again, as current has it.
			kept = append(kept, key)
		case !ok:
			d.b.add(dropViewsAndTriggers, removed, "DROP VIEW "+q(cur.Name))
		case !sqlEqual(cur.SQL, des.SQL):
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
		if !d.cur.Views.Has(key) && !des.Ignored {
			d.b.add(createViews, "", des.SQL)
		}
	}
}

func (d *differ) recreateView(key, comment string) {
	cur, des := d.cur.Views.Get(key), d.des.Views.Get(key)
	sql := des.SQL
	if des.Ignored {
		sql = cur.SQL
	}
	d.b.add(dropViewsAndTriggers, comment, "DROP VIEW "+q(cur.Name))
	d.b.add(createViews, "", sql)
	d.recreatedViews[key] = true
	d.dependencies[key] = cur.Name + ", which is created again"
}

func (d *differ) triggers() {
	for key, cur := range d.cur.Triggers.All() {
		des, ok := d.des.Triggers.GetOk(key)
		table := model.Key(cur.Table)
		// The DROP VIEW of a view created again drops its triggers, and so
		// does the DROP TABLE of a table that is dropped.
		dropped := d.recreatedViews[table] || !d.des.Tables.Has(table) && !d.des.Views.Has(table)
		switch {
		case ok && des.Ignored:
			// An ignored trigger is left as it is: when its view is created
			// again, or the schema check of an ALTER needs it to be, it is
			// created again as current has it. (Its table cannot be dropped:
			// desired would not load.)
			if dropped {
				d.b.add(createTriggers, "", cur.SQL)
			} else if reason := d.dependency(cur.SQL); d.rechecks && reason != "" {
				d.b.add(dropViewsAndTriggers, dependsOn(reason), "DROP TRIGGER "+q(cur.Name))
				d.b.add(createTriggers, "", cur.SQL)
			}
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
		if !d.cur.Triggers.Has(key) && !des.Ignored {
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

// columnEqual compares two columns except for NOT NULL, which ALTER COLUMN
// changes.
func columnEqual(a, b *model.Column) bool {
	return sqlEqual(a.Type, b.Type) &&
		ptrEqual(a.Default, b.Default) &&
		parser.EqualFold(collation(a), collation(b)) &&
		a.Generated == b.Generated &&
		sqlEqual(a.GeneratedExpr, b.GeneratedExpr)
}

// collation returns the collation of a column. A column without COLLATE uses
// BINARY, so writing COLLATE BINARY changes nothing.
func collation(c *model.Column) string {
	if c.Collation == "" {
		return "BINARY"
	}
	return c.Collation
}

// checkEqual compares two CHECK constraints by their expressions. Names are
// not compared, and a CHECK of a column equals the same CHECK of the table.
func checkEqual(a, b *model.Check) bool {
	return sqlEqual(a.Expr, b.Expr)
}

func ptrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
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
