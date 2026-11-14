package command

import (
	"context"
	"fmt"
	"io"

	"github.com/winebarrel/hazel/catalog"
	"github.com/winebarrel/hazel/db"
	"github.com/winebarrel/hazel/dump"
)

// Dump is the dump command.
type Dump struct {
	Filter
	NoFormat bool   `env:"HAZEL_NO_FORMAT" help:"Print the statements as SQLite keeps them, without laying them out."`
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

	fmt.Fprintln(w, databaseLine(target, true))                   //nolint:errcheck
	fmt.Fprintf(w, "-- Dump (%s)\n", summary(cmd.count(current))) //nolint:errcheck
	if out := dump.String(current, dump.Options{NoFormat: cmd.NoFormat, Exclude: func(name string) bool { return !cmd.selects(name) }}); out != "" {
		fmt.Fprint(w, "\n"+out) //nolint:errcheck
	}
	return nil
}
