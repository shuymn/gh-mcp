#!/usr/bin/env bash

set -euo pipefail

readonly EXPECTED_AUTHOR="renovate[bot]"
readonly EXPECTED_BASE_REF="main"
readonly EXPECTED_WORKFLOW="CI"
readonly HEAD_REF_PREFIX="renovate/github-github-mcp-server-"

die() {
  echo "$*" >&2
  exit 1
}

validate_pr_identity() {
  local pr_number=$1
  local pr_state=$2
  local pr_draft=$3
  local pr_author=$4
  local base_ref=$5
  local base_repo=$6
  local head_ref=$7
  local head_repo=$8

  [[ "$pr_number" =~ ^[1-9][0-9]*$ ]] || die "Invalid PR number: ${pr_number}."
  [[ "$pr_state" == open || "$pr_state" == closed ]] ||
    die "Unexpected state for PR #${pr_number}: ${pr_state}."
  if [[ "$pr_state" == open ]]; then
    [[ "$pr_draft" == false ]] || die "PR #${pr_number} is a draft."
  fi
  [[ "$pr_author" == "$EXPECTED_AUTHOR" ]] ||
    die "PR #${pr_number} author is not ${EXPECTED_AUTHOR}."
  [[ "$base_ref" == "$EXPECTED_BASE_REF" ]] ||
    die "PR #${pr_number} does not target ${EXPECTED_BASE_REF}."
  [[ "$base_repo" == "$GITHUB_REPOSITORY" ]] ||
    die "PR #${pr_number} base repository is not ${GITHUB_REPOSITORY}."
  [[ "$head_repo" == "$GITHUB_REPOSITORY" ]] ||
    die "PR #${pr_number} head repository is not ${GITHUB_REPOSITORY}."
  [[ "$head_ref" == "${HEAD_REF_PREFIX}"* ]] ||
    die "PR #${pr_number} head ref does not match ${HEAD_REF_PREFIX}*."
}

load_pr() {
  local pr_number=$1
  local response row

  response="$(gh api "repos/${GITHUB_REPOSITORY}/pulls/${pr_number}")"
  row="$(
    jq -r '
      [
        (.number | tostring), .state, (.draft | tostring), .user.login,
        .base.ref, .base.sha, .base.repo.full_name,
        .head.ref, .head.sha, .head.repo.full_name
      ] | @tsv
    ' <<<"$response"
  )" || die "Failed to parse PR #${pr_number}."

  IFS=$'\t' read -r \
    LIVE_PR_NUMBER LIVE_PR_STATE LIVE_PR_DRAFT LIVE_PR_AUTHOR \
    LIVE_BASE_REF LIVE_BASE_SHA LIVE_BASE_REPO \
    LIVE_HEAD_REF LIVE_HEAD_SHA LIVE_HEAD_REPO <<<"$row"

  [[ "$LIVE_PR_NUMBER" == "$pr_number" ]] ||
    die "GitHub returned PR #${LIVE_PR_NUMBER} while inspecting PR #${pr_number}."
  validate_pr_identity \
    "$LIVE_PR_NUMBER" "$LIVE_PR_STATE" "$LIVE_PR_DRAFT" "$LIVE_PR_AUTHOR" \
    "$LIVE_BASE_REF" "$LIVE_BASE_REPO" "$LIVE_HEAD_REF" "$LIVE_HEAD_REPO"
}

write_current_output() {
  local current=$1

  echo "current=${current}" >>"$GITHUB_OUTPUT"
  if [[ "$current" == true ]]; then
    {
      echo "pr_number=${LIVE_PR_NUMBER}"
      echo "base_sha=${LIVE_BASE_SHA}"
      echo "head_sha=${LIVE_HEAD_SHA}"
    } >>"$GITHUB_OUTPUT"
  fi
}

inspect_workflow_run() {
  [[ $# -eq 0 ]] || die "Usage: $0 inspect"
  require_env GH_TOKEN
  require_env GITHUB_EVENT_PATH
  require_env GITHUB_OUTPUT
  require_env GITHUB_REPOSITORY

  local pr_count event_row comparison_status
  local workflow_name workflow_event workflow_conclusion run_head_ref run_head_sha
  local run_repo run_repo_id run_head_repo run_head_repo_id
  local event_pr_number event_base_ref event_base_sha event_base_repo_id
  local event_head_ref event_head_sha event_head_repo_id

  pr_count="$(jq -r '.workflow_run.pull_requests | length' "$GITHUB_EVENT_PATH")"
  [[ "$pr_count" == 1 ]] || die "CI workflow run must reference exactly one PR."

  event_row="$(
    jq -r '
      .workflow_run as $run |
      $run.pull_requests[0] as $pr |
      [
        $run.name, $run.event, $run.conclusion,
        $run.head_branch, $run.head_sha,
        $run.repository.full_name, ($run.repository.id | tostring),
        $run.head_repository.full_name, ($run.head_repository.id | tostring),
        ($pr.number | tostring), $pr.base.ref, $pr.base.sha,
        ($pr.base.repo.id | tostring), $pr.head.ref, $pr.head.sha,
        ($pr.head.repo.id | tostring)
      ] | @tsv
    ' "$GITHUB_EVENT_PATH"
  )" || die "Failed to parse CI workflow run."

  IFS=$'\t' read -r \
    workflow_name workflow_event workflow_conclusion run_head_ref run_head_sha \
    run_repo run_repo_id run_head_repo run_head_repo_id \
    event_pr_number event_base_ref event_base_sha event_base_repo_id \
    event_head_ref event_head_sha event_head_repo_id <<<"$event_row"

  [[ "$workflow_name" == "$EXPECTED_WORKFLOW" ]] || die "Workflow run is not ${EXPECTED_WORKFLOW}."
  [[ "$workflow_event" == pull_request ]] || die "CI workflow run is not for a pull request."
  [[ "$workflow_conclusion" == success ]] || die "CI workflow run did not succeed."
  [[ "$run_repo" == "$GITHUB_REPOSITORY" && "$run_head_repo" == "$GITHUB_REPOSITORY" ]] ||
    die "CI workflow run is not from ${GITHUB_REPOSITORY}."
  [[ "$run_repo_id" == "$run_head_repo_id" ]] || die "CI workflow run head repository differs."
  [[ "$run_head_ref" == "${HEAD_REF_PREFIX}"* ]] ||
    die "CI workflow run head ref does not match ${HEAD_REF_PREFIX}*."
  [[ "$event_base_ref" == "$EXPECTED_BASE_REF" ]] || die "CI workflow run base is not main."
  [[ "$event_base_repo_id" == "$run_repo_id" && "$event_head_repo_id" == "$run_repo_id" ]] ||
    die "CI workflow run PR repositories differ."
  [[ "$event_head_ref" == "$run_head_ref" && "$event_head_sha" == "$run_head_sha" ]] ||
    die "CI workflow run PR head does not match the tested head."

  load_pr "$event_pr_number"

  if [[ "$LIVE_PR_STATE" == closed ]]; then
    echo "PR #${LIVE_PR_NUMBER} is already closed; skipping stale CI workflow run."
    write_current_output false
    return 0
  fi
  if [[ "$LIVE_BASE_SHA" != "$event_base_sha" || "$LIVE_HEAD_SHA" != "$run_head_sha" ||
    "$LIVE_HEAD_REF" != "$run_head_ref" ]]; then
    echo "PR #${LIVE_PR_NUMBER} moved after this CI workflow run; skipping stale result."
    write_current_output false
    return 0
  fi

  comparison_status="$(
    gh api \
      "repos/${GITHUB_REPOSITORY}/compare/${LIVE_BASE_SHA}...${LIVE_HEAD_SHA}" \
      --jq '.status'
  )"
  case "$comparison_status" in
    ahead | identical) ;;
    behind | diverged)
      echo "PR #${LIVE_PR_NUMBER} does not include base ${LIVE_BASE_SHA}; waiting for Renovate to rebase."
      write_current_output false
      return 0
      ;;
    *) die "Unexpected comparison status for PR #${LIVE_PR_NUMBER}: ${comparison_status}." ;;
  esac

  write_current_output true
}

merge_pr() {
  [[ $# -eq 3 ]] || die "Usage: $0 merge <pr-number> <base-sha> <head-sha>"
  require_env GH_TOKEN
  require_env MERGE_TOKEN
  require_env GITHUB_REPOSITORY

  local pr_number=$1
  local expected_base_sha=$2
  local expected_head_sha=$3
  local response merged merge_sha message

  [[ "$expected_base_sha" =~ ^[0-9a-f]{40}$ ]] || die "Invalid expected base SHA."
  [[ "$expected_head_sha" =~ ^[0-9a-f]{40}$ ]] || die "Invalid expected head SHA."

  # Re-fetch immediately before the merge. The REST merge endpoint also binds the
  # update to expected_head_sha, so a concurrent PR-head update fails atomically.
  load_pr "$pr_number"
  if [[ "$LIVE_PR_STATE" == closed ]]; then
    echo "PR #${LIVE_PR_NUMBER} is already closed; nothing to merge."
    return 0
  fi
  if [[ "$LIVE_BASE_SHA" != "$expected_base_sha" || "$LIVE_HEAD_SHA" != "$expected_head_sha" ]]; then
    echo "PR #${LIVE_PR_NUMBER} moved after canonical verification; skipping stale merge."
    return 0
  fi

  response="$(
    GH_TOKEN="$MERGE_TOKEN" gh api --method PUT \
      "repos/${GITHUB_REPOSITORY}/pulls/${LIVE_PR_NUMBER}/merge" \
      -f merge_method=merge \
      -f "sha=${expected_head_sha}"
  )"
  merged="$(jq -r '.merged // false' <<<"$response")"
  message="$(jq -r '.message // "GitHub returned no merge message."' <<<"$response")"
  [[ "$merged" == true ]] || die "GitHub did not merge PR #${LIVE_PR_NUMBER}: ${message}"
  merge_sha="$(jq -r '.sha // empty' <<<"$response")"
  [[ -n "$merge_sha" ]] || die "GitHub merged PR #${LIVE_PR_NUMBER} without returning a merge SHA."

  echo "Merged PR #${LIVE_PR_NUMBER} as ${merge_sha}."
}

require_env() {
  local name=$1

  [[ -n "${!name:-}" ]] || die "${name} must be set."
}

check_eligibility() {
  [[ $# -eq 2 ]] || die "Usage: $0 eligible <changed> <auto-merge>"
  require_env GITHUB_OUTPUT

  local changed=$1
  local auto_merge=$2

  case "$changed" in
    false) ;;
    true) die "Prepared candidate still has generated changes." ;;
    *) die "Unexpected changed value: ${changed}." ;;
  esac

  case "$auto_merge" in
    true)
      echo "eligible=true" >>"$GITHUB_OUTPUT"
      ;;
    false)
      echo "Major upstream update requires manual merge."
      echo "eligible=false" >>"$GITHUB_OUTPUT"
      ;;
    *) die "Unexpected auto_merge value: ${auto_merge}." ;;
  esac
}

case "${1:-}" in
  inspect)
    shift
    inspect_workflow_run "$@"
    ;;
  eligible)
    shift
    check_eligibility "$@"
    ;;
  merge)
    shift
    merge_pr "$@"
    ;;
  *)
    die "Usage: $0 {inspect|eligible|merge}"
    ;;
esac
