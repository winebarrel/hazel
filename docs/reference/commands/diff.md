# diff

diff -- print the DDL that takes one schema file to another

## Synopsis

```
hazel diff [-I PAT]... [-E PAT]... [--allow-table-rebuild] [--check] OLD_SCHEMA_FILE NEW_SCHEMA_FILE
```

## Description

`diff` loads both schema files as [`plan`](plan.md) loads the desired one, and prints the DDL that takes the schema of `OLD_SCHEMA_FILE` to that of `NEW_SCHEMA_FILE`. No database is read, so the output has no `-- Database:` line, and the statements marked with `-- hazel:execute` are not shown.

```
$ hazel diff old.sql new.sql
-- WARNING: the following will be dropped and their data will be lost:
--   table  tmp_x
--
ALTER TABLE t ADD COLUMN b;
-- drop: removed from schema
DROP TABLE tmp_x;
CREATE TABLE u (
    c
);
```

The output and the rules are those of `plan`: a rebuild needs `--allow-table-rebuild`, and the directives of `NEW_SCHEMA_FILE` apply.

## Options

`-I PAT`, `--include=PAT`, `-E PAT`, `--exclude=PAT`
:   Select the tables, views and virtual tables to manage, as for [`plan`](plan.md#filtering).

`--allow-table-rebuild`
:   Allow rebuilding a table for a change `ALTER TABLE` cannot make.

`--check`
:   Exit with status 2 when there are changes.

## Exit status

0 on success, also when there are changes. 1 on an error. 2 with `--check` when there are changes. 3 when a rebuild is needed and `--allow-table-rebuild` is not given. 80 on a usage error.

## Examples

Review the DDL that a change to the schema file will run, from git:

```
git show HEAD~1:schema.sql > /tmp/old.sql
hazel diff /tmp/old.sql schema.sql
```

## See also

[plan](plan.md)
