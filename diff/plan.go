// Package diff computes the DDL that takes the current schema to the desired
// one.
package diff

import (
	"cmp"
	"slices"
	"strings"
)

// Plan is the DDL that takes the current schema to the desired one.
type Plan struct {
	// DataLoss lists what the plan drops along with its data: tables and
	// columns, as "table  name" and "column table.name".
	DataLoss []string
	// Rebuilds lists the tables the plan rebuilds, which copies every row.
	Rebuilds []string
	// Groups are the statements in the order they run, grouped by the
	// comment that explains them.
	Groups []Group
}

// Group is a run of statements that share a comment. Comment is "" for
// statements that need no explanation, such as a CREATE.
type Group struct {
	Comment string
	Stmts   []string
}

// Statements returns every statement of the plan in the order it runs.
func (p *Plan) Statements() []string {
	var stmts []string
	for _, g := range p.Groups {
		stmts = append(stmts, g.Stmts...)
	}
	return stmts
}

// IsEmpty reports whether the plan has no statement.
func (p *Plan) IsEmpty() bool {
	return len(p.Groups) == 0
}

// String returns the plan as SQL, with the data loss warning first and each
// group after its comment.
func (p *Plan) String() string {
	var b strings.Builder
	if len(p.DataLoss) > 0 {
		b.WriteString("-- WARNING: the following will be dropped and their data will be lost:\n")
		for _, d := range p.DataLoss {
			b.WriteString("--   ")
			b.WriteString(d)
			b.WriteString("\n")
		}
		b.WriteString("--\n")
	}
	for _, g := range p.Groups {
		if g.Comment != "" {
			b.WriteString("-- ")
			b.WriteString(g.Comment)
			b.WriteString("\n")
		}
		for _, s := range g.Stmts {
			b.WriteString(s)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// builder collects the statements of a plan by phase.
type builder struct {
	dataLoss []string
	rebuilds []string
	phases   [phaseCount][]item
}

type item struct {
	comment string
	stmt    string
}

// The phases of a plan, in the order they run. Views and triggers are dropped
// first and created last, so that nothing they read is missing while the
// tables change. SQLite checks little else when objects are created or
// dropped, so no other order is needed.
const (
	dropViewsAndTriggers = iota
	dropIndexes
	renameTables
	alterTables
	rebuildTables
	dropTables
	createTables
	createIndexes
	createViews
	createTriggers
	phaseCount
)

func lossOrder(s string) int {
	if strings.HasPrefix(s, "table") {
		return 0
	}
	return 1
}

func (b *builder) add(phase int, comment, stmt string) {
	b.phases[phase] = append(b.phases[phase], item{comment, stmt + ";"})
}

// plan groups the statements: a statement joins the group before it when the
// two share a comment.
func (b *builder) plan() *Plan {
	// Tables come before columns in the warning.
	slices.SortStableFunc(b.dataLoss, func(x, y string) int {
		return cmp.Compare(lossOrder(x), lossOrder(y))
	})
	p := &Plan{DataLoss: b.dataLoss, Rebuilds: b.rebuilds}
	for _, items := range b.phases {
		for _, it := range items {
			if n := len(p.Groups); n > 0 && p.Groups[n-1].Comment == it.comment {
				p.Groups[n-1].Stmts = append(p.Groups[n-1].Stmts, it.stmt)
				continue
			}
			p.Groups = append(p.Groups, Group{Comment: it.comment, Stmts: []string{it.stmt}})
		}
	}
	return p
}
