# Real-world schema tests

hazel is checked against SQLite schemas that projects on GitHub ship, not only against the fixtures in `diff/testdata/` and `dump/testdata/`. Each schema file is downloaded, loaded into an empty database, and round-tripped through `hazel dump` and `hazel plan`.

## Running

```sh
make test-realworld
```

The target runs `test/realworld/run.sh`, which builds `hazel` and the loader and needs `curl` and access to `raw.githubusercontent.com`. No database server is needed. CI runs it as the `realworld` job.

To check some of the schemas, name them in `REALWORLD`, separated by commas. A name the list does not hold stops the run:

```sh
make test-realworld REALWORLD=sakila,pihole
```

`REALWORLD_CACHE` names a directory in which the downloads are kept, one file per schema and commit, and read from there on the next run. CI keeps the directory between runs.

```sh
REALWORLD_CACHE=~/.cache/hazel-realworld make test-realworld
```

## What the check does

For each schema, the runner:

1. Downloads the file at its pinned commit.
2. Loads it into an empty database with `test/realworld/load`. The loader hands the whole file to SQLite through modernc.org/sqlite, the driver hazel uses, so the schema is created by SQLite alone, with the SQLite version hazel reads it with, and hazel's parser plays no part. Statements other than CREATE, such as the `INSERT`s of chinook, run as well.
3. Runs `hazel dump` on the database.
4. Runs `hazel plan` with the dump as the schema file, and requires `-- No changes`.

`dump` goes from the catalog to SQL, and `plan` loads that SQL and compares it with the catalog. If the plan is not empty, the two disagree about the schema. That is a bug, whichever side is wrong.

Each schema reports `PASS`, `DRIFT` (the plan was not empty, or failed), or `FAIL` with the stage that failed (`fetch`, `load` or `dump`). The output of a failure is printed indented under the name, and the script exits non-zero when any schema did not pass.

## Schemas

The list is the `REALWORLD_SCHEMAS` variable in `realworld.mk`, one record per line: name, repository, commit and path. Every source is pinned to a commit, so an upstream change cannot turn CI red on its own. To move a schema to a newer commit, resolve it with `git ls-remote https://github.com/<owner>/<repo> HEAD`, replace the SHA, and run the test for that schema.

| Name | Objects | Source |
| --- | --- | --- |
| anki | 5 tables, 7 indexes | [ankitects/anki](https://github.com/ankitects/anki) |
| chinook | 11 tables, 11 indexes | [lerocha/chinook-database](https://github.com/lerocha/chinook-database) |
| ejabberd | 42 tables, 60 indexes | [processone/ejabberd](https://github.com/processone/ejabberd) |
| freeradius | 9 tables, 20 indexes | [FreeRADIUS/freeradius-server](https://github.com/FreeRADIUS/freeradius-server) |
| mediawiki | 66 tables, 137 indexes | [wikimedia/mediawiki](https://github.com/wikimedia/mediawiki) |
| memos | 13 tables, 8 indexes | [usememos/memos](https://github.com/usememos/memos) |
| pihole | 10 tables, 7 views, 2 indexes, 11 triggers | [pi-hole/pi-hole](https://github.com/pi-hole/pi-hole) |
| powerdns | 7 tables, 11 indexes | [PowerDNS/pdns](https://github.com/PowerDNS/pdns) |
| roundcube | 18 tables, 18 indexes | [roundcube/roundcubemail](https://github.com/roundcube/roundcubemail) |
| sakila | 16 tables, 5 views, 24 indexes, 30 triggers | [jOOQ/sakila](https://github.com/jOOQ/sakila) |
| temporal | 37 tables | [temporalio/temporal](https://github.com/temporalio/temporal) |
| zotero | 54 tables, 38 indexes | [zotero/zotero](https://github.com/zotero/zotero) |

## Schemas left out

| Project | Why |
| --- | --- |
| [calibre](https://github.com/kovidgoyal/calibre) (`resources/metadata_sqlite.sql`) | Its views call `sortconcat` and other functions that calibre registers with SQLite. Application-defined functions are out of hazel's scope, and a view that calls one cannot be read. |
| [synapse](https://github.com/element-hq/synapse) (`full.sql.sqlite`) | It creates an `fts4` virtual table, and modernc.org/sqlite is built without FTS3/FTS4, so the file does not load. |
