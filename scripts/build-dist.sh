#!/usr/bin/env bash
# Build hook for cli/gh-extension-precompile: one binary per locked platform in dist/.
set -euo pipefail
exec go run ./tools/lock dist -out dist
