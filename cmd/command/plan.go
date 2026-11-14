package command

import (
	"context"
	"fmt"
	"io"

	"github.com/winebarrel/hazel/catalog"
	"github.com/winebarrel/hazel/db"
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

// Run prints the DDL that takes the database to the schema files.
func (cmd *Plan) Run(ctx context.Context, w io.Writer, stderr Stderr) error {
	target, err := db.Resolve(cmd.DB)
	if err != nil {
		return err
	}
	exists, err := target.Exists()
	if err != nil {
		return err
	}
	current := model.NewSchema()
	if exists {
		conn, err := db.OpenReadOnly(target)
		if err != nil {
			return err
		}
		defer conn.Close() //nolint:errcheck
		current, err = catalog.Read(ctx, conn)
		if err != nil {
			return err
		}
	}
	want, err := loadFiles(ctx, cmd.Files, stderr)
	if err != nil {
		return err
	}
	current, want = cmd.Apply(current), cmd.Apply(want)
	plan, err := diff.Diff(current, want)
	if err != nil {
		return err
	}

	fmt.Fprintln(w, databaseLine(target, exists))   //nolint:errcheck
	fmt.Fprintf(w, "-- Plan (%s)\n", summary(want)) //nolint:errcheck
	return writePlan(w, plan, cmd.AllowTableRebuild, cmd.Check.Check)
}

// databaseLine returns the comment that names the database.
func databaseLine(t db.Target, exists bool) string {
	line := "-- Database: " + t.String()
	if !exists && !t.IsMemory() {
		line += " (new file)"
	}
	return line
}
