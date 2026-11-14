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
// DROP COLUMN checks every view and trigger in the schema, so when the plan
// drops a column, a view or a trigger that names a table dropping a column,
// or a view that is dropped before the ALTER, is dropped and created again
// too, and so on for the views that name those.
//
// Definitions are compared with parser.TokensEqual. Changing a column, a
// PRIMARY KEY, a UNIQUE or a FOREIGN KEY constraint, or STRICT or WITHOUT
// ROWID is not supported yet and returns an error.
func Diff(current, desired *model.Schema) (*Plan, error) {
	d := &differ{cur: current, des: desired, dependencies: map[string]string{}, recreatedViews: map[string]bool{}}
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
	// dropsColumns reports whether the plan drops a column.
	dropsColumns bool
	// dependencies maps the key of each table that drops a column, and of
	// each view that is dropped and created again, to the reason a view or a
	// trigger naming it gives: "users, which drops a column". It is used only
	// when the plan drops a column.
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
		if !d.cur.Tables.Has(key) {
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
	// A foreign key on columns being added comes with their definitions.
	desFKs := slices.DeleteFunc(slices.Clone(des.ForeignKeys), func(fk *model.ForeignKey) bool {
		return !slices.ContainsFunc(fk.Columns, func(c string) bool { return !slices.Contains(added, model.Key(c)) })
	})
	switch {
	case cur.Strict != des.Strict:
		return unsupported("STRICT")
	case cur.WithoutRowid != des.WithoutRowid:
		return unsupported("WITHOUT ROWID")
	case !indexColumnsEqual(cur.PrimaryKey, des.PrimaryKey):
		return unsupported("the PRIMARY KEY")
	case !uniquesEqual(cur.Uniques, des.Uniques):
		return unsupported("a UNIQUE constraint")
	case !foreignKeysEqual(cur.ForeignKeys, desFKs):
		return unsupported("a FOREIGN KEY constraint")
	}

	for key, c := range cur.Columns.All() {
		dc, ok := des.Columns.GetOk(key)
		if !ok {
			d.b.add(alterTables, removed, "ALTER TABLE "+q(cur.Name)+" DROP COLUMN "+q(c.Name))
			d.b.dataLoss = append(d.b.dataLoss, "column "+cur.Name+"."+c.Name)
			d.dropsColumns = true
			d.dependencies[model.Key(cur.Name)] = cur.Name + ", which drops a column"
			continue
		}
		if !columnEqual(c, dc) {
			return unsupported("column " + dc.Name)
		}
	}

	if len(added) > 0 {
		defs := columnDefinitions(des.SQL)
		for _, key := range added {
			d.b.add(alterTables, "", "ALTER TABLE "+q(cur.Name)+" ADD COLUMN "+defs[key])
		}
	}
	return nil
}

// columnDefinitions returns the text of each column definition of a CREATE
// TABLE, keyed by the column's key. SQLite stores a CREATE TABLE ... AS
// SELECT as a CREATE TABLE with a list of columns, so every table has one.
func columnDefinitions(sql string) map[string]string {
	defs := map[string]string{}
	for _, e := range parser.TableElements(statement(sql)) {
		if e.Column != "" {
			defs[model.Key(e.Column)] = e.SQL()
		}
	}
	return defs
}

func (d *differ) indexes() {
	for key, cur := range d.cur.Indexes.All() {
		des, ok := d.des.Indexes.GetOk(key)
		switch {
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
		if !d.cur.Indexes.Has(key) {
			d.b.add(createIndexes, "", des.SQL)
		}
	}
}

func (d *differ) views() {
	var kept []string
	for key, cur := range d.cur.Views.All() {
		des, ok := d.des.Views.GetOk(key)
		switch {
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
	for d.dropsColumns {
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
		if !d.cur.Views.Has(key) {
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
		table := model.Key(cur.Table)
		// The DROP VIEW of a view created again drops its triggers, and so
		// does the DROP TABLE of a table that is dropped.
		dropped := d.recreatedViews[table] || !d.des.Tables.Has(table) && !d.des.Views.Has(table)
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
		case d.dropsColumns:
			if reason := d.dependency(cur.SQL); reason != "" {
				d.b.add(dropViewsAndTriggers, dependsOn(reason), "DROP TRIGGER "+q(cur.Name))
				d.b.add(createTriggers, "", des.SQL)
			}
		}
	}
	for key, des := range d.des.Triggers.All() {
		if !d.cur.Triggers.Has(key) {
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

func columnEqual(a, b *model.Column) bool {
	return sqlEqual(a.Type, b.Type) &&
		a.NotNull == b.NotNull &&
		ptrEqual(a.Default, b.Default) &&
		a.Generated == b.Generated
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

// foreignKeysEqual compares the FOREIGN KEY constraints regardless of their
// order.
func foreignKeysEqual(a, b []*model.ForeignKey) bool {
	return sameElements(a, b, foreignKeyEqual)
}

// sameElements reports whether a and b hold the same elements, each as many
// times, in any order.
func sameElements[T any](a, b []T, eq func(T, T) bool) bool {
	if len(a) != len(b) {
		return false
	}
	rest := slices.Clone(b)
	for _, x := range a {
		i := slices.IndexFunc(rest, func(y T) bool { return eq(x, y) })
		if i < 0 {
			return false
		}
		rest = slices.Delete(rest, i, i+1)
	}
	return true
}

func foreignKeyEqual(a, b *model.ForeignKey) bool {
	return namesEqual(a.Columns, b.Columns) &&
		parser.EqualFold(a.RefTable, b.RefTable) &&
		namesEqual(a.RefColumns, b.RefColumns) &&
		a.OnUpdate == b.OnUpdate &&
		a.OnDelete == b.OnDelete
}

func namesEqual(a, b []string) bool {
	return slices.EqualFunc(a, b, parser.EqualFold)
}
