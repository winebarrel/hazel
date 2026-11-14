# Commands

```
hazel [-C FILE] [--[no-]pager] [--version] COMMAND ...
```

| Command | Description |
| --- | --- |
| [plan](plan.md) | Print the DDL that takes a database to the schema files. |
| [apply](apply.md) | Run that DDL in one transaction. |
| [dump](dump.md) | Print the schema of a database as SQL. |
| [fmt](fmt.md) | Lay out schema files in place. |
| [diff](diff.md) | Print the DDL that takes one schema file to another. No database is read. |

## Global options

`-C FILE`, `--config=FILE`
:   Read options from a YAML file. See [Configuration](../configuration.md).

`--pager`, `--no-pager`
:   Send the standard output through the command in `HAZEL_PAGER`. Without either option, the pager is used only when the standard output is a terminal. `--pager` uses it for any output, and `--no-pager` never does. Without `HAZEL_PAGER`, neither does anything. The pager starts on the first output, so a command that prints nothing, or fails before it prints, does not open it.

`--version`
:   Print the version of hazel, and of modernc.org/sqlite and the SQLite it bundles.

`-h`, `--help`
:   Print the usage of hazel or of a command.

## The database argument

The `DB` argument of `plan`, `apply` and `dump` names the database the way the `sqlite3` command-line shell takes it.

| Argument | Meaning |
| --- | --- |
| `:memory:`, `file::memory:` | An in-memory database. It starts empty. |
| A name that begins with `file:` in lowercase | A URI. The path is taken from it, with percent-encoding decoded. The host must be empty or `localhost`, and a query (`?...`) or a fragment (`#...`) is an error. |
| Anything else | A file path, taken literally. `y.db?mode=ro` is a file of that name. |

hazel decides how the file is opened: read-only with `query_only` for `plan` and `dump`, and read-write with a busy timeout for `apply`. Parameters such as `mode` or `_pragma` cannot be given.

## Schema files

A schema file holds CREATE statements: `CREATE TABLE`, `CREATE VIRTUAL TABLE`, `CREATE INDEX`, `CREATE VIEW` and `CREATE TRIGGER`. When several files are given, they are read as one schema; the order of the files and of the statements in them does not matter, except for the statements marked with `-- hazel:execute`, which run in the order written.

Any other statement, such as `INSERT` or `PRAGMA`, is ignored with a warning on the standard error, unless it is marked with [`-- hazel:execute`](../directives.md#execute). A CREATE of a TEMP object is ignored the same way.
