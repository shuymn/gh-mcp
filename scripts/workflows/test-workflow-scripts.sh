#!/usr/bin/env bash

set -euo pipefail

readonly -a EXPECTED_RELEASE_ASSETS=(
  darwin-amd64
  darwin-arm64
  freebsd-386
  freebsd-amd64
  freebsd-arm64
  linux-386
  linux-amd64
  linux-arm
  linux-arm64
  windows-386.exe
  windows-amd64.exe
  windows-arm64.exe
)

fail() {
  echo "not ok - $*" >&2
  exit 1
}

stub_git() {
  local argument
  local name_status=false
  local no_renames=false
  local nul_terminated=false

  if [[ "${GIT_STUB_SCENARIO:-}" == scope-diff-failure && "${1:-}" == diff ]]; then
    for argument in "$@"; do
      case "$argument" in
        --name-status) name_status=true ;;
        --no-renames) no_renames=true ;;
        -z) nul_terminated=true ;;
      esac
    done
    if [[ "$name_status" == true && "$no_renames" == true && "$nul_terminated" == true ]]; then
      printf '%s\0%s\0' M mcp_version.go
      exit 42
    fi
  fi

  exec "${REAL_GIT:?}" "$@"
}

find_api_endpoint() {
  local argument

  for argument in "$@"; do
    if [[ "$argument" == repos/* ]]; then
      printf '%s\n' "$argument"
      return 0
    fi
  done

  return 1
}

print_reordered_release_assets() {
  local index

  for ((index = ${#EXPECTED_RELEASE_ASSETS[@]} - 1; index >= 0; index--)); do
    printf '%s\n' "${EXPECTED_RELEASE_ASSETS[$index]}"
  done
}

stub_gh() {
  [[ "${1:-}" == api ]] || fail "unexpected gh command: $*"

  local endpoint
  endpoint="$(find_api_endpoint "$@")" || fail "gh api endpoint is missing: $*"

  case "${GH_STUB_SCENARIO:?}" in
    missing-release)
      case "$endpoint" in
        */releases\?per_page=100 | */git/matching-refs/tags/*)
          return 0
          ;;
      esac
      ;;
    related-unpublished-tag)
      case "$endpoint" in
        */releases\?per_page=100)
          return 0
          ;;
        */git/matching-refs/tags/*)
          printf 'commit\t%s\n' "${RELATED_SHA:?}"
          return 0
          ;;
      esac
      ;;
    same-target-unpublished-tag)
      case "$endpoint" in
        */releases\?per_page=100)
          return 0
          ;;
        */git/matching-refs/tags/*)
          printf 'commit\t%s\n' "${TARGET_SHA:?}"
          return 0
          ;;
      esac
      ;;
    higher-published-release)
      case "$endpoint" in
        */releases\?per_page=100)
          printf 'v%s\tfalse\n' "${HIGHER_VERSION:?}"
          return 0
          ;;
        */git/matching-refs/tags/*)
          printf 'commit\t%s\n' "${TARGET_SHA:?}"
          return 0
          ;;
        */releases/tags/*)
          printf 'true\n'
          print_reordered_release_assets
          return 0
          ;;
      esac
      ;;
  esac

  fail "unexpected gh api endpoint for ${GH_STUB_SCENARIO}: ${endpoint}"
}

case "${0##*/}" in
  git)
    stub_git "$@"
    exit 0
    ;;
  gh)
    stub_gh "$@"
    exit 0
    ;;
esac

readonly ORIGINAL_PATH="$PATH"
REAL_GIT="$(command -v git)"
readonly REAL_GIT
export REAL_GIT

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT_DIR
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
readonly REPO_ROOT
readonly PREPARE_SCRIPT="${SCRIPT_DIR}/prepare-upstream-release.sh"
readonly RELEASE_SCRIPT="${SCRIPT_DIR}/release.sh"

TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/gh-mcp-workflow-tests.XXXXXX")"
readonly TEST_ROOT
readonly STUB_BIN="${TEST_ROOT}/bin"
mkdir -p "$STUB_BIN"
ln -s "${SCRIPT_DIR}/${BASH_SOURCE[0]##*/}" "${STUB_BIN}/gh"
ln -s "${SCRIPT_DIR}/${BASH_SOURCE[0]##*/}" "${STUB_BIN}/git"

cleanup() {
  rm -rf -- "$TEST_ROOT"
}
trap cleanup EXIT

TARGET_SHA="$($REAL_GIT -C "$REPO_ROOT" rev-parse HEAD)"
RELATED_SHA="$($REAL_GIT -C "$REPO_ROOT" rev-parse HEAD^)"
readonly TARGET_SHA RELATED_SHA
export TARGET_SHA RELATED_SHA

current_version="$(<"${REPO_ROOT}/VERSION")"
IFS=. read -r current_major _ <<<"$current_version"
HIGHER_VERSION="$((current_major + 1)).0.0"
readonly HIGHER_VERSION
export HIGHER_VERSION

assert_contains() {
  local file=$1
  local expected=$2

  grep -Fqx -- "$expected" "$file" || {
    echo "Expected line: ${expected}" >&2
    echo "Actual content:" >&2
    sed 's/^/  /' "$file" >&2
    fail "${file} does not contain the expected line"
  }
}

assert_exact_output() {
  local actual=$1
  shift
  local expected="${TEST_ROOT}/expected"

  printf '%s\n' "$@" >"$expected"
  diff -u "$expected" "$actual" || fail "unexpected workflow output"
}

assert_command_succeeded() {
  local stderr=$1
  local description=$2

  echo "${description} stderr:" >&2
  sed 's/^/  /' "$stderr" >&2
  fail "$description failed"
}

test_prepare_rejects_failed_scope_inspection() {
  local output="${TEST_ROOT}/prepare-output"
  local stdout="${TEST_ROOT}/prepare-stdout"
  local stderr="${TEST_ROOT}/prepare-stderr"

  : >"$output"
  if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GIT_STUB_SCENARIO=scope-diff-failure \
    GITHUB_OUTPUT="$output" \
    "$PREPARE_SCRIPT" prepare "$TARGET_SHA" "$TARGET_SHA" \
    >"$stdout" 2>"$stderr"; then
    fail "prepare accepted a failed scope diff producer"
  fi

  assert_contains "$stderr" "Failed to inspect upstream update."
  echo "ok - prepare rejects a failed scope diff producer"
}

test_release_selects_missing_release() {
  local output="${TEST_ROOT}/missing-release-output"
  local stdout="${TEST_ROOT}/missing-release-stdout"
  local stderr="${TEST_ROOT}/missing-release-stderr"

  : >"$output"
  if ! (
    cd "$REPO_ROOT"
    PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
      GH_STUB_SCENARIO=missing-release \
      GITHUB_OUTPUT="$output" \
      GITHUB_REPOSITORY=test/repository \
      "$RELEASE_SCRIPT" select
  ) >"$stdout" 2>"$stderr"; then
    assert_command_succeeded "$stderr" "selecting a missing release"
  fi

  assert_exact_output "$output" \
    "create_tag=true" \
    "publish=true" \
    "tag=v${current_version}"
  echo "ok - release select prepares a missing release"
}

test_release_rejects_related_unpublished_tag() {
  local output="${TEST_ROOT}/unpublished-tag-output"
  local stdout="${TEST_ROOT}/unpublished-tag-stdout"
  local stderr="${TEST_ROOT}/unpublished-tag-stderr"

  : >"$output"
  if (
    cd "$REPO_ROOT"
    PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
      GH_STUB_SCENARIO=related-unpublished-tag \
      GITHUB_OUTPUT="$output" \
      GITHUB_REPOSITORY=test/repository \
      "$RELEASE_SCRIPT" select
  ) >"$stdout" 2>"$stderr"; then
    fail "release select accepted a related unpublished tag at another commit"
  fi

  assert_contains "$stderr" \
    "Unpublished tag v${current_version} targets ${RELATED_SHA}, not ${TARGET_SHA}; rerun the Release job for ${RELATED_SHA}."
  echo "ok - release select rejects a related unpublished tag"
}

test_release_resumes_same_target_unpublished_tag() {
  local output="${TEST_ROOT}/same-target-tag-output"
  local stdout="${TEST_ROOT}/same-target-tag-stdout"
  local stderr="${TEST_ROOT}/same-target-tag-stderr"

  : >"$output"
  if ! (
    cd "$REPO_ROOT"
    PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
      GH_STUB_SCENARIO=same-target-unpublished-tag \
      GITHUB_OUTPUT="$output" \
      GITHUB_REPOSITORY=test/repository \
      "$RELEASE_SCRIPT" select
  ) >"$stdout" 2>"$stderr"; then
    assert_command_succeeded "$stderr" "resuming a same-target unpublished tag"
  fi

  assert_exact_output "$output" \
    "create_tag=false" \
    "publish=true" \
    "tag=v${current_version}"
  echo "ok - release select resumes a same-target unpublished tag"
}

test_release_selects_higher_published_release() {
  local output="${TEST_ROOT}/published-release-output"
  local stdout="${TEST_ROOT}/published-release-stdout"
  local stderr="${TEST_ROOT}/published-release-stderr"

  : >"$output"
  if ! (
    cd "$REPO_ROOT"
    PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
      GH_STUB_SCENARIO=higher-published-release \
      GITHUB_OUTPUT="$output" \
      GITHUB_REPOSITORY=test/repository \
      "$RELEASE_SCRIPT" select
  ) >"$stdout" 2>"$stderr"; then
    assert_command_succeeded "$stderr" "selecting a higher published release"
  fi

  assert_exact_output "$output" \
    "published=true" \
    "publish=false" \
    "tag=v${HIGHER_VERSION}" \
    "tag_target=${TARGET_SHA}"
  echo "ok - release select accepts reordered assets for a higher immutable release"
}

test_prepare_rejects_failed_scope_inspection
test_release_selects_missing_release
test_release_rejects_related_unpublished_tag
test_release_resumes_same_target_unpublished_tag
test_release_selects_higher_published_release
