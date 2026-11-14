package parser

import (
	"slices"
	"strings"
)

// TableElement is one element of the list in parentheses of a CREATE TABLE:
// a column definition or a table constraint.
type TableElement struct {
	// Column is the name of the column for a column definition, and "" for a
	// table constraint.
	Column string
	// Tokens are the tokens of the element, from its first significant token
	// to its last, with the whitespace and comments between them.
	Tokens []Token
}

// SQL returns the text of the element.
func (e TableElement) SQL() string {
	var b strings.Builder
	for _, t := range e.Tokens {
		b.WriteString(t.Text)
	}
	return b.String()
}

// TableElements returns the elements of a CREATE TABLE. It returns nil for a
// CREATE TABLE ... AS SELECT, which has no list.
func TableElements(s Statement) []TableElement {
	var elems []TableElement
	depth := 0
	start := -1
	for i, t := range s.Tokens {
		switch {
		case depth == 0 && t.IsKeyword("AS"):
			return nil
		case t.IsPunct("("):
			depth++
			if depth == 1 {
				start = i + 1
			}
		case t.IsPunct(")"):
			depth--
			if depth == 0 {
				return append(elems, newTableElement(s.Tokens[start:i]))
			}
		case depth == 1 && t.IsPunct(","):
			elems = append(elems, newTableElement(s.Tokens[start:i]))
			start = i + 1
		}
	}
	return nil
}

// constraintKeywords start a table constraint.
var constraintKeywords = []string{"CONSTRAINT", "PRIMARY", "UNIQUE", "CHECK", "FOREIGN"}

func newTableElement(tokens []Token) TableElement {
	for len(tokens) > 0 && tokens[0].IsTrivia() {
		tokens = tokens[1:]
	}
	for len(tokens) > 0 && tokens[len(tokens)-1].IsTrivia() {
		tokens = tokens[:len(tokens)-1]
	}
	e := TableElement{Tokens: tokens}
	if len(tokens) == 0 {
		return e
	}
	if slices.ContainsFunc(constraintKeywords, tokens[0].IsKeyword) {
		return e
	}
	e.Column = tokens[0].Name()
	return e
}
