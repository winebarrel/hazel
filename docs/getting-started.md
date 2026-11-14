# Getting started

This page takes an existing database under hazel's management: the schema is written to a file, the file is changed, and the change is applied.

## Install

```
brew install winebarrel/hazel/hazel
```

See [Installation](installation.md) for the other ways.

## Write the schema to a file

`hazel dump` prints the schema of the database:

```
$ hazel dump app.db > schema.sql
$ cat schema.sql
-- Database: /path/to/app.db
-- Dump (2 tables, 0 views, 1 index, 0 triggers)

CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE posts (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users (id),
    body TEXT
);

CREATE INDEX posts_user_id ON posts (user_id);
```

From now on, `schema.sql` is the schema of the database. Given back to `plan`, it has nothing to do:

```
$ hazel plan app.db schema.sql
-- Database: /path/to/app.db
-- Plan (2 tables, 0 views, 1 index, 0 triggers)
-- No changes
```

For one file per table and view, use `hazel dump --split=schema app.db` and give `schema/*.sql` to the commands.

## Change the schema

Edit the file to the schema the database should have. Here `users` gets an `email` column with a unique index, and `posts` a `created_at` column:

```sql
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT
);

CREATE UNIQUE INDEX users_email ON users (email);

CREATE TABLE posts (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users (id),
    body TEXT,
    created_at TEXT
);

CREATE INDEX posts_user_id ON posts (user_id);
```

`hazel plan` prints the DDL that takes the database there, without running it:

```
$ hazel plan app.db schema.sql
-- Database: /path/to/app.db
-- Plan (2 tables, 0 views, 2 indexes, 0 triggers)
ALTER TABLE users ADD COLUMN email TEXT;
ALTER TABLE posts ADD COLUMN created_at TEXT;
CREATE UNIQUE INDEX users_email ON users (email);
```

The new column is written at the end of the table, since that is the only place `ADD COLUMN` can put it; the order of the columns is not compared. A change that `ALTER TABLE` cannot make, such as a new column type, needs a rebuild of the table; `plan` then fails with exit status 3 until `--allow-table-rebuild` is given. See [Schema objects](reference/objects.md).

## Apply it

`hazel apply` runs the plan in one transaction:

```
$ hazel apply app.db schema.sql
-- Database: /path/to/app.db
-- Plan (2 tables, 0 views, 2 indexes, 0 triggers)
ALTER TABLE users ADD COLUMN email TEXT;
-- ok (0ms)
ALTER TABLE posts ADD COLUMN created_at TEXT;
-- ok (0ms)
CREATE UNIQUE INDEX users_email ON users (email);
-- ok (0ms)
-- Applied 3 statements
```

If a statement fails, nothing is changed. A new database is set up the same way: `apply` creates the file when it does not exist.

## Keep the file and the database in step

`plan --check` exits with status 2 when the database is not what the files say, which suits a CI job or a deploy step:

```
$ hazel plan --check app.db schema.sql > /dev/null; echo $?
0
```

`hazel fmt schema.sql` lays out a file the way `dump` writes it, and `hazel fmt --check` reports the files that are not laid out so.

## Next steps

- Rename a table or a column without losing its data: [Renaming](guides/renaming.md).
- Keep a table out of hazel's hands, or run SQL after the DDL: [Directives](reference/directives.md).
- Put options in a file: [Configuration](reference/configuration.md).
