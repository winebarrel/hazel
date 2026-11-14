# Configuration

Options can be given on the command line, in environment variables and in a YAML file. A command-line option takes precedence over an environment variable, and an environment variable over the file.

## Configuration file

```yaml
# hazel.yml
exclude:
  - tmp_*
allow-table-rebuild: true
busy-timeout: 10s
```

```
hazel -C hazel.yml plan app.db schema.sql
HAZEL_CONFIG=hazel.yml hazel apply app.db schema.sql
```

The keys are the long option names without `--`. An unknown key is an error. One file serves every command, and a command ignores the keys of options it does not have.

These options are for a single run and cannot be set in the file: `--check`, `--split`, `--strip-renamed-from`. The arguments, such as the database and the schema files, cannot be set either.

## Environment variables

| Variable | Option |
| --- | --- |
| `HAZEL_CONFIG` | `-C`, `--config` |
| `HAZEL_INCLUDE` | `-I`, `--include` (comma-separated) |
| `HAZEL_EXCLUDE` | `-E`, `--exclude` (comma-separated) |
| `HAZEL_ALLOW_TABLE_REBUILD` | `--allow-table-rebuild` |
| `HAZEL_BUSY_TIMEOUT` | `--busy-timeout` |
| `HAZEL_SKIP_FOREIGN_KEY_CHECK` | `--skip-foreign-key-check` |
| `HAZEL_NO_CREATE` | `--no-create` |
| `HAZEL_NO_FORMAT` | `--no-format` |
| `HAZEL_PAGER` | The pager command; see below. |

## Pager

```
HAZEL_PAGER='less -R' hazel dump app.db
```

`HAZEL_PAGER` is the command the standard output is sent through. It runs with `sh -c`, or `cmd /c` on Windows. It is used when the standard output is a terminal, or with `--pager` for any output; `--no-pager` turns it off. `pager: true` or `pager: false` in the configuration file does the same as the options. Without `HAZEL_PAGER`, no pager is used.
