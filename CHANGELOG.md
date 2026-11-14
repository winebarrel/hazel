<!-- On release, rename [Unreleased] to the version and date, point [unreleased] at the new tag and add the link of the version below it. Do not leave an [Unreleased] section behind. -->

# Changelog

## [Unreleased]

* `hazel dump DB` prints the schema of a database as the CREATE statements that make it: each table followed by its indexes and triggers, then the views, each followed by its triggers. Given back as the schema file, the dump plans clean. A CREATE TABLE is laid out with one column or constraint per line, indented by four spaces; nothing else in the statements changes but the whitespace and the quotes of names that do not need them. `--no-format` prints the statements as SQLite keeps them, and `-I` / `-E` select the tables, views and virtual tables.

* `hazel apply DB SCHEMA_FILE...` runs the plan in one transaction, started with `BEGIN IMMEDIATE`, which waits up to `--busy-timeout` (5s by default) for the write lock. It prints each statement with its time, runs the statements marked with `-- hazel:execute` whose condition holds after the DDL, checks the foreign keys before the commit, and rolls everything back when a statement fails. `--skip-foreign-key-check` turns the foreign key check off, and `-- hazel:skip-foreign-key-check` leaves one table out of it. The condition of `-- hazel:execute` must return 0 or 1; any other result, NULL and no rows included, is an error. `plan` shows those statements too, with a note when their condition cannot be evaluated before the DDL.

* The `hazel` command, with `plan` and `diff`. `plan DB SCHEMA_FILE...` prints the DDL that takes a database to the schema files, and `diff OLD_SCHEMA_FILE NEW_SCHEMA_FILE` the DDL that takes one schema file to another. A plan that rebuilds a table fails with exit status 3 unless `--allow-table-rebuild` is given, `--check` exits with status 2 when there are changes, and `-I` / `-E` select the tables, views and virtual tables by name. Options can also come from environment variables (`HAZEL_*`) and from a YAML file given with `-C`.

[unreleased]: https://github.com/winebarrel/hazel/commits/main
