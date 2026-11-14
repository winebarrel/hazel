package db_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/winebarrel/hazel/db"
)

func TestResolveMemory(t *testing.T) {
	for _, arg := range []string{":memory:", "file::memory:"} {
		t.Run(arg, func(t *testing.T) {
			target, err := db.Resolve(arg)
			require.NoError(t, err)
			assert.True(t, target.IsMemory())
			assert.Empty(t, target.Path())
			assert.Equal(t, ":memory:", target.String())
		})
	}
}

func TestResolvePath(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	abs := filepath.Join(cwd, "abs.db")
	slashAbs := filepath.ToSlash(abs)
	if runtime.GOOS == "windows" {
		// file:///C:/x names C:/x.
		slashAbs = "/" + slashAbs
	}

	tests := []struct {
		arg  string
		want string
	}{
		{"app.db", filepath.Join(cwd, "app.db")},
		{abs, abs},
		// Only lowercase "file:" is a URI; the rest are file names.
		{"FILE:x.db", filepath.Join(cwd, "FILE:x.db")},
		{"sqlite://app.db", filepath.Join(cwd, "sqlite:", "app.db")},
		{"h#x.db", filepath.Join(cwd, "h#x.db")},
		{"pct%41.db", filepath.Join(cwd, "pct%41.db")},
		{"file:x.db", filepath.Join(cwd, "x.db")},
		{"file:a%20b.db", filepath.Join(cwd, "a b.db")},
		{"file://" + slashAbs, abs},
		{"file://localhost" + slashAbs, abs},
	}
	if runtime.GOOS == "windows" {
		// ":" cannot appear in a Windows file name.
		tests = tests[:2]
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			target, err := db.Resolve(tt.arg)
			require.NoError(t, err)
			assert.False(t, target.IsMemory())
			assert.Equal(t, tt.want, target.Path())
			assert.Equal(t, tt.want, target.String())
		})
	}
}

func TestResolveError(t *testing.T) {
	tests := []struct {
		arg  string
		want string
	}{
		{"", "database is not specified"},
		{"y.db?mode=ro", "database path must not contain '?'"},
		{"file:x.db?mode=ro", "database URI must not have a query or a fragment"},
		{"file:x.db#frag", "database URI must not have a query or a fragment"},
		{"file::memory:?cache=shared", "database URI must not have a query or a fragment"},
		{"file://host/x.db", "database URI must not name a host"},
		{"file:x%zz.db", "invalid database URI"},
		{"file:", "database URI has no path"},
		{"file://", "database URI has no path"},
		{"file:a%3Fb.db", "database path must not contain '?'"},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			_, err := db.Resolve(tt.arg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) db.Target {
		t.Helper()
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
		target, err := db.Resolve(path)
		require.NoError(t, err)
		return target
	}
	resolve := func(path string) db.Target {
		t.Helper()
		target, err := db.Resolve(path)
		require.NoError(t, err)
		return target
	}

	t.Run("missing", func(t *testing.T) {
		exists, err := resolve(filepath.Join(dir, "missing.db")).Exists()
		require.NoError(t, err)
		assert.False(t, exists)
	})
	t.Run("memory", func(t *testing.T) {
		exists, err := db.Memory.Exists()
		require.NoError(t, err)
		assert.False(t, exists)
	})
	t.Run("empty", func(t *testing.T) {
		exists, err := write("empty.db", "").Exists()
		require.NoError(t, err)
		assert.True(t, exists)
	})
	t.Run("sqlite", func(t *testing.T) {
		exists, err := write("ok.db", "SQLite format 3\x00rest").Exists()
		require.NoError(t, err)
		assert.True(t, exists)
	})
	t.Run("not sqlite", func(t *testing.T) {
		_, err := write("text.db", "CREATE TABLE t (a);").Exists()
		assert.ErrorContains(t, err, "not a SQLite database")
	})
	t.Run("short", func(t *testing.T) {
		_, err := write("short.db", "SQLite").Exists()
		assert.ErrorContains(t, err, "not a SQLite database")
	})
	t.Run("directory", func(t *testing.T) {
		_, err := resolve(dir).Exists()
		assert.ErrorContains(t, err, "not a regular file")
	})
}
