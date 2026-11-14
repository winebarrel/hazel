// Command hazel manages the schema of a SQLite database declaratively.
package main

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/winebarrel/hazel/cmd/command"
	sqlite3 "modernc.org/sqlite/lib"
)

var version = "(devel)"

type cli struct {
	Config  kong.ConfigFlag  `short:"C" name:"config" placeholder:"FILE" env:"HAZEL_CONFIG" help:"Load options from a YAML file."`
	Version kong.VersionFlag `help:"Print the version of hazel and of the SQLite it bundles."`
	Pager   *bool            `negatable:"" help:"Send the output through the command in HAZEL_PAGER even when it does not go to a terminal. --no-pager never does."`

	Plan  command.Plan  `cmd:"" help:"Print the DDL that takes the database to the schema files."`
	Apply command.Apply `cmd:"" help:"Run the DDL that takes the database to the schema files, in one transaction."`
	Dump  command.Dump  `cmd:"" help:"Print the schema of the database as SQL."`
	Fmt   command.Fmt   `cmd:"" help:"Lay out the schema files in place."`
	Diff  command.Diff  `cmd:"" help:"Print the DDL that takes one schema file to another. No database is read."`
}

func main() {
	run(os.Args[1:], os.Stdout, os.Stderr, os.Exit)
}

// run parses args and runs the command. exit stands in for os.Exit and is
// not expected to return; a test passes one that panics.
func run(args []string, stdout, stderr io.Writer, exit func(int)) {
	var cli cli
	parser, err := kong.New(&cli,
		kong.Name("hazel"),
		kong.Description("Declarative schema management for SQLite."),
		kong.Vars{"version": versionString()},
		kong.Configuration(command.YAMLConfig),
		kong.Writers(stdout, stderr),
		kong.Exit(exit),
		kong.BindTo(context.Background(), (*context.Context)(nil)),
		kong.BindTo(stdout, (*io.Writer)(nil)),
		kong.BindTo(stderr, (*command.Stderr)(nil)),
	)
	if err != nil {
		panic(err)
	}
	kctx, err := parser.Parse(args)
	parser.FatalIfErrorf(err)

	w, closePager, err := command.StartPager(stdout, cli.Pager)
	kctx.FatalIfErrorf(err)
	// The pager has to finish before the exit, which skips the defers; the
	// defer covers a panic.
	defer closePager()
	kctx.BindTo(w, (*io.Writer)(nil))

	err = kctx.Run()
	closePager()
	// --check reports changes as exit status 2, and a rebuild that is not
	// allowed as 3, with the message an error takes.
	if _, ok := errors.AsType[*command.RebuildRequiredError](err); ok {
		kctx.Errorf("%s", err)
		kctx.Exit(3)
		return
	}
	if errors.Is(err, command.ErrChanges) {
		kctx.Exit(2)
		return
	}
	kctx.FatalIfErrorf(err)
}

// versionString is what --version prints: the version of hazel, then the
// version of modernc.org/sqlite and of the SQLite it bundles.
func versionString() string {
	var b strings.Builder
	b.WriteString("hazel " + version)
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == "modernc.org/sqlite" {
				b.WriteString("\nmodernc.org/sqlite " + dep.Version)
			}
		}
	}
	b.WriteString(" (SQLite " + sqlite3.SQLITE_VERSION + ")")
	return b.String()
}
