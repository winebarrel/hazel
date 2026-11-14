// Package db resolves the database argument and opens the database.
package db

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Memory is the target of an in-memory database.
var Memory = Target{memory: true}

// Target is the database that the DB argument names: a file or an in-memory
// database.
type Target struct {
	path   string
	memory bool
}

// Path returns the absolute path of the file, or "" for an in-memory
// database.
func (t Target) Path() string {
	return t.path
}

// IsMemory reports whether the target is an in-memory database.
func (t Target) IsMemory() bool {
	return t.memory
}

func (t Target) String() string {
	if t.memory {
		return ":memory:"
	}
	return t.path
}

// Resolve resolves the DB argument the way the sqlite3 command line shell
// does, except that a URI may not carry a query or a fragment:
//
//   - ":memory:" and "file::memory:" name an in-memory database.
//   - A name that starts with "file:" in lowercase is a URI. Its authority
//     must be empty or "localhost", and its path is percent-decoded.
//   - Anything else is a file path, taken literally.
//
// The path is made absolute. A path that contains "?" is refused, since the
// driver would cut the name there.
func Resolve(arg string) (Target, error) {
	if arg == "" {
		return Target{}, errors.New("database is not specified")
	}
	if arg == ":memory:" || arg == "file::memory:" {
		return Memory, nil
	}
	path := arg
	if strings.HasPrefix(arg, "file:") {
		var err error
		path, err = uriPath(arg)
		if err != nil {
			return Target{}, err
		}
	}
	if strings.Contains(path, "?") {
		return Target{}, fmt.Errorf("database path must not contain '?': %s", arg)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Target{}, err
	}
	return Target{path: abs}, nil
}

// uriPath returns the file path of a "file:" URI.
func uriPath(uri string) (string, error) {
	if strings.ContainsAny(uri, "?#") {
		return "", fmt.Errorf("database URI must not have a query or a fragment: %s", uri)
	}
	rest := strings.TrimPrefix(uri, "file:")
	if after, ok := strings.CutPrefix(rest, "//"); ok {
		authority, path, _ := strings.Cut(after, "/")
		if authority != "" && authority != "localhost" {
			return "", fmt.Errorf("database URI must not name a host: %s", uri)
		}
		rest = "/" + path
	}
	path, err := url.PathUnescape(rest)
	if err != nil {
		return "", fmt.Errorf("invalid database URI: %s: %w", uri, err)
	}
	if path == "" || path == "/" {
		return "", fmt.Errorf("database URI has no path: %s", uri)
	}
	return windowsDrivePath(path), nil
}

// windowsDrivePath drops the "/" before a drive letter on Windows, so that
// file:///C:/x names C:/x, as SQLite does.
func windowsDrivePath(path string) string {
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		return path[1:]
	}
	return path
}

// header is the first 16 bytes of every SQLite database file.
const header = "SQLite format 3\x00"

// Exists reports whether the database file exists. An existing file must be
// a regular file that is empty or starts with the SQLite header. An in-memory
// database never exists beforehand.
func (t Target) Exists() (bool, error) {
	if t.memory {
		return false, nil
	}
	info, err := os.Stat(t.path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("not a regular file: %s", t.path)
	}
	if info.Size() == 0 {
		return true, nil
	}
	f, err := os.Open(t.path)
	if err != nil {
		return false, err
	}
	defer f.Close() //nolint:errcheck
	buf := make([]byte, len(header))
	if _, err := io.ReadFull(f, buf); err != nil || string(buf) != header {
		return false, fmt.Errorf("not a SQLite database: %s", t.path)
	}
	return true, nil
}
