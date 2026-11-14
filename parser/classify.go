package parser

import "strings"

// StatementKind is the kind of statement hazel manages.
type StatementKind int

const (
	// Unsupported is any statement hazel does not manage: DML, PRAGMA,
	// ATTACH, DROP, ALTER, a CREATE of a TEMP object, and so on.
	Unsupported StatementKind = iota
	CreateTable
	CreateVirtualTable
	CreateIndex
	CreateView
	CreateTrigger
)

// Classify returns the kind of the statement from its first keywords.
func (s Statement) Classify() StatementKind {
	tokens := s.Significant()
	if len(tokens) < 2 || !tokens[0].IsKeyword("CREATE") {
		return Unsupported
	}
	next := tokens[1]
	switch {
	case next.IsKeyword("TABLE"):
		return CreateTable
	case next.IsKeyword("VIRTUAL"):
		return CreateVirtualTable
	case next.IsKeyword("INDEX"), next.IsKeyword("UNIQUE"):
		return CreateIndex
	case next.IsKeyword("VIEW"):
		return CreateView
	case next.IsKeyword("TRIGGER"):
		return CreateTrigger
	}
	// CREATE TEMP ... and anything else.
	return Unsupported
}

// SQL returns the text of the statement from its first significant token to
// its end, the ";" included when there is one.
func (s Statement) SQL() string {
	var b strings.Builder
	for _, t := range s.Tokens {
		if b.Len() == 0 && t.IsTrivia() {
			continue
		}
		b.WriteString(t.Text)
	}
	return b.String()
}

// Line returns the line of the first significant token, or 0 for an empty
// statement.
func (s Statement) Line() int {
	for _, t := range s.Tokens {
		if !t.IsTrivia() {
			return t.Line
		}
	}
	return 0
}

// ObjectName returns the name of the object a CREATE statement creates, or ""
// for any other statement. A schema name before it is left out.
func (s Statement) ObjectName() string {
	i, _ := s.objectName()
	if i < 0 {
		return ""
	}
	return s.Tokens[i].Name()
}

// WithObjectName returns the SQL of a CREATE statement, from its first
// significant token, with the object's name, and any schema name before it,
// replaced by name.
func (s Statement) WithObjectName(name string) string {
	i, schema := s.objectName()
	var b strings.Builder
	for j, t := range s.Tokens {
		switch {
		case b.Len() == 0 && t.IsTrivia():
		case j == i:
			b.WriteString(QuoteIdent(name))
		case j >= schema && j < i:
			// The schema name and the "." after it.
		default:
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

// objectName returns the index in Tokens of the object's name of a CREATE
// statement, and of the first token of the schema name before it (equal to
// the name's index when there is none). It returns -1 for any other
// statement.
func (s Statement) objectName() (int, int) {
	if s.Classify() == Unsupported {
		return -1, -1
	}
	var idx []int // indexes of the significant tokens
	for j, t := range s.Tokens {
		if !t.IsTrivia() {
			idx = append(idx, j)
		}
	}
	tok := func(i int) Token { return s.Tokens[idx[i]] }
	i := 1
	// UNIQUE INDEX, VIRTUAL TABLE.
	for i < len(idx) && (tok(i).IsKeyword("UNIQUE") || tok(i).IsKeyword("VIRTUAL")) {
		i++
	}
	i++ // TABLE, INDEX, VIEW or TRIGGER
	if i+2 < len(idx) && tok(i).IsKeyword("IF") && tok(i+1).IsKeyword("NOT") && tok(i+2).IsKeyword("EXISTS") {
		i += 3
	}
	schema := i
	if i+2 < len(idx) && tok(i+1).IsPunct(".") {
		i += 2
	}
	if i >= len(idx) {
		return -1, -1
	}
	return idx[i], idx[schema]
}
