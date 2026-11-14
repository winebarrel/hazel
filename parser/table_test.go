package parser_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/winebarrel/hazel/parser"
)

func TestParseCreateTable(t *testing.T) {
	def := parser.ParseCreateTable(statement(t, `CREATE TABLE posts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  title VARCHAR(10) NOT NULL COLLATE "NoCase" CHECK (length(title) > 0),
  body TEXT DEFAULT ('a' COLLATE rtrim) COLLATE rtrim,
  user_id INT CONSTRAINT fk_user REFERENCES users (id) ON DELETE SET NULL DEFERRABLE INITIALLY DEFERRED,
  editor_id INT REFERENCES users ON UPDATE CASCADE NOT DEFERRABLE INITIALLY DEFERRED NOT NULL,
  slug TEXT GENERATED ALWAYS AS (lower( title )) STORED,
  n INT AS (id * 2),
  CONSTRAINT positive CHECK (id > 0),
  CHECK (title <> body),
  CONSTRAINT fk_pair FOREIGN KEY (user_id, editor_id) REFERENCES pairs (a, b) MATCH SIMPLE DEFERRABLE,
  UNIQUE (title COLLATE nocase)
)`))

	assert.True(t, def.Autoincrement)
	assert.Equal(t, []parser.ColumnDef{
		{Name: "id"},
		{Name: "title", Collation: "NoCase"},
		{Name: "body", Collation: "rtrim"},
		{Name: "user_id"},
		{Name: "editor_id"},
		{Name: "slug", Generated: "lower( title )"},
		{Name: "n", Generated: "id * 2"},
	}, def.Columns)
	assert.Equal(t, []parser.CheckDef{
		{Expr: "length(title) > 0", Column: "title"},
		{Name: "positive", Expr: "id > 0"},
		{Expr: "title <> body"},
	}, def.Checks)
	assert.Equal(t, []parser.ForeignKeyDef{
		{Name: "fk_user", Deferred: true, Column: "user_id"},
		// NOT DEFERRABLE INITIALLY DEFERRED is not deferred.
		{Column: "editor_id"},
		// A bare DEFERRABLE is not deferred either.
		{Name: "fk_pair"},
	}, def.ForeignKeys)
}

func TestParseCreateTableConstraintName(t *testing.T) {
	// A name belongs to the constraint right after it only.
	def := parser.ParseCreateTable(statement(t,
		`CREATE TABLE t (a CONSTRAINT nn NOT NULL CHECK (a > 0) REFERENCES p, CONSTRAINT c CHECK (a < 9) CHECK (a <> 5))`))
	assert.Equal(t, []parser.CheckDef{
		{Expr: "a > 0", Column: "a"},
		{Name: "c", Expr: "a < 9"},
		{Expr: "a <> 5"},
	}, def.Checks)
	assert.Equal(t, []parser.ForeignKeyDef{{Column: "a"}}, def.ForeignKeys)
}

func TestParseCreateTableComments(t *testing.T) {
	// Comments inside are kept; those at either end are not.
	def := parser.ParseCreateTable(statement(t, "CREATE TABLE t (a, CHECK ( /* why */ a /* n */ > 0 -- note\n ))"))
	assert.Equal(t, []parser.CheckDef{{Expr: "a /* n */ > 0"}}, def.Checks)
}

func TestParseCreateTableActions(t *testing.T) {
	// The words of an action do not end the foreign key clause early.
	def := parser.ParseCreateTable(statement(t,
		`CREATE TABLE t (a REFERENCES p ON DELETE SET DEFAULT ON UPDATE NO ACTION MATCH FULL DEFERRABLE INITIALLY DEFERRED COLLATE nocase)`))
	assert.Equal(t, []parser.ColumnDef{{Name: "a", Collation: "nocase"}}, def.Columns)
	assert.Equal(t, []parser.ForeignKeyDef{{Deferred: true, Column: "a"}}, def.ForeignKeys)
}

func TestParseCreateTableSeparateDeferrable(t *testing.T) {
	// DEFERRABLE as a column constraint of its own applies to the last
	// foreign key of the table so far, even one of an earlier column.
	def := parser.ParseCreateTable(statement(t, `CREATE TABLE t (
  a REFERENCES p NOT NULL DEFERRABLE INITIALLY DEFERRED,
  b REFERENCES p DEFERRABLE INITIALLY DEFERRED NOT DEFERRABLE,
  c DEFERRABLE INITIALLY DEFERRED,
  d
)`))
	assert.Equal(t, []parser.ForeignKeyDef{
		{Column: "a", Deferred: true},
		{Column: "b", Deferred: true},
	}, def.ForeignKeys)

	// With no foreign key before it, it applies to nothing.
	def = parser.ParseCreateTable(statement(t, `CREATE TABLE t (a DEFERRABLE INITIALLY DEFERRED)`))
	assert.Empty(t, def.ForeignKeys)
}

func TestParseCreateTablePlain(t *testing.T) {
	def := parser.ParseCreateTable(statement(t, `CREATE TABLE t(a,"b c")`))
	assert.False(t, def.Autoincrement)
	assert.Equal(t, []parser.ColumnDef{{Name: "a"}, {Name: "b c"}}, def.Columns)
	assert.Empty(t, def.Checks)
	assert.Empty(t, def.ForeignKeys)
}
