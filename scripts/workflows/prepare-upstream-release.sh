#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT_DIR
TRUSTED_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
readonly TRUSTED_ROOT

readonly -a RELEASE_FILES=(
  VERSION
  mcp_version.go
  bundle_darwin_amd64.go
  bundle_darwin_arm64.go
  bundle_linux_386.go
  bundle_linux_amd64.go
  bundle_linux_arm64.go
  bundle_windows_386.go
  bundle_windows_amd64.go
  bundle_windows_arm64.go
)

usage() {
  cat >&2 <<'EOF'
Usage:
  prepare-upstream-release.sh prepare <base-sha> <head-sha>
  prepare-upstream-release.sh commit <release-version> <head-ref>
EOF
}

die() {
  echo "$*" >&2
  exit 1
}

require_candidate_checkout() {
  git rev-parse --is-inside-work-tree >/dev/null 2>&1 ||
    die "Run this command from the candidate checkout."
}

is_release_file() {
  local candidate="$1"
  local release_file

  for release_file in "${RELEASE_FILES[@]}"; do
    if [[ "$candidate" == "$release_file" ]]; then
      return 0
    fi
  done

  return 1
}

DIFF_HAS_CHANGES=false
AUTO_MERGE=false
NEXT_RELEASE_VERSION=""
NEXT_UPSTREAM_VERSION=""

validate_release_diff() {
  local diff_spec="$1"
  local require_mcp_version="$2"
  local context="$3"
  local diff_fd
  local diff_pid
  local status
  local file
  local validation_error=""
  local saw_version=false

  DIFF_HAS_CHANGES=false
  exec {diff_fd}< <(git diff --no-renames --name-status -z "$diff_spec")
  diff_pid=$!
  while IFS= read -r -d '' status <&"$diff_fd"; do
    if ! IFS= read -r -d '' file <&"$diff_fd"; then
      validation_error="Malformed ${context} from git diff."
      break
    fi

    DIFF_HAS_CHANGES=true
    if [[ -n "$validation_error" ]]; then
      continue
    fi
    if [[ "$status" != M ]]; then
      validation_error="Unexpected status in ${context}: ${status} ${file}; expected M."
    elif ! is_release_file "$file"; then
      validation_error="Unexpected file in ${context}: ${file}"
    elif [[ "$file" == mcp_version.go ]]; then
      saw_version=true
    fi
  done
  exec {diff_fd}<&-

  if ! wait "$diff_pid"; then
    die "Failed to inspect ${context}."
  fi
  [[ -z "$validation_error" ]] || die "$validation_error"
  if [[ "$require_mcp_version" == true && "$saw_version" != true ]]; then
    die "mcp_version.go is not part of the upstream update."
  fi
}

validate_no_untracked_files() {
  local files_fd
  local files_pid
  local file
  local unexpected_file=""

  exec {files_fd}< <(git ls-files --others --exclude-standard -z)
  files_pid=$!
  while IFS= read -r -d '' file <&"$files_fd"; do
    if [[ -z "$unexpected_file" ]]; then
      unexpected_file="$file"
    fi
  done
  exec {files_fd}<&-

  if ! wait "$files_pid"; then
    die "Failed to inspect generated untracked files."
  fi
  [[ -z "$unexpected_file" ]] || die "Unexpected generated file: ${unexpected_file}"
}

validate_scope() {
  local base_sha="$1"
  local head_sha="$2"
  local candidate_head
  local trusted_head

  require_candidate_checkout
  candidate_head="$(git rev-parse HEAD)"
  trusted_head="$(git -C "$TRUSTED_ROOT" rev-parse HEAD)"

  [[ "$candidate_head" == "$head_sha" ]] ||
    die "Candidate checkout is ${candidate_head}, expected ${head_sha}."
  [[ "$trusted_head" == "$base_sha" ]] ||
    die "Trusted checkout is ${trusted_head}, expected ${base_sha}."
  git cat-file -e "${base_sha}^{commit}" 2>/dev/null ||
    die "Base commit ${base_sha} is unavailable in the candidate checkout."
  git merge-base --is-ancestor "$base_sha" "$head_sha" ||
    die "Renovate must rebase this update onto ${base_sha}."
  validate_release_diff "${base_sha}..${head_sha}" true "upstream update"
}

extract_upstream_version() {
  sed -nE 's/^const mcpServerVersion = "(v[^"]+)"$/\1/p'
}

determine_version() {
  local base_sha="$1"
  local current_release
  local current_upstream

  current_release="$(git show "${base_sha}:VERSION")"
  current_upstream="$(git show "${base_sha}:mcp_version.go" | extract_upstream_version)"
  NEXT_UPSTREAM_VERSION="$(extract_upstream_version <mcp_version.go)"
  NEXT_RELEASE_VERSION="$(
    cd "$TRUSTED_ROOT"
    go run ./scripts/release-version next \
      "$current_release" "$current_upstream" "$NEXT_UPSTREAM_VERSION"
  )"
  AUTO_MERGE="$(
    cd "$TRUSTED_ROOT"
    go run ./scripts/release-version auto-merge \
      "$current_upstream" "$NEXT_UPSTREAM_VERSION"
  )"
}

refresh_metadata() {
  local base_sha="$1"
  local release_version="$2"
  local upstream_version="$3"

  # prepare_release validates the entire candidate diff immediately before this
  # call. Restore every mutable input before generating canonical output.
  git restore --source="$base_sha" -- "${RELEASE_FILES[@]}"
  ./scripts/update-bundled-mcp-server.sh "$upstream_version"
  printf '%s\n' "$release_version" >VERSION
}

validate_changed_files() {
  validate_release_diff HEAD false "generated changes"
  validate_no_untracked_files
}

prepare_release() {
  [[ $# -eq 2 ]] || {
    usage
    exit 1
  }

  local base_sha="$1"
  local head_sha="$2"

  : "${GITHUB_OUTPUT:?GITHUB_OUTPUT must be set}"

  validate_scope "$base_sha" "$head_sha"
  determine_version "$base_sha"
  refresh_metadata "$base_sha" "$NEXT_RELEASE_VERSION" "$NEXT_UPSTREAM_VERSION"
  validate_changed_files

  {
    echo "release=${NEXT_RELEASE_VERSION}"
    echo "upstream=${NEXT_UPSTREAM_VERSION}"
    echo "changed=${DIFF_HAS_CHANGES}"
    echo "auto_merge=${AUTO_MERGE}"
  } >>"$GITHUB_OUTPUT"
}

commit_changes() {
  [[ $# -eq 2 ]] || {
    usage
    exit 1
  }

  local release_version="$1"
  local head_ref="$2"

  require_candidate_checkout
  : "${PUSH_TOKEN:?PUSH_TOKEN must be set}"
  : "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY must be set}"
  validate_changed_files
  [[ "$DIFF_HAS_CHANGES" == true ]] || die "No generated release metadata to commit."

  git config user.name "github-actions[bot]"
  git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
  git add -- "${RELEASE_FILES[@]}"
  git commit -m "chore: prepare release ${release_version}"

  # A single non-forced push intentionally fails if the Renovate head moved
  # after validation. Do not fetch, rebase, or retry across that TOCTOU boundary.
  git push \
    "https://x-access-token:${PUSH_TOKEN}@github.com/${GITHUB_REPOSITORY}.git" \
    "HEAD:${head_ref}"
}

main() {
  [[ $# -ge 1 ]] || {
    usage
    exit 1
  }

  local command="$1"
  shift

  case "$command" in
    prepare)
      prepare_release "$@"
      ;;
    commit)
      commit_changes "$@"
      ;;
    *)
      usage
      exit 1
      ;;
  esac
}

main "$@"
