# apply

apply -- run the DDL that takes a database to the schema files

## Synopsis

```
hazel apply [-I PAT]... [-E PAT]... [--allow-table-rebuild] [--busy-timeout=DURATION]
            [--skip-foreign-key-check] [--no-create] DB SCHEMA_FILE...
```

## Description

`apply` computes the plan as [`plan`](plan.md) does and runs it in one transaction. A database file that does not exist is created, unless `--no-create` is given.

The steps are:

1. `PRAGMA foreign_keys = OFF`, outside the transaction, where it can be changed. A rebuild needs it off: with it on, the `DROP TABLE` of the old table would delete or fail on the rows that refer to it.
2. `BEGIN IMMEDIATE`, which waits up to `--busy-timeout` for the write lock.
3. The schema of the database is read and the plan computed, under the lock.
4. The DDL runs, then the statements marked with [`-- hazel:execute`](../directives.md#execute) whose condition returns 1, evaluated after the DDL.
5. `PRAGMA foreign_key_check`. A row that refers to a missing parent row rolls everything back, unless `--skip-foreign-key-check` is given. The tables marked with [`-- hazel:skip-foreign-key-check`](../directives.md#skip-foreign-key-check) are left out of the check.
6. `COMMIT`.

A statement that fails rolls everything back. Each statement is printed with the time it took:

```
-- Database: /path/to/app.db
-- Plan (2 tables, 0 views, 1 index, 0 triggers)
ALTER TABLE users RENAME TO members;
-- ok (0ms)
ALTER TABLE members RENAME COLUMN name TO full_name;
-- ok (0ms)
ALTER TABLE members ADD COLUMN email TEXT;
-- ok (0ms)
-- Applied 3 statements
```

`ALTER TABLE ... SET NOT NULL` and `ALTER TABLE ... ADD CHECK` check the existing rows. When a row breaks the constraint, SQLite reports only `constraint failed`; hazel adds what failed:

```
hazel: error: ALTER TABLE users ALTER COLUMN name SET NOT NULL: existing rows in users have NULL in name: constraint failed (19)
```

## Options

`-I PAT`, `--include=PAT`, `-E PAT`, `--exclude=PAT`
:   Select the tables, views and virtual tables to manage, as for [`plan`](plan.md#filtering).

`--allow-table-rebuild`
:   Allow rebuilding a table for a change `ALTER TABLE` cannot make. Without it, a plan with a rebuild runs nothing and fails with exit status 3.

`--busy-timeout=DURATION`
:   How long to wait for the write lock, as a Go duration such as `5s` or `500ms`. The default is `5s`. It must be greater than 0.

`--skip-foreign-key-check`
:   Do not run `PRAGMA foreign_key_check` before the commit. For a database that holds rows breaking a foreign key from before.

`--no-create`
:   Fail when the database file does not exist, instead of creating it. An in-memory database is not affected.

## Exit status

0 on success. 1 on an error, after which nothing has changed. 3 when a rebuild is needed and `--allow-table-rebuild` is not given. 80 on a usage error.

## Notes

`foreign_keys` is a setting of each connection and is off by default. The `PRAGMA` in step 1 changes only the connection hazel opens; it does not affect the application's connections.

A foreign-key violation that was in the database before the run fails the check too. Use `--skip-foreign-key-check`, or mark the tables that hold such rows with `-- hazel:skip-foreign-key-check`.

## See also

[plan](plan.md), [Directives](../directives.md)
