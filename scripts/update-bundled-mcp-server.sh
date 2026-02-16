#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 <github-mcp-server version (e.g. v0.30.3)>"
  exit 1
fi

VERSION="$1"
VERSION_NO_V="${VERSION#v}"
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

BUNDLED_DIR="bundled"
CHECKSUMS_FILE="${BUNDLED_DIR}/github-mcp-server_${VERSION_NO_V}_checksums.txt"

SHA_TARGETS=(
  "bundle_darwin_arm64.go:github-mcp-server_Darwin_arm64.tar.gz"
  "bundle_darwin_amd64.go:github-mcp-server_Darwin_x86_64.tar.gz"
  "bundle_linux_386.go:github-mcp-server_Linux_i386.tar.gz"
  "bundle_linux_arm64.go:github-mcp-server_Linux_arm64.tar.gz"
  "bundle_linux_amd64.go:github-mcp-server_Linux_x86_64.tar.gz"
  "bundle_windows_arm64.go:github-mcp-server_Windows_arm64.zip"
  "bundle_windows_386.go:github-mcp-server_Windows_i386.zip"
  "bundle_windows_amd64.go:github-mcp-server_Windows_x86_64.zip"
)

"${SCRIPT_DIR}/prepare-bundled-mcp-server.sh" "${VERSION}"

perl -i -pe "s/^const mcpServerVersion = \".*\"$/const mcpServerVersion = \"${VERSION}\"/" mcp_version.go

for target in "${SHA_TARGETS[@]}"; do
  go_file="${target%%:*}"
  asset="${target#*:}"
  checksum="$(awk -v file_name="${asset}" '$2==file_name {print $1}' "${CHECKSUMS_FILE}")"
  if [[ -z "${checksum}" ]]; then
    echo "Failed to find checksum for ${asset}"
    exit 1
  fi
  perl -i -pe "s/^(\\s*bundledMCPArchiveSHA256\\s*=\\s*\").*(\")$/\${1}${checksum}\${2}/" "${go_file}"
done

echo "Bundled metadata updated to ${VERSION}."
echo "Updated mcp_version.go and SHA256 constants in bundle_*.go."
