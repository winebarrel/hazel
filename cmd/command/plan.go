package command

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/winebarrel/hazel/catalog"
	"github.com/winebarrel/hazel/db"
	"github.com/winebarrel/hazel/desired"
	"github.com/winebarrel/hazel/diff"
	"github.com/winebarrel/hazel/model"
)

// Plan is the plan command.
type Plan struct {
	Filter
	Rebuild
	Check
	DB    string   `arg:"" help:"The database file, as the sqlite3 shell takes it. A file that does not exist is taken as an empty database."`
	Files []string `arg:"" name:"schema-file" help:"The schema files."`
}

// Run prints the DDL that takes the database to the schema files, and the
// statements marked with -- hazel:execute whose condition holds.
func (cmd *Plan) Run(ctx context.Context, w io.Writer, stderr Stderr) error {
	target, err := db.Resolve(cmd.DB)
	if err != nil {
		return err
	}
	exists, err := target.Exists()
	if err != nil {
		return err
	}
	want, err := loadFiles(ctx, cmd.Files, stderr)
	if err != nil {
		return err
	}
	// A database that does not exist is read as an empty one, and the
	// conditions are evaluated on it.
	opened := db.Memory
	if exists {
		opened = target
	}
	conn, err := db.OpenReadOnly(opened)
	if err != nil {
		return err
	}
	defer conn.Close() //nolint:errcheck
	current := model.NewSchema()
	if exists {
		if current, err = catalog.Read(ctx, conn); err != nil {
			return err
		}
	}
	plan, err := diff.Diff(current, want.Schema, cmd.option())
	if err != nil {
		return err
	}

	fmt.Fprintln(w, databaseLine(target, exists))                     //nolint:errcheck
	fmt.Fprintf(w, "-- Plan (%s)\n", summary(cmd.count(want.Schema))) //nolint:errcheck
	executes, err := planExecutes(ctx, conn, want.Executes)
	if err != nil {
		return err
	}
	return writePlan(w, plan, executes, cmd.AllowTableRebuild, cmd.Check.Check)
}

// planExecutes evaluates the condition of each statement marked with
// -- hazel:execute and returns the statements to run. A condition that
// fails (it reads a table this run creates, for example) keeps its
// statement, with a note, and apply decides. A condition that returns
// something other than 0 or 1 is an error.
func planExecutes(ctx context.Context, conn *sql.DB, executes []desired.Execute) ([]string, error) {
	var out []string
	for _, e := range executes {
		note := ""
		if e.Condition != "" {
			run, err := evaluate(ctx, conn, e.Condition)
			_, nb := errors.AsType[*NotBooleanError](err)
			switch {
			case nb:
				return nil, err
			case err != nil:
				note = "check SQL could not be evaluated at plan time: " + err.Error() + "; apply will decide"
			case !run:
				continue
			}
		}
		out = append(out, formatExecute(e, note))
	}
	return out, nil
}

// NotBooleanError is returned when the condition of an execute statement
// does not return 0 or 1.
type NotBooleanError struct {
	Condition string
	Got       string
}

func (e *NotBooleanError) Error() string {
	return fmt.Sprintf("the condition of hazel:execute must return 0 or 1: %s: got %s", e.Condition, e.Got)
}

// evaluate runs the condition of an execute statement and reports whether
// it returns 1. Any result other than 0 or 1 is a *NotBooleanError.
func evaluate(ctx context.Context, conn interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, cond string,
) (bool, error) {
	var v any
	if err := conn.QueryRowContext(ctx, cond).Scan(&v); errors.Is(err, sql.ErrNoRows) {
		return false, &NotBooleanError{Condition: cond, Got: "no rows"}
	} else if err != nil {
		return false, err
	}
	if n, ok := v.(int64); ok && (n == 0 || n == 1) {
		return n == 1, nil
	}
	return false, &NotBooleanError{Condition: cond, Got: sqlValue(v)}
}

// sqlValue writes a value read from SQLite as a SQL literal.
func sqlValue(v any) string {
	switch v := v.(type) {
	case nil:
		return "NULL"
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		s := strconv.FormatFloat(v, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eIN") {
			s += ".0"
		}
		return s
	case string:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	default: // []byte
		return fmt.Sprintf("X'%X'", v)
	}
}

// databaseLine returns the comment that names the database.
func databaseLine(t db.Target, exists bool) string {
	line := "-- Database: " + t.String()
	if !exists && !t.IsMemory() {
		line += " (new file)"
	}
	return line
}
