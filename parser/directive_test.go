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
	elems := parser.TableElements(statement(t, `CREATE TABLE t ( -- hazel:renamed-from uid
  id INTEGER PRIMARY KEY, -- a note
  -- hazel:renamed-from name
  display_name TEXT -- not this one
)`))
	require.Len(t, elems, 2)
	// After the opening parenthesis, a directive on the same line is for the
	// first element.
	ds, err := elems[0].Directives()
	require.NoError(t, err)
	assert.Equal(t, []parser.Directive{{Name: "renamed-from", Arg: "uid", Line: 1}}, ds)
	ds, err = elems[1].Directives()
	require.NoError(t, err)
	assert.Equal(t, []parser.Directive{{Name: "renamed-from", Arg: "name", Line: 3}}, ds)
}

func TestDirectivesOnThePreviousLine(t *testing.T) {
	// A directive on the line of the comma or the ";" before it reads as if
	// it were for what comes before, so it is an error.
	elems := parser.TableElements(statement(t, "CREATE TABLE t (\n  a, -- hazel:renamed-from x\n  b\n)"))
	_, err := elems[1].Directives()
	require.EqualError(t, err, "line 2: hazel:renamed-from is on the line of what comes before it; write it on a line of its own")

	tokens, err := parser.Tokenize("CREATE TABLE a (x); -- hazel:ignore\nCREATE TABLE b (y);")
	require.NoError(t, err)
	_, err = parser.Split(tokens)[1].Directives()
	require.EqualError(t, err, "line 1: hazel:ignore is on the line of what comes before it; write it on a line of its own")

	// A block comment that spans lines puts the directive on a line of its
	// own.
	tokens, err = parser.Tokenize("CREATE TABLE a (x); /* note\n */ -- hazel:ignore\nCREATE TABLE b (y);")
	require.NoError(t, err)
	ds, err := parser.Split(tokens)[1].Directives()
	require.NoError(t, err)
	assert.Len(t, ds, 1)
}

func TestParseName(t *testing.T) {
	for _, tt := range []struct {
		s, want string
		ok      bool
	}{
		{"users", "users", true},
		{` "Old Name" `, "Old Name", true},
		{"[a b]", "a b", true},
		{"a b", "", false},
		{"'a'", "a", true},
		{"", "", false},
		{`"open`, "", false},
	} {
		t.Run(tt.s, func(t *testing.T) {
			got, ok := parser.ParseName(tt.s)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
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
		{"CREATE TABLE 'sq' (a)", "sq"},
		{"INSERT INTO t VALUES (1)", ""},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			assert.Equal(t, tt.want, statement(t, tt.src).ObjectName())
		})
	}
}

func TestWithObjectName(t *testing.T) {
	tests := []struct{ src, want string }{
		{"CREATE TABLE t (a)", `CREATE TABLE "new t" (a)`},
		{"-- note\nCREATE TABLE IF NOT EXISTS main . \"t\" (a) STRICT", `CREATE TABLE IF NOT EXISTS "new t" (a) STRICT`},
		{"CREATE TABLE \"t\"(a,b)", `CREATE TABLE "new t"(a,b)`},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			assert.Equal(t, tt.want, statement(t, tt.src).WithObjectName("new t"))
		})
	}
}
