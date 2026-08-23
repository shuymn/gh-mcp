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
readonly -a EXPECTED_DRAFT_ASSET_IDS=(
  11
  23
  37
  53
  71
  97
  127
  163
  211
  269
  337
  419
)
readonly EXPECTED_SIGNER_WORKFLOW="shuymn/gh-mcp/.github/workflows/release.yml"

fail() {
  echo "not ok - $*" >&2
  exit 1
}

stub_git() {
  if [[ "${GIT_STUB_SCENARIO:-}" == scope-diff-failure &&
    "${1:-}" == diff ]] &&
    has_argument --name-status "$@" &&
    has_argument --no-renames "$@" &&
    has_argument -z "$@"; then
    printf '%s\0%s\0' M mcp_version.go
    exit 42
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

has_argument() {
  local expected=$1
  shift
  local argument

  for argument in "$@"; do
    [[ "$argument" == "$expected" ]] && return 0
  done
  return 1
}

read_and_increment_counter() {
  local file=$1
  local count=0

  if [[ -s "$file" ]]; then
    count="$(<"$file")"
  fi
  printf '%s\n' "$((count + 1))" >"$file"
  printf '%s\n' "$count"
}

has_api_header() {
  local expected=$1
  shift

  while (($#)); do
    case "$1" in
      -H | --header)
        [[ "${2:-}" == "$expected" ]] && return 0
        ;;
    esac
    shift
  done
  return 1
}

is_expected_draft_asset_id() {
  local actual=$1
  local expected

  for expected in "${EXPECTED_DRAFT_ASSET_IDS[@]}"; do
    if [[ "$actual" == "$expected" ]]; then
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

print_draft_release() {
  local index
  local separator=""

  printf '[{"id":1,"tag_name":"%s","draft":true,"assets":[' "${RELEASE_TAG:?}"
  for ((index = 0; index < ${#EXPECTED_RELEASE_ASSETS[@]}; index++)); do
    printf '%s{"id":%d,"name":"%s"}' \
      "$separator" "${EXPECTED_DRAFT_ASSET_IDS[$index]}" \
      "${EXPECTED_RELEASE_ASSETS[$index]}"
    separator=,
  done
  printf ']}]\n'
}

print_draft_release_object() {
  print_draft_release | jq -c '.[0]'
}

print_mutated_draft_release_object() {
  print_draft_release | jq -c '.[0] | .assets = .assets[:-1]'
}

stub_gh() {
  local lookup_count

  if [[ "${1:-}" == attestation ]]; then
    case "${GH_STUB_SCENARIO:?}" in
      draft-release-delayed | draft-release-paginated | draft-release-tag-moves | \
        draft-release-mutated)
        ;;
      *)
        fail "unexpected gh command: $*"
        ;;
    esac
    [[ "$#" -eq 10 &&
      "${2:-}" == verify &&
      -s "${3:-}" &&
      "${4:-}" == --repo &&
      "${5:-}" == "${GITHUB_REPOSITORY:?}" &&
      "${6:-}" == --signer-workflow &&
      "${7:-}" == "$EXPECTED_SIGNER_WORKFLOW" &&
      "${8:-}" == --source-digest &&
      "${9:-}" == "${SOURCE_DIGEST:?}" &&
      "${10:-}" == --deny-self-hosted-runners ]] ||
      fail "invalid attestation verification: $*"
    printf '%s\t%s\t%s\t%s\t%s\n' \
      "${3##*/}" "$5" "$7" "$9" "${10}" >>"${DRAFT_ATTESTATION_LOG:?}"
    return 0
  fi
  [[ "${1:-}" == api ]] || fail "unexpected gh command: $*"

  local endpoint
  local asset_id
  endpoint="$(find_api_endpoint "$@")" || fail "gh api endpoint is missing: $*"
  case "$endpoint" in
    */releases\?per_page=100)
      has_argument --paginate "$@" ||
        fail "release list request is missing --paginate"
      ;;
  esac

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
    draft-release-delayed | draft-release-partial-failure | draft-release-paginated | \
      draft-release-tag-moves | draft-release-mutated | draft-release-higher-published)
      case "$endpoint" in
        */releases\?per_page=100)
          if has_argument --jq "$@"; then
            if [[ "${GH_STUB_SCENARIO:?}" == draft-release-higher-published ]]; then
              printf 'v%s\tfalse\n' "${HIGHER_VERSION:?}"
            else
              printf '%s\ttrue\n' "${RELEASE_TAG:?}"
            fi
            return 0
          fi
          lookup_count=0
          if [[ "${GH_STUB_SCENARIO:?}" == draft-release-delayed ||
            "${GH_STUB_SCENARIO:?}" == draft-release-partial-failure ]]; then
            lookup_count="$(read_and_increment_counter "${DRAFT_RELEASE_LOOKUP_COUNT_FILE:?}")"
          fi
          case "${GH_STUB_SCENARIO:?}" in
            draft-release-delayed)
              if ((lookup_count == 0)); then
                printf '[]\n'
                return 0
              fi
              ;;
            draft-release-partial-failure)
              if ((lookup_count == 0)); then
                print_draft_release
                return 7
              fi
              ;;
            draft-release-paginated)
              printf '[]\n'
              print_draft_release
              return 0
              ;;
            draft-release-higher-published)
              printf '[{"tag_name":"v%s","draft":false}]\n' "${HIGHER_VERSION:?}"
              print_draft_release
              return 0
              ;;
          esac
          print_draft_release
          return 0
          ;;
        */releases/assets/*)
          has_api_header 'Accept: application/octet-stream' "$@" ||
            fail "draft release asset request is missing the binary Accept header"
          asset_id="${endpoint##*/}"
          is_expected_draft_asset_id "$asset_id" ||
            fail "unexpected draft release asset ID: ${asset_id}"
          printf '%s\n' "$asset_id" >>"${DRAFT_ASSET_ID_LOG:?}"
          printf '%s\n' "$asset_id"
          return 0
          ;;
        */releases/[0-9]*)
          if [[ "${GH_STUB_SCENARIO:?}" == draft-release-mutated ]]; then
            print_mutated_draft_release_object
          else
            print_draft_release_object
          fi
          return 0
          ;;
        */git/matching-refs/tags/*)
          if [[ "${GH_STUB_SCENARIO:?}" == draft-release-tag-moves ]]; then
            lookup_count=0
            lookup_count="$(read_and_increment_counter "${TAG_TARGET_LOOKUP_COUNT_FILE:?}")"
            if ((lookup_count == 0)); then
              printf 'commit\t%s\n' "${TARGET_SHA:?}"
            else
              printf 'commit\t%s\n' "${RELATED_SHA:?}"
            fi
          else
            printf 'commit\t%s\n' "${TARGET_SHA:?}"
          fi
          return 0
          ;;
      esac
      ;;
  esac

  fail "unexpected gh api endpoint for ${GH_STUB_SCENARIO}: ${endpoint}"
}

stub_sleep() {
  [[ "$#" -eq 1 && "$1" == 2 ]] || fail "unexpected sleep command: $*"
  printf '%s\n' "$1" >>"${SLEEP_STUB_LOG:?}"
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
  sleep)
    stub_sleep "$@"
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
ln -s "${SCRIPT_DIR}/${BASH_SOURCE[0]##*/}" "${STUB_BIN}/sleep"

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

assert_has_line() {
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

assert_draft_asset_payloads() {
  local assets_dir=$1
  local index

  for ((index = 0; index < ${#EXPECTED_RELEASE_ASSETS[@]}; index++)); do
    assert_exact_output \
      "${assets_dir}/${EXPECTED_RELEASE_ASSETS[$index]}" \
      "${EXPECTED_DRAFT_ASSET_IDS[$index]}"
  done
}

assert_draft_attestations() {
  local log=$1
  local repository=$2
  local digest=$3
  local asset
  local -a expected=()

  for asset in "${EXPECTED_RELEASE_ASSETS[@]}"; do
    expected+=("${asset}"$'\t'"${repository}"$'\t'"${EXPECTED_SIGNER_WORKFLOW}"$'\t'"${digest}"$'\t--deny-self-hosted-runners')
  done
  assert_exact_output "$log" "${expected[@]}"
}

fail_command() {
  local stderr=$1
  local description=$2

  echo "${description} stderr:" >&2
  sed 's/^/  /' "$stderr" >&2
  fail "$description failed"
}

test_prepare_rejects_failed_scope_inspection() {
  local output="${TEST_ROOT}/prepare-output"
  local stderr="${TEST_ROOT}/prepare-stderr"

  : >"$output"
  if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GIT_STUB_SCENARIO=scope-diff-failure \
    GITHUB_OUTPUT="$output" \
    "$PREPARE_SCRIPT" prepare "$TARGET_SHA" "$TARGET_SHA" \
    >/dev/null 2>"$stderr"; then
    fail "prepare accepted a failed scope diff producer"
  fi

  assert_has_line "$stderr" "Failed to inspect upstream update."
  echo "ok - prepare rejects a failed scope diff producer"
}

test_release_selects_missing_release() {
  local output="${TEST_ROOT}/missing-release-output"
  local stderr="${TEST_ROOT}/missing-release-stderr"

  : >"$output"
  if ! (
    cd "$REPO_ROOT"
    PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
      GH_STUB_SCENARIO=missing-release \
      GITHUB_OUTPUT="$output" \
      GITHUB_REPOSITORY=test/repository \
      "$RELEASE_SCRIPT" select
  ) >/dev/null 2>"$stderr"; then
    fail_command "$stderr" "selecting a missing release"
  fi

  assert_exact_output "$output" \
    "create_tag=true" \
    "publish=true" \
    "tag=v${current_version}"
  echo "ok - release select prepares a missing release"
}

test_release_rejects_related_unpublished_tag() {
  local output="${TEST_ROOT}/unpublished-tag-output"
  local stderr="${TEST_ROOT}/unpublished-tag-stderr"

  : >"$output"
  if (
    cd "$REPO_ROOT"
    PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
      GH_STUB_SCENARIO=related-unpublished-tag \
      GITHUB_OUTPUT="$output" \
      GITHUB_REPOSITORY=test/repository \
      "$RELEASE_SCRIPT" select
  ) >/dev/null 2>"$stderr"; then
    fail "release select accepted a related unpublished tag at another commit"
  fi

  assert_has_line "$stderr" \
    "Unpublished tag v${current_version} targets ${RELATED_SHA}, not ${TARGET_SHA}; rerun the Release job for ${RELATED_SHA}."
  echo "ok - release select rejects a related unpublished tag"
}

test_release_resumes_same_target_unpublished_tag() {
  local output="${TEST_ROOT}/same-target-tag-output"
  local stderr="${TEST_ROOT}/same-target-tag-stderr"

  : >"$output"
  if ! (
    cd "$REPO_ROOT"
    PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
      GH_STUB_SCENARIO=same-target-unpublished-tag \
      GITHUB_OUTPUT="$output" \
      GITHUB_REPOSITORY=test/repository \
      "$RELEASE_SCRIPT" select
  ) >/dev/null 2>"$stderr"; then
    fail_command "$stderr" "resuming a same-target unpublished tag"
  fi

  assert_exact_output "$output" \
    "create_tag=false" \
    "publish=true" \
    "tag=v${current_version}"
  echo "ok - release select resumes a same-target unpublished tag"
}

test_release_selects_higher_published_release() {
  local output="${TEST_ROOT}/published-release-output"
  local stderr="${TEST_ROOT}/published-release-stderr"

  : >"$output"
  if ! (
    cd "$REPO_ROOT"
    PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
      GH_STUB_SCENARIO=higher-published-release \
      GITHUB_OUTPUT="$output" \
      GITHUB_REPOSITORY=test/repository \
      "$RELEASE_SCRIPT" select
  ) >/dev/null 2>"$stderr"; then
    fail_command "$stderr" "selecting a higher published release"
  fi

  assert_exact_output "$output" \
    "published=true" \
    "publish=false" \
    "tag=v${HIGHER_VERSION}" \
    "tag_target=${TARGET_SHA}"
  echo "ok - release select accepts reordered assets for a higher immutable release"
}

test_release_verifies_draft_by_asset_id() {
  local stderr="${TEST_ROOT}/draft-release-stderr"
  local runner_temp="${TEST_ROOT}/draft-release-runner"
  local asset_id_log="${TEST_ROOT}/draft-release-asset-ids"
  local attestation_log="${TEST_ROOT}/draft-release-attestations"
  local lookup_count_log="${TEST_ROOT}/draft-release-lookups"
  local sleep_log="${TEST_ROOT}/draft-release-sleeps"

  mkdir -p "$runner_temp"
  : >"$asset_id_log"
  : >"$attestation_log"
  : >"$lookup_count_log"
  : >"$sleep_log"
  if ! PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=draft-release-delayed \
    DRAFT_ASSET_ID_LOG="$asset_id_log" \
    DRAFT_ATTESTATION_LOG="$attestation_log" \
    DRAFT_RELEASE_LOOKUP_COUNT_FILE="$lookup_count_log" \
    SLEEP_STUB_LOG="$sleep_log" \
    GITHUB_REPOSITORY=test/repository \
    RELEASE_TAG="v${current_version}" \
    RUNNER_TEMP="$runner_temp" \
    SOURCE_DIGEST="$TARGET_SHA" \
    "$RELEASE_SCRIPT" verify-draft \
    >/dev/null 2>"$stderr"; then
    fail_command "$stderr" "verifying a draft release by asset ID"
  fi

  sort -n -o "$asset_id_log" "$asset_id_log"
  assert_exact_output "$asset_id_log" "${EXPECTED_DRAFT_ASSET_IDS[@]}"
  assert_draft_asset_payloads "${runner_temp}/draft-release-assets"
  assert_draft_attestations "$attestation_log" test/repository "$TARGET_SHA"
  assert_exact_output "$lookup_count_log" "2"
  assert_exact_output "$sleep_log" "2"
  echo "ok - release retries draft lookup and verifies assets and attestations"
}

test_release_verifies_paginated_draft() {
  local stderr="${TEST_ROOT}/paginated-draft-stderr"
  local runner_temp="${TEST_ROOT}/paginated-draft-runner"
  local asset_id_log="${TEST_ROOT}/paginated-draft-asset-ids"
  local attestation_log="${TEST_ROOT}/paginated-draft-attestations"

  mkdir -p "$runner_temp"
  : >"$asset_id_log"
  : >"$attestation_log"
  if ! PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=draft-release-paginated \
    DRAFT_ASSET_ID_LOG="$asset_id_log" \
    DRAFT_ATTESTATION_LOG="$attestation_log" \
    GITHUB_REPOSITORY=test/repository \
    RELEASE_TAG="v${current_version}" \
    RUNNER_TEMP="$runner_temp" \
    SOURCE_DIGEST="$TARGET_SHA" \
    "$RELEASE_SCRIPT" verify-draft \
    >/dev/null 2>"$stderr"; then
    fail_command "$stderr" "verifying a draft release from a later page"
  fi

  assert_exact_output "$asset_id_log" "${EXPECTED_DRAFT_ASSET_IDS[@]}"
  assert_draft_asset_payloads "${runner_temp}/draft-release-assets"
  assert_draft_attestations "$attestation_log" test/repository "$TARGET_SHA"
  echo "ok - release aggregates paginated draft releases"
}

test_release_rejects_moved_draft_tag() {
  local stderr="${TEST_ROOT}/moved-tag-stderr"
  local runner_temp="${TEST_ROOT}/moved-tag-runner"
  local asset_id_log="${TEST_ROOT}/moved-tag-asset-ids"
  local attestation_log="${TEST_ROOT}/moved-tag-attestations"
  local tag_lookup_count_log="${TEST_ROOT}/moved-tag-lookups"

  mkdir -p "$runner_temp"
  : >"$asset_id_log"
  : >"$attestation_log"
  : >"$tag_lookup_count_log"
  if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=draft-release-tag-moves \
    DRAFT_ASSET_ID_LOG="$asset_id_log" \
    DRAFT_ATTESTATION_LOG="$attestation_log" \
    TAG_TARGET_LOOKUP_COUNT_FILE="$tag_lookup_count_log" \
    GITHUB_REPOSITORY=test/repository \
    RELEASE_TAG="v${current_version}" \
    RUNNER_TEMP="$runner_temp" \
    SOURCE_DIGEST="$TARGET_SHA" \
    "$RELEASE_SCRIPT" verify-draft \
    >/dev/null 2>"$stderr"; then
    fail "release verification accepted a tag moved during verification"
  fi

  assert_has_line "$stderr" \
    "Tag v${current_version} resolves to ${RELATED_SHA}, expected ${TARGET_SHA}."
  assert_exact_output "$tag_lookup_count_log" "2"
  assert_exact_output "$asset_id_log" "${EXPECTED_DRAFT_ASSET_IDS[@]}"
  assert_draft_attestations "$attestation_log" test/repository "$TARGET_SHA"
  echo "ok - release rejects a tag moved after attestation"
}

test_release_rejects_changed_draft_release() {
  local stderr="${TEST_ROOT}/changed-draft-stderr"
  local runner_temp="${TEST_ROOT}/changed-draft-runner"
  local asset_id_log="${TEST_ROOT}/changed-draft-asset-ids"
  local attestation_log="${TEST_ROOT}/changed-draft-attestations"

  mkdir -p "$runner_temp"
  : >"$asset_id_log"
  : >"$attestation_log"
  if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=draft-release-mutated \
    DRAFT_ASSET_ID_LOG="$asset_id_log" \
    DRAFT_ATTESTATION_LOG="$attestation_log" \
    GITHUB_REPOSITORY=test/repository \
    RELEASE_TAG="v${current_version}" \
    RUNNER_TEMP="$runner_temp" \
    SOURCE_DIGEST="$TARGET_SHA" \
    "$RELEASE_SCRIPT" verify-draft \
    >/dev/null 2>"$stderr"; then
    fail "release verification accepted a changed draft release"
  fi

  assert_has_line "$stderr" \
    "Draft release v${current_version} changed while it was being verified."
  assert_exact_output "$asset_id_log" "${EXPECTED_DRAFT_ASSET_IDS[@]}"
  assert_draft_attestations "$attestation_log" test/repository "$TARGET_SHA"
  echo "ok - release rejects a changed draft release"
}

test_release_rejects_newer_release_before_tag_creation() {
  local stderr="${TEST_ROOT}/newer-release-tag-stderr"

  if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=higher-published-release \
    GITHUB_REPOSITORY=test/repository \
    RELEASE_TAG="v${current_version}" \
    "$RELEASE_SCRIPT" create-tag \
    >/dev/null 2>"$stderr"; then
    fail "release tag creation ignored a newer published release"
  fi

  assert_has_line "$stderr" \
    "Release v${current_version} is superseded by published v${HIGHER_VERSION}."
  echo "ok - release rejects a newer release before creating a tag"
}

test_release_rejects_newer_release_before_draft_publish() {
  local stderr="${TEST_ROOT}/newer-release-draft-stderr"
  local runner_temp="${TEST_ROOT}/newer-release-draft-runner"
  local asset_id_log="${TEST_ROOT}/newer-release-draft-asset-ids"
  local attestation_log="${TEST_ROOT}/newer-release-draft-attestations"

  mkdir -p "$runner_temp"
  : >"$asset_id_log"
  : >"$attestation_log"
  if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=draft-release-higher-published \
    DRAFT_ASSET_ID_LOG="$asset_id_log" \
    DRAFT_ATTESTATION_LOG="$attestation_log" \
    GITHUB_REPOSITORY=test/repository \
    RELEASE_TAG="v${current_version}" \
    RUNNER_TEMP="$runner_temp" \
    SOURCE_DIGEST="$TARGET_SHA" \
    "$RELEASE_SCRIPT" verify-draft \
    >/dev/null 2>"$stderr"; then
    fail "draft publication ignored a newer published release"
  fi

  assert_has_line "$stderr" \
    "Release v${current_version} is superseded by published v${HIGHER_VERSION}."
  [[ ! -s "$asset_id_log" ]] || fail "draft assets were downloaded after a newer release appeared"
  [[ ! -s "$attestation_log" ]] || fail "draft attestations ran after a newer release appeared"
  echo "ok - release rejects a newer release before draft publication"
}

test_release_rejects_partial_release_list_failure() {
  local stderr="${TEST_ROOT}/partial-release-stderr"
  local runner_temp="${TEST_ROOT}/partial-release-runner"
  local lookup_count_log="${TEST_ROOT}/partial-release-lookups"

  mkdir -p "$runner_temp"
  : >"$lookup_count_log"
  if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=draft-release-partial-failure \
    DRAFT_RELEASE_LOOKUP_COUNT_FILE="$lookup_count_log" \
    GITHUB_REPOSITORY=test/repository \
    RELEASE_TAG="v${current_version}" \
    RUNNER_TEMP="$runner_temp" \
    SOURCE_DIGEST="$TARGET_SHA" \
    "$RELEASE_SCRIPT" verify-draft \
    >/dev/null 2>"$stderr"; then
    fail "release verification accepted a partial release list failure"
  fi

  assert_has_line "$stderr" \
    "Failed to list releases while looking for draft v${current_version} (gh api exited with status 7)."
  if grep -Fq -- "retrying" "$stderr"; then
    fail "release verification retried a failed release list request"
  fi
  assert_exact_output "$lookup_count_log" "1"
  echo "ok - release discards partial failed lookup output without retrying"
}

test_prepare_rejects_failed_scope_inspection
test_release_selects_missing_release
test_release_rejects_related_unpublished_tag
test_release_resumes_same_target_unpublished_tag
test_release_selects_higher_published_release
test_release_verifies_draft_by_asset_id
test_release_verifies_paginated_draft
test_release_rejects_moved_draft_tag
test_release_rejects_changed_draft_release
test_release_rejects_newer_release_before_tag_creation
test_release_rejects_newer_release_before_draft_publish
test_release_rejects_partial_release_list_failure
