// Package parser reads the DDL statements of a schema file.
package parser

import "strings"

// Kind is the kind of a token.
type Kind int

const (
	// Space is a run of whitespace.
	Space Kind = iota
	// Comment is a "--" comment up to the end of the line, or a "/* */"
	// comment.
	Comment
	// Ident is an identifier or a keyword written without quotes.
	Ident
	// QuotedIdent is an identifier written in "", [], or ``.
	QuotedIdent
	// String is a string literal written in ''.
	String
	// Blob is a blob literal, X'...'.
	Blob
	// Number is a numeric literal.
	Number
	// Variable is a parameter: ?, ?NNN, :name, @name or $name.
	Variable
	// Punct is an operator or a punctuation mark.
	Punct
)

// Token is one token of the source. Text is the source text of the token as
// written.
type Token struct {
	Kind   Kind
	Text   string
	Offset int // byte offset in the source
	Line   int // 1-based line number
}

// IsTrivia reports whether the token is whitespace or a comment.
func (t Token) IsTrivia() bool {
	return t.Kind == Space || t.Kind == Comment
}

// IsKeyword reports whether the token is the unquoted word kw. The comparison
// ignores ASCII case, as SQLite does.
func (t Token) IsKeyword(kw string) bool {
	return t.Kind == Ident && EqualFold(t.Text, kw)
}

// IsPunct reports whether the token is the operator or punctuation mark p.
func (t Token) IsPunct(p string) bool {
	return t.Kind == Punct && t.Text == p
}

// Name returns the identifier that the token names: the text of an Ident, or
// the text of a QuotedIdent without its quotes. It returns "" for any other
// kind.
func (t Token) Name() string {
	switch t.Kind {
	case Ident:
		return t.Text
	case QuotedIdent:
		return dequote(t.Text)
	}
	return ""
}

// dequote removes the quotes of a quoted identifier. Inside double quotes
// and backquotes, a doubled quote stands for one; brackets have no escape.
func dequote(s string) string {
	inner := s[1 : len(s)-1]
	switch s[0] {
	case '[':
		return inner
	case '`':
		return strings.ReplaceAll(inner, "``", "`")
	}
	return strings.ReplaceAll(inner, `""`, `"`)
}

// EqualFold reports whether a and b are equal when ASCII letters are compared
// without case. SQLite folds only ASCII when it compares names, so a
// non-ASCII letter and its uppercase form stay different.
func EqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if lower(a[i]) != lower(b[i]) {
			return false
		}
	}
	return true
}

func lower(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}
