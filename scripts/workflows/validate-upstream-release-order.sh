#!/usr/bin/env bash

set -euo pipefail

readonly UPSTREAM_REPOSITORY="github/github-mcp-server"

if [[ $# -ne 2 ]]; then
  echo "Usage: $0 <current-upstream> <next-upstream>" >&2
  exit 1
fi

: "${GH_TOKEN:?GH_TOKEN must be set}"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT_DIR
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
readonly REPO_ROOT

current_upstream=$1
next_upstream=$2

gh api --method GET --paginate \
  "repos/${UPSTREAM_REPOSITORY}/releases?per_page=100" \
  --jq '.[] | select(.draft == false and .prerelease == false) | .tag_name' |
  (
    cd "$REPO_ROOT"
    go run ./scripts/release-version validate-order \
      "$current_upstream" "$next_upstream"
  )

echo "Verified sequential upstream release: ${current_upstream} -> ${next_upstream}."
