// Package catalog reads the schema objects of a database.
package catalog

import (
	"context"
	"database/sql"

	"github.com/winebarrel/orderedmap/v2"
)

// Type is the type of a schema object, as sqlite_schema names it.
type Type string

const (
	Table   Type = "table"
	Index   Type = "index"
	View    Type = "view"
	Trigger Type = "trigger"
)

// Object is one row of sqlite_schema.
type Object struct {
	Type Type
	Name string
	// TblName is the table or view the object belongs to. For a table or a
	// view, it is the object itself.
	TblName string
	// SQL is the CREATE statement as SQLite keeps it.
	SQL string
	// Virtual reports whether a table is a virtual table.
	Virtual bool
}

// Schema holds the objects of the main schema, each kind in the order
// sqlite_schema lists them, which is the order they were created in. The keys
// are the names with ASCII letters in lowercase, since SQLite compares names
// that way.
type Schema struct {
	Tables   *orderedmap.Map[string, *Object]
	Indexes  *orderedmap.Map[string, *Object]
	Views    *orderedmap.Map[string, *Object]
	Triggers *orderedmap.Map[string, *Object]
}

// Read reads the objects of the main schema. It leaves out:
//
//   - names that begin with "sqlite_" in any ASCII case, which SQLite
//     reserves for itself, such as sqlite_sequence and sqlite_stat1;
//   - automatic indexes, which have no SQL and stand for a UNIQUE or PRIMARY
//     KEY constraint of their table;
//   - shadow tables, which a virtual table module creates and owns, and the
//     objects that belong to them.
//
// Whether a table is virtual or a shadow table is what PRAGMA table_list
// reports as its type. The join compares names with NOCASE, which folds
// ASCII only, as SQLite does for names.
func Read(ctx context.Context, db *sql.DB) (*Schema, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT s.type, s.name, s.tbl_name, s.sql, coalesce(l.type, '')
		FROM main.sqlite_schema AS s
		LEFT JOIN pragma_table_list AS l
		  ON l.schema = 'main' AND l.name = s.tbl_name COLLATE NOCASE
		WHERE s.sql IS NOT NULL
		  AND s.name NOT LIKE 'sqlite\_%' ESCAPE '\'
		  AND coalesce(l.type, '') <> 'shadow'
		ORDER BY s.rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	s := &Schema{
		Tables:   orderedmap.New[string, *Object](),
		Indexes:  orderedmap.New[string, *Object](),
		Views:    orderedmap.New[string, *Object](),
		Triggers: orderedmap.New[string, *Object](),
	}
	for rows.Next() {
		var o Object
		var tblType string
		if err := rows.Scan(&o.Type, &o.Name, &o.TblName, &o.SQL, &tblType); err != nil {
			return nil, err
		}
		switch o.Type {
		case Table:
			o.Virtual = tblType == "virtual"
			s.Tables.Set(Key(o.Name), &o)
		case Index:
			s.Indexes.Set(Key(o.Name), &o)
		case View:
			s.Views.Set(Key(o.Name), &o)
		case Trigger:
			s.Triggers.Set(Key(o.Name), &o)
		}
	}
	return s, rows.Err()
}

// Key returns the name with ASCII letters in lowercase. SQLite compares names
// that way, so two names with the same key name the same object.
func Key(name string) string {
	b := []byte(name)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
