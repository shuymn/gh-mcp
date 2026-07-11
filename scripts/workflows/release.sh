#!/usr/bin/env bash

set -euo pipefail

readonly SIGNER_WORKFLOW="shuymn/gh-mcp/.github/workflows/release.yml"
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

die() {
  echo "$*" >&2
  exit 1
}

require_env() {
  local name=$1

  [[ -n "${!name:-}" ]] || die "${name} must be set."
}

assert_exact_assets() {
  local label=$1
  shift
  local -a actual_assets=("$@")
  local actual
  local expected
  local found

  if ((${#actual_assets[@]} != ${#EXPECTED_RELEASE_ASSETS[@]})); then
    die "${label} has an unexpected asset set."
  fi
  for expected in "${EXPECTED_RELEASE_ASSETS[@]}"; do
    found=false
    for actual in "${actual_assets[@]}"; do
      if [[ "$actual" == "$expected" ]]; then
        found=true
        break
      fi
    done
    [[ "$found" == true ]] || die "${label} has an unexpected asset set."
  done
}

assert_release_metadata() {
  local tag=$1
  local field=$2
  local expected_value=$3
  local metadata_error=$4
  local asset_label=$5
  local release_details_output
  local -a release_details

  release_details_output="$(
    gh api "repos/${GITHUB_REPOSITORY}/releases/tags/${tag}" \
      --jq "${field}, ([.assets[].name] | sort | .[])"
  )"
  mapfile -t release_details <<<"$release_details_output"
  if [[ "${release_details[0]:-}" != "$expected_value" ]]; then
    die "$metadata_error"
  fi
  assert_exact_assets "$asset_label" "${release_details[@]:1}"
}

verify_asset_attestations() {
  local tag=$1
  local provenance_digest=$2
  local assets_dir=$3
  local asset
  local path
  local -a downloaded_assets=()
  local -a downloaded_paths

  mkdir -p "$assets_dir"
  gh release download "$tag" \
    --repo "$GITHUB_REPOSITORY" \
    --pattern '*' \
    --dir "$assets_dir"
  shopt -s dotglob nullglob
  downloaded_paths=("$assets_dir"/*)
  shopt -u dotglob nullglob
  for path in "${downloaded_paths[@]}"; do
    [[ -f "$path" ]] || die "Downloaded release ${tag} contains a non-file asset."
    downloaded_assets+=("${path##*/}")
  done
  assert_exact_assets "Downloaded release ${tag}" "${downloaded_assets[@]}"
  for asset in "${EXPECTED_RELEASE_ASSETS[@]}"; do
    gh attestation verify "${assets_dir}/${asset}" \
      --repo "$GITHUB_REPOSITORY" \
      --signer-workflow "$SIGNER_WORKFLOW" \
      --source-digest "$provenance_digest" \
      --deny-self-hosted-runners >/dev/null
  done
}

resolve_tag_target() {
  local tag=$1
  local tag_ref tag_type tag_object

  tag_ref="$(
    gh api "repos/${GITHUB_REPOSITORY}/git/matching-refs/tags/${tag}" \
      --jq ".[] | select(.ref == \"refs/tags/${tag}\") | [.object.type, .object.sha] | @tsv"
  )"
  if [[ -z "$tag_ref" ]]; then
    return 0
  fi

  IFS=$'\t' read -r tag_type tag_object <<<"$tag_ref"
  case "$tag_type" in
    commit)
      echo "$tag_object"
      ;;
    tag)
      gh api "repos/${GITHUB_REPOSITORY}/git/tags/${tag_object}" --jq '.object.sha'
      ;;
    *)
      die "Unsupported tag object type for ${tag}: ${tag_type}"
      ;;
  esac
}

select_release() {
  require_env GITHUB_OUTPUT
  require_env GITHUB_REPOSITORY
  require_env TARGET_SHA

  local version tag release_inventory highest_published highest_candidate
  local release_state tag_target create_tag

  version="$(cat VERSION)"
  if [[ ! "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
    die "VERSION must be canonical major.minor.patch, got: $version"
  fi
  tag="v${version}"

  release_inventory="$(
    gh api --paginate "repos/${GITHUB_REPOSITORY}/releases?per_page=100" \
      --jq '.[] | [.tag_name, .draft] | @tsv'
  )"
  highest_published="$(
    awk -F '\t' '$2 == "false" { print $1 }' <<<"$release_inventory" |
      sed -nE 's/^v((0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*))$/\1/p' |
      sort -V |
      tail -n 1
  )"
  if [[ -n "$highest_published" ]]; then
    highest_candidate="$(printf '%s\n' "$version" "$highest_published" | sort -V | tail -n 1)"
    if [[ "$highest_candidate" != "$version" ]]; then
      echo "Release ${tag} is superseded by published v${highest_published}."
      tag="v${highest_published}"
    fi
  fi

  release_state="$(
    awk -F '\t' -v tag="$tag" \
      '$1 == tag { print ($2 == "true" ? "draft" : "published") }' \
      <<<"$release_inventory"
  )"
  case "$release_state" in
    "" | draft | published) ;;
    *) die "Unexpected release state for ${tag}: ${release_state}" ;;
  esac
  if [[ "$tag" != "v${version}" && "$release_state" != published ]]; then
    die "Superseding release ${tag} is not published."
  fi

  tag_target="$(resolve_tag_target "$tag")"
  if [[ "$release_state" == published ]]; then
    [[ -n "$tag_target" ]] || die "Published release ${tag} has no matching tag."
    if ! git merge-base --is-ancestor "$tag_target" "$TARGET_SHA" &&
      ! git merge-base --is-ancestor "$TARGET_SHA" "$tag_target"; then
      die "Published tag ${tag} and ${TARGET_SHA} are unrelated."
    fi
    assert_release_metadata "$tag" '.immutable' true \
      "Published release ${tag} is not immutable." \
      "Published release ${tag}"

    echo "Release ${tag} is already published with the expected metadata."
    {
      echo "published=true"
      echo "publish=false"
      echo "tag=${tag}"
      echo "tag_target=${tag_target}"
    } >>"$GITHUB_OUTPUT"
    return 0
  fi

  if [[ -n "$tag_target" && "$tag_target" != "$TARGET_SHA" ]]; then
    if git merge-base --is-ancestor "$tag_target" "$TARGET_SHA" ||
      git merge-base --is-ancestor "$TARGET_SHA" "$tag_target"; then
      die "Unpublished tag ${tag} targets ${tag_target}, not ${TARGET_SHA}; rerun the Release job for ${tag_target}."
    fi
    die "Unpublished tag ${tag} and ${TARGET_SHA} are unrelated."
  fi

  if [[ -z "$tag_target" ]]; then
    create_tag=true
  else
    create_tag=false
  fi
  {
    echo "create_tag=${create_tag}"
    echo "publish=true"
    echo "tag=${tag}"
  } >>"$GITHUB_OUTPUT"
}

verify_published_release() {
  require_env GITHUB_REPOSITORY
  require_env RELEASE_TAG
  require_env RUNNER_TEMP
  require_env SOURCE_DIGEST

  local assets_dir marker

  assert_release_metadata "$RELEASE_TAG" '.immutable' true \
    "Published release ${RELEASE_TAG} is not immutable." \
    "Published release ${RELEASE_TAG}"
  gh release verify "$RELEASE_TAG" --repo "$GITHUB_REPOSITORY" >/dev/null

  assets_dir="${RUNNER_TEMP}/release-assets"
  verify_asset_attestations "$RELEASE_TAG" "$SOURCE_DIGEST" "$assets_dir"

  mkdir -p .release-provenance
  marker=".release-provenance/${RELEASE_TAG}-${SOURCE_DIGEST}"
  touch "$marker"
}

create_release_tag() {
  require_env GITHUB_REPOSITORY
  require_env RELEASE_TAG
  require_env TARGET_SHA

  gh api --method POST "repos/${GITHUB_REPOSITORY}/git/refs" \
    -f ref="refs/tags/${RELEASE_TAG}" \
    -f sha="$TARGET_SHA" >/dev/null
}

verify_draft_release() {
  require_env GITHUB_REPOSITORY
  require_env RELEASE_TAG
  require_env RUNNER_TEMP
  require_env SOURCE_DIGEST

  local assets_dir

  assert_release_metadata "$RELEASE_TAG" '.draft' true \
    "Release ${RELEASE_TAG} is not a draft." \
    "Draft release ${RELEASE_TAG}"

  assets_dir="${RUNNER_TEMP}/draft-release-assets"
  verify_asset_attestations "$RELEASE_TAG" "$SOURCE_DIGEST" "$assets_dir"
}

usage() {
  echo "Usage: $0 {select|verify-published|create-tag|verify-draft}" >&2
  exit 2
}

case "${1:-}" in
  select) select_release ;;
  verify-published) verify_published_release ;;
  create-tag) create_release_tag ;;
  verify-draft) verify_draft_release ;;
  *) usage ;;
esac
