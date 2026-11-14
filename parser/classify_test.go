package parser_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/winebarrel/hazel/parser"
)

func statement(t *testing.T, src string) parser.Statement {
	t.Helper()
	tokens, err := parser.Tokenize(src)
	require.NoError(t, err)
	stmts := parser.Split(tokens)
	require.Len(t, stmts, 1)
	return stmts[0]
}

func TestClassify(t *testing.T) {
	tests := []struct {
		src  string
		want parser.StatementKind
	}{
		{"CREATE TABLE t (a)", parser.CreateTable},
		{"create table if not exists t (a)", parser.CreateTable},
		{"CREATE VIRTUAL TABLE t USING fts5(a)", parser.CreateVirtualTable},
		{"CREATE INDEX i ON t (a)", parser.CreateIndex},
		{"CREATE UNIQUE INDEX i ON t (a)", parser.CreateIndex},
		{"CREATE VIEW v AS SELECT 1", parser.CreateView},
		{"CREATE TRIGGER tr AFTER INSERT ON t BEGIN SELECT 1; END", parser.CreateTrigger},
		{"-- c\nCREATE TABLE t (a)", parser.CreateTable},
		{"CREATE TEMP TABLE t (a)", parser.Unsupported},
		{"CREATE TEMPORARY VIEW v AS SELECT 1", parser.Unsupported},
		{"INSERT INTO t VALUES (1)", parser.Unsupported},
		{"PRAGMA user_version = 1", parser.Unsupported},
		{"ATTACH 'x.db' AS x", parser.Unsupported},
		{"CREATE", parser.Unsupported},
		{"", parser.Unsupported},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			tokens, err := parser.Tokenize(tt.src)
			require.NoError(t, err)
			assert.Equal(t, tt.want, parser.Statement{Tokens: tokens}.Classify())
		})
	}
}

func TestStatementSQLAndLine(t *testing.T) {
	s := statement(t, "-- note\n\n  CREATE TABLE t (\n  a -- x\n);")
	assert.Equal(t, "CREATE TABLE t (\n  a -- x\n);", s.SQL())
	assert.Equal(t, 3, s.Line())

	empty := statement(t, "-- only a comment\n")
	assert.Empty(t, empty.SQL())
	assert.Equal(t, 0, empty.Line())
}
