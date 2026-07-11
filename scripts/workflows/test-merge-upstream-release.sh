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
  local method=GET
  local previous=""
  local saw_head_sha=false
  local saw_merge_method=false

  for argument in "$@"; do
    if [[ "$previous" == --method ]]; then
      method=$argument
    fi
    [[ "$argument" == "sha=${TARGET_SHA:-}" ]] && saw_head_sha=true
    [[ "$argument" == merge_method=merge ]] && saw_merge_method=true
    previous=$argument
  done
  endpoint="$(find_api_endpoint "$@")" || fail "gh api endpoint is missing: $*"

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
        PUT:*/pulls/42/merge)
          [[ "$GH_TOKEN" == merge-token ]] || fail "merge request did not use the App token"
          [[ "$saw_head_sha" == true ]] || fail "merge request omitted exact head SHA"
          [[ "$saw_merge_method" == true ]] || fail "merge request omitted merge method"
          printf '{"merged":true,"sha":"%s","message":"merged"}\n' "$MERGE_SHA"
          ;;
        *) fail "unexpected merge endpoint: ${method}:${endpoint}" ;;
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
readonly HEAD_REF="renovate/github-github-mcp-server-1.x"
readonly MERGE_SHA="1111111111111111111111111111111111111111"
readonly TARGET_SHA="3333333333333333333333333333333333333333"
export BASE_SHA HEAD_REF MERGE_SHA TARGET_SHA

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
  local invalid_event="${TEST_ROOT}/pr-count-invalid-event"
  local output="${TEST_ROOT}/pr-count-output"
  local stderr="${TEST_ROOT}/pr-count-stderr"

  create_workflow_run_event "$event"
  jq '.workflow_run.pull_requests = []' "$event" >"$invalid_event"
  : >"$output"
  if PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=merge-success \
    GH_TOKEN=test-token \
    GITHUB_EVENT_PATH="$invalid_event" \
    GITHUB_OUTPUT="$output" \
    GITHUB_REPOSITORY=test/repository \
    "$MERGE_SCRIPT" inspect 2>"$stderr"; then
    fail "merge inspection accepted a workflow run without exactly one PR"
  fi

  assert_has_line "$stderr" "CI workflow run must reference exactly one PR."
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
    "$MERGE_SCRIPT" merge 42 "$BASE_SHA" "$TARGET_SHA" >"$stdout"

  assert_has_line "$stdout" "Merged PR #42 as ${MERGE_SHA}."
  echo "ok - merge uses the exact validated head SHA"
}

test_skips_stale_base() {
  local stdout="${TEST_ROOT}/stale-base-stdout"

  PATH="${STUB_BIN}:${ORIGINAL_PATH}" \
    GH_STUB_SCENARIO=base-mismatch \
    GH_TOKEN=read-token \
    MERGE_TOKEN=merge-token \
    GITHUB_REPOSITORY=test/repository \
    "$MERGE_SCRIPT" merge 42 "$BASE_SHA" "$TARGET_SHA" >"$stdout"

  assert_has_line "$stdout" "PR #42 moved after canonical verification; skipping stale merge."
  echo "ok - merge skips a stale base"
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
test_skips_stale_base
test_skips_major_update
test_accepts_same_major_update
