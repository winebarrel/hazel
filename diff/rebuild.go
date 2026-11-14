package diff

import (
	"strconv"
	"strings"

	"github.com/winebarrel/hazel/model"
)

// rebuild rebuilds a table in the way SQLite documents for the schema changes
// that ALTER TABLE cannot make ("Making Other Kinds Of Table Schema
// Changes"): create the new table under another name, copy the rows, drop
// the old table and rename the new one. The new table is the desired one, so
// every change to the table is made by the rebuild, and changes lists them
// all for the comment.
//
// The columns that both tables have are copied; a new column gets its
// DEFAULT, and a dropped one is lost. AUTOINCREMENT keeps counting where it
// was, since DROP TABLE deletes the table's row from sqlite_sequence. The
// indexes and triggers of the table go with the DROP TABLE and are created
// again, and the views and triggers that name the table are dropped and
// created again, as the RENAME checks the schema.
//
// The rebuild must run with PRAGMA foreign_keys off, as SQLite's steps say:
// with it on, the DROP TABLE would delete the rows that child tables refer
// to, or fail.
func (d *differ) rebuild(cur, des *model.Table, changes []string) {
	tmp := d.tempName(des.Name)
	comment := "table rebuild: " + des.Name + " (" + strings.Join(changes, "; ") + ")"
	add := func(stmt string) { d.b.add(rebuildTables, comment, stmt) }

	add(statement(des.SQL).WithObjectName(tmp))
	var to, from []string
	for key, dc := range des.Columns.All() {
		c, ok := cur.Columns.GetOk(key)
		if ok && dc.Generated == model.NotGenerated {
			to = append(to, q(dc.Name))
			from = append(from, q(c.Name))
		}
	}
	if len(to) > 0 {
		add("INSERT INTO " + q(tmp) + " (" + strings.Join(to, ", ") + ") SELECT " + strings.Join(from, ", ") + " FROM " + q(cur.Name))
	}
	if cur.Autoincrement && des.Autoincrement {
		add("DELETE FROM sqlite_sequence WHERE name = " + literal(tmp))
		add("INSERT INTO sqlite_sequence (name, seq) SELECT " + literal(tmp) + ", seq FROM sqlite_sequence WHERE name = " + literal(cur.Name))
	}
	add("DROP TABLE " + q(cur.Name))
	add("ALTER TABLE " + q(tmp) + " RENAME TO " + q(des.Name))

	for key, c := range cur.Columns.All() {
		if !des.Columns.Has(key) {
			d.b.dataLoss = append(d.b.dataLoss, "column "+cur.Name+"."+c.Name)
		}
	}
	d.b.rebuilds = append(d.b.rebuilds, des.Name)
	key := model.Key(des.Name)
	d.rebuilt[key] = true
	d.rechecks = true
	d.dependencies[key] = des.Name + ", which is rebuilt"
}

// tempName returns a name for the new table of a rebuild that no table,
// index or view has: new_name, or new_name_2, new_name_3 and so on.
func (d *differ) tempName(name string) string {
	taken := func(n string) bool {
		k := model.Key(n)
		return d.cur.Tables.Has(k) || d.des.Tables.Has(k) ||
			d.cur.Indexes.Has(k) || d.des.Indexes.Has(k) ||
			d.cur.Views.Has(k) || d.des.Views.Has(k)
	}
	tmp := "new_" + name
	for i := 2; taken(tmp); i++ {
		tmp = "new_" + name + "_" + strconv.Itoa(i)
	}
	return tmp
}

// literal returns s as an SQL string literal.
func literal(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
