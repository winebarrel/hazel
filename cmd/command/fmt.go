package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/winebarrel/hazel/desired"
	"github.com/winebarrel/hazel/diff"
	"github.com/winebarrel/hazel/format"
)

// Fmt is the fmt command.
type Fmt struct {
	Check            bool     `noconfig:"" help:"Write nothing. Print the files that are not formatted, and exit with status 2 when there are any."`
	StripRenamedFrom bool     `noconfig:"" help:"Drop the -- hazel:renamed-from directives, once the renames are applied."`
	Files            []string `arg:"" name:"schema-file" help:"The schema files."`
}

// Run lays out the schema files in place, as format.File does. The files
// are loaded together before and after, and nothing is written unless the
// two give the same schema: a view may read a table of another file.
func (cmd *Fmt) Run(ctx context.Context, w io.Writer) error {
	srcs, err := desired.ReadFiles(cmd.Files)
	if err != nil {
		return err
	}
	before, err := desired.Load(ctx, srcs)
	if err != nil {
		return err
	}
	formatted := make([]desired.Source, len(srcs))
	for i, src := range srcs {
		formatted[i] = desired.Source{Name: src.Name, Body: format.File(src.Body, cmd.StripRenamedFrom)}
	}
	if err := sameSchema(ctx, before, formatted); err != nil {
		return fmt.Errorf("formatting would change the schema, so no file was written: %w", err)
	}

	// The files that change are named: the ones that are rewritten, or with
	// --check, the ones that would be.
	changed := false
	for i, src := range formatted {
		if src.Body == srcs[i].Body {
			continue
		}
		changed = true
		if !cmd.Check {
			if err := writeFile(src.Name, src.Body); err != nil {
				return err
			}
		}
		fmt.Fprintln(w, src.Name) //nolint:errcheck
	}
	if cmd.Check && changed {
		return ErrChanges
	}
	return nil
}

// sameSchema reports an error unless the formatted files give the schema and
// the -- hazel:execute statements that the files gave before.
func sameSchema(ctx context.Context, before *desired.Result, formatted []desired.Source) error {
	after, err := desired.Load(ctx, formatted)
	if err != nil {
		return err
	}
	plan, err := diff.Diff(before.Schema, after.Schema)
	if err != nil {
		return err
	}
	same := slices.EqualFunc(before.Executes, after.Executes, func(a, b desired.Execute) bool {
		return a.SQL == b.SQL && a.Condition == b.Condition
	})
	if !plan.IsEmpty() || !same {
		return fmt.Errorf("the plan from the files to the formatted files is not empty:\n%s", plan)
	}
	return nil
}

// writeFile writes body to a new file next to the one at path and renames it
// over that one, so that a failure partway through leaves the file as it was.
// The new file keeps the mode of the old one. A symlink is resolved first:
// renaming over the link would replace it with a plain file and leave the
// file it points at as it was.
func writeFile(path, body string) error {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".hazel-*")
	if err != nil {
		return err
	}
	defer func() {
		tmp.Close()           //nolint:errcheck
		os.Remove(tmp.Name()) //nolint:errcheck
	}()
	if _, err := tmp.WriteString(body); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), info.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}
