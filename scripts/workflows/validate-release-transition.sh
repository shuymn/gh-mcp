#!/usr/bin/env bash

set -euo pipefail

: "${BASE_SHA:?BASE_SHA must be set}"

if [[ "${BASE_SHA}" =~ ^0+$ ]]; then
  echo "No previous main commit; skipping release transition validation."
  exit 0
fi

current_upstream="$(
  git show "${BASE_SHA}:mcp_version.go" |
    sed -nE 's/^const mcpServerVersion = "(v[^"]+)"$/\1/p'
)"
next_upstream="$(sed -nE 's/^const mcpServerVersion = "(v[^"]+)"$/\1/p' mcp_version.go)"
current_release="$(git show "${BASE_SHA}:VERSION")"
next_release="$(<VERSION)"

go run ./scripts/release-version validate \
  "${current_release}" \
  "${next_release}" \
  "${current_upstream}" \
  "${next_upstream}"
