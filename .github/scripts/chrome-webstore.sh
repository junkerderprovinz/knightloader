#!/usr/bin/env bash
# Uploads a package to the Chrome Web Store and submits it for review.
#
#   chrome-webstore.sh <zip>
#
# Needs CHROME_TOKEN (an access token for the chromewebstore scope),
# CHROME_PUBLISHER_ID and CHROME_ITEM_ID. The item itself is created once by
# hand in the developer dashboard; the API only takes new versions of it.
set -euo pipefail

zip=$1
item="publishers/$CHROME_PUBLISHER_ID/items/$CHROME_ITEM_ID"
api=https://chromewebstore.googleapis.com

# Prints the store's answer either way, since on a refusal it says why.
call() {
  local answer
  if ! answer=$(curl --fail-with-body -sS -H "Authorization: Bearer $CHROME_TOKEN" "$@"); then
    echo "$answer" >&2
    exit 1
  fi
  echo "$answer"
}

answer=$(call -X POST -T "$zip" "$api/upload/v2/$item:upload")
echo "$answer"
state=$(jq -r .uploadState <<< "$answer")

# A large package is processed after the upload returns.
for _ in $(seq 30); do
  [ "$state" = IN_PROGRESS ] || break
  sleep 10
  state=$(call "$api/v2/$item:fetchStatus" | jq -r .lastAsyncUploadState)
done
if [ "$state" != SUCCEEDED ]; then
  echo "::error::the Chrome Web Store did not take the package: $state."
  exit 1
fi

call -X POST "$api/v2/$item:publish"
