#!/usr/bin/env bash
# Uploads a package to Microsoft Edge Add-ons and submits it for review.
#
#   edge-addons.sh <zip> <notes for certification>
#
# Needs EDGE_CLIENT_ID and EDGE_API_KEY (Partner Center, Microsoft Edge,
# Publish API) and EDGE_PRODUCT_ID. The product itself is created once by hand
# in Partner Center; the API only takes new versions of it.
set -euo pipefail

zip=$1
notes=$2
api="https://api.addons.microsoftedge.microsoft.com/v1/products/$EDGE_PRODUCT_ID"
auth=(-H "Authorization: ApiKey $EDGE_API_KEY" -H "X-ClientID: $EDGE_CLIENT_ID")

# Both steps answer 202 and name an operation in the Location header, which
# is then polled until it leaves InProgress.
start() {
  local body headers location
  body=$(mktemp)
  if ! headers=$(curl --fail-with-body -sS -o "$body" -D - "${auth[@]}" "$@"); then
    cat "$body" >&2
    exit 1
  fi
  location=$(tr -d '\r' <<< "$headers" | awk 'tolower($1) == "location:" { print $2 }')
  if [ -z "$location" ]; then
    echo "::error::Edge Add-ons named no operation to follow." >&2
    exit 1
  fi
  echo "${location##*/}"
}

follow() {
  local url=$1 what=$2 answer status
  for _ in $(seq 60); do
    answer=$(curl --fail-with-body -sS "${auth[@]}" "$url")
    status=$(jq -r .status <<< "$answer")
    [ "$status" = InProgress ] || break
    sleep 10
  done
  echo "$answer"
  if [ "$status" != Succeeded ]; then
    echo "::error::Edge Add-ons: $what ended as $status."
    exit 1
  fi
}

op=$(start -H "Content-Type: application/zip" -X POST -T "$zip" "$api/submissions/draft/package")
follow "$api/submissions/draft/package/operations/$op" "the upload"

op=$(start -H "Content-Type: application/json" -X POST \
  -d "$(jq -n --arg notes "$notes" '{notes: $notes}')" "$api/submissions")
follow "$api/submissions/operations/$op" "the submission"
