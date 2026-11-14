#!/usr/bin/env bash
# Check that hazel round-trips real-world SQLite schemas. For each schema in
# realworld.mk: download it, load it into an empty database with
# test/realworld/load (SQLite runs the file, hazel's parser is not involved),
# run hazel dump, and run hazel plan with the dump as the schema file. The plan
# must be "-- No changes"; anything else means the catalog reader and the
# schema file loader disagree about the schema. See REALWORLD-TESTS.md.
set -euo pipefail

cd "$(dirname "$0")/../.."

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

go build -o "$work/hazel" ./cmd/hazel
go build -o "$work/load" ./test/realworld/load

manifest=$(make -s print-realworld)

# REALWORLD names the schemas to check, separated by commas; unset or empty
# means all of them. A name the manifest does not hold is an error.
selected=()
IFS=',' read -r -a parts <<<"${REALWORLD:-}"
for name in "${parts[@]}"; do
  [ -n "$name" ] || continue
  if ! cut -d'|' -f1 <<<"$manifest" | grep -qxF -- "$name"; then
    echo "No schema named $name. make print-realworld lists them." >&2
    exit 1
  fi
  selected+=("$name")
done

is_selected() {
  [ ${#selected[@]} -eq 0 ] && return 0
  local s
  for s in "${selected[@]}"; do
    [ "$s" = "$1" ] && return 0
  done
  return 1
}

indent() {
  sed 's/^/    /'
}

# fetch <name> <repo> <sha> <path> <out>
# Downloads the file, or reads it from REALWORLD_CACHE. Every source is pinned
# to a commit, so a kept file does not go stale.
fetch() {
  local name="$1" repo="$2" sha="$3" path="$4" out="$5"
  local url="https://raw.githubusercontent.com/$repo/$sha/$path"
  if [ -z "${REALWORLD_CACHE:-}" ]; then
    curl -sSfL --retry 3 --retry-delay 2 -o "$out" "$url"
    return
  fi
  mkdir -p "$REALWORLD_CACHE"
  local cached="$REALWORLD_CACHE/$name-$sha.sql"
  if [ ! -f "$cached" ]; then
    curl -sSfL --retry 3 --retry-delay 2 -o "$cached.tmp" "$url"
    mv "$cached.tmp" "$cached"
  fi
  cp "$cached" "$out"
}

pass=0
fail=0
while IFS='|' read -r name repo sha path; do
  [ -n "$name" ] || continue
  is_selected "$name" || continue
  printf "  %-12s " "$name"
  dir="$work/$name"
  mkdir -p "$dir"

  if ! fetch "$name" "$repo" "$sha" "$path" "$dir/schema.sql" 2>"$dir/err"; then
    echo "FAIL (fetch)"
    indent <"$dir/err" >&2
    fail=$((fail + 1))
    continue
  fi
  if ! "$work/load" "$dir/app.db" "$dir/schema.sql" 2>"$dir/err"; then
    echo "FAIL (load)"
    indent <"$dir/err" >&2
    fail=$((fail + 1))
    continue
  fi
  if ! "$work/hazel" dump "$dir/app.db" >"$dir/dump.sql" 2>"$dir/err"; then
    echo "FAIL (dump)"
    indent <"$dir/err" >&2
    fail=$((fail + 1))
    continue
  fi
  if out=$("$work/hazel" plan "$dir/app.db" "$dir/dump.sql" 2>&1) && [ "$(tail -n 1 <<<"$out")" = "-- No changes" ]; then
    echo "PASS"
    pass=$((pass + 1))
  else
    echo "DRIFT"
    indent <<<"$out" >&2
    fail=$((fail + 1))
  fi
done <<<"$manifest"

echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
