package parser_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/winebarrel/hazel/parser"
)

func TestStatementDirectives(t *testing.T) {
	s := statement(t, `-- a comment
-- hazel:renamed-from  users
--hazel:ignore
/* hazel:ignore is not a directive in a block comment */
-- hazelnut: is not either
CREATE TABLE members (id INTEGER PRIMARY KEY);`)
	ds, err := s.Directives()
	require.NoError(t, err)
	assert.Equal(t, []parser.Directive{
		{Name: "renamed-from", Arg: "users", Line: 2},
		{Name: "ignore", Line: 3},
	}, ds)
}

func TestStatementDirectivesExecute(t *testing.T) {
	s := statement(t, "-- hazel:execute SELECT NOT EXISTS (SELECT 1 FROM settings)\nINSERT INTO settings VALUES (1);")
	ds, err := s.Directives()
	require.NoError(t, err)
	assert.Equal(t, []parser.Directive{{Name: "execute", Arg: "SELECT NOT EXISTS (SELECT 1 FROM settings)", Line: 1}}, ds)
}

func TestStatementDirectivesUnknown(t *testing.T) {
	_, err := statement(t, "\n-- hazel:renamed_from users\nCREATE TABLE t (a);").Directives()
	require.EqualError(t, err, "line 2: unknown directive: hazel:renamed_from")

	// A "hazel:" with no name is a mistyped directive too.
	_, err = statement(t, "-- hazel: ignore\nCREATE TABLE t (a);").Directives()
	assert.EqualError(t, err, "line 1: unknown directive: hazel:")
}

func TestElementDirectives(t *testing.T) {
	elems := parser.TableElements(statement(t, `CREATE TABLE t (
  id INTEGER PRIMARY KEY, -- hazel:renamed-from uid
  -- hazel:renamed-from name
  display_name TEXT -- not this one
)`))
	require.Len(t, elems, 2)
	ds, err := elems[0].Directives()
	require.NoError(t, err)
	assert.Empty(t, ds)
	// A comment after the separator belongs to the next element.
	ds, err = elems[1].Directives()
	require.NoError(t, err)
	assert.Equal(t, []parser.Directive{{Name: "renamed-from", Arg: "uid", Line: 2}, {Name: "renamed-from", Arg: "name", Line: 3}}, ds)
}

func TestWithoutDirectives(t *testing.T) {
	s := statement(t, `-- hazel:renamed-from users
-- a note
CREATE TABLE members (
  id INTEGER PRIMARY KEY,
  -- hazel:renamed-from name
  display_name TEXT -- kept
);`)
	assert.Equal(t, `CREATE TABLE members (
  id INTEGER PRIMARY KEY,
  display_name TEXT -- kept
);`, s.WithoutDirectives())

	// A directive after other text on its line leaves the line.
	s = statement(t, "CREATE TABLE t (\n  a, -- hazel:renamed-from x\n  b\n);")
	assert.Equal(t, "CREATE TABLE t (\n  a, \n  b\n);", s.WithoutDirectives())
}

func TestObjectName(t *testing.T) {
	tests := []struct{ src, want string }{
		{"CREATE TABLE t (a)", "t"},
		{`CREATE TABLE IF NOT EXISTS main."My T" (a)`, "My T"},
		{"CREATE VIRTUAL TABLE docs USING fts5(a)", "docs"},
		{"CREATE UNIQUE INDEX IF NOT EXISTS i ON t (a)", "i"},
		{"CREATE VIEW main.v AS SELECT 1", "v"},
		{"CREATE TRIGGER tr AFTER INSERT ON t BEGIN SELECT 1; END", "tr"},
		{"INSERT INTO t VALUES (1)", ""},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			assert.Equal(t, tt.want, statement(t, tt.src).ObjectName())
		})
	}
}
