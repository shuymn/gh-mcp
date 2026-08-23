#!/usr/bin/env bash

set -euo pipefail

fail() {
  echo "not ok - $*" >&2
  exit 1
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

print_pr() {
  local author=$1
  local state=${2:-open}
  local head_sha=${3:-$TARGET_SHA}
  local base_sha=${4:-$BASE_SHA}
  local draft=${5:-false}

  jq -nc \
    --arg author "$author" \
    --arg base_sha "$base_sha" \
    --arg head_ref "$HEAD_REF" \
    --arg head_sha "$head_sha" \
    --arg repo test/repository \
    --arg state "$state" \
    --argjson draft "$draft" \
    '{
      number: 42,
      state: $state,
      draft: $draft,
      user: {login: $author},
      base: {ref: "main", sha: $base_sha, repo: {full_name: $repo}},
      head: {ref: $head_ref, sha: $head_sha, repo: {full_name: $repo}}
    }'
}

stub_gh() {
  [[ "${1:-}" == api ]] || fail "unexpected gh command: $*"

  local argument
  local endpoint
  local jq_filter=""
  local method=GET
  local previous=""
  local saw_head_sha=false
  local saw_merge_method=false

  for argument in "$@"; do
    if [[ "$previous" == --method ]]; then
      method=$argument
    fi
    if [[ "$previous" == --jq ]]; then
      jq_filter=$argument
    fi
    [[ "$argument" == "sha=${TARGET_SHA:-}" ]] && saw_head_sha=true
    [[ "$argument" == merge_method=merge ]] && saw_merge_method=true
    previous=$argument
  done
  endpoint="$(find_api_endpoint "$@")" || fail "gh api endpoint is missing: $*"
  if [[ "$endpoint" == repos/test/repository/releases/tags/* ]]; then
    [[ "$jq_filter" == 'select(.draft == false and .prerelease == false and .immutable == true) | .tag_name' ]] ||
      fail "release lookup omitted the stable immutable filter"
  fi

  case "${GH_STUB_SCENARIO:?}" in
    identity-mismatch)
      [[ "$endpoint" == */pulls/42 ]] || fail "unexpected endpoint: ${endpoint}"
      print_pr unexpected-user
      ;;
    head-mismatch)
      [[ "$endpoint" == */pulls/42 ]] || fail "unexpected endpoint: ${endpoint}"
      print_pr renovate[bot] open "$BASE_SHA"
      ;;
    base-mismatch)
      [[ "$endpoint" == */pulls/42 ]] || fail "unexpected endpoint: ${endpoint}"
      print_pr renovate[bot] open "$TARGET_SHA" "$TARGET_SHA"
      ;;
    closed)
      [[ "$endpoint" == */pulls/42 ]] || fail "unexpected endpoint: ${endpoint}"
      print_pr renovate[bot] closed
      ;;
    closed-draft)
      [[ "$endpoint" == */pulls/42 ]] || fail "unexpected endpoint: ${endpoint}"
      print_pr renovate[bot] closed "$TARGET_SHA" "$BASE_SHA" true
      ;;
    head-behind)
      case "$endpoint" in
        */pulls/42) print_pr renovate[bot] ;;
        */compare/${BASE_SHA}...${TARGET_SHA}) printf 'diverged\n' ;;
        *) fail "unexpected behind-head endpoint: ${endpoint}" ;;
      esac
      ;;
    merge-success)
      case "${method}:${endpoint}" in
        GET:*/pulls/42)
          print_pr renovate[bot]
          ;;
        GET:*/compare/${BASE_SHA}...${TARGET_SHA})
          printf 'ahead\n'
          ;;
        GET:repos/test/repository/releases/tags/v"${CURRENT_RELEASE}")
          printf 'v%s\n' "$CURRENT_RELEASE"
          ;;
        GET:repos/github/github-mcp-server/releases\?per_page=100)
          [[ "$GH_TOKEN" == read-token ]] || fail "release lookup did not use the read token"
          printf '%s\n' "$NEXT_UPSTREAM" "$CURRENT_UPSTREAM"
          ;;
        PUT:*/pulls/42/merge)
          [[ "$GH_TOKEN" == merge-token ]] || fail "merge request did not use the App token"
          [[ "$saw_head_sha" == true ]] || fail "merge request omitted exact head SHA"
          [[ "$saw_merge_method" == true ]] || fail "merge request omitted merge method"
          printf '{"merged":true,"sha":"%s","message":"merged"}\n' "$MERGE_SHA"
          ;;
        *) fail "unexpected merge endpoint: ${method}:${endpoint}" ;;
      esac
      ;;
    merge-out-of-order)
      case "${method}:${endpoint}" in
        GET:*/pulls/42)
          print_pr renovate[bot]
          ;;
        GET:repos/test/repository/releases/tags/v"${CURRENT_RELEASE}")
          fail "out-of-order PR waited for the current release"
          ;;
        GET:repos/github/github-mcp-server/releases\?per_page=100)
          printf '%s\n' "$LATER_UPSTREAM" "$NEXT_UPSTREAM" "$CURRENT_UPSTREAM"
          ;;
        PUT:*/pulls/42/merge)
          fail "out-of-order PR reached the merge endpoint"
          ;;
        *) fail "unexpected out-of-order endpoint: ${method}:${endpoint}" ;;
      esac
      ;;
    merge-missing-current-release)
      case "${method}:${endpoint}" in
        GET:*/pulls/42)
          print_pr renovate[bot]
          ;;
        GET:repos/test/repository/releases/tags/v"${CURRENT_RELEASE}")
          printf '{"status":"404"}\n'
          return 1
          ;;
        GET:repos/github/github-mcp-server/releases\?per_page=100)
          printf '%s\n' "$NEXT_UPSTREAM" "$CURRENT_UPSTREAM"
          ;;
        PUT:*/pulls/42/merge)
          fail "PR with an unpublished current release reached the merge endpoint"
          ;;
        *) fail "unexpected missing-current-release endpoint: ${method}:${endpoint}" ;;
      esac
      ;;
    merge-partial-current-release)
      case "${method}:${endpoint}" in
        GET:*/pulls/42)
          print_pr renovate[bot]
          ;;
        GET:repos/test/repository/releases/tags/v"${CURRENT_RELEASE}")
          printf 'v%s\n' "$CURRENT_RELEASE"
          return 42
          ;;
        GET:repos/github/github-mcp-server/releases\?per_page=100)
          printf '%s\n' "$NEXT_UPSTREAM" "$CURRENT_UPSTREAM"
          ;;
        PUT:*/pulls/42/merge)
          fail "partial current release response reached the merge endpoint"
          ;;
        *) fail "unexpected partial-current-release endpoint: ${method}:${endpoint}" ;;
      esac
      ;;
    merge-waits-current-release)
      case "${method}:${endpoint}" in
        GET:*/pulls/42)
          print_pr renovate[bot]
          ;;
        GET:repos/test/repository/releases/tags/v"${CURRENT_RELEASE}")
          : "${GH_STUB_COUNT_FILE:?}"
          printf 'attempt\n' >>"$GH_STUB_COUNT_FILE"
          if [[ "$(wc -l <"$GH_STUB_COUNT_FILE")" -eq 1 ]]; then
            printf '{"status":"404"}\n'
            return 1
          fi
          printf 'v%s\n' "$CURRENT_RELEASE"
          ;;
        GET:repos/github/github-mcp-server/releases\?per_page=100)
          printf '%s\n' "$NEXT_UPSTREAM" "$CURRENT_UPSTREAM"
          ;;
        PUT:*/pulls/42/merge)
          printf '{"merged":true,"sha":"%s","message":"merged"}\n' "$MERGE_SHA"
          ;;
        *) fail "unexpected wait-current-release endpoint: ${method}:${endpoint}" ;;
      esac
      ;;
    merge-release-api-error)
      case "${method}:${endpoint}" in
        GET:*/pulls/42)
          print_pr renovate[bot]
          ;;
        GET:repos/test/repository/releases/tags/v"${CURRENT_RELEASE}")
          : "${GH_STUB_HTTP_STATUS:?}"
          printf '{"status":"%s"}\n' "$GH_STUB_HTTP_STATUS"
          return 1
          ;;
        GET:repos/github/github-mcp-server/releases\?per_page=100)
          printf '%s\n' "$NEXT_UPSTREAM" "$CURRENT_UPSTREAM"
          ;;
        PUT:*/pulls/42/merge)
          fail "release API failure reached the merge endpoint"
          ;;
        *) fail "unexpected release-api-error endpoint: ${method}:${endpoint}" ;;
      esac
      ;;
    merge-base-advances-during-wait)
      case "${method}:${endpoint}" in
        GET:*/pulls/42)
          : "${GH_STUB_COUNT_FILE:?}"
          printf 'pull\n' >>"$GH_STUB_COUNT_FILE"
          if [[ "$(wc -l <"$GH_STUB_COUNT_FILE")" -eq 1 ]]; then
            print_pr renovate[bot]
          else
            print_pr renovate[bot] open "$TARGET_SHA" "$TARGET_SHA"
          fi
          ;;
        GET:repos/test/repository/releases/tags/v"${CURRENT_RELEASE}")
          printf 'v%s\n' "$CURRENT_RELEASE"
          ;;
        GET:repos/github/github-mcp-server/releases\?per_page=100)
          printf '%s\n' "$NEXT_UPSTREAM" "$CURRENT_UPSTREAM"
          ;;
        PUT:*/pulls/42/merge)
          fail "PR that moved during the release wait reached the merge endpoint"
          ;;
        *) fail "unexpected base-advance-during-wait endpoint: ${method}:${endpoint}" ;;
      esac
      ;;
    merge-head-moves-during-wait)
      case "${method}:${endpoint}" in
        GET:*/pulls/42)
          : "${GH_STUB_COUNT_FILE:?}"
          printf 'pull\n' >>"$GH_STUB_COUNT_FILE"
          if [[ "$(wc -l <"$GH_STUB_COUNT_FILE")" -eq 1 ]]; then
            print_pr renovate[bot]
          else
            print_pr renovate[bot] open "$MOVED_HEAD_SHA"
          fi
          ;;
        GET:repos/test/repository/releases/tags/v"${CURRENT_RELEASE}")
          printf 'v%s\n' "$CURRENT_RELEASE"
          ;;
        GET:repos/github/github-mcp-server/releases\?per_page=100)
          printf '%s\n' "$NEXT_UPSTREAM" "$CURRENT_UPSTREAM"
          ;;
        PUT:*/pulls/42/merge)
          fail "PR whose head moved during the release wait reached the merge endpoint"
          ;;
        *) fail "unexpected head-move-during-wait endpoint: ${method}:${endpoint}" ;;
      esac
      ;;
    *) fail "unexpected gh scenario: ${GH_STUB_SCENARIO}" ;;
  esac
}

case "${0##*/}" in
  gh)
    stub_gh "$@"
    exit 0
    ;;
esac

readonly ORIGINAL_PATH="$PATH"
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT_DIR
readonly MERGE_SCRIPT="${SCRIPT_DIR}/merge-upstream-release.sh"

TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/gh-mcp-merge-tests.XXXXXX")"
readonly TEST_ROOT
readonly STUB_BIN="${TEST_ROOT}/bin"
mkdir -p "$STUB_BIN"
ln -s "${SCRIPT_DIR}/${BASH_SOURCE[0]##*/}" "${STUB_BIN}/gh"

cleanup() {
  rm -rf -- "$TEST_ROOT"
}
trap cleanup EXIT

readonly BASE_SHA="2222222222222222222222222222222222222222"
readonly CURRENT_RELEASE="3.9.0"
readonly CURRENT_UPSTREAM="v1.10.0"
readonly HEAD_REF="renovate/github-github-mcp-server-1.x"
readonly LATER_UPSTREAM="v1.10.2"
readonly MERGE_SHA="1111111111111111111111111111111111111111"
readonly MOVED_HEAD_SHA="4444444444444444444444444444444444444444"
readonly NEXT_UPSTREAM="v1.10.1"
readonly TARGET_SHA="3333333333333333333333333333333333333333"
export BASE_SHA CURRENT_RELEASE CURRENT_UPSTREAM HEAD_REF LATER_UPSTREAM MERGE_SHA MOVED_HEAD_SHA
export NEXT_UPSTREAM TARGET_SHA

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

create_workflow_run_event() {
  local path=$1

  jq -nc \
    --arg base_sha "$BASE_SHA" \
    --arg head_ref "$HEAD_REF" \
    --arg head_sha "$TARGET_SHA" \
    --arg repo test/repository \
    --argjson repo_id 100 \
    '{
      workflow_run: {
        name: "CI",
        event: "pull_request",
        conclusion: "success",
        head_branch: $head_ref,
        head_sha: $head_sha,
        repository: {id: $repo_id, full_name: $repo},
        head_repository: {id: $repo_id, full_name: $repo},
        pull_requests: [{
          number: 42,
          base: {ref: "main", sha: $base_sha, repo: {id: $repo_id}},
          head: {ref: $head_ref, sha: $head_sha, repo: {id: $repo_id}}
        }]
      }
    }' >"$path"
}

test_rejects_changed_candidate() {
  local output="${TEST_ROOT}/changed-output"
  local stderr="${TEST_ROOT}/changed-stderr"

  : >"$output"
  if GITHUB_OUTPUT="$output" "$MERGE_SCRIPT" eligible true true 2>"$stderr"; then
    fail "merge eligibility accepted generated changes"
  fi

  assert_has_line "$stderr" "Prepared candidate still has generated changes."
  echo "ok - merge eligibility rejects generated changes"
}

test_inspect_rejects_identity_mismatch() {
  local event="${TEST_ROOT}/identity-event"
  local output="${TEST_ROOT}/identity-output"
  local stderr="${TEST_ROOT}/identity-stderr"

  create_workflow_run_event "$event"
  : >"$output"
  if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=identity-mismatch \
    GH_TOKEN=test-token \
    GITHUB_EVENT_PATH="$event" \
    GITHUB_OUTPUT="$output" \
    GITHUB_REPOSITORY=test/repository \
    "$MERGE_SCRIPT" inspect 2>"$stderr"; then
    fail "merge inspection accepted an unexpected PR author"
  fi

  assert_has_line "$stderr" "PR #42 author is not renovate[bot]."
  echo "ok - merge inspection rejects identity mismatch"
}

test_inspect_requires_exactly_one_pr() {
  local event="${TEST_ROOT}/pr-count-event"
  local invalid_event
  local output="${TEST_ROOT}/pr-count-output"
  local pr_count
  local stderr="${TEST_ROOT}/pr-count-stderr"

  create_workflow_run_event "$event"
  for pr_count in 0 2; do
    invalid_event="${TEST_ROOT}/pr-count-${pr_count}-invalid-event"
    if ((pr_count == 0)); then
      jq '.workflow_run.pull_requests = []' "$event" >"$invalid_event"
    else
      jq '.workflow_run.pull_requests += [.workflow_run.pull_requests[0]]' \
        "$event" >"$invalid_event"
    fi

    : >"$output"
    : >"$stderr"
    if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
      GH_STUB_SCENARIO=merge-success \
      GH_TOKEN=test-token \
      GITHUB_EVENT_PATH="$invalid_event" \
      GITHUB_OUTPUT="$output" \
      GITHUB_REPOSITORY=test/repository \
      "$MERGE_SCRIPT" inspect 2>"$stderr"; then
      fail "merge inspection accepted a workflow run with ${pr_count} PRs"
    fi

    assert_has_line "$stderr" "CI workflow run must reference exactly one PR."
  done
  echo "ok - merge inspection requires exactly one PR"
}

test_inspect_skips_stale_head() {
  local event="${TEST_ROOT}/head-event"
  local output="${TEST_ROOT}/head-output"

  create_workflow_run_event "$event"
  : >"$output"
  PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=head-mismatch \
    GH_TOKEN=test-token \
    GITHUB_EVENT_PATH="$event" \
    GITHUB_OUTPUT="$output" \
    GITHUB_REPOSITORY=test/repository \
    "$MERGE_SCRIPT" inspect >/dev/null

  assert_exact_output "$output" "current=false"
  echo "ok - merge inspection skips a stale head"
}

test_inspect_skips_closed_pr() {
  local event="${TEST_ROOT}/closed-event"
  local output="${TEST_ROOT}/closed-output"

  create_workflow_run_event "$event"
  : >"$output"
  PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=closed \
    GH_TOKEN=test-token \
    GITHUB_EVENT_PATH="$event" \
    GITHUB_OUTPUT="$output" \
    GITHUB_REPOSITORY=test/repository \
    "$MERGE_SCRIPT" inspect >/dev/null

  assert_exact_output "$output" "current=false"
  echo "ok - merge inspection skips an already closed PR"
}

test_inspect_skips_closed_draft_pr() {
  local event="${TEST_ROOT}/closed-draft-event"
  local output="${TEST_ROOT}/closed-draft-output"

  create_workflow_run_event "$event"
  : >"$output"
  PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=closed-draft \
    GH_TOKEN=test-token \
    GITHUB_EVENT_PATH="$event" \
    GITHUB_OUTPUT="$output" \
    GITHUB_REPOSITORY=test/repository \
    "$MERGE_SCRIPT" inspect >/dev/null

  assert_exact_output "$output" "current=false"
  echo "ok - merge inspection skips an already closed draft PR"
}

test_inspect_accepts_current_pr() {
  local event="${TEST_ROOT}/current-event"
  local output="${TEST_ROOT}/current-output"

  create_workflow_run_event "$event"
  : >"$output"
  PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=merge-success \
    GH_TOKEN=test-token \
    GITHUB_EVENT_PATH="$event" \
    GITHUB_OUTPUT="$output" \
    GITHUB_REPOSITORY=test/repository \
    "$MERGE_SCRIPT" inspect >/dev/null

  assert_exact_output "$output" \
    "current=true" \
    "pr_number=42" \
    "base_sha=${BASE_SHA}" \
    "head_sha=${TARGET_SHA}"
  echo "ok - merge inspection accepts the exact current PR"
}

test_inspect_waits_for_rebase() {
  local event="${TEST_ROOT}/behind-event"
  local output="${TEST_ROOT}/behind-output"
  local stdout="${TEST_ROOT}/behind-stdout"

  create_workflow_run_event "$event"
  : >"$output"
  PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=head-behind \
    GH_TOKEN=test-token \
    GITHUB_EVENT_PATH="$event" \
    GITHUB_OUTPUT="$output" \
    GITHUB_REPOSITORY=test/repository \
    "$MERGE_SCRIPT" inspect >"$stdout"

  assert_exact_output "$output" "current=false"
  assert_has_line "$stdout" \
    "PR #42 does not include base ${BASE_SHA}; waiting for Renovate to rebase."
  echo "ok - merge inspection waits for Renovate to rebase"
}

test_uses_exact_head_sha() {
  local stdout="${TEST_ROOT}/success-stdout"

  PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=merge-success \
    GH_TOKEN=read-token \
    MERGE_TOKEN=merge-token \
    GITHUB_REPOSITORY=test/repository \
    RELEASE_WAIT_ATTEMPTS=1 \
    "$MERGE_SCRIPT" merge 42 "$BASE_SHA" "$TARGET_SHA" \
    "$CURRENT_RELEASE" "$CURRENT_UPSTREAM" "$NEXT_UPSTREAM" >"$stdout"

  assert_has_line "$stdout" "Merged PR #42 as ${MERGE_SHA}."
  echo "ok - merge uses the exact validated head SHA"
}

test_rejects_stale_base() {
  local stderr="${TEST_ROOT}/stale-base-stderr"

  if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=base-mismatch \
    GH_TOKEN=read-token \
    MERGE_TOKEN=merge-token \
    GITHUB_REPOSITORY=test/repository \
    "$MERGE_SCRIPT" merge 42 "$BASE_SHA" "$TARGET_SHA" \
    "$CURRENT_RELEASE" "$CURRENT_UPSTREAM" "$NEXT_UPSTREAM" >/dev/null 2>"$stderr"; then
    fail "merge accepted a base advance after canonical verification"
  fi

  assert_has_line "$stderr" \
    "PR #42 base advanced after canonical verification; rerun CI against the current base."
  echo "ok - merge rejects a base advance after canonical verification"
}

test_rejects_out_of_order_release() {
  local stderr="${TEST_ROOT}/out-of-order-stderr"

  if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=merge-out-of-order \
    GH_TOKEN=read-token \
    MERGE_TOKEN=merge-token \
    GITHUB_REPOSITORY=test/repository \
    "$MERGE_SCRIPT" merge 42 "$BASE_SHA" "$TARGET_SHA" \
    "$CURRENT_RELEASE" "$CURRENT_UPSTREAM" "$LATER_UPSTREAM" >/dev/null 2>"$stderr"; then
    fail "merge accepted an out-of-order upstream release"
  fi

  assert_has_line "$stderr" \
    'earlier upstream release must be applied first: "v1.10.1" precedes "v1.10.2"'
  echo "ok - merge rejects an out-of-order upstream release"
}

test_rejects_unpublished_current_release() {
  local stderr="${TEST_ROOT}/unpublished-current-release-stderr"

  if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=merge-missing-current-release \
    GH_TOKEN=read-token \
    MERGE_TOKEN=merge-token \
    GITHUB_REPOSITORY=test/repository \
    RELEASE_WAIT_ATTEMPTS=1 \
    "$MERGE_SCRIPT" merge 42 "$BASE_SHA" "$TARGET_SHA" \
    "$CURRENT_RELEASE" "$CURRENT_UPSTREAM" "$NEXT_UPSTREAM" >/dev/null 2>"$stderr"; then
    fail "merge accepted an unpublished current gh-mcp release"
  fi

  assert_has_line "$stderr" "Current gh-mcp release is not published: v${CURRENT_RELEASE}"
  echo "ok - merge rejects an unpublished current gh-mcp release"
}

test_rejects_partial_current_release_response() {
  local stderr="${TEST_ROOT}/partial-current-release-stderr"

  if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=merge-partial-current-release \
    GH_TOKEN=read-token \
    MERGE_TOKEN=merge-token \
    GITHUB_REPOSITORY=test/repository \
    RELEASE_WAIT_ATTEMPTS=1 \
    "$MERGE_SCRIPT" merge 42 "$BASE_SHA" "$TARGET_SHA" \
    "$CURRENT_RELEASE" "$CURRENT_UPSTREAM" "$NEXT_UPSTREAM" >/dev/null 2>"$stderr"; then
    fail "merge accepted a partial current release API response"
  fi

  assert_has_line "$stderr" "Failed to inspect current gh-mcp release v${CURRENT_RELEASE}."
  echo "ok - merge rejects a partial current release API response"
}

test_rejects_release_api_errors() {
  local status stderr

  for status in 401 403; do
    stderr="${TEST_ROOT}/release-api-${status}-stderr"
    if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
      GH_STUB_SCENARIO=merge-release-api-error \
      GH_STUB_HTTP_STATUS="$status" \
      GH_TOKEN=read-token \
      MERGE_TOKEN=merge-token \
      GITHUB_REPOSITORY=test/repository \
      RELEASE_WAIT_ATTEMPTS=2 \
      RELEASE_WAIT_SECONDS=0 \
      "$MERGE_SCRIPT" merge 42 "$BASE_SHA" "$TARGET_SHA" \
      "$CURRENT_RELEASE" "$CURRENT_UPSTREAM" "$NEXT_UPSTREAM" >/dev/null 2>"$stderr"; then
      fail "merge retried an HTTP ${status} release API failure"
    fi

    assert_has_line "$stderr" \
      "Failed to inspect current gh-mcp release v${CURRENT_RELEASE}: HTTP ${status}."
  done
  echo "ok - merge rejects release API authentication and permission errors"
}

test_waits_for_current_release() {
  local count_file="${TEST_ROOT}/release-attempts"
  local stdout="${TEST_ROOT}/wait-current-release-stdout"

  : >"$count_file"
  PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=merge-waits-current-release \
    GH_STUB_COUNT_FILE="$count_file" \
    GH_TOKEN=read-token \
    MERGE_TOKEN=merge-token \
    GITHUB_REPOSITORY=test/repository \
    RELEASE_WAIT_ATTEMPTS=2 \
    RELEASE_WAIT_SECONDS=0 \
    "$MERGE_SCRIPT" merge 42 "$BASE_SHA" "$TARGET_SHA" \
    "$CURRENT_RELEASE" "$CURRENT_UPSTREAM" "$NEXT_UPSTREAM" >"$stdout"

  [[ "$(wc -l <"$count_file")" -eq 2 ]] || fail "merge did not retry current release lookup"
  assert_has_line "$stdout" \
    "Waiting for current gh-mcp release v${CURRENT_RELEASE} to be published."
  assert_has_line "$stdout" "Merged PR #42 as ${MERGE_SHA}."
  echo "ok - merge waits for the current gh-mcp release"
}

test_wait_command_uses_read_token() {
  local stdout="${TEST_ROOT}/wait-command-stdout"

  PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=merge-success \
    GH_TOKEN=read-token \
    GITHUB_REPOSITORY=test/repository \
    RELEASE_WAIT_ATTEMPTS=1 \
    "$MERGE_SCRIPT" wait "$CURRENT_RELEASE" >"$stdout"

  assert_has_line "$stdout" "Verified current gh-mcp release is published: v${CURRENT_RELEASE}."
  echo "ok - release wait command uses the read token"
}

test_wait_command_requires_repository() {
  local stderr="${TEST_ROOT}/wait-command-missing-repository-stderr"

  if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=merge-success \
    GH_TOKEN=read-token \
    GITHUB_REPOSITORY='' \
    RELEASE_WAIT_ATTEMPTS=1 \
    "$MERGE_SCRIPT" wait "$CURRENT_RELEASE" >/dev/null 2>"$stderr"; then
    fail "release wait command accepted a missing repository"
  fi

  assert_has_line "$stderr" "GITHUB_REPOSITORY must be set."
  echo "ok - release wait command requires the repository"
}

test_rejects_base_advance_after_release_wait() {
  local count_file="${TEST_ROOT}/pull-attempts"
  local stderr="${TEST_ROOT}/base-advance-during-wait-stderr"

  : >"$count_file"
  if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=merge-base-advances-during-wait \
    GH_STUB_COUNT_FILE="$count_file" \
    GH_TOKEN=read-token \
    MERGE_TOKEN=merge-token \
    GITHUB_REPOSITORY=test/repository \
    "$MERGE_SCRIPT" merge 42 "$BASE_SHA" "$TARGET_SHA" \
    "$CURRENT_RELEASE" "$CURRENT_UPSTREAM" "$NEXT_UPSTREAM" >/dev/null 2>"$stderr"; then
    fail "merge accepted a base advance during the release wait"
  fi

  [[ "$(wc -l <"$count_file")" -eq 2 ]] || fail "merge did not re-fetch the PR after waiting"
  assert_has_line "$stderr" \
    "PR #42 base advanced while waiting for the current release; rerun CI against the current base."
  echo "ok - merge rejects a base advance after waiting for the current release"
}

test_skips_head_move_after_release_wait() {
  local count_file="${TEST_ROOT}/head-pull-attempts"
  local stdout="${TEST_ROOT}/head-moved-during-wait-stdout"

  : >"$count_file"
  PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=merge-head-moves-during-wait \
    GH_STUB_COUNT_FILE="$count_file" \
    GH_TOKEN=read-token \
    MERGE_TOKEN=merge-token \
    GITHUB_REPOSITORY=test/repository \
    "$MERGE_SCRIPT" merge 42 "$BASE_SHA" "$TARGET_SHA" \
    "$CURRENT_RELEASE" "$CURRENT_UPSTREAM" "$NEXT_UPSTREAM" >"$stdout"

  [[ "$(wc -l <"$count_file")" -eq 2 ]] || fail "merge did not re-fetch the PR after waiting"
  assert_has_line "$stdout" \
    "PR #42 head moved while waiting for the current release; skipping stale merge."
  echo "ok - merge skips a moved head after waiting for the current release"
}

test_skips_major_update() {
  local output="${TEST_ROOT}/major-output"
  local stdout="${TEST_ROOT}/major-stdout"

  : >"$output"
  GITHUB_OUTPUT="$output" "$MERGE_SCRIPT" eligible false false >"$stdout"

  assert_exact_output "$output" "eligible=false"
  assert_has_line "$stdout" "Major upstream update requires manual merge."
  echo "ok - merge eligibility skips major updates"
}

test_accepts_same_major_update() {
  local output="${TEST_ROOT}/same-major-output"

  : >"$output"
  GITHUB_OUTPUT="$output" "$MERGE_SCRIPT" eligible false true

  assert_exact_output "$output" "eligible=true"
  echo "ok - merge eligibility accepts same-major updates"
}

test_rejects_changed_candidate
test_inspect_rejects_identity_mismatch
test_inspect_requires_exactly_one_pr
test_inspect_skips_stale_head
test_inspect_skips_closed_pr
test_inspect_skips_closed_draft_pr
test_inspect_accepts_current_pr
test_inspect_waits_for_rebase
test_uses_exact_head_sha
test_rejects_stale_base
test_rejects_out_of_order_release
test_rejects_unpublished_current_release
test_rejects_partial_current_release_response
test_rejects_release_api_errors
test_waits_for_current_release
test_wait_command_uses_read_token
test_wait_command_requires_repository
test_rejects_base_advance_after_release_wait
test_skips_head_move_after_release_wait
test_skips_major_update
test_accepts_same_major_update
