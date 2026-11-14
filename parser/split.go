package parser

// Statement is one statement of a schema file. Tokens holds the statement
// together with the whitespace and comments before it, and the terminating
// ";" when there is one.
type Statement struct {
	Tokens []Token
}

// Significant returns the tokens of the statement that are neither
// whitespace nor comments.
func (s Statement) Significant() []Token {
	return significant(s.Tokens)
}

// IsEmpty reports whether the statement has nothing but whitespace, comments
// and a ";".
func (s Statement) IsEmpty() bool {
	for _, t := range s.Tokens {
		if !t.IsTrivia() && !t.IsPunct(";") {
			return false
		}
	}
	return true
}

// Split splits tokens into statements at each ";". The ";" inside the body of
// a CREATE TRIGGER does not end the statement: it runs to the END of the
// body. Inside a trigger, CASE ... END pairs are counted so that the END of a
// CASE expression is not taken for the END of the body.
//
// Whitespace and comments after the last statement are returned as a
// statement of their own, which IsEmpty reports.
func Split(tokens []Token) []Statement {
	var stmts []Statement
	start := 0
	var st splitState
	for i, t := range tokens {
		if t.IsTrivia() {
			continue
		}
		if t.IsPunct(";") && st.canEnd() {
			stmts = append(stmts, Statement{Tokens: tokens[start : i+1]})
			start = i + 1
			st = splitState{}
			continue
		}
		st.next(t)
	}
	if start < len(tokens) {
		stmts = append(stmts, Statement{Tokens: tokens[start:]})
	}
	return stmts
}

// splitState follows the significant tokens of one statement to tell whether
// a ";" ends it.
type splitState struct {
	n         int  // significant tokens seen
	create    bool // the statement starts with CREATE
	temp      bool // the second token is TEMP or TEMPORARY
	trigger   bool // the statement is a CREATE TRIGGER
	inBody    bool // past the BEGIN of the trigger body
	bodyEnded bool // past the END of the trigger body
	caseDepth int  // CASE expressions open
}

func (st *splitState) canEnd() bool {
	return !st.trigger || st.bodyEnded
}

func (st *splitState) next(t Token) {
	st.n++
	// CREATE TRIGGER, or CREATE TEMP|TEMPORARY TRIGGER.
	switch st.n {
	case 1:
		st.create = t.IsKeyword("CREATE")
		return
	case 2:
		if st.create {
			st.temp = t.IsKeyword("TEMP") || t.IsKeyword("TEMPORARY")
			st.trigger = t.IsKeyword("TRIGGER")
		}
		return
	case 3:
		if st.temp {
			st.trigger = t.IsKeyword("TRIGGER")
		}
		return
	}
	if !st.trigger {
		return
	}
	switch {
	case t.IsKeyword("CASE"):
		st.caseDepth++
	case t.IsKeyword("END") && st.caseDepth > 0:
		st.caseDepth--
	case t.IsKeyword("BEGIN") && !st.inBody:
		st.inBody = true
	case t.IsKeyword("END") && st.inBody:
		st.bodyEnded = true
	}
}
