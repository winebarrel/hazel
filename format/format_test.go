package format_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/winebarrel/hazel/format"
)

func TestStatement(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "one element per line",
			in:   `CREATE TABLE t (id INTEGER PRIMARY KEY, a TEXT NOT NULL)`,
			want: "CREATE TABLE t (\n    id INTEGER PRIMARY KEY,\n    a TEXT NOT NULL\n)",
		},
		{
			name: "no space before the parenthesis stays so",
			in:   `CREATE TABLE t(a,b)`,
			want: "CREATE TABLE t(\n    a,\n    b\n)",
		},
		{
			name: "runs of whitespace become one space",
			in:   "CREATE  TABLE\tt (\n  a    INTEGER   NOT NULL,\n\n\n  b\n)",
			want: "CREATE TABLE t (\n    a INTEGER NOT NULL,\n    b\n)",
		},
		{
			name: "case is kept",
			in:   `create table T (A integer primary key, b Text)`,
			want: "create table T (\n    A integer primary key,\n    b Text\n)",
		},
		{
			name: "table options",
			in:   `CREATE TABLE t (a TEXT PRIMARY KEY)   WITHOUT ROWID,  STRICT`,
			want: "CREATE TABLE t (\n    a TEXT PRIMARY KEY\n) WITHOUT ROWID, STRICT",
		},
		{
			name: "a multi-line element is indented by the parenthesis depth",
			in: `CREATE TABLE t (
a INTEGER,
CONSTRAINT c CHECK (
a > 0
AND (
a < 10
)
)
)`,
			want: `CREATE TABLE t (
    a INTEGER,
    CONSTRAINT c CHECK (
        a > 0
        AND (
            a < 10
        )
    )
)`,
		},
		{
			name: "a comment on the line of the element stays there",
			in: `CREATE TABLE users (id INTEGER PRIMARY KEY,email TEXT NOT NULL,  -- login
  status TEXT  /* active or closed */
)`,
			want: `CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    email TEXT NOT NULL, -- login
    status TEXT /* active or closed */
)`,
		},
		{
			name: "a comment on a line of its own goes before its element",
			in: `CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  -- NULL after the user leaves
  /* a timestamp */
  deleted_at TEXT
  -- after the last element
)`,
			want: `CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    -- NULL after the user leaves
    /* a timestamp */
    deleted_at TEXT
    -- after the last element
)`,
		},
		{
			name: "a leading comma goes to the element before it",
			in: `CREATE TABLE t (
  a INTEGER -- first
  -- about b
  , b TEXT
)`,
			want: `CREATE TABLE t (
    a INTEGER, -- first
    -- about b
    b TEXT
)`,
		},
		{
			name: "two comments after an element",
			in: `CREATE TABLE t (
  a INTEGER, /* x */ -- y
  b TEXT -- z
)`,
			want: `CREATE TABLE t (
    a INTEGER, /* x */ -- y
    b TEXT -- z
)`,
		},
		{
			name: "a comment in the header",
			in:   "CREATE TABLE t -- the table\n(a)",
			want: "CREATE TABLE t -- the table\n(\n    a\n)",
		},
		{
			name: "a comment on the line of the opening parenthesis",
			in:   "CREATE TABLE t ( -- the table\n  a\n)",
			want: "CREATE TABLE t ( -- the table\n    a\n)",
		},
		{
			name: "IF NOT EXISTS and a schema name",
			in:   `CREATE TABLE IF NOT EXISTS main."t" ("a")`,
			want: "CREATE TABLE IF NOT EXISTS main.t (\n    a\n)",
		},
		{
			name: "CREATE TABLE AS keeps its layout",
			in:   `CREATE TABLE "t" AS SELECT  "a" FROM (SELECT 1 AS a)`,
			want: `CREATE TABLE t AS SELECT  "a" FROM (SELECT 1 AS a)`,
		},
		{
			name: "a statement that does not tokenize is left alone",
			in:   `CREATE TABLE t (a DEFAULT 'x)`,
			want: `CREATE TABLE t (a DEFAULT 'x)`,
		},
		{
			name: "names lose the quotes they do not need",
			in:   `CREATE TABLE "users" ("id" INTEGER PRIMARY KEY, [name] TEXT, "order" INT, "e mail" TEXT, CONSTRAINT "pk" UNIQUE ("name" COLLATE NOCASE, "e mail"), FOREIGN KEY ("id") REFERENCES "teams" ("id"), "g" INT CONSTRAINT "fk" REFERENCES "teams")`,
			want: `CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT,
    "order" INT,
    "e mail" TEXT,
    CONSTRAINT pk UNIQUE (name COLLATE NOCASE, "e mail"),
    FOREIGN KEY (id) REFERENCES teams (id),
    g INT CONSTRAINT fk REFERENCES teams
)`,
		},
		{
			name: "expressions keep their quotes",
			in:   `CREATE TABLE t ("s" TEXT DEFAULT "x" CHECK ("s" IN ("active", "closed")) COLLATE "NOCASE")`,
			want: "CREATE TABLE t (\n    s TEXT DEFAULT \"x\" CHECK (\"s\" IN (\"active\", \"closed\")) COLLATE \"NOCASE\"\n)",
		},
		{
			name: "an index keeps its layout",
			in:   `CREATE UNIQUE INDEX "t_a"  ON "t" ("a" DESC) WHERE "a" > 0`,
			want: `CREATE UNIQUE INDEX t_a  ON t ("a" DESC) WHERE "a" > 0`,
		},
		{
			name: "a view keeps its body",
			in:   "CREATE VIEW \"v\" (\"x\", \"y\") AS\n  SELECT \"a\",  b FROM \"t\"",
			want: "CREATE VIEW v (x, y) AS\n  SELECT \"a\",  b FROM \"t\"",
		},
		{
			name: "a trigger keeps its body",
			in:   "CREATE TRIGGER \"tr\" AFTER UPDATE OF \"a\" ON \"t\" BEGIN\n  UPDATE \"t\" SET b = 1;\nEND",
			want: "CREATE TRIGGER tr AFTER UPDATE OF \"a\" ON t BEGIN\n  UPDATE \"t\" SET b = 1;\nEND",
		},
		{
			name: "a virtual table keeps its arguments",
			in:   `CREATE VIRTUAL TABLE "docs" USING fts5("title", body)`,
			want: `CREATE VIRTUAL TABLE docs USING fts5("title", body)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := format.Statement(tt.in)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, got, format.Statement(got), "formatting again changes it")
		})
	}
}
