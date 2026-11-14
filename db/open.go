package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// OpenReadOnly opens the database for reading with PRAGMA query_only. The
// file must exist; it is never created.
func OpenReadOnly(t Target) (*sql.DB, error) {
	exists, err := t.Exists()
	if err != nil {
		return nil, err
	}
	if !exists && !t.memory {
		return nil, fmt.Errorf("database does not exist: %s", t.path)
	}
	return open(t, "PRAGMA query_only = ON")
}

// OpenReadWrite opens the database for writing, waiting up to busyTimeout
// for a lock. The file is created when it does not exist, but its directory
// must.
func OpenReadWrite(t Target, busyTimeout time.Duration) (*sql.DB, error) {
	if _, err := t.Exists(); err != nil {
		return nil, err
	}
	if !t.memory {
		dir := filepath.Dir(t.path)
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("directory does not exist: %s", dir)
		}
	}
	return open(t, fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeout.Milliseconds()))
}

// open opens the database and runs pragma on its connection. The pool holds
// one connection, since the pragmas are per connection and an in-memory
// database is per connection too.
func open(t Target, pragma string) (*sql.DB, error) {
	// The path goes to the driver as it is, not as a URI: modernc.org/sqlite
	// passes a plain absolute path to SQLite untouched, except for what
	// follows a "?", which Resolve refuses.
	db, err := sql.Open("sqlite", t.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(pragma); err != nil {
		db.Close() //nolint:errcheck
		return nil, err
	}
	return db, nil
}
