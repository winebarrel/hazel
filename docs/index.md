# ![hazel](assets/logo.webp)

hazel is a declarative schema management tool for SQLite. The desired schema is written as CREATE statements in schema files. hazel compares it with the schema of a database and prints, or runs, the DDL that brings the database in line with the files.

```
$ hazel plan app.db schema.sql
-- Database: /path/to/app.db
-- Plan (2 tables, 0 views, 1 index, 0 triggers)
ALTER TABLE users ADD COLUMN email TEXT;
CREATE INDEX users_email ON users (email);
```

<video src="https://github.com/user-attachments/assets/bf842f17-34a3-4966-b49f-be3269bf7773"
       width="800" style="max-width: 100%"
       controls autoplay muted loop playsinline></video>

## How it works

1. The schema files are run in an in-memory database, and the resulting schema is read back from `sqlite_schema` and the schema PRAGMAs. The same reader reads the target database.
2. The two schemas are compared object by object. Expressions and views are compared by the bytecode SQLite compiles them to, so the same definition written differently is not a change.
3. The difference becomes DDL: `ALTER TABLE` where SQLite supports the change, and otherwise a rebuild of the table, which runs only when `--allow-rebuild` is given.
4. `plan` prints the DDL, and `apply` runs it in one transaction.

`hazel dump` prints the schema of a database in the same form. Given back as the schema file, a dump plans clean.

## Try it with Docker

A demo image bundles hazel, sqlite3 and a sample database. It lets you try `hazel` without a local install:

```bash
docker run --rm -it ghcr.io/winebarrel/hazel-demo
```

The container starts a shell in `/demo`, where `demo.db` holds the schema of `current.sql`. Edit `desired.sql`, then run:

```bash
hazel plan  --allow-rebuild demo.db desired.sql    # show the DDL diff
hazel apply --allow-rebuild demo.db desired.sql    # apply the changes
hazel plan  demo.db desired.sql                          # ...should now print -- No changes
hazel dump  demo.db                                      # dump the current schema
```

`desired.sql` changes the type of a column of `posts`, which needs a rebuild of the table, so `plan` and `apply` fail without `--allow-rebuild`.

The source for the image is under [`demo/`](https://github.com/winebarrel/hazel/tree/main/demo).

## Requirements

hazel uses [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite), a SQLite driver written in Go, and needs no C compiler and no SQLite library. The SQLite version is the one the driver bundles, 3.53.0 or later; `hazel --version` prints it.

## Scope

Only the `main` schema is managed. The temp schema, attached databases, extensions, application-defined functions and file-level PRAGMAs such as `user_version` and `journal_mode` are out of scope. Names that begin with `sqlite_` are ignored. Concurrent changes by other connections are out of scope; `apply` takes the write lock for its whole run.

## Where to go next

- [Getting started](getting-started.md) takes an existing database under hazel's management.
- [Commands](reference/commands/index.md) describes `plan`, `apply`, `dump`, `fmt` and `diff`.
- [Schema objects](reference/objects.md) lists what hazel manages and how each change is made.
- [Directives](reference/directives.md) describes the `-- hazel:` comments in schema files.
