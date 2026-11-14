package parser_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/winebarrel/hazel/parser"
)

func TestTokensEqual(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"varchar(10)", "VARCHAR( 10 )", true},
		{"nocase", "NOCASE", true},
		{`"t"`, "[T]", true},
		{"`t`", "t", true},
		{"BEGIN SELECT 1; END", "begin /* c */ select 1;\nend", true},
		{"INT", "INTEGER", false},
		{"CHAR", "CH AR", false},
		{"a <> 1", "a != 1", false},
		{"'x'", "'X'", false},
		{"'x'", `"x"`, false},
		{"1", "1.0", false},
		{"varchar(10)", "varchar(10, 2)", false},
	}
	for _, tt := range tests {
		t.Run(tt.a+" vs "+tt.b, func(t *testing.T) {
			a, err := parser.Tokenize(tt.a)
			require.NoError(t, err)
			b, err := parser.Tokenize(tt.b)
			require.NoError(t, err)
			assert.Equal(t, tt.want, parser.TokensEqual(a, b))
		})
	}
}
