package command

import (
	"context"
	"database/sql"
	"fmt"
	"io"

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
	return writePlan(w, plan, planExecutes(ctx, conn, want.Executes), cmd.AllowTableRebuild, cmd.Check.Check)
}

// planExecutes evaluates the condition of each statement marked with
// -- hazel:execute and returns the statements that apply would run. plan
// runs before the DDL and reads only, so a condition that cannot be
// evaluated (it reads a table this run creates, for example) keeps its
// statement, with a note: apply decides.
func planExecutes(ctx context.Context, conn *sql.DB, executes []desired.Execute) []string {
	var out []string
	for _, e := range executes {
		note := ""
		if e.Condition != "" {
			run, err := evaluate(ctx, conn, e.Condition)
			if err != nil {
				note = "check SQL could not be evaluated at plan time: " + err.Error() + "; apply will decide"
			} else if !run {
				continue
			}
		}
		out = append(out, formatExecute(e, note))
	}
	return out
}

// evaluate runs the condition of an execute statement and reports whether
// it gives a true value.
func evaluate(ctx context.Context, conn interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, cond string,
) (bool, error) {
	var run bool
	err := conn.QueryRowContext(ctx, cond).Scan(&run)
	return run, err
}

// databaseLine returns the comment that names the database.
func databaseLine(t db.Target, exists bool) string {
	line := "-- Database: " + t.String()
	if !exists && !t.IsMemory() {
		line += " (new file)"
	}
	return line
}
