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
| [`backfill EXPRESSION`](#backfill) | A column of a `CREATE TABLE` |

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

## backfill

```sql
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    first_name TEXT,
    last_name TEXT,
    -- hazel:backfill first_name || ' ' || last_name
    full_name TEXT NOT NULL
);
```

`EXPRESSION` is an SQL expression evaluated for each row, which can read the other columns of the row. It fills the rows already in the table in two cases:

- When the plan adds the column, every existing row gets the value.
- When the plan makes an existing column `NOT NULL`, the rows where the column is NULL get the value. The rows that hold a value keep it.

In any other case the directive does nothing, so it can stay in the file.

hazel fills the rows with an `UPDATE`. A column added with `ALTER TABLE` is added without its `NOT NULL`, filled, and then made `NOT NULL`:

```
ALTER TABLE users ADD COLUMN full_name TEXT;
-- backfill: full_name
UPDATE users SET full_name = (first_name || ' ' || last_name);
ALTER TABLE users ALTER COLUMN full_name SET NOT NULL;
```

An existing column made `NOT NULL` is filled where it is NULL:

```
-- backfill: name
UPDATE users SET name = ('user-' || id) WHERE name IS NULL;
ALTER TABLE users ALTER COLUMN name SET NOT NULL;
```

So a `NOT NULL` column with no `DEFAULT` can be added to a table that has rows, and a column that holds NULLs can be made `NOT NULL`. The `UPDATE` writes every row it fills, so on a large table it takes as long as the same `UPDATE` written by hand, and it runs the `UPDATE` triggers of the table.

When the table is rebuilt, no `UPDATE` runs: the copy fills the column, with the expression for an added column and with `coalesce(name, (EXPRESSION))` for a column made `NOT NULL`.

The `DEFAULT` of the column still applies to the rows inserted later. To give the existing rows and the later ones the same constant when a column is added, write a `DEFAULT` instead: SQLite adds such a column without touching the rows. A `DEFAULT` does not fill the NULLs of an existing column.

A generated column cannot be backfilled. The expression is not checked by hazel; an error in it fails `apply`.
