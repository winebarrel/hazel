package diff_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/winebarrel/hazel/catalog"
	"github.com/winebarrel/hazel/db"
	"github.com/winebarrel/hazel/desired"
	"github.com/winebarrel/hazel/diff"
	"github.com/winebarrel/hazel/model"
	"gopkg.in/yaml.v3"
)

// diffTestCase is one fixture in testdata. init is run on an empty database
// to give the current schema, and desired is loaded as the schema files.
// Either plan, the expected plan, or error, a part of the expected error, is
// given.
type diffTestCase struct {
	Init    string `yaml:"init"`
	Desired string `yaml:"desired"`
	Plan    string `yaml:"plan"`
	Error   string `yaml:"error"`
}

func TestDiff(t *testing.T) {
	files, err := filepath.Glob("testdata/*.yml")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		t.Run(strings.TrimSuffix(filepath.Base(file), ".yml"), func(t *testing.T) {
			b, err := os.ReadFile(file)
			require.NoError(t, err)
			var tc diffTestCase
			require.NoError(t, yaml.Unmarshal(b, &tc))

			conn := openDB(t, tc.Init)
			ctx := context.Background()
			current, err := catalog.Read(ctx, conn)
			require.NoError(t, err)
			want := loadDesired(t, tc.Desired)

			plan, err := diff.Diff(current, want)
			if tc.Error != "" {
				assert.ErrorContains(t, err, tc.Error)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.Plan, plan.String())

			// The plan applies, and leaves nothing to do.
			for _, s := range plan.Statements() {
				_, err := conn.Exec(s)
				require.NoError(t, err, s)
			}
			applied, err := catalog.Read(ctx, conn)
			require.NoError(t, err)
			again, err := diff.Diff(applied, want)
			require.NoError(t, err)
			assert.True(t, again.IsEmpty(), "plan after apply:\n%s", again)
		})
	}
}

func openDB(t *testing.T, init string) *sql.DB {
	t.Helper()
	conn, err := db.OpenReadWrite(db.Memory, time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	if init != "" {
		_, err = conn.Exec(init)
		require.NoError(t, err)
	}
	return conn
}

func loadDesired(t *testing.T, body string) *model.Schema {
	t.Helper()
	schema, _, err := desired.Load(context.Background(), []desired.Source{{Name: "desired.sql", Body: body}})
	require.NoError(t, err)
	return schema
}

func TestPlanEmpty(t *testing.T) {
	plan, err := diff.Diff(model.NewSchema(), model.NewSchema())
	require.NoError(t, err)
	assert.True(t, plan.IsEmpty())
	assert.Empty(t, plan.Statements())
	assert.Empty(t, plan.String())
}
