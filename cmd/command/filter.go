package command

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/winebarrel/hazel/diff"
	"github.com/winebarrel/hazel/model"
)

// Filter holds the -I and -E options.
type Filter struct {
	Include []string `short:"I" env:"HAZEL_INCLUDE" placeholder:"PAT" help:"Manage only the tables, views and virtual tables whose name matches the pattern (wildcard: *, ?; /re/ for a regular expression). Can be repeated."`
	Exclude []string `short:"E" env:"HAZEL_EXCLUDE" placeholder:"PAT" help:"Leave out the tables, views and virtual tables whose name matches the pattern. Can be repeated."`
}

// Validate checks the patterns. kong reports an error here as a usage
// error, exit status 80.
func (f *Filter) Validate() error {
	for _, p := range append(append([]string{}, f.Include...), f.Exclude...) {
		if _, err := match(p, ""); err != nil {
			return fmt.Errorf("invalid pattern %q: %w", p, err)
		}
	}
	return nil
}

// match reports whether name matches one pattern. A pattern in slashes is a
// regular expression, which matches anywhere in the name unless anchored;
// anything else is a wildcard, which has to match the whole name. Both
// compare case as written.
func match(pattern, name string) (bool, error) {
	if len(pattern) >= 2 && strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/") {
		re, err := regexp.Compile(pattern[1 : len(pattern)-1])
		if err != nil {
			return false, err
		}
		return re.MatchString(name), nil
	}
	return path.Match(pattern, name)
}

// selects reports whether the name is managed: it matches an -I pattern
// when there is one, and no -E pattern.
func (f *Filter) selects(name string) bool {
	matches := func(patterns []string) bool {
		for _, p := range patterns {
			if ok, _ := match(p, name); ok {
				return true
			}
		}
		return false
	}
	return (len(f.Include) == 0 || matches(f.Include)) && !matches(f.Exclude)
}

// option returns the diff option that leaves out what the filter does not
// select. The schemas are given to diff whole, since a selected view may
// read a table that is left out.
func (f *Filter) option() diff.Option {
	return diff.WithExclude(f.excludes)
}

// excludes reports whether the name is left out: the opposite of selects.
func (f *Filter) excludes(name string) bool {
	return !f.selects(name)
}

// count returns the objects of s that the filter selects, for the summary.
// Indexes and triggers go with the table or view they belong to.
func (f *Filter) count(s *model.Schema) *model.Schema {
	return &model.Schema{
		Tables:   s.Tables.Filter(func(_ string, t *model.Table) bool { return f.selects(t.Name) }),
		Indexes:  s.Indexes.Filter(func(_ string, i *model.Index) bool { return f.selects(i.Table) }),
		Views:    s.Views.Filter(func(_ string, v *model.View) bool { return f.selects(v.Name) }),
		Triggers: s.Triggers.Filter(func(_ string, t *model.Trigger) bool { return f.selects(t.Table) }),
	}
}
