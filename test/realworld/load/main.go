// Command load runs a SQL file against a SQLite database, for the real-world
// schema tests. It hands the file to SQLite as it is, through the driver hazel
// uses, so the schema is loaded without hazel's own parser and with the same
// SQLite that hazel reads it with.
//
// Usage: go run ./test/realworld/load DB FILE
package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: load DB FILE")
		os.Exit(2)
	}
	if err := load(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func load(dbPath, file string) error {
	body, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close() //nolint:errcheck
	_, err = db.Exec(string(body))
	return err
}
