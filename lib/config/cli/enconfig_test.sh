#!/bin/bash

set -euo pipefail

function fail() {
  echo -- "$@" >&2
  exit 1
}

found_enconfig="$(find . -name "enconfig" -type f -executable | head -n 1)"
cli="./lib/config/cli/enconfig_/enconfig"
if [ ! -f "$cli" ]; then
  cli="$found_enconfig"
fi
if [ -z "$cli" ] || [ ! -x "$cli" ]; then
  fail "could not find enconfig binary"
fi

srcdir=$(mktemp -d -t enconfig-src-XXXXXXXX)
dstdir=$(mktemp -d -t enconfig-dst-XXXXXXXX)
convdir=$(mktemp -d -t enconfig-conv-XXXXXXXX)

src_flags=(
  "--src-config-store=directory:json"
  "--src-config-store-directory-path=$srcdir"
  "--src-app=app"
  "--src-namespace=ns1"
)

# put via stdin
printf '{"a": 1, "msg": "hello"}\n' | "$cli" put key1 "${src_flags[@]}" || fail "put failed"

# get to file
outjson="$srcdir/out.json"
"$cli" get key1 "${src_flags[@]}" --output "$outjson" || fail "get failed"

grep '"a"' "$outjson" >/dev/null || fail "get output missing key"

# list
list_out=$("$cli" list "${src_flags[@]}")
echo "$list_out" | grep "key1" >/dev/null || fail "list missing key"

# find
find_out=$("$cli" find "key*" "${src_flags[@]}")
echo "$find_out" | grep "ns1/key1" >/dev/null || fail "find missing key"

# grep
"$cli" grep -q '"msg"' "${src_flags[@]}" || fail "grep -q failed"
grep_out=$("$cli" grep -l '"msg"' "${src_flags[@]}")
echo "$grep_out" | grep "ns1/key1" >/dev/null || fail "grep -l missing key"

# backup
backup="$srcdir/backup.jsonl"
"$cli" backup "${src_flags[@]}" --output "$backup" || fail "backup failed"

# restore into another store
"$cli" restore \
  --dst-config-store=directory:json \
  --dst-config-store-directory-path="$dstdir" \
  --dst-app=app \
  --input "$backup" || fail "restore failed"

"$cli" get key1 \
  --src-config-store=directory:json \
  --src-config-store-directory-path="$dstdir" \
  --src-app=app \
  --src-namespace=ns1 \
  --output - >/dev/null || fail "restore get failed"

# convert into another namespace
"$cli" convert \
  --src-config-store=directory:json \
  --src-config-store-directory-path="$srcdir" \
  --src-app=app \
  --src-namespace=ns1 \
  --dst-config-store=directory:json \
  --dst-config-store-directory-path="$convdir" \
  --dst-app=app \
  --dst-namespace=copy || fail "convert failed"

"$cli" get key1 \
  --src-config-store=directory:json \
  --src-config-store-directory-path="$convdir" \
  --src-app=app \
  --src-namespace=copy \
  --output - >/dev/null || fail "convert get failed"
