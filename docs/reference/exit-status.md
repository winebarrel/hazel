# Exit status

| Status | Meaning |
| --- | --- |
| 0 | Success. Without `--check`, also when there are changes. |
| 1 | An error. `apply` has changed nothing. |
| 2 | `plan --check` or `diff --check` found changes, or `fmt --check` found a file that is not formatted. |
| 3 | `plan`, `apply` or `diff` needs to rebuild a table and `--allow-table-rebuild` is not given. With `--check` and changes, the status is 2. |
| 80 | A usage error: an unknown option, a missing argument, an invalid pattern or option value. |
