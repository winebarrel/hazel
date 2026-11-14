package parser_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/winebarrel/hazel/parser"
)

func TestParseCreateIndex(t *testing.T) {
	tests := []struct {
		src  string
		want parser.IndexDef
	}{
		{"CREATE INDEX i ON t (a)", parser.IndexDef{Exprs: []string{"a"}}},
		{
			`CREATE UNIQUE INDEX IF NOT EXISTS i ON "t" (a COLLATE nocase DESC, lower( b ) ASC, (a + 1) COLLATE "rtrim")`,
			parser.IndexDef{Exprs: []string{"a", "lower( b )", "(a + 1)"}},
		},
		{
			"CREATE INDEX i ON t (a, b) WHERE a != 0 AND (b IS NOT NULL);",
			parser.IndexDef{Exprs: []string{"a", "b"}, Where: "a != 0 AND (b IS NOT NULL)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			assert.Equal(t, tt.want, parser.ParseCreateIndex(statement(t, tt.src)))
		})
	}
}
