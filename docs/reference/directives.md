# Directives

A directive is a line comment of the form `-- hazel:NAME [ARGUMENT]` written on a line of its own before the statement, or the column, it applies to.

```sql
-- hazel:renamed-from users
CREATE TABLE members (
    id INTEGER PRIMARY KEY,
    -- hazel:renamed-from name
    full_name TEXT
);
```

A directive on the line of the `;` or the `,` before it is an error: written there, it reads as if it were for the statement or the column before. An unknown directive name is an error. Directives are not written to the database.

| Directive | Applies to |
| --- | --- |
| [`renamed-from OLD_NAME`](#renamed-from) | `CREATE TABLE`, a column of a `CREATE TABLE` |
| [`ignore`](#ignore) | `CREATE TABLE`, `CREATE VIRTUAL TABLE`, `CREATE VIEW` |
| [`execute [CONDITION]`](#execute) | Any statement |
| [`skip-foreign-key-check`](#skip-foreign-key-check) | `CREATE TABLE` |

## renamed-from

```sql
-- hazel:renamed-from users
CREATE TABLE members (...);
```

The table, or the column, had the name `OLD_NAME`. The plan renames it with `ALTER TABLE ... RENAME TO` or `RENAME COLUMN` instead of dropping it and creating a new one. Renames are never guessed.

```
ALTER TABLE users RENAME TO members;
ALTER TABLE members RENAME COLUMN name TO full_name;
```

`OLD_NAME` is a name as SQL writes it, so a name that needs quotes is written with them: `-- hazel:renamed-from "old name"`.

SQLite updates the foreign keys, views and triggers that refer to the renamed table or column, so they get no DDL of their own. The renames run before any other change to the tables, and a table that is also rebuilt is renamed first and copied from the new name.

When the database has no object of the old name, the directive does nothing, so it can stay in the file after the rename is applied. `hazel fmt --strip-renamed-from` removes the directives once they are no longer needed.

Indexes, views and triggers have no rename: they hold no data and are dropped and created.

## ignore

```sql
-- hazel:ignore
CREATE TABLE legacy (id INTEGER PRIMARY KEY, body TEXT);
```

hazel does not create, change or drop the table, view or virtual table, nor its indexes and triggers, including those that are only in the database. Use it for an object that another tool manages, or that must never be dropped.

The statement is still loaded, so a managed view that reads an ignored table can be compiled. When a change to a managed object would also take an ignored one with it, such as a `DROP COLUMN` that an ignored view reads, hazel does not recreate the ignored object, and `apply` fails.

`ignore` cannot be given to an index or a trigger, which belong to their table or view, nor to a column or a constraint.

## execute

```sql
-- hazel:execute SELECT NOT EXISTS (SELECT 1 FROM settings WHERE key = 'version')
INSERT INTO settings (key, value) VALUES ('version', '1');
```

The statement is not part of the schema. `apply` runs it after the DDL, in the order the execute statements are written, inside the same transaction. Use it for data the schema needs, or for a statement hazel does not manage, such as a `PRAGMA`.

Without a condition, the statement runs on every `apply`. With one, it runs only when the condition returns 1:

- The condition is a query that returns one row and one column, 0 or 1. Write it with `EXISTS`, `NOT EXISTS` or a comparison: `SELECT count(*) > 0 FROM t`. Any other result, NULL and no row included, is an error.
- `plan` evaluates the condition before the DDL and leaves out the statement when it returns 0. A condition that fails there, because it reads a table the plan creates for example, keeps its statement with a note, and `apply` decides after the DDL.
- `apply` evaluates the condition after the DDL.

`diff` reads no database and shows no execute statement.

## skip-foreign-key-check

```sql
-- hazel:skip-foreign-key-check
CREATE TABLE events (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users (id));
```

`apply` leaves the table out of the `PRAGMA foreign_key_check` it runs before the commit. Use it for a table that held rows breaking a foreign key before hazel managed it. `apply --skip-foreign-key-check` turns the check off for the whole database.
