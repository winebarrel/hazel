# plan

plan -- print the DDL that takes a database to the schema files

## Synopsis

```
hazel plan [-I PAT]... [-E PAT]... [--allow-table-rebuild] [--check] DB SCHEMA_FILE...
```

## Description

`plan` reads the schema of the database `DB`, loads the schema files, and prints the DDL that takes the database to the schema of the files. It does not change the database; the database is opened read-only.

A database file that does not exist is taken as an empty database, and is not created. The first line of the output then says `(new file)`.

The output is SQL that can be read as a script:

```
-- Database: /path/to/app.db
-- Plan (2 tables, 0 views, 1 index, 0 triggers)
-- WARNING: the following will be dropped and their data will be lost:
--   table  legacy
--
-- table rebuild: users (NOT NULL of name; type of age; adds column email)
CREATE TABLE new_users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    age INTEGER,
    email TEXT
);
INSERT INTO new_users (id, name, age) SELECT id, name, age FROM users;
DROP TABLE users;
ALTER TABLE new_users RENAME TO users;
-- drop: removed from schema
DROP TABLE legacy;
CREATE INDEX users_name ON users (name);
```

- `-- Database:` names the database, and `-- Plan` counts the objects of the schema files that the plan manages.
- `-- WARNING:` lists the tables and columns whose data the plan drops. It comes first, whatever the order of the statements.
- A comment before a group of statements says why they run: `-- drop: removed from schema`, `-- drop and create: definition changed`, `-- drop and create: depends on ...`, or `-- table rebuild: TABLE (CHANGES)`.
- The statements marked with [`-- hazel:execute`](../directives.md#execute) follow the DDL, each after its directive.
- When there is nothing to do, the output ends with `-- No changes`.

The statements run in this order: views and triggers are dropped, then indexes; tables are renamed, altered and rebuilt; tables are dropped and created; then indexes, views and triggers are created.

A change that `ALTER TABLE` cannot make needs a rebuild of the table (see [Schema objects](../objects.md)). Without `--allow-table-rebuild`, `plan` prints the tables to rebuild and fails with exit status 3:

```
-- Database: /path/to/app.db
-- Plan (1 table, 0 views, 1 index, 0 triggers)
-- table rebuild required: users (NOT NULL of name; type of age; adds column email)
hazel: error: 1 table needs a rebuild; pass --allow-table-rebuild to allow it
```

The condition of an execute statement is evaluated against the database as it is before the DDL. A statement whose condition returns 0 is left out. A condition that fails, for example because it reads a table the plan creates, keeps its statement with a note, and `apply` evaluates it after the DDL. A condition that returns anything other than 0 or 1 is an error.

## Options

`-I PAT`, `--include=PAT`
:   Manage only the tables, views and virtual tables whose name matches the pattern. Can be repeated. See [Filtering](#filtering).

`-E PAT`, `--exclude=PAT`
:   Leave out the tables, views and virtual tables whose name matches the pattern. Can be repeated.

`--allow-table-rebuild`
:   Allow rebuilding a table for a change `ALTER TABLE` cannot make.

`--check`
:   Exit with status 2 when there are changes. With a rebuild that is not allowed as well, the status is 2.

## Filtering

A pattern is a wildcard (`*` and `?`, matched against the whole name) or a regular expression in slashes (`/^tmp_/`, matched anywhere in the name unless anchored). Both compare case as written.

With `-I`, only the names that match one of its patterns are managed; then the names that match an `-E` pattern are left out. Indexes and triggers go with the table or view they belong to.

An object left out gets no DDL at all: it is not created, changed or dropped. It is still part of the schemas that are compared, so that a managed view that reads it can be compiled.

## Exit status

0 on success, also when there are changes. 1 on an error. 2 with `--check` when there are changes. 3 when a rebuild is needed and `--allow-table-rebuild` is not given. 80 on a usage error. See [Exit status](../exit-status.md).

## Examples

Print the plan for two schema files:

```
hazel plan app.db tables.sql views.sql
```

Fail in CI when the database is not up to date:

```
hazel plan --check app.db schema/*.sql
```

## See also

[apply](apply.md), [diff](diff.md)
