#!/usr/bin/env bash

set -euo pipefail

fail() {
  echo "not ok - $*" >&2
  exit 1
}

stub_gh() {
  [[ "${1:-}" == api ]] || fail "unexpected gh command: $*"
  [[ "${GH_TOKEN:-}" == test-token ]] || fail "order check did not use the read token"

  local argument
  local endpoint=""
  local method=""
  local previous=""
  local saw_jq=false
  local saw_paginate=false

  for argument in "$@"; do
    if [[ "$previous" == --method ]]; then
      method=$argument
    elif [[ "$previous" == --jq ]]; then
      saw_jq=true
    fi
    [[ "$argument" == --paginate ]] && saw_paginate=true
    [[ "$argument" == repos/* ]] && endpoint=$argument
    previous=$argument
  done

  [[ "$method" == GET ]] || fail "release lookup did not use GET"
  [[ "$saw_jq" == true ]] || fail "release lookup did not filter stable releases"

  [[ "$saw_paginate" == true ]] || fail "upstream release lookup was not paginated"
  [[ "$endpoint" == repos/github/github-mcp-server/releases\?per_page=100 ]] ||
    fail "unexpected release endpoint: ${endpoint}"

  case "${GH_STUB_SCENARIO:?}" in
    sequential | skipped)
      printf '%s\n' v1.10.1 v1.10.0 v1.9.0
      ;;
    missing)
      printf '%s\n' v1.9.0
      ;;
    api-failure)
      return 42
      ;;
    *) fail "unknown gh stub scenario: ${GH_STUB_SCENARIO}" ;;
  esac
}

case "${0##*/}" in
  gh)
    stub_gh "$@"
    exit 0
    ;;
esac

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT_DIR
readonly CHECK_SCRIPT="${SCRIPT_DIR}/validate-upstream-release-order.sh"
readonly ORIGINAL_PATH="$PATH"

TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/gh-mcp-order-tests.XXXXXX")"
readonly TEST_ROOT
readonly STUB_BIN="${TEST_ROOT}/bin"
mkdir -p "$STUB_BIN"
ln -s "${SCRIPT_DIR}/${BASH_SOURCE[0]##*/}" "${STUB_BIN}/gh"

cleanup() {
  rm -rf -- "$TEST_ROOT"
}
trap cleanup EXIT

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

test_accepts_earliest_release() {
  local stdout="${TEST_ROOT}/sequential-stdout"
  local stderr="${TEST_ROOT}/sequential-stderr"

  if ! PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=sequential \
    GH_TOKEN=test-token \
    "$CHECK_SCRIPT" v1.9.0 v1.10.0 >"$stdout" 2>"$stderr"; then
    sed 's/^/  /' "$stderr" >&2
    fail "order check rejected the earliest release"
  fi

  assert_has_line "$stdout" "Verified sequential upstream release: v1.9.0 -> v1.10.0."
  echo "ok - order check accepts the earliest stable release"
}

test_rejects_skipped_release() {
  local stderr="${TEST_ROOT}/skipped-stderr"

  if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=skipped \
    GH_TOKEN=test-token \
    "$CHECK_SCRIPT" v1.9.0 v1.10.1 >/dev/null 2>"$stderr"; then
    fail "order check accepted a skipped release"
  fi

  assert_has_line "$stderr" \
    'earlier upstream release must be applied first: "v1.10.0" precedes "v1.10.1"'
  echo "ok - order check rejects a skipped stable release"
}

test_rejects_missing_candidate() {
  local stderr="${TEST_ROOT}/missing-stderr"

  if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=missing \
    GH_TOKEN=test-token \
    "$CHECK_SCRIPT" v1.9.0 v1.10.0 >/dev/null 2>"$stderr"; then
    fail "order check accepted an unpublished candidate"
  fi

  assert_has_line "$stderr" \
    'next upstream version is not a published stable release: "v1.10.0"'
  echo "ok - order check rejects an unpublished candidate"
}

test_fails_closed_on_api_error() {
  local stderr="${TEST_ROOT}/api-failure-stderr"

  if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=api-failure \
    GH_TOKEN=test-token \
    "$CHECK_SCRIPT" v1.9.0 v1.10.0 >/dev/null 2>"$stderr"; then
    fail "order check ignored a release API failure"
  fi

  echo "ok - order check fails closed on release API errors"
}

test_accepts_earliest_release
test_rejects_skipped_release
test_rejects_missing_candidate
test_fails_closed_on_api_error
