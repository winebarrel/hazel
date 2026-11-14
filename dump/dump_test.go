package dump_test

import (
	"context"
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
	"github.com/winebarrel/hazel/dump"
	"github.com/winebarrel/hazel/model"
	"gopkg.in/yaml.v3"
)

// dumpTestCase is one fixture in testdata. init is run on an empty database,
// and dump is the expected dump of it. The dump, and the dump without
// formatting, are loaded as the schema files and must plan clean against the
// database.
type dumpTestCase struct {
	Init string `yaml:"init"`
	Dump string `yaml:"dump"`
}

func TestDump(t *testing.T) {
	files, err := filepath.Glob("testdata/*.yml")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		t.Run(strings.TrimSuffix(filepath.Base(file), ".yml"), func(t *testing.T) {
			b, err := os.ReadFile(file)
			require.NoError(t, err)
			var tc dumpTestCase
			require.NoError(t, yaml.Unmarshal(b, &tc))

			current := read(t, tc.Init)
			got := dump.String(current, dump.Options{})
			assert.Equal(t, tc.Dump, got)

			for _, out := range []string{got, dump.String(current, dump.Options{NoFormat: true})} {
				result, err := desired.Load(context.Background(), []desired.Source{{Name: "dump.sql", Body: out}})
				require.NoError(t, err, out)
				plan, err := diff.Diff(current, result.Schema)
				require.NoError(t, err)
				assert.True(t, plan.IsEmpty(), "plan of the dump:\n%s\ndump:\n%s", plan, out)
			}
		})
	}
}

func read(t *testing.T, init string) *model.Schema {
	t.Helper()
	conn, err := db.OpenReadWrite(db.Memory, time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	_, err = conn.Exec(init)
	require.NoError(t, err)
	s, err := catalog.Read(context.Background(), conn)
	require.NoError(t, err)
	return s
}

func TestDumpNoFormat(t *testing.T) {
	s := read(t, `
CREATE TABLE "t" (a,b);
CREATE INDEX "t_a" ON t (a);
`)
	assert.Equal(t, "CREATE TABLE \"t\" (a,b);\n\nCREATE INDEX \"t_a\" ON t (a);\n", dump.String(s, dump.Options{NoFormat: true}))
}

func TestDumpExclude(t *testing.T) {
	s := read(t, `
CREATE TABLE t (a);
CREATE INDEX t_a ON t (a);
CREATE TRIGGER t_tr AFTER INSERT ON t BEGIN SELECT 1; END;
CREATE TABLE u (a);
CREATE VIEW v AS SELECT a FROM t;
CREATE TRIGGER v_tr INSTEAD OF INSERT ON v BEGIN SELECT 1; END;
`)
	exclude := func(name string) bool { return name != "u" }
	assert.Equal(t, "CREATE TABLE u (\n    a\n);\n", dump.String(s, dump.Options{Exclude: exclude}))
	assert.Empty(t, dump.String(model.NewSchema(), dump.Options{}))
}

// upperE and lowerE are E with an acute accent, which SQLite takes as two
// names, since it folds only ASCII.
const (
	upperE = "\u00c9"
	lowerE = "\u00e9"
)

func TestFiles(t *testing.T) {
	s := read(t, `
CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);
CREATE INDEX users_name ON users (name);
CREATE VIEW v AS SELECT name FROM users;
CREATE TRIGGER v_tr INSTEAD OF INSERT ON v BEGIN SELECT 1; END;
CREATE TABLE "`+upperE+`" (a);
CREATE TABLE "`+lowerE+`" (a);
CREATE TABLE "`+lowerE+`_2" (a);
`)
	files, err := dump.Files(s, dump.Options{})
	require.NoError(t, err)
	assert.Equal(t, []dump.File{
		{Name: "users.sql", Body: "CREATE TABLE users (\n    id INTEGER PRIMARY KEY,\n    name TEXT\n);\n\nCREATE INDEX users_name ON users (name);\n"},
		{Name: upperE + ".sql", Body: "CREATE TABLE \"" + upperE + "\" (\n    a\n);\n"},
		// Names that differ in case only would be one file on a file system
		// that ignores case.
		{Name: lowerE + "_2.sql", Body: "CREATE TABLE \"" + lowerE + "\" (\n    a\n);\n"},
		{Name: lowerE + "_2_2.sql", Body: "CREATE TABLE \"" + lowerE + "_2\" (\n    a\n);\n"},
		{Name: "v.sql", Body: "CREATE VIEW v AS SELECT name FROM users;\n\nCREATE TRIGGER v_tr INSTEAD OF INSERT ON v BEGIN SELECT 1; END;\n"},
	}, files)
}

func TestFilesUnsafeName(t *testing.T) {
	for _, name := range []string{"a/b", `a\b`} {
		t.Run(name, func(t *testing.T) {
			s := read(t, `CREATE TABLE "`+name+`" (a);`)
			_, err := dump.Files(s, dump.Options{})
			assert.EqualError(t, err, `cannot write a file for "`+name+`": the name is not a plain file name`)
		})
	}
}
