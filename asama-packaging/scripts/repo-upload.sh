#!/usr/bin/env bash
# Upload one package into an existing package repository and publish that repository.
# The API returns /pulp/api/v3 hrefs; those are joined onto the repository base.
set -euo pipefail

: "${ASAMA_REPO_USERNAME:?ASAMA_REPO_USERNAME is required}"
: "${ASAMA_REPO_PASSWORD:?ASAMA_REPO_PASSWORD is required}"

kind="${1:-}"
repository="${2:-}"
file="${3:-}"
distribution="${4:-}"
component="${5:-}"

if [[ "$kind" != "deb" && "$kind" != "rpm" ]] || [[ -z "$repository" || ! -f "$file" ]]; then
  echo "usage: repo-upload.sh deb|rpm REPOSITORY FILE [DISTRIBUTION COMPONENT]" >&2
  exit 2
fi
if [[ "$kind" == "deb" && ( -z "$distribution" || -z "$component" ) ]]; then
  echo "deb upload requires DISTRIBUTION and COMPONENT" >&2
  exit 2
fi

api="https://console.asama.cloud/pulp-publish/api/v3"
auth=(curl -sS --fail-with-body --connect-timeout 15 --user "${ASAMA_REPO_USERNAME}:${ASAMA_REPO_PASSWORD}")
deb_signing_service="asama-deb-signer"

wait_task() {
  local task_href="$1" task_url body state deadline
  task_url="${api}/${task_href#*/api/v3/}"
  # The publish step is about 80s. Three minutes covers one slow task.
  deadline=$((SECONDS + 180))
  while (( SECONDS < deadline )); do
    body=$("${auth[@]}" --max-time 30 --retry 3 --retry-all-errors "$task_url")
    state=$(printf '%s' "$body" | jq -r '.state // empty')
    case "$state" in
      completed) return 0 ;;
      failed|canceled)
        printf '%s\n' "$body" >&2
        return 1
        ;;
    esac
    sleep 2
  done
  echo "repository task did not finish: ${task_url}" >&2
  return 1
}

if [[ "$kind" == "deb" ]]; then
  repo_list="${api}/repositories/deb/apt/"
  upload_url="${api}/content/deb/packages/"
else
  repo_list="${api}/repositories/rpm/rpm/"
  upload_url="${api}/content/rpm/packages/"
fi

repo_href=$("${auth[@]}" --get --data-urlencode "name=${repository}" "$repo_list" | jq -r '.results[0].pulp_href // empty')
if [[ -z "$repo_href" ]]; then
  echo "repository ${repository} was not found" >&2
  exit 1
fi

form=(-F "file=@${file}" -F "repository=${repo_href}")
if [[ "$kind" == "deb" ]]; then
  form+=(-F "distribution=${distribution}" -F "component=${component}")
fi
task_href=$("${auth[@]}" "${form[@]}" "$upload_url" | jq -er '.task')
wait_task "$task_href"
echo "uploaded ${file} to ${repository}"

if [[ "$kind" == "deb" ]]; then
  sign_href=$("${auth[@]}" --get --data-urlencode "name=${deb_signing_service}" "${api}/signing-services/" | jq -r '.results[0].pulp_href // empty')
  if [[ -z "$sign_href" ]]; then
    echo "signing service ${deb_signing_service} was not found" >&2
    exit 1
  fi
  pub_url="${api}/publications/deb/apt/"
  pub_body=$(jq -n --arg repo "$repo_href" --arg sign "$sign_href" '{repository:$repo, signing_service:$sign}')
else
  pub_url="${api}/publications/rpm/rpm/"
  pub_body=$(jq -n --arg repo "$repo_href" '{repository:$repo}')
fi
task_href=$("${auth[@]}" -H 'Content-Type: application/json' -d "$pub_body" "$pub_url" | jq -er '.task')
wait_task "$task_href"
echo "published ${repository}"
