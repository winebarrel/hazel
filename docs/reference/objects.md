# Schema objects

## Objects

| Object | Managed | Notes |
| --- | --- | --- |
| Table | Yes | Including `WITHOUT ROWID`, `STRICT`, `AUTOINCREMENT` and `CREATE TABLE ... AS SELECT`. |
| Index | Yes | Including `UNIQUE`, expressions, partial indexes, `COLLATE` and `ASC` / `DESC`. |
| View | Yes | |
| Trigger | Yes | Including `BEFORE`, `AFTER`, `INSTEAD OF`, `UPDATE OF` and `WHEN`. |
| Virtual table | Yes | Such as fts5 and rtree. See [Virtual tables](#virtual-tables). |
| Automatic index | With its table | The `sqlite_autoindex_*` indexes of `PRIMARY KEY` and `UNIQUE` constraints are part of the table. |
| Shadow table | No | The tables a virtual table module creates for its data. |
| `sqlite_` names | No | `sqlite_schema`, `sqlite_sequence`, `sqlite_stat1` and the like. Names that begin with `sqlite_`, in any case, are ignored on both sides. |
| File-level PRAGMAs | No | `user_version`, `application_id`, `journal_mode` and the like. A `PRAGMA` in a schema file is ignored with a warning; use [`-- hazel:execute`](directives.md#execute) to run one. |
| temp schema, attached databases | No | |

## How each change is made

SQLite's `ALTER TABLE` can rename a table or a column, add or drop a column, add or drop `NOT NULL`, add a `CHECK` constraint, and drop a named constraint. Any other change to a table needs a rebuild.

| Change | Made with |
| --- | --- |
| Add a column | `ALTER TABLE ... ADD COLUMN`, unless the column needs a rebuild (below) |
| Drop a column | `ALTER TABLE ... DROP COLUMN`, unless the column needs a rebuild (below) |
| Rename a table or a column | `ALTER TABLE ... RENAME`, with [`-- hazel:renamed-from`](directives.md#renamed-from) |
| Add or drop `NOT NULL` | `ALTER TABLE ... ALTER COLUMN ... SET NOT NULL` / `DROP NOT NULL` |
| Add a `CHECK` constraint | `ALTER TABLE ... ADD [CONSTRAINT name] CHECK (...)` |
| Drop a named `CHECK` or `FOREIGN KEY` constraint | `ALTER TABLE ... DROP CONSTRAINT` |
| Create, drop or change an index | `CREATE INDEX` / `DROP INDEX` |
| Create, drop or change a view or a trigger | `CREATE` / `DROP` |
| Change a virtual table | `DROP` and `CREATE`. Its data is lost. |
| Change the type, `DEFAULT` or `COLLATE` of a column | Rebuild |
| Change the `PRIMARY KEY`, add or drop a `UNIQUE` constraint | Rebuild |
| Add or change a `FOREIGN KEY` | Rebuild |
| Drop an unnamed `CHECK` or `FOREIGN KEY` constraint | Rebuild |
| Change the `ON CONFLICT` clause of a `PRIMARY KEY` or `UNIQUE` | Rebuild |
| Change the expression of a generated column, or turn a column into one or back | Rebuild |
| Turn `STRICT`, `WITHOUT ROWID` or `AUTOINCREMENT` on or off | Rebuild |
| Add a column with `PRIMARY KEY` or `UNIQUE`, a `STORED` generated column, or a column whose `DEFAULT` is not a constant | Rebuild |
| Drop a column that is in the `PRIMARY KEY` or a `UNIQUE` constraint, or that an unnamed constraint or a remaining generated column refers to | Rebuild |

`SET NOT NULL` and `ADD CHECK` check the existing rows, and fail when one breaks the constraint. Adding a `NOT NULL` column without a non-NULL `DEFAULT` fails when the table has rows. A [`-- hazel:backfill`](directives.md#backfill) on the column fills those rows first, with an `UPDATE`.

A table that needs a rebuild gets no `ALTER TABLE`: the new table is created from the schema file, so it takes every change at once. Name constraints that may have to be dropped later: a named one can be dropped with `ALTER TABLE`, an unnamed one only by a rebuild.

## Rebuilding a table

A rebuild follows the steps of the SQLite documentation ([Making Other Kinds Of Table Schema Changes](https://www.sqlite.org/lang_altertable.html#otheralter)):

```
-- table rebuild: users (type of age)
CREATE TABLE new_users (...);
INSERT INTO new_users (id, name, age) SELECT id, name, age FROM users;
DROP TABLE users;
ALTER TABLE new_users RENAME TO users;
CREATE INDEX users_name ON users (name);
```

- The columns that are in both the old and the new table are copied. A new column takes its `DEFAULT`, or the value of its [`-- hazel:backfill`](directives.md#backfill), which also fills the NULLs of a column made `NOT NULL`.
- In a rowid table without an `INTEGER PRIMARY KEY`, the `rowid` is copied as well, so the rowids do not change.
- With `AUTOINCREMENT`, the value in `sqlite_sequence` is carried over.
- The indexes and triggers of the table are created again, and the views and triggers that depend on it are dropped first and created again after.

A rebuild runs only with `--allow-table-rebuild`. `apply` turns `foreign_keys` off for its connection and runs `PRAGMA foreign_key_check` before the commit.

## Drops

What is in the database but not in the schema files is dropped: tables, columns, indexes, views, triggers and constraints. There is no option that turns drops off. Each drop is printed with a comment that says why, and the tables and columns whose data is lost are listed in a `-- WARNING:` at the top of the plan.

To keep an object out of hazel's hands, mark it with [`-- hazel:ignore`](directives.md#ignore) or leave it out with `-E`.

## How objects are compared

Objects are matched by name. Names are compared with ASCII letters folded, as SQLite compares them.

| What | Compared by |
| --- | --- |
| Columns | Name. The order of the columns is not compared, since SQLite cannot reorder them; a new column is added at the end. |
| Constraints | Content: the columns, the actions and the expressions. Names are not compared, and a constraint that only changes its name is not a change. |
| `NOT NULL`, keys, foreign key actions, index columns | The schema PRAGMAs. |
| `CHECK`, `DEFAULT`, generated columns, index expressions, the `WHERE` of a partial index | The bytecode of `EXPLAIN SELECT (expr) FROM t`. `a != 0` and `a <> 0` are the same; `a > 0` and `0 < a` are not. |
| Views | The bytecode of `EXPLAIN SELECT * FROM v`, and the names of its columns, compared with case. |
| Type names, collation names, triggers, virtual tables | Tokens, with whitespace, comments, ASCII case and the quotes of names ignored. |

An expression that does not compile, such as a `DEFAULT` written as a bare word (`DEFAULT active`), is compared by tokens. Expressions are not normalized beyond that: `DEFAULT active` and `DEFAULT 'active'` mean the same to SQLite, but changing one to the other is a change, and rebuilds the table once.

## Virtual tables

A virtual table is created and dropped like a table. SQLite cannot alter one, so a change to its definition drops it and creates it again, and its data is lost; the table is listed in the `-- WARNING:` of the plan. Its arguments are compared as tokens, not parsed.

The shadow tables a module creates for its data are left to the module.
