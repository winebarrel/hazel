// Package model holds the schema objects that hazel compares.
package model

import "github.com/winebarrel/orderedmap/v2"

// Schema holds the objects of the main schema, each kind in the order they
// were created in. The keys are the names with ASCII letters in lowercase
// (see Key), since SQLite compares names that way.
type Schema struct {
	Tables   *orderedmap.Map[string, *Table]
	Indexes  *orderedmap.Map[string, *Index]
	Views    *orderedmap.Map[string, *View]
	Triggers *orderedmap.Map[string, *Trigger]
}

// NewSchema returns an empty schema.
func NewSchema() *Schema {
	return &Schema{
		Tables:   orderedmap.New[string, *Table](),
		Indexes:  orderedmap.New[string, *Index](),
		Views:    orderedmap.New[string, *View](),
		Triggers: orderedmap.New[string, *Trigger](),
	}
}

// Table is a table or a virtual table.
type Table struct {
	Name string
	// SQL is the CREATE statement as SQLite keeps it.
	SQL string
	// Virtual reports whether the table is a virtual table. A virtual table
	// is compared by its SQL alone, so its columns and constraints are not
	// read.
	Virtual      bool
	Strict       bool
	WithoutRowid bool
	// Autoincrement reports whether the PRIMARY KEY has AUTOINCREMENT.
	Autoincrement bool
	Columns       *orderedmap.Map[string, *Column]
	// PrimaryKey lists the columns of the PRIMARY KEY in key order. It is
	// empty when the table has none. The key of a rowid alias (an INTEGER
	// PRIMARY KEY) has no index, so its Desc is always false, even when
	// written as PRIMARY KEY (x DESC); the order makes no difference to how
	// the rows are stored.
	PrimaryKey []*IndexColumn
	// ForeignKeys are the FOREIGN KEY constraints, in the order written.
	ForeignKeys []*ForeignKey
	// Uniques are the UNIQUE constraints, in the order written.
	Uniques []*Unique
	// Checks are the CHECK constraints of the columns and of the table, in
	// the order written.
	Checks []*Check
}

// Generated is how a generated column is stored.
type Generated string

const (
	NotGenerated     Generated = ""
	GeneratedVirtual Generated = "VIRTUAL"
	GeneratedStored  Generated = "STORED"
)

// Column is a column of a table.
type Column struct {
	Name string
	// Type is the declared type as PRAGMA table_xinfo reports it: one of the
	// six standard type names in uppercase, or any other type name as
	// written. It is "" when no type is declared.
	Type    string
	NotNull bool
	// Default is the text of the DEFAULT expression, or nil when there is
	// none.
	Default *string
	// Collation is the name of the COLLATE, as written, or "".
	Collation string
	Generated Generated
	// GeneratedExpr is the expression of a generated column, as written, or
	// "".
	GeneratedExpr string
}

// IndexColumn is one key of an index, a PRIMARY KEY or a UNIQUE constraint.
type IndexColumn struct {
	// Name is the column name, or "" for an expression.
	Name      string
	Desc      bool
	Collation string
}

// ForeignKey is a FOREIGN KEY constraint.
type ForeignKey struct {
	Columns  []string
	RefTable string
	// RefColumns lists the parent columns. An entry is "" when the
	// constraint does not name the parent columns and refers to the parent's
	// PRIMARY KEY.
	RefColumns []string
	OnUpdate   string
	OnDelete   string
	// Deferred reports DEFERRABLE INITIALLY DEFERRED.
	Deferred bool
	// Name is the CONSTRAINT name, or "".
	Name string
	// Column is the column whose definition holds the REFERENCES, or "" for
	// a table constraint.
	Column string
}

// Check is a CHECK constraint.
type Check struct {
	// Name is the CONSTRAINT name, or "".
	Name string
	// Expr is the expression, as written, without the parentheses around it.
	Expr string
	// Column is the column whose definition holds the constraint, or "" for a
	// table constraint.
	Column string
}

// Unique is a UNIQUE constraint.
type Unique struct {
	Columns []*IndexColumn
}

// Index is an index created with CREATE INDEX.
type Index struct {
	Name    string
	Table   string
	SQL     string
	Unique  bool
	Partial bool
	Columns []*IndexColumn
}

// View is a view.
type View struct {
	Name string
	SQL  string
	// Columns lists the names of the result columns.
	Columns []string
}

// Trigger is a trigger.
type Trigger struct {
	Name  string
	Table string
	SQL   string
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
