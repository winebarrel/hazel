package main

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

type exited int

// hazel runs the command and returns its output and exit status.
func hazel(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := 0
	func() {
		defer func() {
			if r := recover(); r != nil {
				e, ok := r.(exited)
				if !ok {
					panic(r)
				}
				code = int(e)
			}
		}()
		run(args, &stdout, &stderr, func(c int) { panic(exited(c)) })
	}()
	return stdout.String(), stderr.String(), code
}

// workdir changes to a new directory with the files written in it.
func workdir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
	return dir
}

func createDB(t *testing.T, path, sqlText string) {
	t.Helper()
	conn, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.Exec(sqlText)
	require.NoError(t, err)
}

func TestVersion(t *testing.T) {
	out, _, code := hazel(t, "--version")
	assert.Equal(t, 0, code)
	assert.True(t, strings.HasPrefix(out, "hazel "), out)
	assert.Contains(t, out, "modernc.org/sqlite")
	assert.Contains(t, out, "(SQLite 3.")
}

func TestPlanNewFile(t *testing.T) {
	dir := workdir(t, map[string]string{"schema.sql": `
CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);
CREATE INDEX users_name ON users (name);
INSERT INTO users VALUES (1, 'a');
`})
	out, errOut, code := hazel(t, "plan", "app.db", "schema.sql")
	assert.Equal(t, 0, code)
	assert.Equal(t, "-- Database: "+filepath.Join(dir, "app.db")+` (new file)
-- Plan (1 table, 0 views, 1 index, 0 triggers)
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT
);
CREATE INDEX users_name ON users (name);
`, out)
	assert.Equal(t, "hazel: warning: schema.sql:4: ignored unsupported statement: INSERT INTO users VALUES (1, 'a');\n", errOut)
	// plan does not create the file.
	_, err := os.Stat("app.db")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestPlanNoChanges(t *testing.T) {
	workdir(t, map[string]string{"schema.sql": "CREATE TABLE t (a);"})
	createDB(t, "app.db", "CREATE TABLE t (a)")
	for _, args := range [][]string{{"plan"}, {"plan", "--check"}} {
		out, _, code := hazel(t, append(args, "app.db", "schema.sql")...)
		assert.Equal(t, 0, code)
		assert.True(t, strings.HasSuffix(out, "-- Plan (1 table, 0 views, 0 indexes, 0 triggers)\n-- No changes\n"), out)
	}
}

func TestPlanCheck(t *testing.T) {
	workdir(t, map[string]string{"schema.sql": "CREATE TABLE t (a, b);"})
	createDB(t, "app.db", "CREATE TABLE t (a)")
	out, _, code := hazel(t, "plan", "--check", "app.db", "schema.sql")
	assert.Equal(t, 2, code)
	assert.Contains(t, out, "ALTER TABLE t ADD COLUMN b;\n")

	out, _, code = hazel(t, "plan", "app.db", "schema.sql")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "ALTER TABLE t ADD COLUMN b;\n")
}

func TestPlanRebuild(t *testing.T) {
	workdir(t, map[string]string{"schema.sql": "CREATE TABLE t (a TEXT);"})
	createDB(t, "app.db", "CREATE TABLE t (a INT)")

	out, errOut, code := hazel(t, "plan", "app.db", "schema.sql")
	assert.Equal(t, 3, code)
	assert.True(t, strings.HasSuffix(out, "-- table rebuild required: t (type of a)\n"), out)
	assert.NotContains(t, out, "CREATE TABLE new_t")
	assert.Equal(t, "hazel: error: 1 table needs a rebuild; pass --allow-table-rebuild to allow it\n", errOut)

	// With --check, the changes win: exit status 2.
	_, _, code = hazel(t, "plan", "--check", "app.db", "schema.sql")
	assert.Equal(t, 2, code)

	out, _, code = hazel(t, "plan", "--allow-table-rebuild", "app.db", "schema.sql")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "-- table rebuild: t (type of a)\nCREATE TABLE new_t (\n    a TEXT\n);\n")

	t.Setenv("HAZEL_ALLOW_TABLE_REBUILD", "true")
	_, _, code = hazel(t, "plan", "app.db", "schema.sql")
	assert.Equal(t, 0, code)
}

func TestPlanRebuildMany(t *testing.T) {
	workdir(t, map[string]string{"schema.sql": "CREATE TABLE t (a TEXT); CREATE TABLE u (b TEXT);"})
	createDB(t, "app.db", "CREATE TABLE t (a INT); CREATE TABLE u (b INT)")
	_, errOut, code := hazel(t, "plan", "app.db", "schema.sql")
	assert.Equal(t, 3, code)
	assert.Equal(t, "hazel: error: 2 tables need a rebuild; pass --allow-table-rebuild to allow it\n", errOut)
}

func TestPlanFilter(t *testing.T) {
	workdir(t, map[string]string{"schema.sql": `
CREATE TABLE users (id INTEGER PRIMARY KEY);
CREATE TABLE posts (id INTEGER PRIMARY KEY);
`})
	createDB(t, "app.db", `
CREATE TABLE tmp_import (x);
CREATE INDEX tmp_import_x ON tmp_import (x);
CREATE TABLE TMP_Other (x);
`)
	tests := []struct {
		args []string
		want []string
		not  []string
	}{
		{
			args: []string{"-E", "tmp_*"},
			want: []string{"DROP TABLE TMP_Other;", "CREATE TABLE users", "CREATE TABLE posts"},
			not:  []string{"tmp_import"},
		},
		{
			// Patterns compare case as written.
			args: []string{"-E", "/^tmp_/", "-E", "/^TMP_/"},
			want: []string{"CREATE TABLE users"},
			not:  []string{"tmp_import", "TMP_Other"},
		},
		{
			args: []string{"-I", "users", "-I", "tmp_import"},
			want: []string{"DROP TABLE tmp_import;", "CREATE TABLE users"},
			not:  []string{"posts", "TMP_Other", "DROP INDEX"},
		},
		{
			args: []string{"-I", "/s$/", "-E", "posts"},
			want: []string{"CREATE TABLE users"},
			not:  []string{"posts", "tmp_import", "TMP_Other"},
		},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			out, _, code := hazel(t, append(append([]string{"plan"}, tt.args...), "app.db", "schema.sql")...)
			assert.Equal(t, 0, code)
			for _, w := range tt.want {
				assert.Contains(t, out, w)
			}
			for _, n := range tt.not {
				assert.NotContains(t, out, n)
			}
		})
	}
}

func TestPlanBadPattern(t *testing.T) {
	workdir(t, map[string]string{"schema.sql": "CREATE TABLE t (a);"})
	_, errOut, code := hazel(t, "plan", "-E", "/(/", "app.db", "schema.sql")
	assert.Equal(t, 80, code)
	assert.Contains(t, errOut, `invalid pattern "/(/"`)

	_, _, code = hazel(t, "plan", "-E", "[", "app.db", "schema.sql")
	assert.Equal(t, 80, code)
}

func TestConfig(t *testing.T) {
	workdir(t, map[string]string{
		"schema.sql": "CREATE TABLE t (a TEXT); CREATE TABLE tmp_x (a);",
		"hazel.yml":  "allow-table-rebuild: true\nexclude:\n  - tmp_*\n",
		"check.yml":  "check: true\n",
		"bad.yml":    "allow_table_rebuild: true\n",
		"broken.yml": "exclude: [",
	})
	createDB(t, "app.db", "CREATE TABLE t (a INT)")

	out, _, code := hazel(t, "-C", "hazel.yml", "plan", "app.db", "schema.sql")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "-- table rebuild: t (type of a)")
	assert.NotContains(t, out, "tmp_x")

	t.Setenv("HAZEL_CONFIG", "hazel.yml")
	_, _, code = hazel(t, "plan", "app.db", "schema.sql")
	assert.Equal(t, 0, code)

	// An environment variable wins over the file.
	t.Setenv("HAZEL_ALLOW_TABLE_REBUILD", "false")
	_, _, code = hazel(t, "plan", "app.db", "schema.sql")
	assert.Equal(t, 3, code)

	_, errOut, code := hazel(t, "-C", "check.yml", "plan", "app.db", "schema.sql")
	assert.Equal(t, 80, code)
	assert.Contains(t, errOut, "config key(s) only for the command line: check")

	_, errOut, code = hazel(t, "-C", "bad.yml", "plan", "app.db", "schema.sql")
	assert.Equal(t, 80, code)
	assert.Contains(t, errOut, "unknown config key(s): allow_table_rebuild")

	_, errOut, code = hazel(t, "-C", "broken.yml", "plan", "app.db", "schema.sql")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errOut, "yaml: line 1")
}

func TestPlanErrors(t *testing.T) {
	workdir(t, map[string]string{
		"schema.sql": "CREATE TABLE t (a);",
		"text.db":    "this is not a database",
		"broken.sql": "CREATE TABLE t (",
	})
	// A view whose table was dropped cannot be read.
	createDB(t, "broken_view.db", "CREATE TABLE b (x); CREATE VIEW v AS SELECT x FROM b; DROP TABLE b;")
	tests := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"plan", "app.db"}, 80, `expected "<schema-file> ..."`},
		{[]string{"plan", "text.db", "schema.sql"}, 1, "not a SQLite database"},
		{[]string{"plan", "app.db", "missing.sql"}, 1, "missing.sql"},
		{[]string{"plan", "app.db", "broken.sql"}, 1, "broken.sql:1:"},
		{[]string{"plan", "app.db?mode=ro", "schema.sql"}, 1, "must not contain '?'"},
		{[]string{"plan", "--no-such-flag", "app.db", "schema.sql"}, 80, "unknown flag --no-such-flag"},
		{[]string{"plan", "broken_view.db", "schema.sql"}, 1, "view v: "},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			_, errOut, code := hazel(t, tt.args...)
			assert.Equal(t, tt.code, code)
			assert.Contains(t, errOut, tt.want)
		})
	}
}

func TestPlanMemory(t *testing.T) {
	workdir(t, map[string]string{"schema.sql": "CREATE TABLE t (a);"})
	out, _, code := hazel(t, "plan", ":memory:", "schema.sql")
	assert.Equal(t, 0, code)
	assert.True(t, strings.HasPrefix(out, "-- Database: :memory:\n"), out)
	assert.Contains(t, out, "CREATE TABLE t (\n    a\n);")
}

func TestDiff(t *testing.T) {
	workdir(t, map[string]string{
		"old.sql": "CREATE TABLE t (a INT); CREATE TABLE tmp_x (a);",
		"new.sql": "CREATE TABLE t (a INT, b); CREATE TABLE u (c);",
		"reb.sql": "CREATE TABLE t (a TEXT); CREATE TABLE tmp_x (a);",
	})
	out, _, code := hazel(t, "diff", "old.sql", "new.sql")
	assert.Equal(t, 0, code)
	assert.Equal(t, `-- WARNING: the following will be dropped and their data will be lost:
--   table  tmp_x
--
ALTER TABLE t ADD COLUMN b;
-- drop: removed from schema
DROP TABLE tmp_x;
CREATE TABLE u (
    c
);
`, out)

	_, _, code = hazel(t, "diff", "--check", "old.sql", "new.sql")
	assert.Equal(t, 2, code)

	out, _, code = hazel(t, "diff", "--check", "old.sql", "old.sql")
	assert.Equal(t, 0, code)
	assert.Equal(t, "-- No changes\n", out)

	out, _, code = hazel(t, "diff", "-E", "tmp_*", "old.sql", "new.sql")
	assert.Equal(t, 0, code)
	assert.NotContains(t, out, "tmp_x")

	_, _, code = hazel(t, "diff", "old.sql", "reb.sql")
	assert.Equal(t, 3, code)
	_, _, code = hazel(t, "diff", "--allow-table-rebuild", "old.sql", "reb.sql")
	assert.Equal(t, 0, code)

	_, _, code = hazel(t, "diff", "old.sql")
	assert.Equal(t, 80, code)

	_, errOut, code := hazel(t, "diff", "missing.sql", "new.sql")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "missing.sql")
	_, errOut, code = hazel(t, "diff", "old.sql", "missing.sql")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "missing.sql")
}

func TestPlanFilterKeepsWhatViewsRead(t *testing.T) {
	const schema = `
CREATE TABLE legacy_users (id INTEGER PRIMARY KEY, name TEXT);
CREATE TABLE users (id INTEGER PRIMARY KEY, legacy_id INT);
CREATE VIEW v AS SELECT u.id, l.name FROM users u JOIN legacy_users l ON l.id = u.legacy_id;
`
	workdir(t, map[string]string{
		"same.sql": schema,
		"renamed.sql": `
CREATE TABLE legacy_users (id INTEGER PRIMARY KEY, name TEXT);
CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  -- hazel:renamed-from legacy_id
  old_id INT
);
CREATE VIEW v AS SELECT u.id, l.name FROM users u JOIN legacy_users l ON l.id = u.old_id;
`,
	})
	createDB(t, "app.db", schema)

	// The view reads an excluded table, which stays in the schemas diff
	// compiles, so the view compares equal.
	out, _, code := hazel(t, "plan", "-E", "legacy_*", "app.db", "same.sql")
	assert.Equal(t, 0, code)
	assert.True(t, strings.HasSuffix(out, "-- Plan (1 table, 1 view, 0 indexes, 0 triggers)\n-- No changes\n"), out)

	// The rename is replayed with the excluded table in place.
	out, errOut, code := hazel(t, "plan", "-E", "legacy_*", "app.db", "renamed.sql")
	assert.Equal(t, 0, code, errOut)
	assert.True(t, strings.HasSuffix(out, "ALTER TABLE users RENAME COLUMN legacy_id TO old_id;\n"), out)
}

func TestPlanFilterPlansNothingForExcluded(t *testing.T) {
	// legacy_v is excluded, so nothing is planned for it, not even to make
	// way for the DROP COLUMN, which it stands in the way of: that apply
	// fails, and it is for the user to deal with.
	workdir(t, map[string]string{"schema.sql": `
CREATE TABLE users (id INTEGER PRIMARY KEY, legacy_id INT);
CREATE TABLE legacy_users (id INTEGER PRIMARY KEY, name TEXT);
`})
	createDB(t, "app.db", `
CREATE TABLE users (id INTEGER PRIMARY KEY, legacy_id INT, old TEXT);
CREATE TABLE legacy_users (id INTEGER PRIMARY KEY, name TEXT);
CREATE VIEW legacy_v AS SELECT u.id, u.old FROM users u;
CREATE INDEX legacy_users_name ON legacy_users (name);
CREATE TRIGGER legacy_users_ai AFTER INSERT ON legacy_users BEGIN SELECT 1; END;
`)
	out, _, code := hazel(t, "plan", "-E", "legacy_*", "app.db", "schema.sql")
	assert.Equal(t, 0, code)
	assert.True(t, strings.HasSuffix(out, `-- drop: removed from schema
ALTER TABLE users DROP COLUMN old;
`), out)
	assert.NotContains(t, out, "legacy_v")
	assert.NotContains(t, out, "legacy_users_name")
	assert.NotContains(t, out, "legacy_users_ai")
}

func TestPlanSummaryWithFilter(t *testing.T) {
	workdir(t, map[string]string{"schema.sql": `
CREATE TABLE t (a);
CREATE INDEX t_a ON t (a);
CREATE TRIGGER t_ai AFTER INSERT ON t BEGIN SELECT 1; END;
CREATE TABLE tmp_x (a);
CREATE INDEX tmp_x_a ON tmp_x (a);
CREATE TRIGGER tmp_x_ai AFTER INSERT ON tmp_x BEGIN SELECT 1; END;
CREATE VIEW tmp_v AS SELECT a FROM t;
`})
	out, _, code := hazel(t, "plan", "-E", "tmp_*", "app.db", "schema.sql")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "-- Plan (1 table, 0 views, 1 index, 1 trigger)\n")
}

func query(t *testing.T, path, q string) string {
	t.Helper()
	conn, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer conn.Close()
	var out sql.NullString
	require.NoError(t, conn.QueryRow(q).Scan(&out))
	return out.String
}

func TestApply(t *testing.T) {
	dir := workdir(t, map[string]string{"schema.sql": `
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT);
CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);
-- hazel:execute SELECT NOT EXISTS (SELECT 1 FROM settings WHERE key = 'version')
INSERT INTO settings VALUES ('version', '1');
-- hazel:execute
UPDATE settings SET value = value || '!' WHERE key = 'version';
`})
	out, _, code := hazel(t, "apply", "app.db", "schema.sql")
	require.Equal(t, 0, code, out)
	assert.Regexp(t, `^-- Database: `+regexp.QuoteMeta(filepath.Join(dir, "app.db"))+` \(new file\)
-- Plan \(2 tables, 0 views, 0 indexes, 0 triggers\)
CREATE TABLE settings \(
    key TEXT PRIMARY KEY,
    value TEXT
\);
-- ok \(\d+ms\)
CREATE TABLE users \(
    id INTEGER PRIMARY KEY,
    name TEXT
\);
-- ok \(\d+ms\)
-- hazel:execute SELECT NOT EXISTS \(SELECT 1 FROM settings WHERE key = 'version'\)
INSERT INTO settings VALUES \('version', '1'\);
-- ok \(\d+ms\)
-- hazel:execute
UPDATE settings SET value = value \|\| '!' WHERE key = 'version';
-- ok \(\d+ms\)
-- Applied 4 statements
$`, out)
	assert.Equal(t, "1!", query(t, "app.db", "SELECT value FROM settings"))

	// The condition is false now; the statement without one runs again.
	out, _, code = hazel(t, "apply", "app.db", "schema.sql")
	assert.Equal(t, 0, code)
	assert.NotContains(t, out, "INSERT INTO settings")
	assert.Contains(t, out, "-- Applied 1 statement\n")
	assert.Equal(t, "1!!", query(t, "app.db", "SELECT value FROM settings"))
}

func TestApplyNoChanges(t *testing.T) {
	workdir(t, map[string]string{"schema.sql": "CREATE TABLE t (a);"})
	createDB(t, "app.db", "CREATE TABLE t (a)")
	out, _, code := hazel(t, "apply", "app.db", "schema.sql")
	assert.Equal(t, 0, code)
	assert.True(t, strings.HasSuffix(out, "-- No changes\n"), out)
}

func TestPlanExecute(t *testing.T) {
	workdir(t, map[string]string{"schema.sql": `
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT);
-- hazel:execute SELECT NOT EXISTS (SELECT 1 FROM settings WHERE key = 'version')
INSERT INTO settings VALUES ('version', '1');
-- hazel:execute SELECT count(*) = 0 FROM new_table
INSERT INTO settings VALUES ('other', '2');
`})
	createDB(t, "app.db", "CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT); INSERT INTO settings VALUES ('version', '0');")

	// The condition that is false leaves its statement out; the one that
	// cannot be evaluated keeps it, with a note.
	out, _, code := hazel(t, "plan", "app.db", "schema.sql")
	assert.Equal(t, 0, code)
	assert.True(t, strings.HasSuffix(out, `-- hazel:execute SELECT count(*) = 0 FROM new_table
-- check SQL could not be evaluated at plan time: SQL logic error: no such table: new_table (1); apply will decide
INSERT INTO settings VALUES ('other', '2');
`), out)
	assert.NotContains(t, out, "'version', '1'")

	// A statement to run is a change for --check.
	_, _, code = hazel(t, "plan", "--check", "app.db", "schema.sql")
	assert.Equal(t, 2, code)

	// apply cannot leave it to anyone.
	_, errOut, code := hazel(t, "apply", "app.db", "schema.sql")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "failed to evaluate the condition of hazel:execute: SELECT count(*) = 0 FROM new_table")
}

func TestExecuteConditionNotBoolean(t *testing.T) {
	for _, tc := range []struct {
		cond string
		got  string
	}{
		{"SELECT count(*) FROM settings", "2"},
		{"SELECT NULL", "NULL"},
		{"SELECT 'yes'", "'yes'"},
		{"SELECT 1.0", "1.0"},
		{"SELECT X'01'", "X'01'"},
		{"SELECT 1e100", "1e+100"},
		{"SELECT 1 FROM settings WHERE 0", "no rows"},
	} {
		t.Run(tc.cond, func(t *testing.T) {
			workdir(t, map[string]string{"schema.sql": `
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT);
-- hazel:execute ` + tc.cond + `
INSERT INTO settings VALUES ('c', '3');
`})
			createDB(t, "app.db", "CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT); INSERT INTO settings VALUES ('a', '1'), ('b', '2');")
			want := "the condition of hazel:execute must return 0 or 1: " + tc.cond + ": got " + tc.got

			_, errOut, code := hazel(t, "plan", "app.db", "schema.sql")
			assert.Equal(t, 1, code)
			assert.Contains(t, errOut, want)

			_, errOut, code = hazel(t, "apply", "app.db", "schema.sql")
			assert.Equal(t, 1, code)
			assert.Contains(t, errOut, want)
			assert.Equal(t, "2", query(t, "app.db", "SELECT count(*) FROM settings"))
		})
	}
}

// A condition that plan cannot evaluate is checked again by apply, after
// the DDL.
func TestApplyExecuteConditionNotBoolean(t *testing.T) {
	workdir(t, map[string]string{"schema.sql": `
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT);
CREATE TABLE t (a);
-- hazel:execute SELECT max(a) FROM t
INSERT INTO settings VALUES ('c', '3');
`})
	createDB(t, "app.db", "CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT);")

	out, _, code := hazel(t, "plan", "app.db", "schema.sql")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "-- check SQL could not be evaluated at plan time: SQL logic error: no such table: t (1); apply will decide\n")

	_, errOut, code := hazel(t, "apply", "app.db", "schema.sql")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "the condition of hazel:execute must return 0 or 1: SELECT max(a) FROM t: got NULL")
	assert.Equal(t, "0", query(t, "app.db", "SELECT count(*) FROM sqlite_schema WHERE name = 't'"))
}

func TestApplyRebuild(t *testing.T) {
	workdir(t, map[string]string{"schema.sql": "CREATE TABLE t (id INTEGER PRIMARY KEY, a TEXT);"})
	createDB(t, "app.db", "CREATE TABLE t (id INTEGER PRIMARY KEY, a INT); INSERT INTO t VALUES (1, 10);")

	out, errOut, code := hazel(t, "apply", "app.db", "schema.sql")
	assert.Equal(t, 3, code)
	assert.True(t, strings.HasSuffix(out, "-- table rebuild required: t (type of a)\n"), out)
	assert.Contains(t, errOut, "1 table needs a rebuild")
	assert.Equal(t, "INT", query(t, "app.db", "SELECT type FROM pragma_table_info('t') WHERE name = 'a'"))

	_, _, code = hazel(t, "apply", "--allow-table-rebuild", "app.db", "schema.sql")
	assert.Equal(t, 0, code)
	assert.Equal(t, "TEXT", query(t, "app.db", "SELECT type FROM pragma_table_info('t') WHERE name = 'a'"))
	assert.Equal(t, "10", query(t, "app.db", "SELECT a FROM t"))
}

func TestApplyRollsBack(t *testing.T) {
	// Each plan adds a column before the statement that fails; after the
	// rollback, the column is not there.
	tests := []struct {
		name          string
		init          string
		schema        string
		table, column string
		want          string
	}{
		{
			name:   "set not null",
			init:   "CREATE TABLE t (a, b); INSERT INTO t VALUES (NULL, 1);",
			schema: "CREATE TABLE t (a NOT NULL, b, c);",
			table:  "t", column: "c",
			want: "ALTER TABLE t ALTER COLUMN a SET NOT NULL: existing rows in t have NULL in a: constraint failed",
		},
		{
			name:   "named check",
			init:   "CREATE TABLE t (a); INSERT INTO t VALUES (-1);",
			schema: "CREATE TABLE t (a, b, CONSTRAINT a_positive CHECK (a > 0));",
			table:  "t", column: "b",
			want: "ALTER TABLE t ADD CONSTRAINT a_positive CHECK (a > 0): existing rows in t violate a_positive: constraint failed",
		},
		{
			name:   "unnamed check",
			init:   "CREATE TABLE t (a); INSERT INTO t VALUES (-1);",
			schema: "CREATE TABLE t (a, b, CHECK (a > 0));",
			table:  "t", column: "b",
			want: "ALTER TABLE t ADD CHECK (a > 0): existing rows in t violate the CHECK constraint: constraint failed",
		},
		{
			name:   "other",
			init:   "CREATE TABLE t (a); INSERT INTO t VALUES (1), (1);",
			schema: "CREATE TABLE t (a, b); CREATE UNIQUE INDEX t_a ON t (a);",
			table:  "t", column: "b",
			want: "CREATE UNIQUE INDEX t_a ON t (a): constraint failed: UNIQUE constraint failed: t.a",
		},
		{
			name:   "foreign key check",
			init:   "CREATE TABLE p (id INTEGER PRIMARY KEY); CREATE TABLE c (pid); INSERT INTO c VALUES (9);",
			schema: "CREATE TABLE p (id INTEGER PRIMARY KEY); CREATE TABLE c (pid REFERENCES p (id), x);",
			table:  "c", column: "x",
			want: "foreign key violations, rolled back: c (rowid 1) refers to a missing row of p",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workdir(t, map[string]string{"schema.sql": tt.schema})
			createDB(t, "app.db", tt.init)
			_, errOut, code := hazel(t, "apply", "--allow-table-rebuild", "app.db", "schema.sql")
			assert.Equal(t, 1, code)
			assert.Contains(t, errOut, tt.want)
			assert.Equal(t, "0", query(t, "app.db",
				"SELECT count(*) FROM pragma_table_info('"+tt.table+"') WHERE name = '"+tt.column+"'"))
		})
	}
}

func TestApplyBusy(t *testing.T) {
	workdir(t, map[string]string{"schema.sql": "CREATE TABLE t (a);"})
	createDB(t, "app.db", "CREATE TABLE u (a)")
	other, err := sql.Open("sqlite", "app.db")
	require.NoError(t, err)
	defer other.Close()
	tx, err := other.Begin()
	require.NoError(t, err)
	_, err = tx.Exec("INSERT INTO u VALUES (1)")
	require.NoError(t, err)
	defer tx.Rollback()

	_, errOut, code := hazel(t, "apply", "--busy-timeout=100ms", "app.db", "schema.sql")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "could not lock the database within 100ms")
}

func TestApplyOptions(t *testing.T) {
	workdir(t, map[string]string{"schema.sql": "CREATE TABLE t (a);", "hazel.yml": "busy-timeout: 2s\n"})
	_, errOut, code := hazel(t, "apply", "--busy-timeout=0s", "app.db", "schema.sql")
	assert.Equal(t, 80, code)
	assert.Contains(t, errOut, "--busy-timeout must be greater than 0")

	_, errOut, code = hazel(t, "apply", "-E", "/(/", "app.db", "schema.sql")
	assert.Equal(t, 80, code)
	assert.Contains(t, errOut, "invalid pattern")

	t.Setenv("HAZEL_BUSY_TIMEOUT", "-1s")
	_, _, code = hazel(t, "apply", "app.db", "schema.sql")
	assert.Equal(t, 80, code)

	_, _, code = hazel(t, "-C", "hazel.yml", "apply", "--busy-timeout=1s", "app.db", "schema.sql")
	assert.Equal(t, 0, code)
}

func TestApplyMemory(t *testing.T) {
	workdir(t, map[string]string{"schema.sql": "CREATE TABLE t (a);"})
	out, _, code := hazel(t, "apply", ":memory:", "schema.sql")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "-- Applied 1 statement\n")
}

func TestApplyErrors(t *testing.T) {
	workdir(t, map[string]string{"schema.sql": "CREATE TABLE t (a);", "text.db": "this is not a database"})
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"apply", "text.db", "schema.sql"}, "not a SQLite database"},
		{[]string{"apply", "app.db", "missing.sql"}, "missing.sql"},
		{[]string{"apply", "no/such/dir/app.db", "schema.sql"}, "directory does not exist"},
		{[]string{"apply", "app.db?x", "schema.sql"}, "must not contain '?'"},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			_, errOut, code := hazel(t, tt.args...)
			assert.Equal(t, 1, code)
			assert.Contains(t, errOut, tt.want)
		})
	}
}

func TestApplyDataLoss(t *testing.T) {
	workdir(t, map[string]string{"schema.sql": "CREATE TABLE t (a);"})
	createDB(t, "app.db", "CREATE TABLE t (a, b); CREATE TABLE old (x);")
	out, _, code := hazel(t, "apply", "app.db", "schema.sql")
	assert.Equal(t, 0, code)
	assert.Regexp(t, `-- Plan \(1 table, 0 views, 0 indexes, 0 triggers\)
-- WARNING: the following will be dropped and their data will be lost:
--   table  old
--   column t.b
--
-- drop: removed from schema
ALTER TABLE t DROP COLUMN b;
-- ok \(\d+ms\)
DROP TABLE old;
-- ok \(\d+ms\)
-- Applied 2 statements
$`, out)
}

func TestApplyExecuteFails(t *testing.T) {
	// The last statement has no ";", and the plan adds one.
	workdir(t, map[string]string{"schema.sql": "CREATE TABLE t (a);\n-- hazel:execute\nINSERT INTO nope VALUES (1)"})
	out, errOut, code := hazel(t, "apply", "app.db", "schema.sql")
	assert.Equal(t, 1, code)
	assert.Contains(t, out, "-- hazel:execute\nINSERT INTO nope VALUES (1);\n")
	assert.Contains(t, errOut, "INSERT INTO nope VALUES (1): SQL logic error: no such table: nope")
	// The CREATE TABLE is rolled back too.
	assert.Equal(t, "0", query(t, "app.db", "SELECT count(*) FROM sqlite_schema"))
}

func TestApplyUnreadableDatabase(t *testing.T) {
	workdir(t, map[string]string{"schema.sql": "CREATE TABLE t (a);"})
	createDB(t, "app.db", "CREATE TABLE b (x); CREATE VIEW v AS SELECT x FROM b; DROP TABLE b;")
	_, errOut, code := hazel(t, "apply", "app.db", "schema.sql")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "view v: ")
}

func TestApplyExistingForeignKeyViolation(t *testing.T) {
	const tables = `
CREATE TABLE p (id INTEGER PRIMARY KEY);
CREATE TABLE s (k TEXT PRIMARY KEY, v TEXT);
`
	workdir(t, map[string]string{
		"schema.sql": tables + `
CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER REFERENCES p (id));
CREATE TABLE d (id INTEGER PRIMARY KEY, pid INTEGER REFERENCES p (id));
CREATE INDEX s_v ON s (v);
`,
		"skip_c.sql": tables + `
-- hazel:skip-foreign-key-check
CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER REFERENCES p (id));
CREATE TABLE d (id INTEGER PRIMARY KEY, pid INTEGER REFERENCES p (id));
CREATE INDEX s_v ON s (v);
`,
	})
	newDB := func(t *testing.T, extra string) {
		t.Helper()
		require.NoError(t, os.RemoveAll("app.db"))
		createDB(t, "app.db", tables+`
CREATE TABLE c (id INTEGER PRIMARY KEY, pid INTEGER REFERENCES p (id));
CREATE TABLE d (id INTEGER PRIMARY KEY, pid INTEGER REFERENCES p (id));
INSERT INTO c VALUES (1, 99);
`+extra)
	}

	// The violation was there before, and still fails the apply.
	newDB(t, "")
	_, errOut, code := hazel(t, "apply", "app.db", "schema.sql")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "foreign key violations, rolled back: c (rowid 1) refers to a missing row of p (--skip-foreign-key-check, or -- hazel:skip-foreign-key-check on the table, leaves rows that were there before out of the check)\n")
	assert.Equal(t, "0", query(t, "app.db", "SELECT count(*) FROM sqlite_schema WHERE name = 's_v'"))

	// --skip-foreign-key-check runs no check.
	out, _, code := hazel(t, "apply", "--skip-foreign-key-check", "app.db", "schema.sql")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "-- Applied 1 statement\n")

	newDB(t, "")
	t.Setenv("HAZEL_SKIP_FOREIGN_KEY_CHECK", "true")
	_, _, code = hazel(t, "apply", "app.db", "schema.sql")
	assert.Equal(t, 0, code)
	t.Setenv("HAZEL_SKIP_FOREIGN_KEY_CHECK", "false")

	// The directive leaves c out of the check, and only c.
	newDB(t, "")
	_, _, code = hazel(t, "apply", "app.db", "skip_c.sql")
	assert.Equal(t, 0, code)

	newDB(t, "INSERT INTO d VALUES (1, 98);")
	_, errOut, code = hazel(t, "apply", "app.db", "skip_c.sql")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "foreign key violations, rolled back: d (rowid 1) refers to a missing row of p")
	assert.NotContains(t, errOut, "c (rowid 1)")
}

func TestDump(t *testing.T) {
	dir := workdir(t, nil)
	createDB(t, "app.db", `
CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);
CREATE INDEX users_name ON users (name);
CREATE TABLE tmp_x (a);
CREATE VIEW v AS SELECT name FROM users;
`)
	out, _, code := hazel(t, "dump", "app.db")
	assert.Equal(t, 0, code)
	assert.Equal(t, "-- Database: "+filepath.Join(dir, "app.db")+`
-- Dump (2 tables, 1 view, 1 index, 0 triggers)

CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT
);

CREATE INDEX users_name ON users (name);

CREATE TABLE tmp_x (
    a
);

CREATE VIEW v AS SELECT name FROM users;
`, out)

	// The dump plans clean.
	require.NoError(t, os.WriteFile("dump.sql", []byte(out), 0o644))
	_, _, code = hazel(t, "plan", "--check", "app.db", "dump.sql")
	assert.Equal(t, 0, code)
	// and fmt leaves it as it is.
	_, _, code = hazel(t, "fmt", "--check", "dump.sql")
	assert.Equal(t, 0, code)

	out, _, code = hazel(t, "dump", "--no-format", "-E", "tmp_*", "app.db")
	assert.Equal(t, 0, code)
	assert.True(t, strings.HasSuffix(out, `
-- Dump (1 table, 1 view, 1 index, 0 triggers)

CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);

CREATE INDEX users_name ON users (name);

CREATE VIEW v AS SELECT name FROM users;
`), out)

	out, _, code = hazel(t, "dump", ":memory:")
	assert.Equal(t, 0, code)
	assert.Equal(t, "-- Database: :memory:\n-- Dump (0 tables, 0 views, 0 indexes, 0 triggers)\n", out)
}

func TestDumpErrors(t *testing.T) {
	workdir(t, map[string]string{"text.db": "this is not a database"})
	createDB(t, "broken_view.db", "CREATE TABLE b (x); CREATE VIEW v AS SELECT x FROM b; DROP TABLE b;")
	tests := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"dump"}, 80, `expected "<db>"`},
		{[]string{"dump", "missing.db"}, 1, "missing.db: no such file"},
		{[]string{"dump", "text.db"}, 1, "not a SQLite database"},
		{[]string{"dump", "-I", "/(/", "text.db"}, 80, "invalid pattern"},
		{[]string{"dump", "app.db?mode=ro"}, 1, "must not contain '?'"},
		{[]string{"dump", "broken_view.db"}, 1, "view v: "},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			_, errOut, code := hazel(t, tt.args...)
			assert.Equal(t, tt.code, code)
			assert.Contains(t, errOut, tt.want)
		})
	}
	// dump does not create the file.
	_, err := os.Stat("missing.db")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestDumpSplit(t *testing.T) {
	dir := workdir(t, nil)
	createDB(t, "app.db", `
CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);
CREATE INDEX users_name ON users (name);
CREATE TABLE tmp_x (a);
CREATE VIEW v AS SELECT name FROM users;
`)
	// A file left from an earlier dump stays.
	require.NoError(t, os.MkdirAll("schema", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("schema", "old.sql"), []byte("CREATE TABLE old (a);\n"), 0o644))

	out, _, code := hazel(t, "dump", "--split", "schema/sub", "-E", "tmp_*", "app.db")
	assert.Equal(t, 0, code)
	assert.Equal(t, "-- Database: "+filepath.Join(dir, "app.db")+"\n-- Wrote 2 files to schema/sub\n", out)
	b, err := os.ReadFile(filepath.Join("schema", "sub", "users.sql"))
	require.NoError(t, err)
	assert.Equal(t, "CREATE TABLE users (\n    id INTEGER PRIMARY KEY,\n    name TEXT\n);\n\nCREATE INDEX users_name ON users (name);\n", string(b))
	b, err = os.ReadFile(filepath.Join("schema", "sub", "v.sql"))
	require.NoError(t, err)
	assert.Equal(t, "CREATE VIEW v AS SELECT name FROM users;\n", string(b))
	assert.NoFileExists(t, filepath.Join("schema", "sub", "tmp_x.sql"))

	// The files plan clean, and an existing file is overwritten.
	_, _, code = hazel(t, "plan", "--check", "-E", "tmp_*", "app.db", "schema/sub/users.sql", "schema/sub/v.sql")
	assert.Equal(t, 0, code)
	_, _, code = hazel(t, "dump", "--split", "schema", "--no-format", "app.db")
	assert.Equal(t, 0, code)
	b, err = os.ReadFile(filepath.Join("schema", "users.sql"))
	require.NoError(t, err)
	assert.Equal(t, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);\n\nCREATE INDEX users_name ON users (name);\n", string(b))
	assert.FileExists(t, filepath.Join("schema", "old.sql"))

	out, _, code = hazel(t, "dump", "--split", "schema", "-I", "users", "app.db")
	assert.Equal(t, 0, code)
	assert.True(t, strings.HasSuffix(out, "\n-- Wrote 1 file to schema\n"), out)
}

func TestDumpSplitErrors(t *testing.T) {
	workdir(t, map[string]string{"file": "", "hazel.yml": "split: schema\n"})
	createDB(t, "app.db", "CREATE TABLE t (a);")
	createDB(t, "slash.db", `CREATE TABLE "a/b" (a);`)
	// A directory where the file of t goes.
	require.NoError(t, os.MkdirAll(filepath.Join("dir", "t.sql"), 0o755))
	tests := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"dump", "--split", "out", "slash.db"}, 1, `cannot write a file for "a/b"`},
		{[]string{"dump", "--split", "file", "app.db"}, 1, "file"},
		{[]string{"dump", "--split", "dir", "app.db"}, 1, "t.sql"},
		{[]string{"-C", "hazel.yml", "dump", "app.db"}, 80, "split"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			_, errOut, code := hazel(t, tt.args...)
			assert.Equal(t, tt.code, code)
			assert.Contains(t, errOut, tt.want)
		})
	}
	// Nothing is written for a name that is not a plain file name.
	assert.NoDirExists(t, "out")
}

func TestFmt(t *testing.T) {
	workdir(t, map[string]string{
		"tables.sql": "-- hazel:renamed-from members\nCREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);\n",
		"views.sql":  "CREATE VIEW v AS SELECT name FROM users;\n",
	})
	require.NoError(t, os.Chmod("tables.sql", 0o600))

	// views.sql reads a table of tables.sql, and is already formatted.
	out, errOut, code := hazel(t, "fmt", "--check", "tables.sql", "views.sql")
	assert.Equal(t, 2, code)
	assert.Equal(t, "tables.sql\n", out)
	assert.Empty(t, errOut)

	out, _, code = hazel(t, "fmt", "tables.sql", "views.sql")
	assert.Equal(t, 0, code)
	assert.Empty(t, out)
	b, err := os.ReadFile("tables.sql")
	require.NoError(t, err)
	assert.Equal(t, "-- hazel:renamed-from members\nCREATE TABLE users (\n    id INTEGER PRIMARY KEY,\n    name TEXT\n);\n", string(b))
	if runtime.GOOS != "windows" {
		info, err := os.Stat("tables.sql")
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	_, _, code = hazel(t, "fmt", "--check", "tables.sql", "views.sql")
	assert.Equal(t, 0, code)

	_, _, code = hazel(t, "fmt", "--strip-renamed-from", "tables.sql")
	assert.Equal(t, 0, code)
	b, err = os.ReadFile("tables.sql")
	require.NoError(t, err)
	assert.Equal(t, "CREATE TABLE users (\n    id INTEGER PRIMARY KEY,\n    name TEXT\n);\n", string(b))
}

func TestFmtErrors(t *testing.T) {
	workdir(t, map[string]string{
		"good.sql":   "CREATE TABLE t (a,b);",
		"broken.sql": "CREATE TABLE u (",
		"hazel.yml":  "strip-renamed-from: true\n",
	})
	tests := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"fmt"}, 80, `expected "<schema-file> ..."`},
		{[]string{"fmt", "good.sql", "broken.sql"}, 1, "broken.sql:1:"},
		{[]string{"fmt", "good.sql", "missing.sql"}, 1, "missing.sql"},
		{[]string{"-C", "hazel.yml", "fmt", "good.sql"}, 80, "strip-renamed-from"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			_, errOut, code := hazel(t, tt.args...)
			assert.Equal(t, tt.code, code)
			assert.Contains(t, errOut, tt.want)
		})
	}
	// No file is written when one of them fails.
	b, err := os.ReadFile("good.sql")
	require.NoError(t, err)
	assert.Equal(t, "CREATE TABLE t (a,b);", string(b))

	// A file that cannot be written. root can write a read-only file.
	if os.Geteuid() == 0 {
		return
	}
	require.NoError(t, os.Chmod("good.sql", 0o444))
	_, errOut, code := hazel(t, "fmt", "good.sql")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "good.sql")
}
