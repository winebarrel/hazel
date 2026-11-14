package parser

import (
	"errors"
	"fmt"
	"strings"
)

// Error is an error at a line of the source.
type Error struct {
	Line int
	Err  error
}

func (e *Error) Error() string {
	return fmt.Sprintf("line %d: %v", e.Line, e.Err)
}

func (e *Error) Unwrap() error {
	return e.Err
}

// Tokenize splits src into tokens, whitespace and comments included, so that
// joining the Text of every token gives back src. It follows the rules of
// SQLite's tokenizer (src/tokenize.c). An error is an *Error.
func Tokenize(src string) ([]Token, error) {
	var tokens []Token
	line := 1
	for i := 0; i < len(src); {
		kind, n, err := scan(src[i:])
		if err != nil {
			return nil, &Error{Line: line, Err: err}
		}
		text := src[i : i+n]
		tokens = append(tokens, Token{Kind: kind, Text: text, Offset: i, Line: line})
		line += strings.Count(text, "\n")
		i += n
	}
	return tokens, nil
}

// scan returns the kind and the length of the token at the start of s.
func scan(s string) (Kind, int, error) {
	c := s[0]
	switch {
	case isSpace(c):
		n := 1
		for n < len(s) && isSpace(s[n]) {
			n++
		}
		return Space, n, nil
	case c == '-' && strings.HasPrefix(s, "--"):
		n := strings.IndexByte(s, '\n')
		if n < 0 {
			n = len(s)
		}
		return Comment, n, nil
	case c == '/' && strings.HasPrefix(s, "/*"):
		// SQLite accepts a comment that is not closed before the end.
		n := strings.Index(s[2:], "*/")
		if n < 0 {
			return Comment, len(s), nil
		}
		return Comment, n + 4, nil
	case c == '\'':
		n, err := scanQuoted(s, '\'', "string")
		return String, n, err
	case c == '"' || c == '`':
		n, err := scanQuoted(s, c, "identifier")
		return QuotedIdent, n, err
	case c == '[':
		n := strings.IndexByte(s, ']')
		if n < 0 {
			return 0, 0, errors.New("unterminated identifier: [")
		}
		return QuotedIdent, n + 1, nil
	case (c == 'x' || c == 'X') && len(s) > 1 && s[1] == '\'':
		n, err := scanQuoted(s[1:], '\'', "blob")
		return Blob, n + 1, err
	case isDigit(c) || (c == '.' && len(s) > 1 && isDigit(s[1])):
		return scanNumber(s)
	case c == '?':
		n := 1
		for n < len(s) && isDigit(s[n]) {
			n++
		}
		return Variable, n, nil
	case c == ':' || c == '@' || c == '$':
		n := 1
		for n < len(s) && isIDChar(s[n]) {
			n++
		}
		if n == 1 {
			return 0, 0, fmt.Errorf("unrecognized token: %q", s[:1])
		}
		return Variable, n, nil
	case isIDStart(c):
		n := 1
		for n < len(s) && isIDChar(s[n]) {
			n++
		}
		return Ident, n, nil
	}
	return scanPunct(s)
}

// scanQuoted returns the length of a token quoted with q, where a doubled q
// stands for one q. The error names only the kind of the token and its quote,
// since the rest of the source can be long.
func scanQuoted(s string, q byte, what string) (int, error) {
	for i := 1; i < len(s); i++ {
		if s[i] != q {
			continue
		}
		if i+1 < len(s) && s[i+1] == q {
			i++
			continue
		}
		return i + 1, nil
	}
	return 0, fmt.Errorf("unterminated %s: %c", what, q)
}

// scanNumber returns the length of a numeric literal: an integer, a real with
// an optional exponent, or a hexadecimal integer. Digits may be separated by
// "_" (SQLite 3.46.0 and later).
func scanNumber(s string) (Kind, int, error) {
	n := 0
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		n = 2
		for n < len(s) && (isHexDigit(s[n]) || s[n] == '_') {
			n++
		}
	} else {
		digits := func() {
			for n < len(s) && (isDigit(s[n]) || s[n] == '_') {
				n++
			}
		}
		digits()
		if n < len(s) && s[n] == '.' {
			n++
			digits()
		}
		if n < len(s) && (s[n] == 'e' || s[n] == 'E') {
			m := n + 1
			if m < len(s) && (s[m] == '+' || s[m] == '-') {
				m++
			}
			if m < len(s) && isDigit(s[m]) {
				n = m
				digits()
			}
		}
	}
	if n < len(s) && isIDChar(s[n]) {
		return 0, 0, fmt.Errorf("unrecognized token: %q", s[:n+1])
	}
	return Number, n, nil
}

// puncts lists the operators and punctuation marks, longest first.
var puncts = []string{
	"->>", "->", "||", "<<", ">>", "<=", ">=", "<>", "==", "!=",
	"(", ")", ";", ",", ".", "+", "-", "*", "/", "%", "=", "<", ">", "&", "|", "~",
}

func scanPunct(s string) (Kind, int, error) {
	for _, p := range puncts {
		if strings.HasPrefix(s, p) {
			return Punct, len(p), nil
		}
	}
	return 0, 0, fmt.Errorf("unrecognized token: %q", s[:1])
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
}

func isDigit(c byte) bool {
	return '0' <= c && c <= '9'
}

func isHexDigit(c byte) bool {
	return isDigit(c) || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}

// isIDStart reports whether c can start an identifier. Like SQLite, every byte
// of a multi-byte UTF-8 character counts as an identifier character.
func isIDStart(c byte) bool {
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || c == '_' || c >= 0x80
}

func isIDChar(c byte) bool {
	return isIDStart(c) || isDigit(c) || c == '$'
}
