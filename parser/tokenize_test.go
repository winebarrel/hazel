package parser_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/winebarrel/hazel/parser"
)

type tok struct {
	Kind parser.Kind
	Text string
}

func significantTokens(t *testing.T, src string) []tok {
	t.Helper()
	tokens, err := parser.Tokenize(src)
	require.NoError(t, err)
	var out []tok
	for _, x := range tokens {
		if !x.IsTrivia() {
			out = append(out, tok{x.Kind, x.Text})
		}
	}
	return out
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		src  string
		want []tok
	}{
		{"CREATE TABLE t", []tok{{parser.Ident, "CREATE"}, {parser.Ident, "TABLE"}, {parser.Ident, "t"}}},
		{`"a""b" [c d] ` + "`e``f`", []tok{{parser.QuotedIdent, `"a""b"`}, {parser.QuotedIdent, "[c d]"}, {parser.QuotedIdent, "`e``f`"}}},
		{"'it''s' x'0A' X'ff'", []tok{{parser.String, "'it''s'"}, {parser.Blob, "x'0A'"}, {parser.Blob, "X'ff'"}}},
		{"1 1.5 .5 1e10 1.5E-3 0x1F 1_000", []tok{
			{parser.Number, "1"},
			{parser.Number, "1.5"},
			{parser.Number, ".5"},
			{parser.Number, "1e10"},
			{parser.Number, "1.5E-3"},
			{parser.Number, "0x1F"},
			{parser.Number, "1_000"},
		}},
		{"? ?1 :a @b $c", []tok{
			{parser.Variable, "?"},
			{parser.Variable, "?1"},
			{parser.Variable, ":a"},
			{parser.Variable, "@b"},
			{parser.Variable, "$c"},
		}},
		{"a->>b->c||d<>e!=f==g<=h>=i<<j>>k", []tok{
			{parser.Ident, "a"},
			{parser.Punct, "->>"},
			{parser.Ident, "b"},
			{parser.Punct, "->"},
			{parser.Ident, "c"},
			{parser.Punct, "||"},
			{parser.Ident, "d"},
			{parser.Punct, "<>"},
			{parser.Ident, "e"},
			{parser.Punct, "!="},
			{parser.Ident, "f"},
			{parser.Punct, "=="},
			{parser.Ident, "g"},
			{parser.Punct, "<="},
			{parser.Ident, "h"},
			{parser.Punct, ">="},
			{parser.Ident, "i"},
			{parser.Punct, "<<"},
			{parser.Ident, "j"},
			{parser.Punct, ">>"},
			{parser.Ident, "k"},
		}},
		{"a$b _c t.c", []tok{{parser.Ident, "a$b"}, {parser.Ident, "_c"}, {parser.Ident, "t"}, {parser.Punct, "."}, {parser.Ident, "c"}}},
		{"\u00e4b", []tok{{parser.Ident, "\u00e4b"}}},
		{"a - b", []tok{{parser.Ident, "a"}, {parser.Punct, "-"}, {parser.Ident, "b"}}},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			assert.Equal(t, tt.want, significantTokens(t, tt.src))
		})
	}
}

func TestTokenizeComments(t *testing.T) {
	src := "a -- c1\n/* c2 */ b /* open"
	tokens, err := parser.Tokenize(src)
	require.NoError(t, err)

	var comments []string
	for _, x := range tokens {
		if x.Kind == parser.Comment {
			comments = append(comments, x.Text)
		}
	}
	assert.Equal(t, []string{"-- c1", "/* c2 */", "/* open"}, comments)

	tokens, err = parser.Tokenize("a -- last")
	require.NoError(t, err)
	assert.Equal(t, "-- last", tokens[len(tokens)-1].Text)
}

func TestTokenizeRoundTrip(t *testing.T) {
	src := "CREATE TABLE \"t\" (\n  a INT, -- note\n  b TEXT DEFAULT 'x'\n);\n"
	tokens, err := parser.Tokenize(src)
	require.NoError(t, err)

	var b strings.Builder
	for _, x := range tokens {
		b.WriteString(x.Text)
	}
	assert.Equal(t, src, b.String())
}

func TestTokenizeLine(t *testing.T) {
	tokens, err := parser.Tokenize("a\n/* x\ny */ b\nc")
	require.NoError(t, err)

	lines := map[string]int{}
	for _, x := range tokens {
		if x.Kind == parser.Ident {
			lines[x.Text] = x.Line
		}
	}
	assert.Equal(t, map[string]int{"a": 1, "b": 3, "c": 4}, lines)
}

func TestTokenizeError(t *testing.T) {
	tests := []struct {
		src  string
		want string
	}{
		{"'abc", "line 1: unterminated '-quoted token"},
		{"a\n\"abc", "line 2: unterminated \"-quoted token"},
		{"[abc", "line 1: unterminated identifier"},
		{"12abc", `line 1: unrecognized token: "12a"`},
		{"a ! b", `line 1: unrecognized token: "!"`},
		{"a : b", `line 1: unrecognized token: ":"`},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			_, err := parser.Tokenize(tt.src)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestTokenName(t *testing.T) {
	tests := []struct {
		src  string
		want string
	}{
		{"abc", "abc"},
		{`"a""b"`, `a"b`},
		{"[a b]", "a b"},
		{"`a``b`", "a`b"},
		{"'abc'", ""},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			tokens, err := parser.Tokenize(tt.src)
			require.NoError(t, err)
			require.Len(t, tokens, 1)
			assert.Equal(t, tt.want, tokens[0].Name())
		})
	}
}

func TestEqualFold(t *testing.T) {
	assert.True(t, parser.EqualFold("Users", "USERS"))
	assert.False(t, parser.EqualFold("users", "user"))
	// Only ASCII is folded, as in SQLite.
	assert.False(t, parser.EqualFold("\u00c4", "\u00e4"))
}
