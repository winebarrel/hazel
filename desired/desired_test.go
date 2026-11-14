package desired_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/winebarrel/hazel/desired"
)

func load(t *testing.T, srcs ...desired.Source) ([]string, []desired.Warning, error) {
	t.Helper()
	schema, warnings, err := desired.Load(context.Background(), srcs)
	if err != nil {
		return nil, nil, err
	}
	var names []string
	for x := range schema.Tables.Values() {
		names = append(names, "table "+x.Name)
	}
	for x := range schema.Indexes.Values() {
		names = append(names, "index "+x.Name)
	}
	for x := range schema.Views.Values() {
		names = append(names, "view "+x.Name)
	}
	for x := range schema.Triggers.Values() {
		names = append(names, "trigger "+x.Name)
	}
	return names, warnings, nil
}

func TestLoadOrder(t *testing.T) {
	// The objects are written in an order that would fail as it is.
	names, warnings, err := load(t, desired.Source{Name: "schema.sql", Body: `
CREATE TRIGGER tr AFTER INSERT ON users BEGIN INSERT INTO log VALUES (new.id); END;
CREATE VIEW recent AS SELECT * FROM active ORDER BY id DESC;
CREATE VIEW active AS SELECT * FROM users WHERE deleted_at IS NULL;
CREATE INDEX users_deleted ON users (deleted_at);
CREATE TABLE users (id INTEGER PRIMARY KEY, deleted_at TEXT);
CREATE TABLE log (x);
CREATE VIRTUAL TABLE docs USING fts5(body);
`})
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, []string{
		"table users", "table log", "table docs",
		"index users_deleted",
		"view recent", "view active",
		"trigger tr",
	}, names)
}

func TestLoadFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.sql")
	b := filepath.Join(dir, "b.sql")
	require.NoError(t, os.WriteFile(a, []byte("CREATE INDEX t_x ON t (x);"), 0o644))
	require.NoError(t, os.WriteFile(b, []byte("CREATE TABLE t (x);"), 0o644))

	srcs, err := desired.ReadFiles([]string{a, b})
	require.NoError(t, err)
	assert.Equal(t, []desired.Source{
		{Name: a, Body: "CREATE INDEX t_x ON t (x);"},
		{Name: b, Body: "CREATE TABLE t (x);"},
	}, srcs)

	names, _, err := load(t, srcs...)
	require.NoError(t, err)
	assert.Equal(t, []string{"table t", "index t_x"}, names)

	_, err = desired.ReadFiles([]string{filepath.Join(dir, "missing.sql")})
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestLoadWarnings(t *testing.T) {
	names, warnings, err := load(t, desired.Source{Name: "schema.sql", Body: `CREATE TABLE users (id INTEGER PRIMARY KEY);
INSERT INTO users VALUES (1);
PRAGMA user_version = 3;
CREATE TEMP TABLE scratch (a);
-- trailing comment
`})
	require.NoError(t, err)
	// The ignored statements are not executed: users has no row, and
	// nothing else is created.
	assert.Equal(t, []string{"table users"}, names)
	assert.Equal(t, []desired.Warning{
		{File: "schema.sql", Line: 2, SQL: "INSERT INTO users VALUES (1);"},
		{File: "schema.sql", Line: 3, SQL: "PRAGMA user_version = 3;"},
		{File: "schema.sql", Line: 4, SQL: "CREATE TEMP TABLE scratch (a);"},
	}, warnings)
	assert.Equal(t, "schema.sql:2: ignored unsupported statement: INSERT INTO users VALUES (1);", warnings[0].String())
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"syntax", "CREATE TABLE a (x);\n\nCREATE TABLE b (", `schema.sql:3: `},
		{"duplicate", "CREATE TABLE a (x);\nCREATE TABLE a (y);", "schema.sql:2: SQL logic error: table a already exists"},
		{"index on missing table", "CREATE INDEX i ON nope (x);", "schema.sql:1: SQL logic error: no such table: main.nope"},
		// SQLite creates a view without checking what it reads; the error
		// comes when its columns are read.
		{"view on missing table", "CREATE VIEW v AS SELECT * FROM nope;", "view v: SQL logic error: no such table: main.nope"},
		{"views reading each other", "CREATE VIEW a AS SELECT * FROM b;\nCREATE VIEW b AS SELECT * FROM a;", "view a: SQL logic error: view a is circularly defined"},
		{"tokenize", "CREATE TABLE 'a", "schema.sql: line 1: unterminated string"},
		// The statement passes the classification, and the authorizer
		// refuses what it would do.
		{"temp schema", "CREATE TABLE temp.x (a);", "schema.sql:1: authorization denied"},
		{"temp index", "CREATE TABLE t (a);\nCREATE INDEX temp.i ON t (a);", "schema.sql:2: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := load(t, desired.Source{Name: "schema.sql", Body: tt.body})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestLoadCreateTableAs(t *testing.T) {
	names, _, err := load(t, desired.Source{Name: "schema.sql", Body: "CREATE TABLE t AS SELECT 1 AS x;"})
	require.NoError(t, err)
	assert.Equal(t, []string{"table t"}, names)
}

func TestLoadEmpty(t *testing.T) {
	names, warnings, err := load(t)
	require.NoError(t, err)
	assert.Empty(t, names)
	assert.Empty(t, warnings)
}
