// Package dump writes the schema of a database as the CREATE statements
// that make it.
package dump

import (
	"strings"

	"github.com/winebarrel/hazel/format"
	"github.com/winebarrel/hazel/model"
)

// Options holds what String writes.
type Options struct {
	// NoFormat writes the statements as SQLite keeps them, without laying
	// them out.
	NoFormat bool
	// Exclude reports the tables, views and virtual tables to leave out. The
	// indexes and triggers go with the table or view they belong to. A nil
	// Exclude leaves nothing out.
	Exclude func(name string) bool
}

// Object is a table, a view or a virtual table, and the statements that
// make it: its CREATE statement, then its indexes and triggers.
type Object struct {
	Name  string
	Stmts []string
}

// Objects returns the tables, each followed by its indexes and triggers,
// then the views, each followed by its triggers, every kind in the order the
// objects were created in. The statements end with ";".
//
// The order lets the dump run as it is: a trigger needs its table or view,
// and a view may read a table or a view that comes after it, since SQLite
// does not look up the tables of a view until it is used.
func Objects(s *model.Schema, o Options) []Object {
	stmt := func(sql string) string {
		if !o.NoFormat {
			sql = format.Statement(sql)
		}
		return sql + ";"
	}
	var objs []Object
	add := func(name, sql string) {
		if o.Exclude != nil && o.Exclude(name) {
			return
		}
		obj := Object{Name: name, Stmts: []string{stmt(sql)}}
		key := model.Key(name)
		for i := range s.Indexes.Values() {
			if model.Key(i.Table) == key {
				obj.Stmts = append(obj.Stmts, stmt(i.SQL))
			}
		}
		for t := range s.Triggers.Values() {
			if model.Key(t.Table) == key {
				obj.Stmts = append(obj.Stmts, stmt(t.SQL))
			}
		}
		objs = append(objs, obj)
	}
	for t := range s.Tables.Values() {
		add(t.Name, t.SQL)
	}
	for v := range s.Views.Values() {
		add(v.Name, v.SQL)
	}
	return objs
}

// String returns the statements of Objects, separated by blank lines.
func String(s *model.Schema, o Options) string {
	var stmts []string
	for _, obj := range Objects(s, o) {
		stmts = append(stmts, obj.Stmts...)
	}
	if len(stmts) == 0 {
		return ""
	}
	return strings.Join(stmts, "\n\n") + "\n"
}
