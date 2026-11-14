# Renaming

hazel matches tables and columns by name, so a new name in the schema file reads as a new object, and the old one as removed. Without more, the plan drops the old table and creates an empty one:

```
$ hazel plan app.db schema.sql
-- Database: /path/to/app.db
-- Plan (1 table, 0 views, 0 indexes, 0 triggers)
-- WARNING: the following will be dropped and their data will be lost:
--   table  users
--
-- drop: removed from schema
DROP TABLE users;
CREATE TABLE members (
    id INTEGER PRIMARY KEY,
    display_name TEXT NOT NULL,
    email TEXT
);
```

A rename is said with the [`-- hazel:renamed-from`](../reference/directives.md#renamed-from) directive. hazel never guesses one.

## Rename

Write the new name, and the old one in the directive, on the table or on the column:

```sql
-- hazel:renamed-from users
CREATE TABLE members (
    id INTEGER PRIMARY KEY,
    -- hazel:renamed-from name
    display_name TEXT NOT NULL,
    email TEXT
);

CREATE UNIQUE INDEX users_email ON members (email);

CREATE TABLE posts (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES members (id),
    body TEXT,
    created_at TEXT
);

CREATE INDEX posts_user_id ON posts (user_id);
```

The plan renames instead of dropping:

```
$ hazel plan app.db schema.sql
-- Database: /path/to/app.db
-- Plan (2 tables, 0 views, 2 indexes, 0 triggers)
ALTER TABLE users RENAME TO members;
ALTER TABLE members RENAME COLUMN name TO display_name;
```

SQLite rewrites what refers to the renamed table or column: the foreign key of `posts`, the index `users_email`, and any view or trigger. They match the schema file after the rename, so they get no DDL. Write them in the file with the new names, as above.

The renames run before any other change to the tables. When the same table also needs a rebuild, it is renamed first and copied from the new name.

An index keeps its name. To rename an index, change its name in the file; it holds no data, so the plan drops it and creates it.

## After the rename

Once the rename is applied, the database has no object of the old name, and the directive does nothing:

```
$ hazel apply app.db schema.sql
...
-- Applied 2 statements
$ hazel plan app.db schema.sql
-- Database: /path/to/app.db
-- Plan (2 tables, 0 views, 2 indexes, 0 triggers)
-- No changes
```

The directive can stay in the file until every database the file is applied to has the new name. Then remove the directives with `fmt`:

```
$ hazel fmt --strip-renamed-from schema.sql
```

Removing a directive before a database is renamed makes the next plan for that database drop the table, as at the top of this page. The `-- WARNING:` at the top of the plan lists the tables and columns whose data would be lost.
