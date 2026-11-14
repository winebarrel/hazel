package command

import (
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/alecthomas/kong"
	"gopkg.in/yaml.v3"
)

// Built-in kong meta flags are not loadable from the config file. --help is
// matched by pointer via Application.HelpFlag; the rest by their kong type.
var metaFlagTypes = map[reflect.Type]bool{
	reflect.TypeFor[kong.ConfigFlag]():  true,
	reflect.TypeFor[kong.VersionFlag](): true,
}

// YAMLConfig is a kong.ConfigurationLoader that reads options from a YAML
// file. The keys are the flag names (busy-timeout, not busy_timeout), and a
// key that names no flag is an error. A value applies
// only to a flag that is not set on the command line or by an environment
// variable: flag > environment variable > config file > default.
func YAMLConfig(r io.Reader) (kong.Resolver, error) {
	values := map[string]any{}
	if err := yaml.NewDecoder(r).Decode(&values); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return &yamlResolver{values: values}, nil
}

type yamlResolver struct {
	values map[string]any
}

func (y *yamlResolver) Resolve(_ *kong.Context, _ *kong.Path, flag *kong.Flag) (any, error) {
	if isMetaFlag(flag) {
		return nil, nil
	}
	v, ok := y.values[flag.Name]
	if !ok {
		return nil, nil
	}
	// kong applies an environment variable before resolvers run, so defer
	// to it when one is set for this flag.
	for _, env := range flag.Tag.Envs {
		if _, set := os.LookupEnv(env); set {
			return nil, nil
		}
	}
	return v, nil
}

func (y *yamlResolver) Validate(app *kong.Application) error {
	// configurable maps a flag name to whether the file may set it.
	configurable := map[string]bool{}
	collectFlagNames(app.Node, app.HelpFlag, configurable)

	var unknown, commandLineOnly []string
	for key := range y.values {
		ok, found := configurable[key]
		switch {
		case !found:
			unknown = append(unknown, key)
		case !ok:
			commandLineOnly = append(commandLineOnly, key)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return fmt.Errorf("unknown config key(s): %s", strings.Join(unknown, ", "))
	}
	if len(commandLineOnly) > 0 {
		slices.Sort(commandLineOnly)
		return fmt.Errorf("config key(s) only for the command line: %s", strings.Join(commandLineOnly, ", "))
	}
	return nil
}

func collectFlagNames(node *kong.Node, help *kong.Flag, out map[string]bool) {
	for _, flag := range node.Flags {
		if flag == help || isMetaFlag(flag) {
			continue
		}
		out[flag.Name] = !flag.Tag.Has("noconfig")
	}
	for _, child := range node.Children {
		collectFlagNames(child, help, out)
	}
}

func isMetaFlag(flag *kong.Flag) bool {
	return metaFlagTypes[flag.Target.Type()]
}
