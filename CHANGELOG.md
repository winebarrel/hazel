<!-- On release, rename [Unreleased] to the version and date, point [unreleased] at the new tag and add the link of the version below it. Do not leave an [Unreleased] section behind. -->

# Changelog

## [Unreleased]

* hazel is generally available.

## [0.1.0] - 2026-11-14

* A demo image, `ghcr.io/winebarrel/hazel-demo`, bundles hazel, sqlite3 and a sample database: `docker run --rm -it ghcr.io/winebarrel/hazel-demo`.

* A name written in single quotes where SQLite expects a name, such as `CREATE TABLE 'sq' ('b' TEXT)`, is read as that name. `dump` and the CREATE statements hazel runs keep such a name as it is written.

* The output goes through the command in `HAZEL_PAGER`, run with `sh -c` (`cmd /c` on Windows), when it goes to a terminal. `--pager` uses the pager for any output, and `--no-pager` never does; without `HAZEL_PAGER`, neither does anything. The pager starts on the first output, so a command that prints nothing, or fails before it prints, does not open it.

* `plan`, `apply` and `diff` write the CREATE statements they run in the layout of `dump`: a CREATE TABLE has one column or constraint per line, and names lose the quotes they do not need. The statements are kept that way in the database.

* A CREATE statement that ends with a `--` comment gets its `;` on the next line in the output of `plan`, `apply` and `diff`.

* `hazel fmt SCHEMA_FILE...` lays out the schema files in place, the way `dump` writes the statements. The statements are separated by one blank line, the comments before a statement stay before it, and a comment on the line of a `;` stays on that line. Statements that hazel does not manage and those marked with `-- hazel:execute` are left as they are. The files are loaded together before and after, and nothing is written unless both give the same schema. `--check` writes nothing, prints the files that are not formatted and exits with status 2 when there are any. `--strip-renamed-from` drops the `-- hazel:renamed-from` directives.

* `hazel dump --split=DIR` writes a file for each table, view and virtual table, named after it with `.sql`, holding its CREATE statement, then its indexes and triggers. The directory is created if it does not exist, a file of the same name is overwritten, and files of objects no longer in the database are left alone. A name that differs from an earlier one in case only gets `_2`, `_3` and so on, and a name with `/` or `\` is an error.

* `hazel dump DB` prints the schema of a database as the CREATE statements that make it: each table followed by its indexes and triggers, then the views, each followed by its triggers. Given back as the schema file, the dump plans clean. A CREATE TABLE is laid out with one column or constraint per line, indented by four spaces; nothing else in the statements changes but the whitespace and the quotes of names that do not need them. `--no-format` prints the statements as SQLite keeps them, and `-I` / `-E` select the tables, views and virtual tables.

* `hazel apply DB SCHEMA_FILE...` runs the plan in one transaction, started with `BEGIN IMMEDIATE`, which waits up to `--busy-timeout` (5s by default) for the write lock. It prints each statement with its time, runs the statements marked with `-- hazel:execute` whose condition holds after the DDL, checks the foreign keys before the commit, and rolls everything back when a statement fails. `--skip-foreign-key-check` turns the foreign key check off, and `-- hazel:skip-foreign-key-check` leaves one table out of it. The condition of `-- hazel:execute` must return 0 or 1; any other result, NULL and no rows included, is an error. `plan` shows those statements too, with a note when their condition cannot be evaluated before the DDL.

* The `hazel` command, with `plan` and `diff`. `plan DB SCHEMA_FILE...` prints the DDL that takes a database to the schema files, and `diff OLD_SCHEMA_FILE NEW_SCHEMA_FILE` the DDL that takes one schema file to another. A plan that rebuilds a table fails with exit status 3 unless `--allow-table-rebuild` is given, `--check` exits with status 2 when there are changes, and `-I` / `-E` select the tables, views and virtual tables by name. Options can also come from environment variables (`HAZEL_*`) and from a YAML file given with `-C`.

[unreleased]: https://github.com/winebarrel/hazel/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/winebarrel/hazel/commits/v0.1.0
