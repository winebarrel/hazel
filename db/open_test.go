package db_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/winebarrel/hazel/db"
)

func resolve(t *testing.T, path string) db.Target {
	t.Helper()
	target, err := db.Resolve(path)
	require.NoError(t, err)
	return target
}

func TestOpenReadWriteCreates(t *testing.T) {
	target := resolve(t, filepath.Join(t.TempDir(), "new.db"))

	conn, err := db.OpenReadWrite(target, 1500*time.Millisecond)
	require.NoError(t, err)
	defer conn.Close()

	var timeout int
	require.NoError(t, conn.QueryRow("PRAGMA busy_timeout").Scan(&timeout))
	assert.Equal(t, 1500, timeout)

	_, err = conn.Exec("CREATE TABLE t (a)")
	require.NoError(t, err)

	exists, err := target.Exists()
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestOpenReadWriteMissingDirectory(t *testing.T) {
	target := resolve(t, filepath.Join(t.TempDir(), "no", "such", "new.db"))
	_, err := db.OpenReadWrite(target, time.Second)
	assert.ErrorContains(t, err, "directory does not exist")
}

func TestOpenReadWriteNotSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "text.db")
	require.NoError(t, os.WriteFile(path, []byte("hello, world!!!!"), 0o644))
	_, err := db.OpenReadWrite(resolve(t, path), time.Second)
	assert.ErrorContains(t, err, "not a SQLite database")
}

func TestOpenReadOnly(t *testing.T) {
	target := resolve(t, filepath.Join(t.TempDir(), "app.db"))
	rw, err := db.OpenReadWrite(target, time.Second)
	require.NoError(t, err)
	_, err = rw.Exec("CREATE TABLE t (a)")
	require.NoError(t, err)
	require.NoError(t, rw.Close())

	conn, err := db.OpenReadOnly(target)
	require.NoError(t, err)
	defer conn.Close()

	var n int
	require.NoError(t, conn.QueryRow("SELECT count(*) FROM sqlite_schema").Scan(&n))
	assert.Equal(t, 1, n)

	_, err = conn.Exec("CREATE TABLE u (a)")
	assert.ErrorContains(t, err, "readonly")
}

func TestOpenReadOnlyMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")
	_, err := db.OpenReadOnly(resolve(t, path))
	require.ErrorContains(t, err, "database does not exist")

	_, err = os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist, "the file must not be created")
}

func TestOpenReadOnlyNotSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "text.db")
	require.NoError(t, os.WriteFile(path, []byte("hello, world!!!!"), 0o644))
	_, err := db.OpenReadOnly(resolve(t, path))
	assert.ErrorContains(t, err, "not a SQLite database")
}

func TestOpenPragmaOnEveryConnection(t *testing.T) {
	target := resolve(t, filepath.Join(t.TempDir(), "app.db"))
	rw, err := db.OpenReadWrite(target, 1500*time.Millisecond)
	require.NoError(t, err)
	defer rw.Close()
	ro, err := db.OpenReadOnly(target)
	require.NoError(t, err)
	defer ro.Close()

	// A connection the pool opens again gets the pragmas too.
	rw.SetConnMaxLifetime(time.Nanosecond)
	ro.SetConnMaxLifetime(time.Nanosecond)
	for range 2 {
		time.Sleep(time.Millisecond)
		var timeout, queryOnly int
		require.NoError(t, rw.QueryRow("PRAGMA busy_timeout").Scan(&timeout))
		assert.Equal(t, 1500, timeout)
		require.NoError(t, ro.QueryRow("PRAGMA query_only").Scan(&queryOnly))
		assert.Equal(t, 1, queryOnly)
	}
}

func TestOpenMemory(t *testing.T) {
	rw, err := db.OpenReadWrite(db.Memory, time.Second)
	require.NoError(t, err)
	defer rw.Close()
	_, err = rw.Exec("CREATE TABLE t (a)")
	require.NoError(t, err)
	// The pool keeps one connection, so the in-memory database survives
	// between statements.
	var n int
	require.NoError(t, rw.QueryRow("SELECT count(*) FROM sqlite_schema").Scan(&n))
	assert.Equal(t, 1, n)

	ro, err := db.OpenReadOnly(db.Memory)
	require.NoError(t, err)
	defer ro.Close()
	_, err = ro.Exec("CREATE TABLE t (a)")
	assert.ErrorContains(t, err, "readonly")
}

func TestOpenSpecialCharacters(t *testing.T) {
	// The path goes to the driver as it is, so "#" and "%" stay part of the
	// file name.
	dir := t.TempDir()
	for _, name := range []string{"h#x.db", "pct%41.db", "sp ace.db"} {
		t.Run(name, func(t *testing.T) {
			target := resolve(t, filepath.Join(dir, name))
			conn, err := db.OpenReadWrite(target, time.Second)
			require.NoError(t, err)
			_, err = conn.Exec("CREATE TABLE t (a)")
			require.NoError(t, err)
			require.NoError(t, conn.Close())

			_, err = os.Stat(filepath.Join(dir, name))
			assert.NoError(t, err)
		})
	}
}

func TestOpenReadWriteUnwritableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs a directory the user cannot write to")
	}
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	_, err := db.OpenReadWrite(resolve(t, filepath.Join(dir, "new.db")), time.Second)
	assert.ErrorContains(t, err, "unable to open database file")
}

func TestOpenReadOnlyUnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs a file the user cannot read")
	}
	path := filepath.Join(t.TempDir(), "app.db")
	require.NoError(t, os.WriteFile(path, []byte("SQLite format 3\x00"), 0o000))

	_, err := db.OpenReadOnly(resolve(t, path))
	assert.ErrorIs(t, err, os.ErrPermission)
}
