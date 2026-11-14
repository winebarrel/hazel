package parser

import (
	"fmt"
	"strings"
)

// DirectivePrefix starts a directive comment: -- hazel:name [argument].
const DirectivePrefix = "hazel:"

// Directive is a comment of the form "-- hazel:name argument".
type Directive struct {
	Name string
	Arg  string
	Line int
}

// The directives.
const (
	RenamedFrom         = "renamed-from"
	SkipForeignKeyCheck = "skip-foreign-key-check"
	Ignore              = "ignore"
	Execute             = "execute"
)

// directive returns the directive that the token is, if it is one: a "--"
// comment whose text starts with "hazel:".
func directive(t Token) (Directive, bool) {
	if t.Kind != Comment || !strings.HasPrefix(t.Text, "--") {
		return Directive{}, false
	}
	text := strings.TrimSpace(strings.TrimPrefix(t.Text, "--"))
	body, ok := strings.CutPrefix(text, DirectivePrefix)
	if !ok {
		return Directive{}, false
	}
	name, arg, _ := strings.Cut(body, " ")
	return Directive{Name: name, Arg: strings.TrimSpace(arg), Line: t.Line}, true
}

// directives returns the directives among tokens, the whitespace and
// comments before a statement or an element. It returns an error for a name
// that is not a directive, and, when tokens come after a separator, for a
// directive on the line the separator ends: written there, it reads as if it
// were for the statement or the element before.
func directives(tokens []Token, afterSeparator bool) ([]Directive, error) {
	var ds []Directive
	newLine := !afterSeparator
	for _, t := range tokens {
		d, ok := directive(t)
		if !ok {
			newLine = newLine || strings.Contains(t.Text, "\n")
			continue
		}
		if !newLine {
			return nil, &Error{Line: d.Line, Err: fmt.Errorf(
				"%s%s is on the line of what comes before it; write it on a line of its own", DirectivePrefix, d.Name)}
		}
		switch d.Name {
		case RenamedFrom, Ignore, Execute, SkipForeignKeyCheck:
			ds = append(ds, d)
		default:
			return nil, &Error{Line: d.Line, Err: fmt.Errorf("unknown directive: %s%s", DirectivePrefix, d.Name)}
		}
	}
	return ds, nil
}

// Directives returns the directives in the comments before the statement.
func (s Statement) Directives() ([]Directive, error) {
	afterSeparator := len(s.Tokens) > 0 && s.Tokens[0].Offset > 0
	return directives(s.leading(), afterSeparator)
}

// Directives returns the directives in the comments before the element.
func (e TableElement) Directives() ([]Directive, error) {
	return directives(e.Leading, e.afterComma)
}

// ParseName reads one name, quoted or not, as an identifier is written in
// SQL, and returns it without its quotes.
func ParseName(s string) (string, bool) {
	tokens, err := Tokenize(s)
	if err != nil {
		return "", false
	}
	tokens = significant(tokens)
	if len(tokens) != 1 || (tokens[0].Kind != Ident && tokens[0].Kind != QuotedIdent) {
		return "", false
	}
	return tokens[0].Name(), true
}

func (s Statement) leading() []Token {
	n := 0
	for n < len(s.Tokens) && s.Tokens[n].IsTrivia() {
		n++
	}
	return s.Tokens[:n]
}

// WithoutDirectives returns the SQL of the statement with its directive
// comments removed, so that they are not kept in sqlite_schema. A directive
// on a line of its own is removed with its line.
func (s Statement) WithoutDirectives() string {
	var b strings.Builder
	dropNewline := false
	for _, t := range s.Tokens[len(s.leading()):] {
		if _, ok := directive(t); ok {
			// Drop the indentation before the comment, and the line break
			// after it, when the comment has the line to itself.
			out := b.String()
			line := strings.LastIndexByte(out, '\n')
			if strings.TrimLeft(out[line+1:], " \t") == "" {
				b.Reset()
				b.WriteString(out[:line+1])
				dropNewline = true
			}
			continue
		}
		text := t.Text
		if dropNewline && t.Kind == Space {
			text = strings.TrimPrefix(text, "\n")
			text = strings.TrimPrefix(text, "\r\n")
		}
		dropNewline = false
		b.WriteString(text)
	}
	return b.String()
}
