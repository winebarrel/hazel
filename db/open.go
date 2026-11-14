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
// file is not created, because it must exist.
func OpenReadOnly(t Target) (*sql.DB, error) {
	exists, err := t.Exists()
	if err != nil {
		return nil, err
	}
	if !exists && !t.memory {
		return nil, fmt.Errorf("database does not exist: %s", t.path)
	}
	return open(t, "query_only(1)")
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
	return open(t, fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()))
}

// open opens the database with pragma set on every connection the pool
// opens, so that a connection reopened by database/sql keeps it. The pool
// holds one connection, since an in-memory database is per connection.
func open(t Target, pragma string) (*sql.DB, error) {
	// The path goes to the driver as it is, not as a URI. modernc.org/sqlite
	// cuts a plain name at the first "?" and reads what follows as its own
	// parameters; Resolve refuses "?" in a path, so the "?" here is the only
	// one.
	db, err := sql.Open("sqlite", t.String()+"?_pragma="+pragma)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close() //nolint:errcheck
		return nil, err
	}
	return db, nil
}
