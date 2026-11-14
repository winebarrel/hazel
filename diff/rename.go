package diff

import (
	"context"
	"fmt"

	"github.com/winebarrel/hazel/catalog"
	"github.com/winebarrel/hazel/db"
	"github.com/winebarrel/hazel/model"
)

// renames emits the renames that the -- hazel:renamed-from directives ask
// for and replaces current with what it is after them.
//
// A rename is done when the old name is in current and the new one is not.
// When neither is there, or only the new one, the directive has nothing to
// do: the rename was applied before, or the object is new. When both are
// there, which one to keep is not clear, and it is an error.
func (d *differ) renames() error {
	var stmts []string
	for key, des := range d.des.Tables.All() {
		table := des.Name
		cur, ok := d.cur.Tables.GetOk(key)
		if des.RenamedFrom != "" {
			old, hasOld := d.cur.Tables.GetOk(model.Key(des.RenamedFrom))
			switch {
			case hasOld && ok:
				return fmt.Errorf("table %s: both %s and %s exist, so it cannot be renamed", des.Name, des.RenamedFrom, des.Name)
			case hasOld:
				stmts = append(stmts, "ALTER TABLE "+q(old.Name)+" RENAME TO "+q(des.Name))
				cur, ok = old, true
			}
		}
		if !ok {
			continue
		}
		for ckey, c := range des.Columns.All() {
			if c.RenamedFrom == "" {
				continue
			}
			old, hasOld := cur.Columns.GetOk(model.Key(c.RenamedFrom))
			switch {
			case hasOld && cur.Columns.Has(ckey):
				return fmt.Errorf("table %s: both columns %s and %s exist, so %s cannot be renamed",
					table, c.RenamedFrom, c.Name, c.RenamedFrom)
			case hasOld:
				stmts = append(stmts, "ALTER TABLE "+q(table)+" RENAME COLUMN "+q(old.Name)+" TO "+q(c.Name))
			}
		}
	}
	if len(stmts) == 0 {
		return nil
	}
	for _, s := range stmts {
		d.b.add(renameTables, "", s)
	}
	d.rechecks = true
	renamed, err := replay(d.cur, stmts)
	if err != nil {
		return err
	}
	d.cur = renamed
	return nil
}

// replay creates the schema in an in-memory database, runs stmts on it, and
// reads it back.
func replay(s *model.Schema, stmts []string) (*model.Schema, error) {
	conn, err := db.OpenReadWrite(db.Memory, 0)
	if err != nil {
		return nil, err
	}
	defer conn.Close() //nolint:errcheck
	var all []string
	for t := range s.Tables.Values() {
		all = append(all, t.SQL)
	}
	for x := range s.Indexes.Values() {
		all = append(all, x.SQL)
	}
	for v := range s.Views.Values() {
		all = append(all, v.SQL)
	}
	for t := range s.Triggers.Values() {
		all = append(all, t.SQL)
	}
	for _, sql := range append(all, stmts...) {
		if _, err := conn.Exec(sql); err != nil {
			return nil, fmt.Errorf("%s: %w", sql, err)
		}
	}
	return catalog.Read(context.Background(), conn)
}
