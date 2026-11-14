package parser

// TokensEqual reports whether two token sequences are the same once these
// differences are removed:
//
//   - Whitespace and comments are skipped.
//   - Identifiers and keywords are compared without ASCII case, and a quoted
//     identifier is compared without its quotes, so "t", [t], `t` and T are
//     the same.
//   - Every other token (operators, strings, numbers) is compared as written.
//
// A quoted keyword is taken for the keyword, so "end" equals END even though
// SQLite reads the first as an identifier. Two definitions that differ only
// there are too rare to tell apart.
//
// It is used for what SQLite keeps as text and hazel does not compile: type
// names, collation names and trigger definitions.
func TokensEqual(a, b []Token) bool {
	a, b = significant(a), significant(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !tokenEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func tokenEqual(a, b Token) bool {
	if isName(a) && isName(b) {
		return EqualFold(a.Name(), b.Name())
	}
	return a.Kind == b.Kind && a.Text == b.Text
}

func isName(t Token) bool {
	return t.Kind == Ident || t.Kind == QuotedIdent
}

func significant(tokens []Token) []Token {
	var out []Token
	for _, t := range tokens {
		if !t.IsTrivia() {
			out = append(out, t)
		}
	}
	return out
}
