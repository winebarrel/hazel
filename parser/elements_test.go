package parser_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/winebarrel/hazel/parser"
)

func TestTableElements(t *testing.T) {
	s := statement(t, `CREATE TABLE "t(x" (
  id INTEGER PRIMARY KEY, -- the key
  "na,me" TEXT DEFAULT ('a,b') CHECK (length("na,me") > 0),
  price NUMERIC(10, 2),
  CONSTRAINT u UNIQUE (id, price),
  CHECK (price > 0),
  FOREIGN KEY (id) REFERENCES p (id),
  PRIMARY KEY (id)
) STRICT;`)

	type elem struct{ Column, SQL string }
	var got []elem
	for _, e := range parser.TableElements(s) {
		got = append(got, elem{e.Column, e.SQL()})
	}
	assert.Equal(t, []elem{
		{"id", "id INTEGER PRIMARY KEY"},
		{"na,me", `"na,me" TEXT DEFAULT ('a,b') CHECK (length("na,me") > 0)`},
		{"price", "price NUMERIC(10, 2)"},
		{"", "CONSTRAINT u UNIQUE (id, price)"},
		{"", "CHECK (price > 0)"},
		{"", "FOREIGN KEY (id) REFERENCES p (id)"},
		{"", "PRIMARY KEY (id)"},
	}, got)
}

func TestTableElementsComment(t *testing.T) {
	// A comment before an element belongs to the separator, not the element.
	s := statement(t, "CREATE TABLE t (a, -- note\n  b /* inner */ INT)")
	elems := parser.TableElements(s)
	assert.Len(t, elems, 2)
	assert.Equal(t, "b /* inner */ INT", elems[1].SQL())
}

func TestTableElementsNone(t *testing.T) {
	assert.Nil(t, parser.TableElements(statement(t, "CREATE TABLE t AS SELECT (1) AS x")))
	assert.Nil(t, parser.TableElements(statement(t, "CREATE TABLE t")))
	elems := parser.TableElements(statement(t, "CREATE TABLE t ()"))
	assert.Len(t, elems, 1)
	assert.Empty(t, elems[0].SQL())
}

func TestQuoteIdent(t *testing.T) {
	tests := []struct{ name, want string }{
		{"users", "users"},
		{"_a1", "_a1"},
		{"Users", "Users"},
		{"order", `"order"`},
		{"Select", `"Select"`},
		{"1a", `"1a"`},
		{"a b", `"a b"`},
		{`a"b`, `"a""b"`},
		{"\u00e4", "\"\u00e4\""},
		{"", `""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parser.QuoteIdent(tt.name))
		})
	}
}
