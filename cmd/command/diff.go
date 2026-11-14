package command

import (
	"context"
	"io"

	"github.com/winebarrel/hazel/diff"
)

// Diff is the diff command.
type Diff struct {
	Filter
	Rebuild
	Check
	Old string `arg:"" name:"old-schema-file" help:"The schema file to take as the current schema."`
	New string `arg:"" name:"new-schema-file" help:"The schema file to take as the desired schema."`
}

// Run prints the DDL that takes one schema file to the other. No database is
// read.
func (cmd *Diff) Run(ctx context.Context, w io.Writer, stderr Stderr) error {
	current, err := loadFiles(ctx, []string{cmd.Old}, stderr)
	if err != nil {
		return err
	}
	want, err := loadFiles(ctx, []string{cmd.New}, stderr)
	if err != nil {
		return err
	}
	plan, err := diff.Diff(cmd.Apply(current), cmd.Apply(want))
	if err != nil {
		return err
	}
	return writePlan(w, plan, cmd.AllowTableRebuild, cmd.Check.Check)
}
