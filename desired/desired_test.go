package desired_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/winebarrel/hazel/desired"
	"modernc.org/sqlite"
)

func load(t *testing.T, srcs ...desired.Source) ([]string, []desired.Warning, error) {
	t.Helper()
	result, err := desired.Load(context.Background(), srcs)
	if err != nil {
		return nil, nil, err
	}
	schema, warnings := result.Schema, result.Warnings
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
CREATE VIRTUAL TABLE boxes USING rtree(id, minx, maxx);
CREATE VIRTUAL TABLE shapes USING geopoly(name);
`})
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, []string{
		"table users", "table log", "table docs", "table boxes", "table shapes",
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

func TestWarningString(t *testing.T) {
	// Only the first line of the statement is reported.
	w := desired.Warning{File: "s.sql", Line: 3, SQL: "INSERT INTO t VALUES\n  (1),\n  (2);"}
	assert.Equal(t, "s.sql:3: ignored unsupported statement: INSERT INTO t VALUES ...", w.String())

	w = desired.Warning{File: "s.sql", Line: 3, SQL: "PRAGMA user_version = 1; -- note\n"}
	assert.Equal(t, "s.sql:3: ignored unsupported statement: PRAGMA user_version = 1; -- note", w.String())
}

func TestAuthorize(t *testing.T) {
	tests := []struct {
		name                 string
		action               sqlite.AuthorizerActionCode
		arg1, arg2, database string
		want                 sqlite.AuthorizerReturnCode
	}{
		{"create table", sqlite.AuthCreateTable, "t", "", "main", sqlite.AuthorizerOK},
		{"function", sqlite.AuthFunction, "", "lower", "", sqlite.AuthorizerOK},
		{"delete from a shadow table", sqlite.AuthDelete, "r_node", "", "main", sqlite.AuthorizerOK},
		{"read page_size", sqlite.AuthPragma, "page_size", "", "main", sqlite.AuthorizerOK},
		{"set page_size", sqlite.AuthPragma, "page_size", "4096", "main", sqlite.AuthorizerDeny},
		{"other pragma", sqlite.AuthPragma, "journal_mode", "", "main", sqlite.AuthorizerDeny},
		{"attach", sqlite.AuthAttach, "x.db", "", "", sqlite.AuthorizerDeny},
		{"transaction", sqlite.AuthTransaction, "BEGIN", "", "", sqlite.AuthorizerDeny},
		{"drop table", sqlite.AuthDropTable, "t", "", "main", sqlite.AuthorizerDeny},
		{"temp schema", sqlite.AuthCreateTable, "t", "", "temp", sqlite.AuthorizerDeny},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, desired.Authorize(tt.action, tt.arg1, tt.arg2, tt.database, ""))
		})
	}
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
		{"tokenize", "CREATE TABLE a (x);\nCREATE TABLE 'a", "schema.sql:2: unterminated string"},
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

func TestLoadDirectives(t *testing.T) {
	result, err := desired.Load(context.Background(), []desired.Source{{Name: "schema.sql", Body: `-- hazel:renamed-from users
CREATE TABLE members (
  id INTEGER PRIMARY KEY,
  -- hazel:renamed-from name
  display_name TEXT
);
-- hazel:ignore
CREATE TABLE legacy (x);
-- hazel:ignore
CREATE VIRTUAL TABLE docs USING fts5(body);
CREATE INDEX legacy_x ON legacy (x);
-- hazel:ignore
CREATE VIEW v AS SELECT x FROM legacy;
CREATE TRIGGER tr AFTER INSERT ON legacy BEGIN SELECT 1; END;
-- hazel:skip-foreign-key-check
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT);
-- hazel:execute SELECT NOT EXISTS (SELECT 1 FROM settings)
INSERT INTO settings VALUES ('version', '1');
-- hazel:execute
UPDATE settings SET value = '2';
`}})
	require.NoError(t, err)
	s := result.Schema

	members := s.Tables.Get("members")
	assert.Equal(t, "users", members.RenamedFrom)
	assert.Equal(t, "name", members.Columns.Get("display_name").RenamedFrom)
	assert.Empty(t, members.Columns.Get("id").RenamedFrom)
	assert.False(t, members.Ignored)
	// The directives are not kept in the SQL.
	assert.NotContains(t, members.SQL, "hazel:")

	assert.True(t, s.Tables.Get("legacy").Ignored)
	assert.True(t, s.Tables.Get("docs").Ignored)
	assert.True(t, s.Views.Get("v").Ignored)
	assert.False(t, s.Tables.Get("settings").Ignored)
	assert.True(t, s.Tables.Get("settings").SkipForeignKeyCheck)
	assert.False(t, members.SkipForeignKeyCheck)

	// The statements marked with execute are kept, not run, and not warned
	// about.
	assert.Empty(t, result.Warnings)
	assert.Equal(t, []desired.Execute{
		{
			File: "schema.sql", Line: 18, SQL: "INSERT INTO settings VALUES ('version', '1');",
			Condition: "SELECT NOT EXISTS (SELECT 1 FROM settings)",
		},
		{File: "schema.sql", Line: 20, SQL: "UPDATE settings SET value = '2';"},
	}, result.Executes)
}

func TestLoadDirectiveErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"unknown", "-- hazel:renamed_from x\nCREATE TABLE t (a);", "schema.sql:1: unknown directive: hazel:renamed_from"},
		{"unknown on a column", "CREATE TABLE t (\n  -- hazel:nope\n  a\n);", "schema.sql:2: unknown directive: hazel:nope"},
		{"execute on a CREATE", "-- hazel:execute\nCREATE TABLE t (a);", "schema.sql:1: hazel:execute is for a statement that hazel does not manage"},
		{"ignore on an INSERT", "-- hazel:ignore\nINSERT INTO t VALUES (1);", "schema.sql:1: hazel:ignore is for a table, a view or a virtual table"},
		// An index or a trigger is ignored with its table or view.
		{"ignore on an index", "CREATE TABLE t (a);\n-- hazel:ignore\nCREATE INDEX i ON t (a);", "schema.sql:2: hazel:ignore is for a table, a view or a virtual table"},
		{"ignore on a trigger", "CREATE TABLE t (a);\n-- hazel:ignore\nCREATE TRIGGER tr AFTER INSERT ON t BEGIN SELECT 1; END;", "schema.sql:2: hazel:ignore is for a table, a view or a virtual table"},
		{"skip-foreign-key-check on a view", "CREATE TABLE t (a);\n-- hazel:skip-foreign-key-check\nCREATE VIEW v AS SELECT a FROM t;", "schema.sql:2: hazel:skip-foreign-key-check is for a table"},
		{"skip-foreign-key-check on a virtual table", "-- hazel:skip-foreign-key-check\nCREATE VIRTUAL TABLE f USING fts5(a);", "schema.sql:1: hazel:skip-foreign-key-check is for a table"},
		{"renamed-from on an index", "CREATE TABLE t (a);\n-- hazel:renamed-from j\nCREATE INDEX i ON t (a);", "schema.sql:2: hazel:renamed-from is for a table or a column"},
		{"renamed-from without a name", "-- hazel:renamed-from\nCREATE TABLE t (a);", "schema.sql:1: hazel:renamed-from needs one name, the old one, as it is written in SQL"},
		{"column renamed-from without a name", "CREATE TABLE t (\n  -- hazel:renamed-from\n  a\n);", "schema.sql:2: hazel:renamed-from needs one name, the old one, as it is written in SQL"},
		{"renamed-from with two names", "-- hazel:renamed-from a b\nCREATE TABLE t (x);", "schema.sql:1: hazel:renamed-from needs one name, the old one, as it is written in SQL"},
		{"directive after a statement", "CREATE TABLE a (x); -- hazel:ignore\nCREATE TABLE b (y);", "schema.sql:1: hazel:ignore is on the line of what comes before it; write it on a line of its own"},
		{"directive after a column", "CREATE TABLE t (\n  a, -- hazel:renamed-from x\n  b\n);", "schema.sql:2: hazel:renamed-from is on the line of what comes before it; write it on a line of its own"},
		{"ignore on a column", "CREATE TABLE t (\n  -- hazel:ignore\n  a\n);", "schema.sql:2: hazel:ignore cannot be written before a column or a table constraint"},
		{"backfill on a table", "-- hazel:backfill 1\nCREATE TABLE t (a);", "schema.sql:1: hazel:backfill is for a column"},
		{"backfill without an expression", "CREATE TABLE t (\n  -- hazel:backfill\n  a\n);", "schema.sql:2: hazel:backfill needs an expression"},
		{"backfill with only a note", "CREATE TABLE t (\n  -- hazel:backfill -- a note\n  a\n);", "schema.sql:2: hazel:backfill needs an expression"},
		{"backfill with an open string", "CREATE TABLE t (\n  -- hazel:backfill 'abc\n  a\n);", "schema.sql:2: hazel:backfill: unterminated string: '"},
		{"backfill on a table constraint", "CREATE TABLE t (\n  a,\n  -- hazel:backfill 1\n  CHECK (a > 0)\n);", "schema.sql:3: hazel:backfill cannot be written before a column or a table constraint"},
		{"backfill on a generated column", "CREATE TABLE t (\n  a,\n  -- hazel:backfill 1\n  g AS (a)\n);", "schema.sql:3: hazel:backfill cannot fill a generated column: g"},
		{"renamed-from on a constraint", "CREATE TABLE t (\n  a,\n  -- hazel:renamed-from c\n  CHECK (a > 0)\n);", "schema.sql:3: hazel:renamed-from cannot be written before a column or a table constraint"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := desired.Load(context.Background(), []desired.Source{{Name: "schema.sql", Body: tt.body}})
			assert.EqualError(t, err, tt.want)
		})
	}
}
