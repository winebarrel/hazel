// Package command implements the hazel commands.
package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/winebarrel/hazel/db"
	"github.com/winebarrel/hazel/desired"
	"github.com/winebarrel/hazel/diff"
	"github.com/winebarrel/hazel/model"
)

// Stderr is where warnings go. main binds it to the standard error.
type Stderr io.Writer

// ErrChanges is returned when --check is set and there is something to do.
// main turns it into exit status 2.
var ErrChanges = errors.New("there are changes")

// RebuildRequiredError is returned when the plan rebuilds a table and
// --allow-rebuild is not set. main turns it into exit status 3.
type RebuildRequiredError struct {
	Rebuilds []diff.Rebuild
}

func (e *RebuildRequiredError) Error() string {
	n := len(e.Rebuilds)
	return fmt.Sprintf("%d %s a rebuild; pass --allow-rebuild to allow it", n, plural(n, "table needs", "tables need"))
}

// Rebuild holds --allow-rebuild.
type Rebuild struct {
	AllowRebuild bool `env:"HAZEL_ALLOW_REBUILD" help:"Allow rebuilding a table for a change ALTER TABLE cannot make. Without it, such a plan fails with exit status 3."`
}

// NoCreate holds --no-create.
type NoCreate struct {
	NoCreate bool `env:"HAZEL_NO_CREATE" help:"Fail when the database file does not exist, instead of creating it (apply) or taking it as an empty database (plan)."`
}

// refuseMissing fails when --no-create is given and the database file does
// not exist.
func (o NoCreate) refuseMissing(t db.Target, exists bool) error {
	if o.NoCreate && !exists && !t.IsMemory() {
		return fmt.Errorf("database does not exist: %s", t)
	}
	return nil
}

// Check holds --check, which the config file cannot set: it is an operation
// of one run.
type Check struct {
	Check bool `noconfig:"" help:"Exit with status 2 when there are changes."`
}

// loadFiles loads the schema files and writes their warnings.
func loadFiles(ctx context.Context, paths []string, stderr io.Writer) (*desired.Result, error) {
	srcs, err := desired.ReadFiles(paths)
	if err != nil {
		return nil, err
	}
	result, err := desired.Load(ctx, srcs)
	if err != nil {
		return nil, err
	}
	for _, w := range result.Warnings {
		fmt.Fprintf(stderr, "hazel: warning: %s\n", w) //nolint:errcheck
	}
	return result, nil
}

// refuseRebuild lists the tables the plan rebuilds and returns the error that
// refuses them, when --allow-rebuild is not set. With --check, the
// changes win: ErrChanges.
func refuseRebuild(w io.Writer, plan *diff.Plan, allowRebuild, check bool) error {
	if len(plan.Rebuilds) == 0 || allowRebuild {
		return nil
	}
	for _, r := range plan.Rebuilds {
		fmt.Fprintf(w, "-- table rebuild required: %s\n", r) //nolint:errcheck
	}
	if check {
		return ErrChanges
	}
	return &RebuildRequiredError{Rebuilds: plan.Rebuilds}
}

// writePlan writes the plan and the execute statements after it, and
// decides how the run ends.
func writePlan(w io.Writer, plan *diff.Plan, executes []string, allowRebuild, check bool) error {
	if err := refuseRebuild(w, plan, allowRebuild, check); err != nil {
		return err
	}
	if plan.IsEmpty() && len(executes) == 0 {
		fmt.Fprintln(w, "-- No changes") //nolint:errcheck
		return nil
	}
	fmt.Fprint(w, plan) //nolint:errcheck
	for _, e := range executes {
		fmt.Fprintln(w, e) //nolint:errcheck
	}
	if check {
		return ErrChanges
	}
	return nil
}

// formatExecute writes a statement marked with -- hazel:execute as the plan
// shows it: the directive with its condition, an optional note, and the
// statement.
func formatExecute(e desired.Execute, note string) string {
	directive := "-- hazel:execute"
	if e.Condition != "" {
		directive += " " + e.Condition
	}
	if note != "" {
		directive += "\n-- " + strings.Join(strings.Fields(note), " ")
	}
	sql := strings.TrimRight(e.SQL, " \t\r\n")
	if !strings.HasSuffix(sql, ";") {
		sql += ";"
	}
	return directive + "\n" + sql
}

// summary returns "2 tables, 1 view, 3 indexes, 0 triggers".
func summary(s *model.Schema) string {
	return fmt.Sprintf("%d %s, %d %s, %d %s, %d %s",
		s.Tables.Len(), plural(s.Tables.Len(), "table", "tables"),
		s.Views.Len(), plural(s.Views.Len(), "view", "views"),
		s.Indexes.Len(), plural(s.Indexes.Len(), "index", "indexes"),
		s.Triggers.Len(), plural(s.Triggers.Len(), "trigger", "triggers"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
