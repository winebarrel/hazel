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

// Split splits tokens into statements at each ";". A CREATE TRIGGER does not
// end at the ";" inside its body: like sqlite3_complete() (src/complete.c), it
// ends at the ";" of a ";" END ";" sequence. A body cannot be empty, and the
// END of a CASE expression never comes right after a ";", so an END there is
// always the end of the body, even where END is used as an identifier.
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
	n       int  // significant tokens seen
	create  bool // the statement starts with CREATE
	temp    bool // the second token is TEMP or TEMPORARY
	trigger bool // the statement is a CREATE TRIGGER
	semi    bool // in a trigger, the last token was ";"
	end     bool // in a trigger, the last tokens were ";" END
}

func (st *splitState) canEnd() bool {
	return !st.trigger || st.end
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
	if st.trigger {
		st.end = st.semi && t.IsKeyword("END")
		st.semi = t.IsPunct(";")
	}
}
