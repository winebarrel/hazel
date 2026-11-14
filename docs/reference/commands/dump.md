# dump

dump -- print the schema of a database as SQL

## Synopsis

```
hazel dump [-I PAT]... [-E PAT]... [--no-format] [--split=DIR] DB
```

## Description

`dump` prints the CREATE statements of the database `DB`. The database file must exist; it is opened read-only.

Each table comes first with its indexes and triggers, then each view with its triggers, every kind in the order the objects were created in. A trigger needs its table or view, and SQLite does not look up what a view reads until the view is used, so the output runs as it is.

```
-- Database: /path/to/app.db
-- Dump (2 tables, 1 view, 1 index, 0 triggers)

CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL, -- display name
    CHECK (length("name") > 0)
);

CREATE INDEX users_name ON users (name);

CREATE TABLE posts (
    id INTEGER PRIMARY KEY,
    user_id INTEGER REFERENCES users (id),
    body TEXT
);

CREATE VIEW active_users AS SELECT * FROM users WHERE name <> '';
```

Given back as the schema file, the output plans clean: `hazel plan DB dump.sql` prints `-- No changes`. `hazel fmt` leaves it as it is.

The statements are laid out from the SQL that SQLite keeps in `sqlite_schema`:

- A `CREATE TABLE` gets one column or table constraint per line, indented by four spaces. A line break inside a column or a constraint is kept, and the line is indented by the depth of the parentheses. Any other whitespace becomes one space.
- A comment inside a `CREATE TABLE` is kept. A comment on the line where a column ends follows the column's comma; a comment on a line of its own goes before the next column.
- Indexes, views, triggers, virtual tables and `CREATE TABLE ... AS SELECT` keep their layout.
- A name loses quotes it does not need where the statement names something: the object, the table of an index or a trigger, the columns of a view, and in a `CREATE TABLE` the columns, the constraint names, the tables a foreign key refers to and the column lists of the constraints. A name needs its quotes when it is a keyword or is not a plain ASCII identifier.
- Nothing else changes: keywords, names and expressions keep the case and the text they were written with.

Expressions, the columns of an index and the bodies of views and triggers keep their quotes. There SQLite takes a `"..."` that names no column as a string, so removing the quotes could change what the statement means.

Comments outside the CREATE statements are not in the database, so a dump does not have them.

## Options

`-I PAT`, `--include=PAT`, `-E PAT`, `--exclude=PAT`
:   Select the tables, views and virtual tables to dump, as for [`plan`](plan.md#filtering). Indexes and triggers go with the table or view they belong to.

`--no-format`
:   Print the statements as SQLite keeps them, without laying them out.

`--split=DIR`
:   Write a file for each table, view and virtual table to the directory `DIR`, instead of printing the statements.

## Splitting into files

With `--split`, each table, view and virtual table gets a file named after it with `.sql`, which holds its CREATE statement, then its indexes and triggers. The standard output only reports what was written:

```
$ hazel dump --split=schema app.db
-- Database: /path/to/app.db
-- Wrote 3 files to schema
```

- The directory is created, with its parents, if it does not exist.
- A file of the same name is overwritten. A file of an object that is no longer in the database is left as it is; removing it is up to the user.
- A name that is the same as an earlier one when case is ignored gets `_2`, `_3` and so on. SQLite folds only ASCII letters, so names that differ only in the case of a letter outside ASCII, such as an accented E, are two tables; a file system that ignores case would keep one file for both.
- A name with `/` or `\`, or one that is not a plain file name on the platform, is an error, and no file is written.

`--split` cannot be set in the configuration file.

## Exit status

0 on success. 1 on an error. 80 on a usage error.

## See also

[fmt](fmt.md), [plan](plan.md)
