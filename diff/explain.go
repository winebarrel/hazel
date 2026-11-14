package diff

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/winebarrel/hazel/db"
	"github.com/winebarrel/hazel/model"
	"github.com/winebarrel/hazel/parser"
)

// explainer compares expressions and views by the bytecode SQLite compiles
// them to, so that the ways of writing them that SQLite does not tell apart
// (operator spellings, extra parentheses, the case of a function name,
// number formats) make no difference. Both sides are compiled in one
// in-memory database that holds the desired tables and views: a column is
// read by its position, so the two must be compiled against the same table.
// A current expression that uses a column the desired table does not have
// fails to compile, and is different.
type explainer struct {
	conn *sql.DB
	n    int // views created for comparison so far
}

func newExplainer(desired *model.Schema) (*explainer, error) {
	conn, err := db.OpenReadWrite(db.Memory, 0)
	if err != nil {
		return nil, err
	}
	var stmts []string
	for t := range desired.Tables.Values() {
		stmts = append(stmts, t.SQL)
	}
	for v := range desired.Views.Values() {
		stmts = append(stmts, v.SQL)
	}
	for _, s := range stmts {
		if _, err := conn.Exec(s); err != nil {
			conn.Close() //nolint:errcheck
			return nil, fmt.Errorf("%s: %w", s, err)
		}
	}
	return &explainer{conn: conn}, nil
}

func (e *explainer) close() {
	e.conn.Close() //nolint:errcheck
}

// bytecode returns the program of EXPLAIN q, one instruction per line,
// without the comments.
func (e *explainer) bytecode(q string) (string, error) {
	rows, err := e.conn.Query("EXPLAIN " + q)
	if err != nil {
		return "", err
	}
	defer rows.Close() //nolint:errcheck
	var b strings.Builder
	for rows.Next() {
		var addr, p1, p2, p3 int64
		var opcode string
		var p4, p5, comment any
		if err := rows.Scan(&addr, &opcode, &p1, &p2, &p3, &p4, &p5, &comment); err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "%s %d %d %d %v %v\n", opcode, p1, p2, p3, p4, p5)
	}
	return b.String(), rows.Err()
}

// sameExpr reports whether two expressions compile to the same bytecode,
// as columns of table, or with no table when table is "".
//
// When either does not compile, they are compared by their tokens instead.
// A DEFAULT written as a bare word is a string to SQLite but does not
// compile in a SELECT, and the same text must still compare equal. A current
// expression that uses a column the desired table lacks fails on one side
// only, and its tokens differ too.
func (e *explainer) sameExpr(a, b, table string) bool {
	from := ""
	if table != "" {
		from = " FROM " + q(table)
	}
	x, errA := e.bytecode("SELECT (" + a + ")" + from)
	y, errB := e.bytecode("SELECT (" + b + ")" + from)
	if errA != nil || errB != nil {
		return sqlEqual(a, b)
	}
	return x == y
}

// sameView reports whether the current view reads the same as the desired
// one: the bytecode of SELECT * from each, and the names of their columns,
// since a column alias does not show in the bytecode. The names are compared
// with their case, unlike other names: the case is what an application
// reading the view sees. The current view is
// created next to the desired one under another name to be compiled.
func (e *explainer) sameView(cur, des *model.View) bool {
	e.n++
	tmp := fmt.Sprintf("hazel_current_view_%d", e.n)
	if _, err := e.conn.Exec(statement(cur.SQL).WithObjectName(tmp)); err != nil {
		return false
	}
	defer e.conn.Exec("DROP VIEW " + q(tmp)) //nolint:errcheck
	x, errA := e.bytecode("SELECT * FROM " + q(tmp))
	y, errB := e.bytecode("SELECT * FROM " + q(des.Name))
	if errA != nil || errB != nil || x != y {
		return false
	}
	// The names are joined with NUL, which a name cannot hold.
	var names string
	err := e.conn.QueryRow(`SELECT group_concat(name, char(0)) FROM (SELECT name FROM pragma_table_info(?, 'main') ORDER BY cid)`, tmp).Scan(&names)
	return err == nil && names == strings.Join(des.Columns, "\x00")
}

// sameIndex reports whether two indexes are the same: on the same table,
// both UNIQUE or not, with the same keys (column, DESC and collation from
// PRAGMA index_xinfo, and the expression of an expression key), and the same
// WHERE, if partial.
func (e *explainer) sameIndex(cur, des *model.Index) bool {
	if model.Key(cur.Table) != model.Key(des.Table) || cur.Unique != des.Unique ||
		cur.Partial != des.Partial || len(cur.Columns) != len(des.Columns) {
		return false
	}
	curDef := parser.ParseCreateIndex(statement(cur.SQL))
	desDef := parser.ParseCreateIndex(statement(des.SQL))
	for i, c := range cur.Columns {
		d := des.Columns[i]
		if !parser.EqualFold(c.Name, d.Name) || c.Desc != d.Desc || !parser.EqualFold(c.Collation, d.Collation) {
			return false
		}
		if c.Name == "" && !e.sameExpr(curDef.Exprs[i], desDef.Exprs[i], des.Table) {
			return false
		}
	}
	return !cur.Partial || e.sameExpr(curDef.Where, desDef.Where, des.Table)
}
