#!/usr/bin/env bash

set -euo pipefail

if [[ $# -gt 1 ]]; then
  echo "Usage: $0 [github-mcp-server version (e.g. v0.30.3)]"
  exit 1
fi

if ! command -v gh >/dev/null 2>&1; then
  echo "gh CLI is required but not installed."
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
UPSTREAM_REPOSITORY="github/github-mcp-server"
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

ensure_gh_auth() {
  if [[ -n "${GH_TOKEN:-}" || -n "${GITHUB_TOKEN:-}" ]]; then
    return
  fi

  if gh auth status >/dev/null 2>&1; then
    return
  fi

  echo "gh authentication is required."
  echo "Run 'gh auth login' locally, or set GH_TOKEN (or GITHUB_TOKEN) in CI."
  exit 1
}

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

download_release_asset() {
  local asset_name="$1"

  gh release download "${VERSION}" \
    --repo "${UPSTREAM_REPOSITORY}" \
    --pattern "${asset_name}" \
    --dir "${BUNDLED_DIR}" \
    --clobber
}

checksum_for_asset_from_file() {
  local asset_name="$1"
  awk -v file_name="${asset_name}" '$2==file_name {print $1; exit}' "${CHECKSUMS_FILE}"
}

verify_asset_against_checksums_file() {
  local asset_name="$1"
  local file_path="$2"

  local expected actual
  expected="$(checksum_for_asset_from_file "${asset_name}")"
  if [[ -z "${expected}" ]]; then
    echo "Failed to find checksum for ${asset_name} in ${CHECKSUMS_FILE}"
    exit 1
  fi

  actual="$(sha256_file "${file_path}")"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "Checksum mismatch for ${asset_name}"
    echo "expected=${expected}"
    echo "actual=${actual}"
    exit 1
  fi
}

ensure_gh_auth
mkdir -p "${BUNDLED_DIR}"

echo "Downloading checksums for ${VERSION}..."
download_release_asset "${CHECKSUMS_ASSET}"

echo "Verifying attestation for ${CHECKSUMS_ASSET}..."
gh release verify-asset "${VERSION}" "${CHECKSUMS_FILE}" --repo "${UPSTREAM_REPOSITORY}" >/dev/null

for asset in "${ASSETS[@]}"; do
  echo "Downloading ${asset}..."
  download_release_asset "${asset}"
  verify_asset_against_checksums_file "${asset}" "${BUNDLED_DIR}/${asset}"
done

echo "Bundled assets prepared for ${VERSION}."
