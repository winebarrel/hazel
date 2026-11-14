// Package desired loads the desired schema from schema files.
package desired

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/winebarrel/hazel/catalog"
	"github.com/winebarrel/hazel/db"
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

func (w Warning) String() string {
	return fmt.Sprintf("%s:%d: ignored unsupported statement: %s", w.File, w.Line, w.SQL)
}

// statement is a statement with where it was written.
type statement struct {
	file string
	stmt parser.Statement
}

func (s statement) errorf(err error) error {
	return fmt.Errorf("%s:%d: %w", s.file, s.stmt.Line(), err)
}

// Load executes the CREATE statements of the sources in an in-memory
// database and reads the schema back, so that SQLite validates it. Any other
// statement is not executed; it is reported as a warning.
//
// The statements run in the order tables (virtual tables included), indexes,
// views and triggers, so the order they are written in does not matter.
// SQLite does not check what a view reads when it creates the view, so views
// need no order among themselves; a view that reads a missing table fails
// when its columns are read.
func Load(ctx context.Context, srcs []Source) (*model.Schema, []Warning, error) {
	var warnings []Warning
	phases := make([][]statement, triggerPhase+1)
	for _, src := range srcs {
		tokens, err := parser.Tokenize(src.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", src.Name, err)
		}
		for _, stmt := range parser.Split(tokens) {
			if stmt.IsEmpty() {
				continue
			}
			phase, ok := phaseOf[stmt.Classify()]
			if !ok {
				warnings = append(warnings, Warning{File: src.Name, Line: stmt.Line(), SQL: stmt.SQL()})
				continue
			}
			phases[phase] = append(phases[phase], statement{file: src.Name, stmt: stmt})
		}
	}

	conn, err := db.OpenReadWrite(db.Memory, 0)
	if err != nil {
		return nil, nil, err
	}
	defer conn.Close() //nolint:errcheck
	if err := execAll(ctx, conn, phases); err != nil {
		return nil, nil, err
	}
	schema, err := catalog.Read(ctx, conn)
	if err != nil {
		return nil, nil, err
	}
	return schema, warnings, nil
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
			if _, err := conn.ExecContext(ctx, s.stmt.SQL()); err != nil {
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
// writes its shadow tables, and CREATE INDEX presents REINDEX.
var allowed = map[sqlite.AuthorizerActionCode]bool{
	sqlite.AuthCreateTable:   true,
	sqlite.AuthCreateIndex:   true,
	sqlite.AuthCreateView:    true,
	sqlite.AuthCreateTrigger: true,
	sqlite.AuthCreateVTable:  true,
	sqlite.AuthInsert:        true,
	sqlite.AuthUpdate:        true,
	sqlite.AuthRead:          true,
	sqlite.AuthSelect:        true,
	sqlite.AuthFunction:      true,
	sqlite.AuthRecursive:     true,
	sqlite.AuthReindex:       true,
}

// authorize allows the actions in allowed on the main schema. CREATE TABLE
// temp.x presents CREATE TABLE on "temp", not CREATE TEMP TABLE, so the
// database is checked too. Functions and SELECT have no database.
func authorize(action sqlite.AuthorizerActionCode, _, _, database, _ string) sqlite.AuthorizerReturnCode {
	if allowed[action] && (database == "main" || database == "") {
		return sqlite.AuthorizerOK
	}
	return sqlite.AuthorizerDeny
}
