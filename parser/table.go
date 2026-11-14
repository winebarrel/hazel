package parser

import (
	"slices"
	"strings"
)

// TableDef is what a CREATE TABLE says that PRAGMAs do not report.
type TableDef struct {
	// Autoincrement reports whether the PRIMARY KEY has AUTOINCREMENT.
	Autoincrement bool
	// Columns holds the column definitions in the order written.
	Columns []ColumnDef
	// Checks holds the CHECK constraints, of the columns and of the table, in
	// the order written.
	Checks []CheckDef
	// ForeignKeys holds the foreign keys, of the columns and of the table, in
	// the order written, which is the order PRAGMA foreign_key_list reports.
	ForeignKeys []ForeignKeyDef
}

// ColumnDef is what a column definition says that PRAGMAs do not report.
type ColumnDef struct {
	Name string
	// Collation is the name of the COLLATE, or "".
	Collation string
	// Generated is the expression of a generated column, without the
	// parentheses around it, or "".
	Generated string
}

// CheckDef is a CHECK constraint.
type CheckDef struct {
	// Name is the CONSTRAINT name, or "".
	Name string
	// Expr is the expression, without the parentheses around it.
	Expr string
	// Column is the column whose definition holds the constraint, or "" for a
	// table constraint.
	Column string
}

// ForeignKeyDef is what a foreign key says that PRAGMA foreign_key_list does
// not report.
type ForeignKeyDef struct {
	// Name is the CONSTRAINT name, or "".
	Name string
	// Deferred reports DEFERRABLE INITIALLY DEFERRED. NOT DEFERRABLE, a bare
	// DEFERRABLE and INITIALLY IMMEDIATE all mean the same to SQLite.
	Deferred bool
	// Column is the column whose definition holds the REFERENCES, or "" for
	// a table constraint.
	Column string
}

// ParseCreateTable reads a CREATE TABLE that SQLite has accepted. It looks at
// the keywords outside parentheses only, which is enough since SQLite has
// checked the grammar: a CHECK, a COLLATE or a REFERENCES inside an
// expression is inside parentheses.
//
// A CONSTRAINT name belongs to the constraint right after it, as for ALTER
// TABLE ... DROP CONSTRAINT (alter.c), which is what the name is for.
func ParseCreateTable(s Statement) TableDef {
	var def TableDef
	for _, t := range s.Tokens {
		if t.IsKeyword("AUTOINCREMENT") {
			def.Autoincrement = true
		}
	}
	for _, e := range TableElements(s) {
		tokens := significant(e.Tokens)
		text := func(from, to int) string {
			return textBetween(e.Tokens, tokens[from], tokens[to])
		}
		if e.Column != "" {
			def.Columns = append(def.Columns, ColumnDef{Name: e.Column})
		}
		col := &ColumnDef{}
		if e.Column != "" {
			col = &def.Columns[len(def.Columns)-1]
		}
		name := ""
		for i := 0; i < len(tokens); i++ {
			t := tokens[i]
			switch {
			case t.IsKeyword("CONSTRAINT") && i+1 < len(tokens):
				name = tokens[i+1].Name()
				i++
				continue
			case t.IsKeyword("CHECK"):
				end := closing(tokens, i+1)
				def.Checks = append(def.Checks, CheckDef{Name: name, Expr: text(i+1, end), Column: e.Column})
				i = end
			case t.IsKeyword("FOREIGN"):
				// FOREIGN KEY (columns) REFERENCES ...: the name goes on to
				// the REFERENCES.
				i = closing(tokens, i+2)
				continue
			case t.IsKeyword("REFERENCES"):
				fk, end := foreignKeyClause(tokens, i+1)
				fk.Name = name
				fk.Column = e.Column
				def.ForeignKeys = append(def.ForeignKeys, fk)
				i = end
			case t.IsKeyword("COLLATE") && i+1 < len(tokens):
				col.Collation = tokens[i+1].Name()
				i++
			case t.IsKeyword("AS") && i+1 < len(tokens) && tokens[i+1].IsPunct("("):
				end := closing(tokens, i+1)
				col.Generated = text(i+1, end)
				i = end
			case t.IsPunct("("):
				// The type's size, a column list, or a DEFAULT expression.
				i = closing(tokens, i)
			}
			name = ""
		}
	}
	return def
}

// closing returns the index of the parenthesis that closes the one at
// tokens[i].
func closing(tokens []Token, i int) int {
	depth := 0
	for j := i; j < len(tokens); j++ {
		switch {
		case tokens[j].IsPunct("("):
			depth++
		case tokens[j].IsPunct(")"):
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return len(tokens) - 1
}

// textBetween returns the text of the tokens of all that come after from and
// before to, as written, without the whitespace and comments at either end.
func textBetween(all []Token, from, to Token) string {
	var inner []Token
	for _, t := range all {
		if t.Offset > from.Offset && t.Offset < to.Offset {
			inner = append(inner, t)
		}
	}
	for len(inner) > 0 && inner[0].IsTrivia() {
		inner = inner[1:]
	}
	for len(inner) > 0 && inner[len(inner)-1].IsTrivia() {
		inner = inner[:len(inner)-1]
	}
	var b strings.Builder
	for _, t := range inner {
		b.WriteString(t.Text)
	}
	return b.String()
}

// foreignKeyClause reads the clause after REFERENCES, starting at tokens[i],
// and returns the index of its last token:
//
//	name [(columns)] {ON DELETE|UPDATE action | MATCH name} [[NOT] DEFERRABLE [INITIALLY DEFERRED|IMMEDIATE]]
func foreignKeyClause(tokens []Token, i int) (ForeignKeyDef, int) {
	var fk ForeignKeyDef
	end := i // the parent table
	if end+1 < len(tokens) && tokens[end+1].IsPunct("(") {
		end = closing(tokens, end+1)
	}
	for end+1 < len(tokens) {
		t := tokens[end+1]
		switch {
		case t.IsKeyword("ON") || t.IsKeyword("MATCH"):
			// ON DELETE|UPDATE SET NULL|SET DEFAULT|CASCADE|RESTRICT|NO ACTION,
			// or MATCH name: skip to the next clause.
			end += 2
			for end+1 < len(tokens) && isActionWord(tokens[end+1]) {
				end++
			}
		case t.IsKeyword("NOT") && end+2 < len(tokens) && tokens[end+2].IsKeyword("DEFERRABLE"):
			end += 2
			end = initially(tokens, end)
		case t.IsKeyword("DEFERRABLE"):
			end++
			if end+2 < len(tokens) && tokens[end+1].IsKeyword("INITIALLY") && tokens[end+2].IsKeyword("DEFERRED") {
				fk.Deferred = true
			}
			end = initially(tokens, end)
		default:
			return fk, end
		}
	}
	return fk, end
}

// isActionWord reports whether t is a word that continues a foreign key
// action after ON DELETE or ON UPDATE.
func isActionWord(t Token) bool {
	return slices.ContainsFunc([]string{"SET", "NULL", "DEFAULT", "CASCADE", "RESTRICT", "NO", "ACTION"}, t.IsKeyword)
}

// initially skips an INITIALLY DEFERRED or INITIALLY IMMEDIATE after
// tokens[i].
func initially(tokens []Token, i int) int {
	if i+2 < len(tokens) && tokens[i+1].IsKeyword("INITIALLY") {
		return i + 2
	}
	return i
}
