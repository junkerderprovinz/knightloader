#!/usr/bin/env bash
# Prints an access token for one Google API scope.
#
#   google-token.sh <scope>
#
# Needs GOOGLE_KEY, a service account key in JSON. The key signs the token
# request itself, so the account needs no role for minting tokens and the IAM
# Credentials API can stay switched off.
set -euo pipefail

scope=$1
b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }

email=$(jq -r .client_email <<< "$GOOGLE_KEY")
uri=$(jq -r .token_uri <<< "$GOOGLE_KEY")
now=$(date +%s)
header=$(printf '{"alg":"RS256","typ":"JWT"}' | b64url)
claims=$(jq -cn --arg iss "$email" --arg scope "$scope" --arg aud "$uri" --argjson iat "$now" \
  '{iss: $iss, scope: $scope, aud: $aud, iat: $iat, exp: ($iat + 600)}' | b64url)
key=$(mktemp)
trap 'rm -f "$key"' EXIT
jq -r .private_key <<< "$GOOGLE_KEY" > "$key"
signature=$(printf '%s.%s' "$header" "$claims" | openssl dgst -sha256 -sign "$key" | b64url)

curl --fail-with-body -sS "$uri" \
  --data-urlencode grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer \
  --data-urlencode "assertion=$header.$claims.$signature" | jq -r .access_token
