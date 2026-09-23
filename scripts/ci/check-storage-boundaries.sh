#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd "${script_dir}/../.." && pwd)

matches=$(
  git -C "${repo_root}" grep -n -E '"(MATCH|CREATE|MERGE|DELETE|RETURN|CALL) ' -- '*.go' \
    ':!internal/storage/ladybug/**' \
    ':!internal/storage/lattice/**' \
    ':!*_test.go' || true
)

if [ -n "${matches}" ]; then
  printf 'raw Cypher must stay in an authorized storage adapter:\n%s\n' "${matches}" >&2
  exit 1
fi

printf 'Storage boundary contract passed\n'
