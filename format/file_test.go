package format_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/winebarrel/hazel/format"
)

func TestFile(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		strip bool
		want  string
	}{
		{
			name: "statements, comments and directives",
			in: `-- users
-- hazel:renamed-from members
CREATE TABLE "users" (id INTEGER PRIMARY KEY,name TEXT); -- the body
CREATE INDEX users_name ON users (name);


-- hazel:execute SELECT NOT EXISTS (SELECT 1 FROM settings)
INSERT INTO   settings VALUES (1);
`,
			want: `-- users
-- hazel:renamed-from members
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT
); -- the body

CREATE INDEX users_name ON users (name);

-- hazel:execute SELECT NOT EXISTS (SELECT 1 FROM settings)
INSERT INTO   settings VALUES (1);
`,
		},
		{
			name: "blank lines between comments shrink to one",
			in:   "  -- section\n\n\n  /* about t */\nCREATE TABLE t (a);\n\n\n-- the end\n\n",
			want: "-- section\n\n/* about t */\nCREATE TABLE t (\n    a\n);\n\n-- the end\n",
		},
		{
			name: "a statement without a semicolon gets one",
			in:   "CREATE TABLE t (a)",
			want: "CREATE TABLE t (\n    a\n);\n",
		},
		{
			name: "a comment before the semicolon",
			in:   "CREATE VIEW v AS SELECT 1 -- one\n;",
			want: "CREATE VIEW v AS SELECT 1 -- one\n;\n",
		},
		{
			name: "a statement hazel does not manage is left as it is",
			in:   "PRAGMA  foreign_keys = ON;\nINSERT INTO t\n  VALUES (1)",
			want: "PRAGMA  foreign_keys = ON;\n\nINSERT INTO t\n  VALUES (1)\n",
		},
		{
			name: "an execute statement is left as it is",
			in:   "-- hazel:execute\nCREATE TABLE \"x\" (a,b);",
			want: "-- hazel:execute\nCREATE TABLE \"x\" (a,b);\n",
		},
		{
			name: "an empty statement is dropped",
			in:   "CREATE TABLE t (a);;\n;",
			want: "CREATE TABLE t (\n    a\n);\n",
		},
		{
			name: "an empty file",
			in:   "\n\n",
			want: "",
		},
		{
			name:  "--strip-renamed-from",
			strip: true,
			in: `-- users
-- hazel:renamed-from members
CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  -- hazel:renamed-from full_name
  name TEXT
);
`,
			want: `-- users
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT
);
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := format.File(tt.in, tt.strip)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, got, format.File(got, false), "formatting again changes it")
		})
	}
}

func TestFileDoesNotTokenize(t *testing.T) {
	assert.Equal(t, "CREATE TABLE t (a DEFAULT 'x);", format.File("CREATE TABLE t (a DEFAULT 'x);", false))
}
