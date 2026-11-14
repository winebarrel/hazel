package parser

// IndexDef is what a CREATE INDEX says that PRAGMAs do not report.
type IndexDef struct {
	// Exprs holds the text of each indexed term, in order, without its
	// COLLATE, ASC or DESC. For a column it is the column name.
	Exprs []string
	// Where is the condition of a partial index, or "".
	Where string
}

// ParseCreateIndex reads a CREATE INDEX that SQLite has accepted.
func ParseCreateIndex(s Statement) IndexDef {
	var def IndexDef
	tokens := significant(s.Tokens)
	i := 0
	for i < len(tokens) && !tokens[i].IsPunct("(") {
		i++
	}
	end := closing(tokens, i)
	start := i + 1
	depth := 0
	for j := start; j <= end; j++ {
		t := tokens[j]
		switch {
		case t.IsPunct("("):
			depth++
		case t.IsPunct(")") && depth > 0:
			depth--
		case depth == 0 && (t.IsPunct(",") || j == end):
			def.Exprs = append(def.Exprs, indexTerm(s.Tokens, tokens[start:j]))
			start = j + 1
		}
	}
	if end+2 < len(tokens) && tokens[end+1].IsKeyword("WHERE") {
		last := len(tokens) - 1
		if tokens[last].IsPunct(";") {
			last--
		}
		def.Where = textBetween(s.Tokens, tokens[end+1], afterLast(tokens[last]))
	}
	return def
}

// indexTerm returns the text of an indexed term without the COLLATE, ASC
// and DESC at its end.
func indexTerm(all, term []Token) string {
	n := len(term)
	if n > 0 && (term[n-1].IsKeyword("ASC") || term[n-1].IsKeyword("DESC")) {
		n--
	}
	if n >= 2 && term[n-2].IsKeyword("COLLATE") {
		n -= 2
	}
	return textBetween(all, beforeFirst(term[0]), afterLast(term[n-1]))
}

// beforeFirst and afterLast give tokens just outside t, for textBetween.
func beforeFirst(t Token) Token {
	return Token{Offset: t.Offset - 1}
}

func afterLast(t Token) Token {
	return Token{Offset: t.Offset + 1}
}
