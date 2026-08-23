#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT_DIR

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

if [[ "${current_upstream}" != "${next_upstream}" ]]; then
  "${SCRIPT_DIR}/validate-upstream-release-order.sh" \
    "${current_upstream}" \
    "${next_upstream}"
fi
