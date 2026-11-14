package command

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/winebarrel/hazel/catalog"
	"github.com/winebarrel/hazel/db"
	"github.com/winebarrel/hazel/diff"
	"github.com/winebarrel/hazel/model"
	"github.com/winebarrel/hazel/parser"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Apply is the apply command.
type Apply struct {
	Filter
	Rebuild
	NoCreate
	BusyTimeout         time.Duration `default:"5s" env:"HAZEL_BUSY_TIMEOUT" placeholder:"DURATION" help:"How long to wait for the write lock, as a Go duration such as 5s."`
	SkipForeignKeyCheck bool          `env:"HAZEL_SKIP_FOREIGN_KEY_CHECK" help:"Do not run PRAGMA foreign_key_check before the commit. For a database that holds rows breaking a foreign key from before."`
	DB                  string        `arg:"" help:"The database file, as the sqlite3 shell takes it. A file that does not exist is created, unless --no-create is given."`
	Files               []string      `arg:"" name:"schema-file" help:"The schema files."`
}

// Validate checks --busy-timeout and the patterns. kong reports an error
// here as a usage error, exit status 80.
func (cmd *Apply) Validate() error {
	if cmd.BusyTimeout <= 0 {
		return fmt.Errorf("--busy-timeout must be greater than 0: %s", cmd.BusyTimeout)
	}
	return cmd.Filter.Validate()
}

// Run applies the plan in one transaction:
//
//  1. make sure PRAGMA foreign_keys is off, outside the transaction, where
//     it can be changed;
//  2. BEGIN IMMEDIATE, which waits up to --busy-timeout for the write lock;
//  3. read the database and compute the plan under the lock;
//  4. run the DDL, then the statements marked with -- hazel:execute whose
//     condition holds, evaluated after the DDL;
//  5. PRAGMA foreign_key_check, and roll back on a violation, unless
//     --skip-foreign-key-check is given; a table marked with
//     -- hazel:skip-foreign-key-check is left out of it;
//  6. COMMIT.
//
// A statement that fails rolls everything back.
func (cmd *Apply) Run(ctx context.Context, w io.Writer, stderr Stderr) error {
	target, err := db.Resolve(cmd.DB)
	if err != nil {
		return err
	}
	exists, err := target.Exists()
	if err != nil {
		return err
	}
	if err := cmd.refuseMissing(target, exists); err != nil {
		return err
	}
	want, err := loadFiles(ctx, cmd.Files, stderr)
	if err != nil {
		return err
	}
	pool, err := db.OpenReadWrite(target, cmd.BusyTimeout)
	if err != nil {
		return err
	}
	defer pool.Close() //nolint:errcheck
	// The pragmas and the transaction belong to one connection.
	conn, err := pool.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close() //nolint:errcheck

	// foreign_keys is a setting of the connection, off by default, and
	// cannot be changed inside a transaction. A table rebuild needs it off:
	// with it on, the DROP TABLE would delete or fail on the rows that refer
	// to the table.
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		if isBusy(err) {
			return fmt.Errorf("could not lock the database within %s: %w", cmd.BusyTimeout, err)
		}
		return err
	}
	committed := false
	defer func() {
		if !committed {
			conn.ExecContext(ctx, "ROLLBACK") //nolint:errcheck
		}
	}()

	current, err := catalog.Read(ctx, conn)
	if err != nil {
		return err
	}
	plan, err := diff.Diff(current, want.Schema, cmd.option())
	if err != nil {
		return err
	}
	fmt.Fprintln(w, databaseLine(target, exists))                     //nolint:errcheck
	fmt.Fprintf(w, "-- Plan (%s)\n", summary(cmd.count(want.Schema))) //nolint:errcheck
	if err := refuseRebuild(w, plan, cmd.AllowTableRebuild, false); err != nil {
		return err
	}

	run := func(stmt string) error {
		fmt.Fprintln(w, stmt) //nolint:errcheck
		start := time.Now()
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return statementError(stmt, err)
		}
		fmt.Fprintf(w, "-- ok (%dms)\n", time.Since(start).Milliseconds()) //nolint:errcheck
		return nil
	}
	n := 0
	if len(plan.DataLoss) > 0 {
		fmt.Fprint(w, (&diff.Plan{DataLoss: plan.DataLoss}).String()) //nolint:errcheck
	}
	for _, g := range plan.Groups {
		if g.Comment != "" {
			fmt.Fprintln(w, "-- "+g.Comment) //nolint:errcheck
		}
		for _, stmt := range g.Stmts {
			if err := run(stmt); err != nil {
				return err
			}
			n++
		}
	}
	for _, e := range want.Executes {
		if e.Condition != "" {
			ok, err := evaluate(ctx, conn, e.Condition)
			if _, nb := errors.AsType[*NotBooleanError](err); nb {
				return err
			} else if err != nil {
				return fmt.Errorf("failed to evaluate the condition of hazel:execute: %s: %w", e.Condition, err)
			}
			if !ok {
				continue
			}
		}
		directive, stmt, _ := strings.Cut(formatExecute(e, ""), "\n")
		fmt.Fprintln(w, directive) //nolint:errcheck
		if err := run(stmt); err != nil {
			return err
		}
		n++
	}
	if n == 0 {
		fmt.Fprintln(w, "-- No changes") //nolint:errcheck
		return nil
	}

	if !cmd.SkipForeignKeyCheck {
		if err := foreignKeyCheck(ctx, conn, want.Schema); err != nil {
			return err
		}
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	fmt.Fprintf(w, "-- Applied %d %s\n", n, plural(n, "statement", "statements")) //nolint:errcheck
	return nil
}

func isBusy(err error) bool {
	var serr *sqlite.Error
	return errors.As(err, &serr) && serr.Code()&0xff == sqlite3.SQLITE_BUSY
}

// foreignKeyCheck fails when a row refers to a parent row that is not
// there. foreign_keys is off while the plan runs, so nothing else checks.
// The check covers the whole database, except the tables marked with
// -- hazel:skip-foreign-key-check.
func foreignKeyCheck(ctx context.Context, conn *sql.Conn, want *model.Schema) error {
	skip := []any{}
	for t := range want.Tables.Values() {
		if t.SkipForeignKeyCheck {
			skip = append(skip, t.Name)
		}
	}
	in := strings.TrimSuffix(strings.Repeat("?, ", len(skip)), ", ")
	var violations sql.NullString
	err := conn.QueryRowContext(ctx, `
		SELECT group_concat(
		  "table" || coalesce(' (rowid ' || rowid || ')', '') || ' refers to a missing row of ' || parent,
		  '; ')
		FROM pragma_foreign_key_check
		WHERE "table" COLLATE NOCASE NOT IN (`+in+`)`, skip...).Scan(&violations)
	if err != nil {
		return err
	}
	if violations.Valid {
		return fmt.Errorf("foreign key violations, rolled back: %s (--skip-foreign-key-check, or -- hazel:skip-foreign-key-check on the table, leaves rows that were there before out of the check)", violations.String)
	}
	return nil
}

// statementError reports a statement that failed, with what the failure
// means for the statements whose SQLite message does not say it: SET NOT NULL
// and ADD CHECK fail on existing rows with a bare "constraint failed".
func statementError(stmt string, err error) error {
	text := strings.TrimSuffix(stmt, ";")
	tokens, _ := parser.Tokenize(stmt)
	t := parser.Statement{Tokens: tokens}.Significant()
	if n := len(t); n > 0 && t[n-1].IsPunct(";") {
		t = t[:n-1]
	}
	// ALTER TABLE t ALTER [COLUMN] c SET NOT NULL
	// ALTER TABLE t ADD [CONSTRAINT name] CHECK (...)
	if len(t) > 4 && t[0].IsKeyword("ALTER") && t[1].IsKeyword("TABLE") {
		table, rest := t[2].Name(), t[3:]
		switch {
		case rest[0].IsKeyword("ALTER") && len(rest) >= 5 && rest[len(rest)-3].IsKeyword("SET"):
			column := rest[1]
			if column.IsKeyword("COLUMN") {
				column = rest[2]
			}
			return fmt.Errorf("%s: existing rows in %s have NULL in %s: %w", text, table, column.Name(), err)
		case rest[0].IsKeyword("ADD") && rest[1].IsKeyword("CONSTRAINT"):
			return fmt.Errorf("%s: existing rows in %s violate %s: %w", text, table, rest[2].Name(), err)
		case rest[0].IsKeyword("ADD") && rest[1].IsKeyword("CHECK"):
			return fmt.Errorf("%s: existing rows in %s violate the CHECK constraint: %w", text, table, err)
		}
	}
	return fmt.Errorf("%s: %w", text, err)
}
