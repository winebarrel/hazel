package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/winebarrel/hazel/catalog"
	"github.com/winebarrel/hazel/db"
	"github.com/winebarrel/hazel/dump"
	"github.com/winebarrel/hazel/model"
)

// Dump is the dump command.
type Dump struct {
	Filter
	NoFormat bool   `env:"HAZEL_NO_FORMAT" help:"Print the statements as SQLite keeps them, without laying them out."`
	Split    string `noconfig:"" placeholder:"DIR" help:"Write a file for each table, view and virtual table, with its indexes and triggers, to the directory. The directory is created if it does not exist; a file of the same name is overwritten."`
	DB       string `arg:"" help:"The database file, as the sqlite3 shell takes it. The file must exist."`
}

// Run prints the schema of the database as the CREATE statements that make
// it. Fed back as the schema file, the output plans clean.
func (cmd *Dump) Run(ctx context.Context, w io.Writer) error {
	target, err := db.Resolve(cmd.DB)
	if err != nil {
		return err
	}
	exists, err := target.Exists()
	if err != nil {
		return err
	}
	if !exists && !target.IsMemory() {
		return fmt.Errorf("%s: no such file", target)
	}
	conn, err := db.OpenReadOnly(target)
	if err != nil {
		return err
	}
	defer conn.Close() //nolint:errcheck
	current, err := catalog.Read(ctx, conn)
	if err != nil {
		return err
	}

	opts := dump.Options{NoFormat: cmd.NoFormat, Exclude: cmd.excludes}
	fmt.Fprintln(w, databaseLine(target, true)) //nolint:errcheck
	if cmd.Split != "" {
		return cmd.split(w, current, opts)
	}
	fmt.Fprintf(w, "-- Dump (%s)\n", summary(cmd.count(current))) //nolint:errcheck
	if out := dump.String(current, opts); out != "" {
		fmt.Fprint(w, "\n"+out) //nolint:errcheck
	}
	return nil
}

// split writes the files of dump.Files to the --split directory. Files of
// objects that are no longer in the database are left alone.
func (cmd *Dump) split(w io.Writer, current *model.Schema, opts dump.Options) error {
	files, err := dump.Files(current, opts)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cmd.Split, 0o755); err != nil {
		return err
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(cmd.Split, f.Name), []byte(f.Body), 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(w, "-- Wrote %d %s to %s\n", len(files), plural(len(files), "file", "files"), cmd.Split) //nolint:errcheck
	return nil
}
