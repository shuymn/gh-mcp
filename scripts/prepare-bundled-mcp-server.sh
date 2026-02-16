#!/usr/bin/env bash

set -euo pipefail

if [[ $# -gt 1 ]]; then
  echo "Usage: $0 [github-mcp-server version (e.g. v0.30.3)]"
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required but not installed."
  exit 1
fi

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

VERSION="${1:-}"
if [[ -z "${VERSION}" ]]; then
  VERSION="$(sed -nE 's/^const mcpServerVersion = "(v[^"]+)"$/\1/p' mcp_version.go)"
  if [[ -z "${VERSION}" ]]; then
    echo "failed to parse mcpServerVersion from mcp_version.go"
    exit 1
  fi
fi

VERSION_NO_V="${VERSION#v}"
BASE_URL="https://github.com/github/github-mcp-server/releases/download/${VERSION}"
API_URL="https://api.github.com/repos/github/github-mcp-server/releases/tags/${VERSION}"
BUNDLED_DIR="bundled"
CHECKSUMS_FILE="${BUNDLED_DIR}/github-mcp-server_${VERSION_NO_V}_checksums.txt"
CHECKSUMS_ASSET="github-mcp-server_${VERSION_NO_V}_checksums.txt"

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

sha256_file() {
  local file_path="$1"

  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${file_path}" | awk '{print $1}'
    return
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${file_path}" | awk '{print $1}'
    return
  fi

  echo "shasum or sha256sum is required but neither was found."
  exit 1
}

mkdir -p "${BUNDLED_DIR}"

echo "Loading release metadata for ${VERSION}..."
RELEASE_METADATA_JSON="$(curl -fsSL "${API_URL}")"

release_asset_digest() {
  local asset_name="$1"
  jq -er --arg name "${asset_name}" '.assets[] | select(.name == $name) | .digest' <<<"${RELEASE_METADATA_JSON}"
}

verify_asset_against_release_metadata() {
  local asset_name="$1"
  local file_path="$2"

  local expected_with_prefix
  expected_with_prefix="$(release_asset_digest "${asset_name}")"

  if [[ "${expected_with_prefix}" != sha256:* ]]; then
    echo "Unsupported digest format for ${asset_name}: ${expected_with_prefix}"
    exit 1
  fi

  local expected actual
  expected="${expected_with_prefix#sha256:}"
  actual="$(sha256_file "${file_path}")"

  if [[ "${actual}" != "${expected}" ]]; then
    echo "Release metadata digest mismatch for ${asset_name}"
    echo "expected=${expected}"
    echo "actual=${actual}"
    exit 1
  fi
}

echo "Downloading checksums for ${VERSION}..."
curl -fsSL "${BASE_URL}/${CHECKSUMS_ASSET}" -o "${CHECKSUMS_FILE}"
verify_asset_against_release_metadata "${CHECKSUMS_ASSET}" "${CHECKSUMS_FILE}"

for asset in "${ASSETS[@]}"; do
  echo "Downloading ${asset}..."
  curl -fsSL "${BASE_URL}/${asset}" -o "${BUNDLED_DIR}/${asset}"
  verify_asset_against_release_metadata "${asset}" "${BUNDLED_DIR}/${asset}"
done

echo "Verifying checksums file entries..."
while read -r expected file_name; do
  [[ -n "${expected}" ]] || continue

  target="${BUNDLED_DIR}/${file_name}"
  if [[ ! -f "${target}" ]]; then
    continue
  fi

  actual="$(sha256_file "${target}")"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "Checksum mismatch for ${file_name}"
    echo "expected=${expected}"
    echo "actual=${actual}"
    exit 1
  fi
done < "${CHECKSUMS_FILE}"

echo "Bundled assets prepared for ${VERSION}."
