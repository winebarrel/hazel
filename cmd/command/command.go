// Package command implements the hazel commands.
package command

import (
	"context"
	"errors"
	"fmt"
	"io"

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
// --allow-table-rebuild is not set. main turns it into exit status 3.
type RebuildRequiredError struct {
	Rebuilds []diff.Rebuild
}

func (e *RebuildRequiredError) Error() string {
	n := len(e.Rebuilds)
	return fmt.Sprintf("%d %s a rebuild; pass --allow-table-rebuild to allow it", n, plural(n, "table needs", "tables need"))
}

// Rebuild holds --allow-table-rebuild.
type Rebuild struct {
	AllowTableRebuild bool `env:"HAZEL_ALLOW_TABLE_REBUILD" help:"Allow rebuilding a table for a change ALTER TABLE cannot make. Without it, such a plan fails with exit status 3."`
}

// Check holds --check, which the config file cannot set: it is an operation
// of one run.
type Check struct {
	Check bool `noconfig:"" help:"Exit with status 2 when there are changes."`
}

// loadFiles loads the schema files and writes their warnings.
func loadFiles(ctx context.Context, paths []string, stderr io.Writer) (*model.Schema, error) {
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
	return result.Schema, nil
}

// writePlan writes the plan, and decides how the run ends. A plan that
// rebuilds a table without --allow-table-rebuild is not written: the tables
// and their changes are listed instead.
func writePlan(w io.Writer, plan *diff.Plan, allowRebuild, check bool) error {
	if len(plan.Rebuilds) > 0 && !allowRebuild {
		for _, r := range plan.Rebuilds {
			fmt.Fprintf(w, "-- table rebuild required: %s\n", r) //nolint:errcheck
		}
		if check {
			return ErrChanges
		}
		return &RebuildRequiredError{Rebuilds: plan.Rebuilds}
	}
	if plan.IsEmpty() {
		fmt.Fprintln(w, "-- No changes") //nolint:errcheck
		return nil
	}
	fmt.Fprint(w, plan) //nolint:errcheck
	if check {
		return ErrChanges
	}
	return nil
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
