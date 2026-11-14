package catalog_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/winebarrel/hazel/catalog"
	"github.com/winebarrel/hazel/db"
)

func readSchema(t *testing.T, stmts ...string) *catalog.Schema {
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

func names(m interface{ CollectKeys() []string }) []string {
	return m.CollectKeys()
}

func TestRead(t *testing.T) {
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
	assert.Equal(t, []string{"users", "posts"}, names(schema.Tables))
	assert.Equal(t, []string{"posts_user_id"}, names(schema.Indexes))
	assert.Equal(t, []string{"v"}, names(schema.Views))
	assert.Equal(t, []string{"tr"}, names(schema.Triggers))

	users := schema.Tables.Get("users")
	assert.Equal(t, &catalog.Object{
		Type:    catalog.Table,
		Name:    "Users",
		TblName: "Users",
		SQL:     `CREATE TABLE "Users" (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT UNIQUE)`,
	}, users)

	idx := schema.Indexes.Get("posts_user_id")
	assert.Equal(t, catalog.Index, idx.Type)
	assert.Equal(t, "posts", idx.TblName)

	tr := schema.Triggers.Get("tr")
	assert.Equal(t, catalog.Trigger, tr.Type)
	assert.Equal(t, "posts", tr.TblName)
	assert.Equal(t, "CREATE TRIGGER tr AFTER INSERT ON posts BEGIN SELECT 1; END", tr.SQL)

	assert.Equal(t, catalog.View, schema.Views.Get("v").Type)
}

func TestReadVirtualTable(t *testing.T) {
	schema := readSchema(t,
		`CREATE VIRTUAL TABLE docs USING fts5(title, body)`,
		`CREATE VIRTUAL TABLE boxes USING rtree(id, minx, maxx)`,
	)

	// The shadow tables (docs_data, docs_idx, boxes_node, ...) are left out.
	assert.Equal(t, []string{"docs", "boxes"}, names(schema.Tables))
	assert.True(t, schema.Tables.Get("docs").Virtual)
	assert.True(t, schema.Tables.Get("boxes").Virtual)
	assert.Equal(t, 0, schema.Indexes.Len())
}

func TestReadOrder(t *testing.T) {
	schema := readSchema(t,
		`CREATE TABLE b (x)`,
		`CREATE TABLE a (x)`,
		`CREATE VIEW vb AS SELECT * FROM b`,
		`CREATE VIEW va AS SELECT * FROM vb`,
	)
	// The order is the order of creation, which keeps a view after the view
	// it reads.
	assert.Equal(t, []string{"b", "a"}, names(schema.Tables))
	assert.Equal(t, []string{"vb", "va"}, names(schema.Views))
}

func TestReadEmpty(t *testing.T) {
	schema := readSchema(t)
	assert.Equal(t, 0, schema.Tables.Len())
	assert.Equal(t, 0, schema.Indexes.Len())
	assert.Equal(t, 0, schema.Views.Len())
	assert.Equal(t, 0, schema.Triggers.Len())
}

func TestKey(t *testing.T) {
	assert.Equal(t, "users", catalog.Key("UsErS"))
	// Only ASCII is folded, and other bytes are kept as they are.
	assert.Equal(t, "\u00c4b", catalog.Key("\u00c4B"))
	assert.Equal(t, "\xffa", catalog.Key("\xffA"))
}

func TestReadClosed(t *testing.T) {
	conn, err := db.OpenReadWrite(db.Memory, time.Second)
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	_, err = catalog.Read(context.Background(), conn)
	assert.Error(t, err)
}

func TestReadReservedNames(t *testing.T) {
	// SQLite refuses to create names beginning with "sqlite_", but a table
	// named like it without the underscore is an ordinary table.
	schema := readSchema(t, `CREATE TABLE sqlitex (a)`, `CREATE TABLE "SQLite" (a)`)
	assert.Equal(t, []string{"sqlitex", "sqlite"}, names(schema.Tables))
}

func TestReadIndexOnTableNamedInOtherCase(t *testing.T) {
	schema := readSchema(t,
		`CREATE TABLE Posts (a)`,
		`CREATE INDEX posts_a ON POSTS (a)`,
		`CREATE VIRTUAL TABLE Docs USING fts5(body)`,
	)
	assert.Equal(t, []string{"posts_a"}, names(schema.Indexes))
	// SQLite stores the name of the table as it was created, not as the
	// CREATE INDEX wrote it.
	assert.Equal(t, "Posts", schema.Indexes.Get("posts_a").TblName)
	assert.True(t, schema.Tables.Get("docs").Virtual)
}
