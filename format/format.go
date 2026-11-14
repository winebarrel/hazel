// Package format lays out the CREATE statements hazel writes. It moves only
// the whitespace between the tokens: the keywords, the names and the
// expressions come out as they were written, in the case they were written
// in. The one exception is a name that does not need its quotes, which loses
// them where the statement names an object or a column (see unquote).
//
// A CREATE TABLE gets one column or table constraint per line, indented by
// four spaces. Every other statement keeps its layout.
package format

import (
	"slices"
	"strings"

	"github.com/winebarrel/hazel/parser"
)

const indentUnit = "    "

// Statement returns the CREATE statement sql laid out, without a ";". A
// statement that does not tokenize is returned as it is.
func Statement(sql string) string {
	tokens, err := parser.Tokenize(sql)
	if err != nil {
		return sql
	}
	f := &formatter{tokens: tokens, text: make([]string, len(tokens))}
	for i, t := range tokens {
		f.text[i] = comment(t)
	}
	f.unquote()
	stmt := parser.Statement{Tokens: tokens}
	if open, closing, ok := f.list(); ok && stmt.Classify() == parser.CreateTable {
		f.table(open, closing)
	} else {
		for _, s := range f.text {
			f.b.WriteString(s)
		}
	}
	return strings.TrimSpace(f.b.String())
}

func (f *formatter) write(s ...string) {
	for _, x := range s {
		f.b.WriteString(x)
	}
}

type formatter struct {
	tokens []parser.Token
	// text is what each token is written as.
	text []string
	// sig holds the indexes of the significant tokens.
	sig []int
	b   strings.Builder
}

func isLineComment(t parser.Token) bool {
	return t.Kind == parser.Comment && strings.HasPrefix(t.Text, "--")
}

func hasNewline(t parser.Token) bool {
	return t.Kind == parser.Space && strings.Contains(t.Text, "\n")
}

// list returns the parentheses of the element list of a CREATE TABLE. A
// CREATE TABLE ... AS SELECT has none.
func (f *formatter) list() (int, int, bool) {
	open := -1
	depth := 0
	for i, t := range f.tokens {
		switch {
		case open < 0 && t.IsKeyword("AS"):
			return 0, 0, false
		case t.IsPunct("("):
			if open < 0 {
				open = i
			}
			depth++
		case t.IsPunct(")"):
			depth--
			if depth == 0 && open >= 0 {
				return open, i, true
			}
		}
	}
	return 0, 0, false
}

// element is one column definition or table constraint. Its tokens run from
// lo to hi, from the first significant token to the last. before holds the
// comments on lines of their own before it, and after the ones on its last
// line, which follow its comma.
type element struct {
	lo, hi int
	before []int
	after  []int
}

// table writes a CREATE TABLE: the part before the list on one line, each
// element of the list on a line of its own, and the closing parenthesis and
// the table options on the last line.
func (f *formatter) table(open, closing int) {
	f.inline(0, open)
	f.b.WriteString(f.text[open])

	// The elements are separated by the commas outside any parentheses.
	var elems []*element
	start, depth := open+1, 0
	for i := open + 1; i <= closing; i++ {
		t := f.tokens[i]
		switch {
		case t.IsPunct("(") && i < closing:
			depth++
		case t.IsPunct(")") && i < closing:
			depth--
		case i == closing || depth == 0 && t.IsPunct(","):
			e := &element{lo: start, hi: i}
			for e.lo < i && f.tokens[e.lo].IsTrivia() {
				e.lo++
			}
			for e.hi > e.lo && f.tokens[e.hi-1].IsTrivia() {
				e.hi--
			}
			elems = append(elems, e)
			start = i + 1
		}
	}

	// A comment on the line where an element ends goes after it, and one on
	// a line of its own before the next. The trivia between two elements is
	// what follows the first and what precedes the second.
	var opening, closingComments []int
	between := func(prev, next *element, lo, hi int) {
		newline := false
		for i := lo; i < hi; i++ {
			t := f.tokens[i]
			switch {
			case hasNewline(t):
				newline = true
			case t.Kind != parser.Comment:
			case newline && next != nil:
				next.before = append(next.before, i)
			case newline:
				closingComments = append(closingComments, i)
			case prev != nil:
				prev.after = append(prev.after, i)
			default:
				opening = append(opening, i)
			}
		}
	}
	for n, e := range elems {
		var prev *element
		lo := open + 1
		if n > 0 {
			prev, lo = elems[n-1], elems[n-1].hi
		}
		between(prev, e, lo, e.lo)
	}
	between(elems[len(elems)-1], nil, elems[len(elems)-1].hi, closing)

	for _, c := range opening {
		f.write(" ", f.text[c])
	}
	for n, e := range elems {
		for _, c := range e.before {
			f.write("\n", indentUnit, f.text[c])
		}
		f.write("\n", indentUnit)
		f.body(e.lo, e.hi)
		if n < len(elems)-1 {
			f.b.WriteString(",")
		}
		// A "--" comment ends its line, so it can only be the last.
		for _, c := range e.after {
			f.write(" ", f.text[c])
		}
	}
	for _, c := range closingComments {
		f.write("\n", indentUnit, f.text[c])
	}
	f.write("\n", f.text[closing])
	f.inline(closing+1, len(f.tokens))
}

// body writes the tokens of one element. A line break inside it is kept,
// and the line it opens is indented by the depth of the parentheses; any
// other whitespace becomes one space.
func (f *formatter) body(lo, hi int) {
	depth := 0
	for i := lo; i < hi; i++ {
		t := f.tokens[i]
		switch {
		case t.Kind == parser.Space && (hasNewline(t) || isLineComment(f.tokens[i-1])):
			d := depth
			if f.tokens[i+1].IsPunct(")") {
				d--
			}
			f.write("\n", strings.Repeat(indentUnit, 1+d))
		case t.Kind == parser.Space:
			f.b.WriteString(" ")
		default:
			f.b.WriteString(f.text[i])
			switch {
			case t.IsPunct("("):
				depth++
			case t.IsPunct(")"):
				depth--
			}
		}
	}
}

// inline writes the tokens from lo to hi on one line, each run of
// whitespace as one space, except after a "--" comment, which ends its line.
func (f *formatter) inline(lo, hi int) {
	for i := lo; i < hi; i++ {
		t := f.tokens[i]
		switch {
		case t.Kind != parser.Space:
			f.b.WriteString(f.text[i])
		case i > 0 && isLineComment(f.tokens[i-1]):
			f.b.WriteString("\n")
		default:
			f.b.WriteString(" ")
		}
	}
}

// unquote drops the quotes of the names that do not need them where the
// statement names something: the object it creates, the table of an index
// or a trigger, the columns of a view, and in a CREATE TABLE the columns,
// the constraint names, the tables a foreign key refers to and the column
// lists of the constraints. A name needs its quotes when it is a keyword or
// is not a plain ASCII identifier (see parser.QuoteIdent).
//
// Expressions, the columns of an index and the bodies of views and triggers
// keep theirs. There SQLite takes a "..." that names no column as a string,
// so the quotes can change what the statement means.
func (f *formatter) unquote() {
	for i, t := range f.tokens {
		if !t.IsTrivia() {
			f.sig = append(f.sig, i)
		}
	}

	// CREATE [UNIQUE | VIRTUAL] TABLE | INDEX | VIEW | TRIGGER [IF NOT EXISTS] [schema .] name
	k := 1
	for f.tok(k).IsKeyword("UNIQUE") || f.tok(k).IsKeyword("VIRTUAL") {
		k++
	}
	k++
	if f.tok(k).IsKeyword("IF") && f.tok(k+1).IsKeyword("NOT") && f.tok(k+2).IsKeyword("EXISTS") {
		k += 3
	}
	if f.tok(k + 1).IsPunct(".") {
		k += 2
	}
	f.mark(k)
	k++

	switch (parser.Statement{Tokens: f.tokens}).Classify() {
	case parser.CreateIndex, parser.CreateTrigger:
		// The first ON comes before any parenthesis.
		for ; k < len(f.sig); k++ {
			if f.tok(k).IsKeyword("ON") {
				f.mark(k + 1)
				break
			}
		}
	case parser.CreateView:
		if f.tok(k).IsPunct("(") {
			f.markList(k)
		}
	case parser.CreateTable:
		if f.tok(k).IsPunct("(") {
			f.unquoteElements(k)
		}
	}
}

// tok returns the significant token at k, or a zero Token past the end.
func (f *formatter) tok(k int) parser.Token {
	if k < len(f.sig) {
		return f.tokens[f.sig[k]]
	}
	return parser.Token{}
}

// mark drops the quotes of the significant token at k, when it is a quoted
// name that does not need them.
func (f *formatter) mark(k int) {
	if t := f.tok(k); t.Kind == parser.QuotedIdent && parser.QuoteIdent(t.Name()) == t.Name() {
		f.text[f.sig[k]] = t.Name()
	}
}

// markList marks the first token of each item of the list that opens at the
// significant token k, and returns the position of its closing parenthesis.
func (f *formatter) markList(k int) int {
	depth := 0
	for ; k < len(f.sig); k++ {
		switch t := f.tok(k); {
		case t.IsPunct("("):
			depth++
			if depth == 1 {
				f.mark(k + 1)
			}
		case t.IsPunct(")"):
			depth--
		case depth == 1 && t.IsPunct(","):
			f.mark(k + 1)
		}
		if depth == 0 {
			break
		}
	}
	return k
}

// unquoteElements marks the names in the element list of a CREATE TABLE
// that opens at the significant token open.
func (f *formatter) unquoteElements(open int) {
	start := true // the next token starts an element
	depth := 1
	for k := open + 1; k < len(f.sig) && depth > 0; k++ {
		t := f.tok(k)
		if start {
			start = false
			if !slices.ContainsFunc([]string{"CONSTRAINT", "PRIMARY", "UNIQUE", "CHECK", "FOREIGN"}, t.IsKeyword) {
				f.mark(k) // the column name
				continue
			}
		}
		switch {
		case t.IsPunct("("):
			depth++
		case t.IsPunct(")"):
			depth--
		case depth > 1:
		case t.IsPunct(","):
			start = true
		case t.IsKeyword("CONSTRAINT"):
			f.mark(k + 1)
		case t.IsKeyword("REFERENCES"):
			f.mark(k + 1)
			if f.tok(k + 2).IsPunct("(") {
				k = f.markList(k + 2)
			}
		case t.IsKeyword("UNIQUE") && f.tok(k+1).IsPunct("("):
			k = f.markList(k + 1)
		case (t.IsKeyword("PRIMARY") || t.IsKeyword("FOREIGN")) && f.tok(k+1).IsKeyword("KEY") && f.tok(k+2).IsPunct("("):
			k = f.markList(k + 2)
		}
	}
}
