package parser_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/winebarrel/hazel/parser"
)

// splitTexts returns the source text of each statement, trimmed.
func splitTexts(t *testing.T, src string) []string {
	t.Helper()
	tokens, err := parser.Tokenize(src)
	require.NoError(t, err)
	var out []string
	for _, s := range parser.Split(tokens) {
		var b strings.Builder
		for _, x := range s.Tokens {
			b.WriteString(x.Text)
		}
		out = append(out, strings.TrimSpace(b.String()))
	}
	return out
}

func TestSplit(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "statements",
			src:  "CREATE TABLE a (x);\nCREATE TABLE b (y);",
			want: []string{"CREATE TABLE a (x);", "CREATE TABLE b (y);"},
		},
		{
			name: "no trailing semicolon",
			src:  "CREATE TABLE a (x);\nCREATE TABLE b (y)",
			want: []string{"CREATE TABLE a (x);", "CREATE TABLE b (y)"},
		},
		{
			name: "semicolon in string and comment",
			src:  "CREATE TABLE a (x DEFAULT ';'); -- ;\nCREATE TABLE b (y);",
			want: []string{"CREATE TABLE a (x DEFAULT ';');", "-- ;\nCREATE TABLE b (y);"},
		},
		{
			name: "trigger body",
			src:  "CREATE TRIGGER tr AFTER INSERT ON a BEGIN INSERT INTO b VALUES (1); DELETE FROM c; END;\nCREATE TABLE d (z);",
			want: []string{
				"CREATE TRIGGER tr AFTER INSERT ON a BEGIN INSERT INTO b VALUES (1); DELETE FROM c; END;",
				"CREATE TABLE d (z);",
			},
		},
		{
			name: "temp trigger",
			src:  "create temp trigger tr insert on a begin select 1; end; select 2;",
			want: []string{"create temp trigger tr insert on a begin select 1; end;", "select 2;"},
		},
		{
			name: "case in trigger body and when",
			src: "CREATE TRIGGER tr AFTER UPDATE ON a WHEN CASE WHEN new.x THEN 1 END BEGIN " +
				"SELECT CASE WHEN new.x THEN 1 ELSE 2 END; SELECT CASE CASE 1 WHEN 1 THEN 2 END WHEN 2 THEN 3 END; END;\nSELECT 1;",
			want: []string{
				"CREATE TRIGGER tr AFTER UPDATE ON a WHEN CASE WHEN new.x THEN 1 END BEGIN " +
					"SELECT CASE WHEN new.x THEN 1 ELSE 2 END; SELECT CASE CASE 1 WHEN 1 THEN 2 END WHEN 2 THEN 3 END; END;",
				"SELECT 1;",
			},
		},
		{
			name: "end as an identifier in trigger body",
			src: "CREATE TRIGGER tr AFTER INSERT ON a BEGIN UPDATE a SET end = 1; INSERT INTO b VALUES (new.end); END;\n" +
				"CREATE TABLE c (z);",
			want: []string{
				"CREATE TRIGGER tr AFTER INSERT ON a BEGIN UPDATE a SET end = 1; INSERT INTO b VALUES (new.end); END;",
				"CREATE TABLE c (z);",
			},
		},
		{
			name: "end of case right before the end of the body",
			src:  "CREATE TRIGGER tr AFTER INSERT ON a BEGIN SELECT CASE WHEN 1 THEN 2 END; END; SELECT 1;",
			want: []string{"CREATE TRIGGER tr AFTER INSERT ON a BEGIN SELECT CASE WHEN 1 THEN 2 END; END;", "SELECT 1;"},
		},
		{
			name: "not a create trigger",
			src:  "DROP TRIGGER tr; SELECT 1;",
			want: []string{"DROP TRIGGER tr;", "SELECT 1;"},
		},
		{
			name: "trailing comment",
			src:  "CREATE TABLE a (x);\n-- end\n",
			want: []string{"CREATE TABLE a (x);", "-- end"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, splitTexts(t, tt.src))
		})
	}
}

func TestStatementIsEmpty(t *testing.T) {
	tokens, err := parser.Tokenize("CREATE TABLE a (x); ; -- c\n")
	require.NoError(t, err)
	stmts := parser.Split(tokens)
	require.Len(t, stmts, 3)
	assert.False(t, stmts[0].IsEmpty())
	assert.True(t, stmts[1].IsEmpty())
	assert.True(t, stmts[2].IsEmpty())
}

func TestStatementSignificant(t *testing.T) {
	tokens, err := parser.Tokenize("-- c\nCREATE  TABLE a;")
	require.NoError(t, err)
	stmts := parser.Split(tokens)
	require.Len(t, stmts, 1)

	var texts []string
	for _, x := range stmts[0].Significant() {
		texts = append(texts, x.Text)
	}
	assert.Equal(t, []string{"CREATE", "TABLE", "a", ";"}, texts)
}
