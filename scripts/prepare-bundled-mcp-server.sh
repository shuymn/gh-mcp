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
CHECKSUMS_ASSET="github-mcp-server_${VERSION_NO_V}_checksums.txt"
DOWNLOAD_RETRY_COUNT="${DOWNLOAD_RETRY_COUNT:-3}"
DOWNLOAD_RETRY_DELAY_SECONDS="${DOWNLOAD_RETRY_DELAY_SECONDS:-2}"

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

STAGING_DIR=""
CHECKSUMS_FILE=""

cleanup_staging_dir() {
  if [[ -n "${STAGING_DIR}" && -d "${STAGING_DIR}" ]]; then
    rm -rf "${STAGING_DIR}"
  fi
}
trap cleanup_staging_dir EXIT

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

run_with_retry() {
  local description="$1"
  shift

  local attempt=1
  while true; do
    if "$@"; then
      return 0
    fi

    if (( attempt >= DOWNLOAD_RETRY_COUNT )); then
      echo "Failed: ${description} (attempts=${attempt})"
      return 1
    fi

    echo "Retrying: ${description} (${attempt}/${DOWNLOAD_RETRY_COUNT}) in ${DOWNLOAD_RETRY_DELAY_SECONDS}s..."
    sleep "${DOWNLOAD_RETRY_DELAY_SECONDS}"
    attempt=$((attempt + 1))
  done
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
  local output_dir="$2"

  gh release download "${VERSION}" \
    --repo "${UPSTREAM_REPOSITORY}" \
    --pattern "${asset_name}" \
    --dir "${output_dir}" \
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

verify_checksums_attestation() {
  gh release verify-asset "${VERSION}" "${CHECKSUMS_FILE}" --repo "${UPSTREAM_REPOSITORY}" >/dev/null
}

download_and_verify_asset() {
  local asset_name="$1"
  local file_path="${STAGING_DIR}/${asset_name}"

  echo "Downloading ${asset_name}..."
  run_with_retry "download ${asset_name}" download_release_asset "${asset_name}" "${STAGING_DIR}"
  verify_asset_against_checksums_file "${asset_name}" "${file_path}"
}

promote_staged_asset() {
  local asset_name="$1"

  mv -f "${STAGING_DIR}/${asset_name}" "${BUNDLED_DIR}/${asset_name}"
}

ensure_gh_auth
mkdir -p "${BUNDLED_DIR}"
STAGING_DIR="$(mktemp -d "${BUNDLED_DIR}/.prepare-bundled-mcp-server.XXXXXX")"
CHECKSUMS_FILE="${STAGING_DIR}/${CHECKSUMS_ASSET}"

echo "Downloading checksums for ${VERSION}..."
run_with_retry "download ${CHECKSUMS_ASSET}" download_release_asset "${CHECKSUMS_ASSET}" "${STAGING_DIR}"

echo "Verifying attestation for ${CHECKSUMS_ASSET}..."
run_with_retry "verify attestation for ${CHECKSUMS_ASSET}" verify_checksums_attestation

for asset in "${ASSETS[@]}"; do
  download_and_verify_asset "${asset}"
done

for asset in "${ASSETS[@]}"; do
  promote_staged_asset "${asset}"
done
promote_staged_asset "${CHECKSUMS_ASSET}"

echo "Bundled assets prepared for ${VERSION}."
