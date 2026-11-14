# Repository guidelines for AI agents

## Project overview

hazel is a declarative schema management tool for SQLite, written in Go. It reads desired schema definitions from SQL files and generates the DDL that brings the current database in line with the desired state.

hazel requires SQLite 3.53.0 or later and uses modernc.org/sqlite, a pure Go driver that bundles SQLite. The SQLite version is fixed by the driver version.

## Design policy

`hazel dump` output fed back as the desired schema must plan clean. A break in that round trip is a bug.

hazel is not meant to be used as a library.

Only the `main` schema is managed. The temp schema, ATTACH, extensions, application-defined functions and file-level PRAGMAs such as `user_version` and `journal_mode` are out of scope. Names that begin with `sqlite_` are ignored on both sides.

Concurrency with other connections is out of scope.

Avoid normalization that makes the diff complex.

Never rebuild a table implicitly.

Drops are always executed, but never silently.

New features are opt-in. Turning off a feature that is on by default is hard once users depend on it, so ship it behind a flag or a directive.

Do not chase corner cases. A rare input is not worth an implementation that is hard to follow, and the schemas people actually write come first.

Judge a bug by a concrete use case. A bug reached only through an operation users rarely perform is low priority, unless it loses data.

Do not coddle the user. Assume they know SQLite and their own schema.

## Build & test

```sh
make build # go build ./cmd/hazel (outputs ./hazel at the repo root)
make vet   # go vet ./...
make test  # go test -v ./... $(TEST_OPTS)
make lint  # golangci-lint run
make fix   # golangci-lint run --fix (auto-fix lint errors)
```

- The tests need no database server. CI runs them on Linux, macOS and Windows, with `-coverpkg=./...`, since the CLI tests in `cmd/hazel` cover `cmd/command`.

## Project structure

- `cmd/hazel/` - CLI entrypoint, and the CLI tests
- `cmd/command/` - CLI command implementations, the -I/-E filter and the YAML config loader
- `catalog/` - Reads the schema of a database from `sqlite_schema` and the schema PRAGMAs into `model`
- `db/` - Resolves the DB argument and opens the database
- `diff/` - Computes the plan, the DDL that takes the current schema to the desired one
- `desired/` - Loads the desired schema: runs the CREATE statements of the schema files in `:memory:` and reads them back with `catalog`
- `model/` - The schema objects hazel compares (tables, columns, constraints, indexes, views, triggers)
- `parser/` - Tokenizer, statement splitter, statement classification, CREATE TABLE parsing (what PRAGMAs do not report), identifier quoting and token comparison for schema files

## Development workflow

1. Create a feature branch before starting implementation.
2. Write tests that assert the expected behavior first, confirm they fail, then implement the fix/feature.
3. Prefer simplicity; avoid complex implementations when a straightforward approach works.
4. After implementation:
   - Verify test cases are comprehensive (check for missing scenarios and edge cases).
   - Verify coverage has not decreased and cover any reachable paths that can be tested naturally (do not write unnatural tests for unreachable defensive code).
   - Consider whether similar issues exist elsewhere in the codebase.
   - Run `make lint` to check for lint errors.
   - Add a `CHANGELOG.md` entry under `## [Unreleased]` when the change is one a user of hazel would notice, creating that section above the newest release if it is not there. The release fills in the version and date, and the link definitions at the bottom of the file; leave them alone. What they cannot see gets no entry: a test or fixture, documentation, a dependency bump, or a refactor that leaves behavior alone.

## Pull request labels

Give each pull request one label for the kind of change it is. The release pull request lists the pull requests it releases grouped by label, `feat`, `bug`, `refactor` and `test` first, and a pull request with no label goes under `other`.

- `feat` - a new feature, or a change to what hazel does that is not a fix.
- `bug` - a fix for behavior that was wrong.
- `refactor` - a code change that leaves behavior alone.
- `test` - tests, fixtures or the test harness only.
- `documentation` - documentation only.

Leave a change to CI, the build or other repository tooling without a label. Do not add `release` or `dependencies`: the release workflow adds the first, and Renovate and Dependabot the second.

## Code conventions

- Package-level tests generally use external test packages (e.g., `package parser_test`). Use same-package tests only when access to unexported identifiers is required.
- New plan/apply/dump tests should be added as YAML fixtures in `testdata/` whenever the test is purely SQL-input -> SQL/dump-output. Reach for a Go test only when the scenario cannot be expressed that way. When the harness lacks a field for a behavior you want to assert in a fixture, prefer extending the test case struct with one optional field (defaulting to nil/zero so existing fixtures are unaffected) over keeping the test in Go.
- Diff tests are YAML fixtures in `diff/testdata/`, one case per file: `init` runs on an empty database to give the current schema, `desired` is loaded as the schema files, and either `plan` (the expected `Plan.String()`) or `error` (a part of the expected error) is given. Every case with a plan also applies it to the database and requires the diff to be empty afterwards, unless `apply_error` gives a part of the error the apply is expected to fail with.
- Use `orderedmap.Map` (github.com/winebarrel/orderedmap) for deterministic iteration order of schema objects.
- Let kong do what kong does. A flag's type, default, allowed values and the combinations it refuses go in its tag, not in code that reads the options back. Write that code only where the answer is not in the flags alone.

## Writing style

- Use ASCII characters only (no non-ASCII characters).
- Avoid exaggerated or roundabout expressions.
- Write simply and concisely, in plain, readable English.
- Write in prose.
- Model the documentation under `docs/` on the PostgreSQL reference manual: its tone, its structure and the way it states behavior.
- Every document in the repository, `README.md` included, is a technical document, not marketing. State what the tool does; do not sell it.
- In `CHANGELOG.md`, focus on the change in behavior rather than the change in implementation.
