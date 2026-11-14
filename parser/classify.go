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
