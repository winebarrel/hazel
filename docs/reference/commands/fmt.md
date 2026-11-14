# fmt

fmt -- lay out schema files in place

## Synopsis

```
hazel fmt [--check] [--strip-renamed-from] SCHEMA_FILE...
```

## Description

`fmt` rewrites the schema files in the layout [`dump`](dump.md) uses, and prints nothing.

```sql
-- before
-- users
-- hazel:renamed-from members
CREATE TABLE "users" (id INTEGER PRIMARY KEY,name TEXT); -- the body
CREATE INDEX users_name ON users (name);


-- hazel:execute SELECT NOT EXISTS (SELECT 1 FROM settings)
INSERT INTO   settings VALUES (1);
```

```sql
-- after
-- users
-- hazel:renamed-from members
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT
); -- the body

CREATE INDEX users_name ON users (name);

-- hazel:execute SELECT NOT EXISTS (SELECT 1 FROM settings)
INSERT INTO   settings VALUES (1);
```

- The statements are separated by one blank line.
- The comments before a statement, directives included, stay before it, each on a line of its own at the first column. A run of blank lines between them becomes one.
- A comment on the line of a statement's `;` stays on that line.
- The CREATE statements are laid out as `dump` lays them out, and get a `;` when they lack one. When a `--` comment comes right before the `;`, the `;` goes on the next line.
- Any other statement, and a statement marked with `-- hazel:execute`, is left as it is written. An empty statement (`;;`) is removed.

Before anything is written, the files are loaded together as they are and as they would be formatted, and the two must give the same schema and the same `-- hazel:execute` statements. They are loaded together because a view may read a table in another file. A file that does not load is an error, and no file is written.

## Options

`--check`
:   Write nothing. Print the names of the files that are not formatted, and exit with status 2 when there are any.

`--strip-renamed-from`
:   Remove the [`-- hazel:renamed-from`](../directives.md#renamed-from) directives of tables and columns. For cleaning up once the renames are applied.

Neither option can be set in the configuration file.

## Exit status

0 on success. 1 on an error. 2 with `--check` when a file is not formatted. 80 on a usage error.

## See also

[dump](dump.md)
