// Package dump writes the schema of a database as the CREATE statements
// that make it.
package dump

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/winebarrel/hazel/format"
	"github.com/winebarrel/hazel/model"
	"github.com/winebarrel/hazel/parser"
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

// File is one file of a dump split by object.
type File struct {
	Name string
	Body string
}

// Files returns a file for each object of Objects, named after it with
// ".sql". A name that is the same as an earlier one when case is ignored,
// which SQLite allows for letters outside ASCII but a file system that
// ignores case does not, gets "_2", "_3" and so on. A name that is not a
// plain file name, such as one with a "/", is an error.
func Files(s *model.Schema, o Options) ([]File, error) {
	var files []File
	used := map[string]bool{}
	for _, obj := range Objects(s, o) {
		if strings.ContainsAny(obj.Name, `/\`) || !filepath.IsLocal(obj.Name+".sql") {
			return nil, fmt.Errorf("cannot write a file for %s: the name is not a plain file name", parser.QuoteIdent(obj.Name))
		}
		name := obj.Name + ".sql"
		for n := 2; used[strings.ToLower(name)]; n++ {
			name = fmt.Sprintf("%s_%d.sql", obj.Name, n)
		}
		used[strings.ToLower(name)] = true
		files = append(files, File{Name: name, Body: strings.Join(obj.Stmts, "\n\n") + "\n"})
	}
	return files, nil
}
