package format

import (
	"slices"
	"strings"

	"github.com/winebarrel/hazel/parser"
)

// File returns a schema file laid out. The statements are separated by one
// blank line, each with the comments before it on lines of their own, and a
// comment on the line of a statement's ";" stays there. Runs of blank lines
// between the comments become one. The CREATE statements are laid out as
// Statement does, and get a ";" when they lack one; any other statement, and
// a statement marked with -- hazel:execute, is left as it is.
//
// With stripRenamedFrom, the -- hazel:renamed-from directives are dropped.
// A file that does not tokenize is returned as it is.
func File(src string, stripRenamedFrom bool) string {
	tokens, err := parser.Tokenize(src)
	if err != nil {
		return src
	}
	if stripRenamedFrom {
		tokens = slices.DeleteFunc(tokens, func(t parser.Token) bool {
			d, ok := t.Directive()
			return ok && d.Name == parser.RenamedFrom
		})
	}

	var out []string
	for _, s := range parser.Split(tokens) {
		lead := 0
		for lead < len(s.Tokens) && s.Tokens[lead].IsTrivia() {
			lead++
		}
		// The comments up to the first line break are on the line of the
		// previous statement's ";".
		i := 0
		for ; i < lead && !hasNewline(s.Tokens[i]); i++ {
			if t := s.Tokens[i]; t.Kind == parser.Comment && len(out) > 0 {
				out[len(out)-1] += " " + comment(t)
			}
		}
		if len(out) == 0 {
			i = 0
		}
		var b strings.Builder
		for ; i < lead; i++ {
			switch t := s.Tokens[i]; {
			case t.Kind == parser.Comment:
				b.WriteString(comment(t) + "\n")
			case b.Len() > 0 && strings.Count(t.Text, "\n") >= 2:
				b.WriteString("\n")
			}
		}
		if s.IsEmpty() {
			if b.Len() > 0 {
				out = append(out, strings.TrimRight(b.String(), "\n"))
			}
			continue
		}
		b.WriteString(statement(s, s.Tokens[lead:]))
		out = append(out, b.String())
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n\n") + "\n"
}

func comment(t parser.Token) string {
	if isLineComment(t) {
		return strings.TrimRight(t.Text, " \t\r")
	}
	return t.Text
}

// statement returns one statement, from its first significant token.
func statement(s parser.Statement, tokens []parser.Token) string {
	// tokens start at a significant token, so SQL gives them whole.
	text := func(tokens []parser.Token) string { return parser.Statement{Tokens: tokens}.SQL() }
	ds, _ := s.Directives()
	execute := slices.ContainsFunc(ds, func(d parser.Directive) bool { return d.Name == parser.Execute })
	if s.Classify() == parser.Unsupported || execute {
		return strings.TrimRight(text(tokens), " \t\r\n")
	}
	if n := len(tokens); tokens[n-1].IsPunct(";") {
		tokens = tokens[:n-1]
	}
	sql := Statement(text(tokens))
	// A "--" comment at the end would take the ";" in.
	for _, t := range slices.Backward(tokens) {
		if t.Kind == parser.Space {
			continue
		}
		if isLineComment(t) {
			return sql + "\n;"
		}
		break
	}
	return sql + ";"
}
