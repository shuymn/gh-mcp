#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 <github-mcp-server version (e.g. v0.30.3)>"
  exit 1
fi

VERSION="$1"
VERSION_NO_V="${VERSION#v}"
BASE_URL="https://github.com/github/github-mcp-server/releases/download/${VERSION}"
BUNDLED_DIR="bundled"
CHECKSUMS_FILE="${BUNDLED_DIR}/github-mcp-server_${VERSION_NO_V}_checksums.txt"

ASSETS=(
  "github-mcp-server_Darwin_arm64.tar.gz"
  "github-mcp-server_Darwin_x86_64.tar.gz"
  "github-mcp-server_Linux_i386.tar.gz"
  "github-mcp-server_Linux_arm64.tar.gz"
  "github-mcp-server_Linux_x86_64.tar.gz"
  "github-mcp-server_Windows_arm64.zip"
  "github-mcp-server_Windows_i386.zip"
  "github-mcp-server_Windows_x86_64.zip"
)

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

mkdir -p "${BUNDLED_DIR}"

echo "Downloading checksums for ${VERSION}..."
curl -fsSL "${BASE_URL}/github-mcp-server_${VERSION_NO_V}_checksums.txt" -o "${CHECKSUMS_FILE}"

for asset in "${ASSETS[@]}"; do
  echo "Downloading ${asset}..."
  curl -fsSL "${BASE_URL}/${asset}" -o "${BUNDLED_DIR}/${asset}"
done

echo "Verifying checksums..."
while read -r expected file_name; do
  [[ -n "${expected}" ]] || continue

  target="${BUNDLED_DIR}/${file_name}"
  if [[ ! -f "${target}" ]]; then
    continue
  fi

  actual="$(shasum -a 256 "${target}" | awk '{print $1}')"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "Checksum mismatch for ${file_name}"
    echo "expected=${expected}"
    echo "actual=${actual}"
    exit 1
  fi
done < "${CHECKSUMS_FILE}"

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

echo "Bundled assets updated to ${VERSION}."
echo "Updated mcp_version.go and SHA256 constants in bundle_*.go."
