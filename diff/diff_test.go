package diff_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/winebarrel/hazel/catalog"
	"github.com/winebarrel/hazel/db"
	"github.com/winebarrel/hazel/desired"
	"github.com/winebarrel/hazel/diff"
	"github.com/winebarrel/hazel/model"
	"github.com/winebarrel/orderedmap/v2"
	"gopkg.in/yaml.v3"
)

// diffTestCase is one fixture in testdata. init is run on an empty database
// to give the current schema, and desired is loaded as the schema files.
// Either plan, the expected plan, or error, a part of the expected error, is
// given.
type diffTestCase struct {
	Init    string `yaml:"init"`
	Desired string `yaml:"desired"`
	Plan    string `yaml:"plan"`
	Error   string `yaml:"error"`
}

func TestDiff(t *testing.T) {
	files, err := filepath.Glob("testdata/*.yml")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		t.Run(strings.TrimSuffix(filepath.Base(file), ".yml"), func(t *testing.T) {
			b, err := os.ReadFile(file)
			require.NoError(t, err)
			var tc diffTestCase
			require.NoError(t, yaml.Unmarshal(b, &tc))

			conn := openDB(t, tc.Init)
			ctx := context.Background()
			current, err := catalog.Read(ctx, conn)
			require.NoError(t, err)
			want := loadDesired(t, tc.Desired)

			plan, err := diff.Diff(current, want)
			if tc.Error != "" {
				assert.ErrorContains(t, err, tc.Error)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.Plan, plan.String())

			// The plan applies, and leaves nothing to do.
			for _, s := range plan.Statements() {
				_, err := conn.Exec(s)
				require.NoError(t, err, s)
			}
			applied, err := catalog.Read(ctx, conn)
			require.NoError(t, err)
			again, err := diff.Diff(applied, want)
			require.NoError(t, err)
			assert.True(t, again.IsEmpty(), "plan after apply:\n%s", again)
		})
	}
}

func openDB(t *testing.T, init string) *sql.DB {
	t.Helper()
	conn, err := db.OpenReadWrite(db.Memory, time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	if init != "" {
		_, err = conn.Exec(init)
		require.NoError(t, err)
	}
	return conn
}

func loadDesired(t *testing.T, body string) *model.Schema {
	t.Helper()
	result, err := desired.Load(context.Background(), []desired.Source{{Name: "desired.sql", Body: body}})
	require.NoError(t, err)
	return result.Schema
}

func TestPlanEmpty(t *testing.T) {
	plan, err := diff.Diff(model.NewSchema(), model.NewSchema())
	require.NoError(t, err)
	assert.True(t, plan.IsEmpty())
	assert.Empty(t, plan.Statements())
	assert.Empty(t, plan.String())
}

// applyPlan computes the plan from the database to desired and runs it.
func applyPlan(t *testing.T, conn *sql.DB, body string) *diff.Plan {
	t.Helper()
	current, err := catalog.Read(context.Background(), conn)
	require.NoError(t, err)
	plan, err := diff.Diff(current, loadDesired(t, body))
	require.NoError(t, err)
	for _, s := range plan.Statements() {
		_, err := conn.Exec(s)
		require.NoError(t, err, s)
	}
	return plan
}

func TestRebuildKeepsRows(t *testing.T) {
	conn := openDB(t, `
CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age TEXT, legacy TEXT);
INSERT INTO users VALUES (1, 'alice', '30', 'x'), (2, 'bob', NULL, 'y');
`)
	plan := applyPlan(t, conn, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, age INTEGER, email TEXT DEFAULT 'none');`)
	assert.Equal(t, []diff.Rebuild{{
		Table:   "users",
		Changes: []string{"NOT NULL of name", "type of age", "drops column legacy", "adds column email"},
	}}, plan.Rebuilds)
	assert.Equal(t, "users (NOT NULL of name; type of age; drops column legacy; adds column email)", plan.Rebuilds[0].String())

	rows, err := conn.Query(`SELECT id, name, age, typeof(age), email FROM users ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	type row struct {
		ID      int
		Name    string
		Age     any
		AgeType string
		Email   string
	}
	var got []row
	for rows.Next() {
		var r row
		require.NoError(t, rows.Scan(&r.ID, &r.Name, &r.Age, &r.AgeType, &r.Email))
		got = append(got, r)
	}
	// The rows are copied, the new INTEGER affinity applies to age, and the
	// new column gets its DEFAULT.
	assert.Equal(t, []row{
		{1, "alice", int64(30), "integer", "none"},
		{2, "bob", nil, "null", "none"},
	}, got)
}

func TestRebuildKeepsAutoincrement(t *testing.T) {
	conn := openDB(t, `
CREATE TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT, a INT);
INSERT INTO t (a) VALUES (1), (2), (3);
DELETE FROM t WHERE id = 3;
`)
	applyPlan(t, conn, `CREATE TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT, a TEXT);`)

	// DROP TABLE deletes the row of t in sqlite_sequence; the rebuild keeps
	// the count, so 3 is not used again.
	_, err := conn.Exec(`INSERT INTO t (a) VALUES ('4')`)
	require.NoError(t, err)
	var id int
	require.NoError(t, conn.QueryRow(`SELECT max(id) FROM t`).Scan(&id))
	assert.Equal(t, 4, id)
	var n int
	require.NoError(t, conn.QueryRow(`SELECT count(*) FROM sqlite_sequence`).Scan(&n))
	assert.Equal(t, 1, n)
}

func TestNoRebuild(t *testing.T) {
	conn := openDB(t, `CREATE TABLE t (a);`)
	plan := applyPlan(t, conn, `CREATE TABLE t (a, b);`)
	assert.Empty(t, plan.Rebuilds)
}

func TestIsConstant(t *testing.T) {
	tests := []struct {
		dflt string
		want bool
	}{
		{"0", true},
		{"-1", true},
		{"((+1.5))", true},
		{"'a'", true},
		{"x'00'", true},
		{"NULL", true},
		{"TRUE", true},
		{"abc", true},
		{`"x"`, true},
		{"(1)", true},
		{"(1 + 1)", false},
		{"- -1", false},
		{"CURRENT_TIMESTAMP", false},
		{"current_date", false},
		{"(abs(-1))", false},
		{"?", false},
	}
	for _, tt := range tests {
		t.Run(tt.dflt, func(t *testing.T) {
			assert.Equal(t, tt.want, diff.IsConstant(tt.dflt))
		})
	}
}

func TestRebuildKeepsRowids(t *testing.T) {
	conn := openDB(t, `
CREATE TABLE notes (body TEXT, n INT);
CREATE VIRTUAL TABLE notes_fts USING fts5(body, content='notes', content_rowid='rowid');
INSERT INTO notes VALUES ('apple', 1), ('banana', 2), ('cherry', 3);
DELETE FROM notes WHERE rowid = 1;
INSERT INTO notes_fts(notes_fts) VALUES ('rebuild');
`)
	applyPlan(t, conn, `
CREATE TABLE notes (body TEXT, n TEXT);
CREATE VIRTUAL TABLE notes_fts USING fts5(body, content='notes', content_rowid='rowid');
`)

	// The gap in the rowids is kept, so the FTS index still finds the rows.
	var rowid int
	var body string
	require.NoError(t, conn.QueryRow(`SELECT rowid, body FROM notes WHERE body = 'cherry'`).Scan(&rowid, &body))
	assert.Equal(t, 3, rowid)
	require.NoError(t, conn.QueryRow(`SELECT notes.body FROM notes_fts JOIN notes ON notes.rowid = notes_fts.rowid WHERE notes_fts MATCH 'cherry'`).Scan(&body))
	assert.Equal(t, "cherry", body)
}

func TestRebuildRowidColumn(t *testing.T) {
	// A column named rowid hides the rowid, so _rowid_ copies it.
	conn := openDB(t, `
CREATE TABLE t ("rowid" TEXT, a INT);
INSERT INTO t VALUES ('x', 1), ('y', 2);
DELETE FROM t WHERE a = 1;
`)
	plan := applyPlan(t, conn, `CREATE TABLE t ("rowid" TEXT, a TEXT);`)
	// The column named rowid is written without quotes, which names the
	// column, as it hides the rowid.
	assert.Contains(t, plan.Statements(), `INSERT INTO new_t (_rowid_, rowid, a) SELECT _rowid_, rowid, a FROM t;`)
	var rowid int
	var v string
	require.NoError(t, conn.QueryRow(`SELECT _rowid_, "rowid" FROM t`).Scan(&rowid, &v))
	assert.Equal(t, 2, rowid)
	assert.Equal(t, "y", v)
}

func TestRebuildAllRowidNamesTaken(t *testing.T) {
	// With columns under all three names, the rowid cannot be read, and only
	// the columns are copied.
	conn := openDB(t, `CREATE TABLE t ("rowid", "_rowid_", "oid", a INT);`)
	plan := applyPlan(t, conn, `CREATE TABLE t ("rowid", "_rowid_", "oid", a TEXT);`)
	assert.Contains(t, plan.Statements(), `INSERT INTO new_t (rowid, _rowid_, oid, a) SELECT rowid, _rowid_, oid, a FROM t;`)
}

func TestRebuildToRowidAlias(t *testing.T) {
	// id becomes the INTEGER PRIMARY KEY, so copying it sets the rowid, and
	// the old rowid is not copied.
	conn := openDB(t, `
CREATE TABLE t (id INT PRIMARY KEY, a);
INSERT INTO t VALUES (10, 'x'), (20, 'y');
`)
	plan := applyPlan(t, conn, `CREATE TABLE t (id INTEGER PRIMARY KEY, a);`)
	assert.Contains(t, plan.Statements(), `INSERT INTO new_t (id, a) SELECT id, a FROM t;`)
	var rowid int
	require.NoError(t, conn.QueryRow(`SELECT rowid FROM t WHERE a = 'y'`).Scan(&rowid))
	assert.Equal(t, 20, rowid)
}

func TestRebuildWithoutRowid(t *testing.T) {
	conn := openDB(t, `CREATE TABLE t (k TEXT PRIMARY KEY, a INT);`)
	plan := applyPlan(t, conn, `CREATE TABLE t (k TEXT PRIMARY KEY, a INT) WITHOUT ROWID;`)
	assert.Contains(t, plan.Statements(), `INSERT INTO new_t (k, a) SELECT k, a FROM t;`)
}

func TestDiffInvalidDesired(t *testing.T) {
	// The desired schema is compiled in memory to compare expressions, so a
	// schema SQLite rejects is an error. desired.Load never gives one; this
	// one is built by hand.
	des := model.NewSchema()
	des.Tables.Set("t", &model.Table{Name: "t", SQL: "CREATE TABLE t (", Columns: orderedmap.New[string, *model.Column]()})
	_, err := diff.Diff(model.NewSchema(), des)
	assert.ErrorContains(t, err, "CREATE TABLE t (")
}
