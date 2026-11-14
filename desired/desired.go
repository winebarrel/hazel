// Package desired loads the desired schema from schema files.
package desired

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/winebarrel/hazel/catalog"
	"github.com/winebarrel/hazel/db"
	"github.com/winebarrel/hazel/format"
	"github.com/winebarrel/hazel/model"
	"github.com/winebarrel/hazel/parser"
	"modernc.org/sqlite"
)

// Source is the content of one schema file.
type Source struct {
	Name string
	Body string
}

// ReadFiles reads the schema files.
func ReadFiles(paths []string) ([]Source, error) {
	srcs := make([]Source, len(paths))
	for i, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		srcs[i] = Source{Name: p, Body: string(b)}
	}
	return srcs, nil
}

// Warning reports a statement that was ignored.
type Warning struct {
	File string
	Line int
	SQL  string
}

// String reports the first line of the statement only, so that a long INSERT
// left in a schema file does not fill the output.
func (w Warning) String() string {
	sql, rest, cut := strings.Cut(w.SQL, "\n")
	if cut && strings.TrimSpace(rest) != "" {
		sql += " ..."
	}
	return fmt.Sprintf("%s:%d: ignored unsupported statement: %s", w.File, w.Line, sql)
}

// statement is a statement with where it was written.
type statement struct {
	file string
	stmt parser.Statement
}

// sql returns the statement as hazel runs it, and as SQLite then keeps it:
// without its directives and its ";", and laid out by format.Statement.
func (s statement) sql() string {
	sql := strings.TrimSpace(s.stmt.WithoutDirectives())
	return format.Statement(strings.TrimSuffix(sql, ";"))
}

func (s statement) errorf(err error) error {
	return fmt.Errorf("%s:%d: %w", s.file, s.stmt.Line(), err)
}

// Result is the desired schema loaded from schema files.
type Result struct {
	Schema *model.Schema
	// Warnings report the statements that were ignored.
	Warnings []Warning
	// Executes are the statements marked with -- hazel:execute, in the order
	// written. They are not run here.
	Executes []Execute
}

// Execute is a statement marked with -- hazel:execute, to run after the DDL.
type Execute struct {
	File string
	Line int
	SQL  string
	// Condition is the SQL given after the directive, or "". The statement
	// runs only when it gives a true value.
	Condition string
}

// Load executes the CREATE statements of the sources in an in-memory
// database and reads the schema back, so that SQLite validates it. Any other
// statement is not executed; it is reported as a warning, or kept as an
// Execute when it is marked with -- hazel:execute.
//
// The statements run in the order tables (virtual tables included), indexes,
// views and triggers, so the order they are written in does not matter.
// SQLite does not check what a view reads when it creates the view, so views
// need no order among themselves; a view that reads a missing table fails
// when its columns are read.
//
// The directives are read from the comments before a statement or a column
// definition, and are removed from the SQL that runs, so that the schema
// read back does not hold them. A table, a view or a virtual table marked
// with -- hazel:ignore runs like any other, so that what depends on it can
// be created, and is marked Ignored in the schema. Its indexes and triggers
// are ignored with it; they cannot be marked on their own.
func Load(ctx context.Context, srcs []Source) (*Result, error) {
	l := &loader{
		result:       &Result{},
		phases:       make([][]statement, triggerPhase+1),
		ignored:      map[parser.StatementKind]map[string]bool{},
		tableRename:  map[string]string{},
		skipFKCheck:  map[string]bool{},
		columnRename: map[string]map[string]string{},
		backfill:     map[string]map[string]backfill{},
	}
	for _, src := range srcs {
		if err := l.read(src); err != nil {
			return nil, err
		}
	}

	conn, err := db.OpenReadWrite(db.Memory, 0)
	if err != nil {
		return nil, err
	}
	defer conn.Close() //nolint:errcheck
	if err := execAll(ctx, conn, l.phases); err != nil {
		return nil, err
	}
	schema, err := catalog.Read(ctx, conn)
	if err != nil {
		return nil, err
	}
	if err := l.mark(schema); err != nil {
		return nil, err
	}
	l.result.Schema = schema
	return l.result, nil
}

type loader struct {
	result *Result
	phases [][]statement
	// ignored holds the keys of the objects marked with -- hazel:ignore, by
	// the kind of their statement.
	ignored map[parser.StatementKind]map[string]bool
	// tableRename maps a table's key to its old name.
	tableRename map[string]string
	// skipFKCheck holds the keys of the tables marked with
	// -- hazel:skip-foreign-key-check.
	skipFKCheck map[string]bool
	// columnRename maps a table's key and a column's key to the column's old
	// name.
	columnRename map[string]map[string]string
	// backfill maps a table's key and a column's key to the column's
	// -- hazel:backfill.
	backfill map[string]map[string]backfill
}

// backfill is a -- hazel:backfill directive and where it is written.
type backfill struct {
	expr string
	file string
	line int
}

func (l *loader) read(src Source) error {
	tokens, err := parser.Tokenize(src.Body)
	if err != nil {
		return located(src.Name, err)
	}
	for _, stmt := range parser.Split(tokens) {
		if stmt.IsEmpty() {
			continue
		}
		if err := l.readStatement(src.Name, stmt); err != nil {
			return err
		}
	}
	return nil
}

// located reports err, a *parser.Error, as file:line.
func located(file string, err error) error {
	var perr *parser.Error
	errors.As(err, &perr)
	return fmt.Errorf("%s:%d: %w", file, perr.Line, perr.Err)
}

func (l *loader) readStatement(file string, stmt parser.Statement) error {
	ds, err := stmt.Directives()
	if err != nil {
		return located(file, err)
	}
	kind := stmt.Classify()
	key := model.Key(stmt.ObjectName())
	misplaced := func(d parser.Directive, what string) error {
		return fmt.Errorf("%s:%d: %s%s is for %s", file, d.Line, parser.DirectivePrefix, d.Name, what)
	}
	execute := false
	for _, d := range ds {
		switch d.Name {
		case parser.Execute:
			if kind != parser.Unsupported {
				return misplaced(d, "a statement that hazel does not manage")
			}
			l.result.Executes = append(l.result.Executes,
				Execute{File: file, Line: stmt.Line(), SQL: stmt.WithoutDirectives(), Condition: d.Arg})
			execute = true
		case parser.Ignore:
			// Only what stands on its own can be ignored. An index or a
			// trigger belongs to its table or view, and is ignored with it.
			if kind != parser.CreateTable && kind != parser.CreateVirtualTable && kind != parser.CreateView {
				return misplaced(d, "a table, a view or a virtual table")
			}
			if l.ignored[kind] == nil {
				l.ignored[kind] = map[string]bool{}
			}
			l.ignored[kind][key] = true
		case parser.Backfill:
			return misplaced(d, "a column")
		case parser.SkipForeignKeyCheck:
			if kind != parser.CreateTable {
				return misplaced(d, "a table")
			}
			l.skipFKCheck[key] = true
		case parser.RenamedFrom:
			if kind != parser.CreateTable {
				return misplaced(d, "a table or a column")
			}
			name, ok := parser.ParseName(d.Arg)
			if !ok {
				return needsName(file, d)
			}
			l.tableRename[key] = name
		}
	}
	if kind == parser.CreateTable {
		if err := l.readColumns(file, key, stmt); err != nil {
			return err
		}
	}

	phase, ok := phaseOf[kind]
	if !ok {
		if !execute {
			l.result.Warnings = append(l.result.Warnings, Warning{File: file, Line: stmt.Line(), SQL: stmt.SQL()})
		}
		return nil
	}
	l.phases[phase] = append(l.phases[phase], statement{file: file, stmt: stmt})
	return nil
}

// readColumns reads the directives of the column definitions. Only
// -- hazel:renamed-from and -- hazel:backfill can be written there.
func (l *loader) readColumns(file, table string, stmt parser.Statement) error {
	for _, e := range parser.TableElements(stmt) {
		ds, err := e.Directives()
		if err != nil {
			return located(file, err)
		}
		for _, d := range ds {
			switch {
			case d.Name != parser.RenamedFrom && d.Name != parser.Backfill || e.Column == "":
				return fmt.Errorf("%s:%d: %s%s cannot be written before a column or a table constraint",
					file, d.Line, parser.DirectivePrefix, d.Name)
			case d.Name == parser.Backfill:
				if d.Arg == "" {
					return fmt.Errorf("%s:%d: %s%s needs an expression", file, d.Line, parser.DirectivePrefix, d.Name)
				}
				if l.backfill[table] == nil {
					l.backfill[table] = map[string]backfill{}
				}
				l.backfill[table][model.Key(e.Column)] = backfill{expr: d.Arg, file: file, line: d.Line}
				continue
			}
			name, ok := parser.ParseName(d.Arg)
			if !ok {
				return needsName(file, d)
			}
			if l.columnRename[table] == nil {
				l.columnRename[table] = map[string]string{}
			}
			l.columnRename[table][model.Key(e.Column)] = name
		}
	}
	return nil
}

// needsName reports a renamed-from whose argument is not one name.
func needsName(file string, d parser.Directive) error {
	return fmt.Errorf("%s:%d: %s%s needs one name, the old one, as it is written in SQL", file, d.Line, parser.DirectivePrefix, d.Name)
}

// mark puts the directives on the schema read back. A backfill of a
// generated column, which has no value of its own to fill, is an error.
func (l *loader) mark(s *model.Schema) error {
	for key, t := range s.Tables.All() {
		t.RenamedFrom = l.tableRename[key]
		t.SkipForeignKeyCheck = l.skipFKCheck[key]
		t.Ignored = l.ignored[parser.CreateTable][key] || l.ignored[parser.CreateVirtualTable][key]
		for ckey, c := range t.Columns.All() {
			c.RenamedFrom = l.columnRename[key][ckey]
			if b, ok := l.backfill[key][ckey]; ok {
				if c.Generated != model.NotGenerated {
					return fmt.Errorf("%s:%d: %s%s cannot fill a generated column: %s",
						b.file, b.line, parser.DirectivePrefix, parser.Backfill, c.Name)
				}
				c.Backfill = b.expr
			}
		}
	}
	for key, x := range s.Views.All() {
		x.Ignored = l.ignored[parser.CreateView][key]
	}
	return nil
}

// The phases the statements run in.
const (
	tablePhase = iota
	indexPhase
	viewPhase
	triggerPhase
)

var phaseOf = map[parser.StatementKind]int{
	parser.CreateTable:        tablePhase,
	parser.CreateVirtualTable: tablePhase,
	parser.CreateIndex:        indexPhase,
	parser.CreateView:         viewPhase,
	parser.CreateTrigger:      triggerPhase,
}

// execAll runs the statements under an authorizer. The statements were
// classified by their first keywords, and the authorizer refuses what a
// CREATE statement does not need, ATTACH and PRAGMA among them, in case
// anything got past that.
func execAll(ctx context.Context, pool *sql.DB, phases [][]statement) error {
	conn, err := pool.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close() //nolint:errcheck
	if err := setAuthorizer(conn, authorize); err != nil {
		return err
	}
	for _, stmts := range phases {
		for _, s := range stmts {
			if _, err := conn.ExecContext(ctx, s.sql()); err != nil {
				return s.errorf(err)
			}
		}
	}
	return setAuthorizer(conn, nil)
}

// setAuthorizer installs fn on the connection, or removes the authorizer when
// fn is nil. The driver is always modernc.org/sqlite.
func setAuthorizer(conn *sql.Conn, fn sqlite.AuthorizerFn) error {
	return conn.Raw(func(dc any) error {
		return dc.(sqlite.AuthorizerRegisterer).RegisterAuthorizer(fn)
	})
}

// allowed lists the actions that the CREATE statements hazel manages need.
// SQLite writes sqlite_schema with INSERT and UPDATE, a virtual table module
// writes its shadow tables (rtree and geopoly also DELETE from them), and
// CREATE INDEX presents REINDEX. No CREATE statement deletes from a user
// table, since trigger bodies do not run, and the database is a scratch one.
var allowed = map[sqlite.AuthorizerActionCode]bool{
	sqlite.AuthCreateTable:   true,
	sqlite.AuthCreateIndex:   true,
	sqlite.AuthCreateView:    true,
	sqlite.AuthCreateTrigger: true,
	sqlite.AuthCreateVTable:  true,
	sqlite.AuthInsert:        true,
	sqlite.AuthUpdate:        true,
	sqlite.AuthDelete:        true,
	sqlite.AuthRead:          true,
	sqlite.AuthSelect:        true,
	sqlite.AuthFunction:      true,
	sqlite.AuthRecursive:     true,
	sqlite.AuthReindex:       true,
}

// authorize allows the actions in allowed on the main schema. CREATE TABLE
// temp.x presents CREATE TABLE on "temp", not CREATE TEMP TABLE, so the
// database is checked too. Functions and SELECT have no database. The one
// PRAGMA allowed is reading page_size, which rtree and geopoly do to size
// their nodes.
func authorize(action sqlite.AuthorizerActionCode, arg1, arg2, database, _ string) sqlite.AuthorizerReturnCode {
	if database != "main" && database != "" {
		return sqlite.AuthorizerDeny
	}
	if allowed[action] || (action == sqlite.AuthPragma && arg1 == "page_size" && arg2 == "") {
		return sqlite.AuthorizerOK
	}
	return sqlite.AuthorizerDeny
}
