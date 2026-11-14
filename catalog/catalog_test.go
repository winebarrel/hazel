package catalog_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/winebarrel/hazel/catalog"
	"github.com/winebarrel/hazel/db"
	"github.com/winebarrel/hazel/model"
)

func readSchema(t *testing.T, stmts ...string) *model.Schema {
	t.Helper()
	conn, err := db.OpenReadWrite(db.Memory, time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	for _, s := range stmts {
		_, err := conn.Exec(s)
		require.NoError(t, err, s)
	}
	schema, err := catalog.Read(context.Background(), conn)
	require.NoError(t, err)
	return schema
}

func TestReadObjects(t *testing.T) {
	schema := readSchema(t,
		`CREATE TABLE "Users" (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT UNIQUE)`,
		`CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INT)`,
		`CREATE INDEX posts_user_id ON posts (user_id)`,
		`CREATE VIEW v AS SELECT * FROM posts`,
		`CREATE TRIGGER tr AFTER INSERT ON posts BEGIN SELECT 1; END`,
		`INSERT INTO Users (email) VALUES ('a')`, // creates sqlite_sequence
		`ANALYZE`,                                // creates sqlite_stat1
	)

	// The keys are lowercase; the names are kept as written. sqlite_sequence,
	// sqlite_stat1 and the automatic index of UNIQUE are left out.
	assert.Equal(t, []string{"users", "posts"}, schema.Tables.CollectKeys())
	assert.Equal(t, []string{"posts_user_id"}, schema.Indexes.CollectKeys())
	assert.Equal(t, []string{"v"}, schema.Views.CollectKeys())
	assert.Equal(t, []string{"tr"}, schema.Triggers.CollectKeys())

	users := schema.Tables.Get("users")
	assert.Equal(t, "Users", users.Name)
	assert.Equal(t, `CREATE TABLE "Users" (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT UNIQUE)`, users.SQL)

	assert.Equal(t, &model.Trigger{
		Name:  "tr",
		Table: "posts",
		SQL:   "CREATE TRIGGER tr AFTER INSERT ON posts BEGIN SELECT 1; END",
	}, schema.Triggers.Get("tr"))
}

func TestReadColumns(t *testing.T) {
	schema := readSchema(t,
		`CREATE TABLE t (
			id INTEGER PRIMARY KEY,
			a int NOT NULL,
			b VarChar( 10 ) DEFAULT ( 'x' ),
			c,
			d INT GENERATED ALWAYS AS (id * 2) VIRTUAL,
			e INT AS (id) STORED
		)`,
	)
	tbl := schema.Tables.Get("t")
	assert.False(t, tbl.Strict)
	assert.False(t, tbl.WithoutRowid)
	assert.False(t, tbl.Virtual)
	// Standard type names come back in uppercase, others as written. The
	// outer parentheses of a DEFAULT are dropped by SQLite.
	assert.Equal(t, []*model.Column{
		{Name: "id", Type: "INTEGER"},
		{Name: "a", Type: "INT", NotNull: true},
		{Name: "b", Type: "VarChar( 10 )", Default: new("'x'")},
		{Name: "c"},
		{Name: "d", Type: "INT", Generated: model.GeneratedVirtual},
		{Name: "e", Type: "INT", Generated: model.GeneratedStored},
	}, tbl.Columns.CollectValues())
	assert.Equal(t, []*model.IndexColumn{{Name: "id", Collation: "BINARY"}}, tbl.PrimaryKey)
	assert.Empty(t, tbl.ForeignKeys)
	assert.Empty(t, tbl.Uniques)
}

func TestReadPrimaryKey(t *testing.T) {
	schema := readSchema(t,
		`CREATE TABLE none (a)`,
		`CREATE TABLE rowid (a, b, PRIMARY KEY (a DESC))`,
		`CREATE TABLE text_pk (a TEXT PRIMARY KEY)`,
		`CREATE TABLE kv (k TEXT, v TEXT COLLATE NOCASE, u UNIQUE, PRIMARY KEY (k DESC, v)) WITHOUT ROWID`,
		`CREATE TABLE s (a INT PRIMARY KEY) STRICT`,
	)
	assert.Empty(t, schema.Tables.Get("none").PrimaryKey)
	// a has no declared type, so the key is not the rowid and has an index.
	assert.Equal(t, []*model.IndexColumn{{Name: "a", Desc: true, Collation: "BINARY"}},
		schema.Tables.Get("rowid").PrimaryKey)
	assert.Equal(t, []*model.IndexColumn{{Name: "a", Collation: "BINARY"}},
		schema.Tables.Get("text_pk").PrimaryKey)

	kv := schema.Tables.Get("kv")
	assert.True(t, kv.WithoutRowid)
	assert.Equal(t, []*model.IndexColumn{
		{Name: "k", Desc: true, Collation: "BINARY"},
		{Name: "v", Collation: "NOCASE"},
	}, kv.PrimaryKey)
	// The PRIMARY KEY of a WITHOUT ROWID table implies NOT NULL.
	assert.True(t, kv.Columns.Get("k").NotNull)
	assert.Equal(t, []*model.Unique{{Columns: []*model.IndexColumn{{Name: "u", Collation: "BINARY"}}}}, kv.Uniques)

	s := schema.Tables.Get("s")
	assert.True(t, s.Strict)
	assert.Equal(t, []*model.IndexColumn{{Name: "a", Collation: "BINARY"}}, s.PrimaryKey)
}

func TestReadForeignKeys(t *testing.T) {
	schema := readSchema(t,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, org INT, UNIQUE (id, org))`,
		`CREATE TABLE posts (
			id INTEGER PRIMARY KEY,
			user_id INT REFERENCES users ON DELETE CASCADE,
			editor_id INT,
			org INT,
			FOREIGN KEY (editor_id) REFERENCES Users (id) ON UPDATE SET NULL,
			FOREIGN KEY (user_id, org) REFERENCES users (id, org) MATCH FULL
		)`,
	)
	// The constraints come in the order they are written in. A reference
	// without parent columns has "" there. MATCH is not kept by SQLite.
	assert.Equal(t, []*model.ForeignKey{
		{
			Columns: []string{"user_id"}, RefTable: "users", RefColumns: []string{""},
			OnUpdate: "NO ACTION", OnDelete: "CASCADE",
		},
		{
			Columns: []string{"editor_id"}, RefTable: "Users", RefColumns: []string{"id"},
			OnUpdate: "SET NULL", OnDelete: "NO ACTION",
		},
		{
			Columns: []string{"user_id", "org"}, RefTable: "users", RefColumns: []string{"id", "org"},
			OnUpdate: "NO ACTION", OnDelete: "NO ACTION",
		},
	}, schema.Tables.Get("posts").ForeignKeys)
}

func TestReadUniques(t *testing.T) {
	schema := readSchema(t,
		`CREATE TABLE t (
			a TEXT UNIQUE COLLATE NOCASE,
			b, c,
			UNIQUE (b, c DESC),
			UNIQUE (c)
		)`,
		`CREATE INDEX t_b ON t (b)`,
	)
	// The constraints come in the order they are written in. A CREATE INDEX
	// is not a constraint. The collation of a column shows in its UNIQUE.
	assert.Equal(t, []*model.Unique{
		{Columns: []*model.IndexColumn{{Name: "a", Collation: "NOCASE"}}},
		{Columns: []*model.IndexColumn{{Name: "b", Collation: "BINARY"}, {Name: "c", Desc: true, Collation: "BINARY"}}},
		{Columns: []*model.IndexColumn{{Name: "c", Collation: "BINARY"}}},
	}, schema.Tables.Get("t").Uniques)
}

func TestReadUniquesOnConflictReplace(t *testing.T) {
	// PRAGMA index_list moves an ON CONFLICT REPLACE index to the end; the
	// written order is kept anyway.
	schema := readSchema(t,
		`CREATE TABLE t (a, b UNIQUE, c, UNIQUE (c), UNIQUE (a) ON CONFLICT REPLACE, UNIQUE (a, c))`,
	)
	var got [][]string
	for _, u := range schema.Tables.Get("t").Uniques {
		var cols []string
		for _, c := range u.Columns {
			cols = append(cols, c.Name)
		}
		got = append(got, cols)
	}
	assert.Equal(t, [][]string{{"b"}, {"c"}, {"a"}, {"a", "c"}}, got)
}

func TestReadRowidAliasDesc(t *testing.T) {
	schema := readSchema(t, `CREATE TABLE t (x INTEGER, PRIMARY KEY (x DESC))`)
	// x is a rowid alias and has no index, so the DESC is not reported.
	assert.Equal(t, []*model.IndexColumn{{Name: "x", Collation: "BINARY"}}, schema.Tables.Get("t").PrimaryKey)
}

func TestReadIndexes(t *testing.T) {
	schema := readSchema(t,
		`CREATE TABLE Posts (id INTEGER PRIMARY KEY, title TEXT, user_id INT)`,
		`CREATE UNIQUE INDEX posts_title ON POSTS (title COLLATE NOCASE)`,
		`CREATE INDEX posts_expr ON posts (lower(title), user_id DESC) WHERE user_id > 0`,
	)
	// SQLite stores the name of the table as it was created, not as the
	// CREATE INDEX wrote it.
	assert.Equal(t, &model.Index{
		Name:    "posts_title",
		Table:   "Posts",
		SQL:     "CREATE UNIQUE INDEX posts_title ON POSTS (title COLLATE NOCASE)",
		Unique:  true,
		Columns: []*model.IndexColumn{{Name: "title", Collation: "NOCASE"}},
	}, schema.Indexes.Get("posts_title"))

	// An expression has no name.
	expr := schema.Indexes.Get("posts_expr")
	assert.False(t, expr.Unique)
	assert.True(t, expr.Partial)
	assert.Equal(t, []*model.IndexColumn{
		{Name: "", Collation: "BINARY"},
		{Name: "user_id", Desc: true, Collation: "BINARY"},
	}, expr.Columns)
}

func TestReadViews(t *testing.T) {
	schema := readSchema(t,
		`CREATE TABLE t (a, b)`,
		`CREATE VIEW v AS SELECT a AS x, b FROM t`,
		`CREATE VIEW w AS SELECT * FROM v`,
	)
	assert.Equal(t, &model.View{Name: "v", SQL: "CREATE VIEW v AS SELECT a AS x, b FROM t", Columns: []string{"x", "b"}},
		schema.Views.Get("v"))
	// The order is the order of creation, which keeps a view after the view
	// it reads.
	assert.Equal(t, []string{"v", "w"}, schema.Views.CollectKeys())
	assert.Equal(t, []string{"x", "b"}, schema.Views.Get("w").Columns)
}

func TestReadVirtualTable(t *testing.T) {
	schema := readSchema(t,
		`CREATE VIRTUAL TABLE Docs USING fts5(title, body)`,
		`CREATE VIRTUAL TABLE boxes USING rtree(id, minx, maxx)`,
	)
	// The shadow tables (Docs_data, Docs_idx, boxes_node, ...) are left out,
	// and the columns of a virtual table are not read.
	assert.Equal(t, []string{"docs", "boxes"}, schema.Tables.CollectKeys())
	docs := schema.Tables.Get("docs")
	assert.True(t, docs.Virtual)
	assert.Equal(t, 0, docs.Columns.Len())
	assert.True(t, schema.Tables.Get("boxes").Virtual)
	assert.Equal(t, 0, schema.Indexes.Len())
}

func TestReadReservedNames(t *testing.T) {
	// SQLite refuses to create names beginning with "sqlite_", but a name
	// like it without the underscore is an ordinary table.
	schema := readSchema(t, `CREATE TABLE sqlitex (a)`, `CREATE TABLE "SQLite" (a)`)
	assert.Equal(t, []string{"sqlitex", "sqlite"}, schema.Tables.CollectKeys())
}

func TestReadEmpty(t *testing.T) {
	schema := readSchema(t)
	assert.Equal(t, 0, schema.Tables.Len())
	assert.Equal(t, 0, schema.Indexes.Len())
	assert.Equal(t, 0, schema.Views.Len())
	assert.Equal(t, 0, schema.Triggers.Len())
}

func TestReadViewOfDroppedTable(t *testing.T) {
	conn, err := db.OpenReadWrite(db.Memory, time.Second)
	require.NoError(t, err)
	defer conn.Close()
	for _, s := range []string{
		`CREATE TABLE base (x)`,
		`CREATE VIEW v AS SELECT x FROM base`,
		`DROP TABLE base`,
	} {
		_, err := conn.Exec(s)
		require.NoError(t, err)
	}

	// SQLite does not check the views a DROP TABLE breaks. The error names
	// the view.
	_, err = catalog.Read(context.Background(), conn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "view v: ")
	assert.Contains(t, err.Error(), "no such table: main.base")
}

func TestReadClosed(t *testing.T) {
	conn, err := db.OpenReadWrite(db.Memory, time.Second)
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	_, err = catalog.Read(context.Background(), conn)
	assert.Error(t, err)
}
