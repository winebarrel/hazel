<!-- On release, rename [Unreleased] to the version and date, point [unreleased] at the new tag and add the link of the version below it. Do not leave an [Unreleased] section behind. -->

# Changelog

## [Unreleased]

* The `hazel` command, with `plan` and `diff`. `plan DB SCHEMA_FILE...` prints the DDL that takes a database to the schema files, and `diff OLD_SCHEMA_FILE NEW_SCHEMA_FILE` the DDL that takes one schema file to another. A plan that rebuilds a table fails with exit status 3 unless `--allow-table-rebuild` is given, `--check` exits with status 2 when there are changes, and `-I` / `-E` select the tables, views and virtual tables by name. Options can also come from environment variables (`HAZEL_*`) and from a YAML file given with `-C`.

[unreleased]: https://github.com/winebarrel/hazel/commits/main
