#!/usr/bin/env bash
# Puts the newest build of the app or the browser extension into a standing
# release, under a file name without a version, for the README's download
# buttons.
#
#   publish-newest.sh <surface> <file> <asset name> <product>
#
#   surface     mobile or extension: the tag prefix. The standing release is
#               <surface>/latest.
#   file        the build this tag produced
#   asset name  the fixed name it is published under
#   product     as the release titles name it: App, Browser Extension
#
# A standing release is needed because a stable download link exists only as
# /releases/latest/download/<name>, and "latest" belongs to the main vX.Y.Z release;
# surface releases are never marked latest (make_latest in the calling
# workflows).
#
# Only the newest tag publishes, judged from tags fetched at the end of the run,
# so an older build finishing late cannot put its file behind the button. Only
# plain X.Y.Z tags count, so a pre-release can neither take the button nor keep
# it.
#
# The standing tag is created once and never moved: moving it with GITHUB_TOKEN
# is refused when the workflow files differ between the commits. The title and
# notes say which version the file is.
set -euo pipefail

surface=$1
file=$2
asset=$3
product=$4

standing="$surface/latest"
version=${GITHUB_REF_NAME#"$surface"/v}
notes=".github/release-notes/$GITHUB_REF_NAME.md"
title="$product $version, newest"

git fetch --tags --force --quiet
newest=$(git tag -l "$surface/v*" | grep -E "^$surface/v[0-9]+[.][0-9]+[.][0-9]+$" | sort -V | tail -1)
if [ "$newest" != "$GITHUB_REF_NAME" ]; then
  echo "$GITHUB_REF_NAME is not the newest $surface tag ($newest), so $standing stays as it is"
  exit 0
fi

cp "$file" "$asset"
if ! gh release view "$standing" --repo "$GITHUB_REPOSITORY" >/dev/null 2>&1; then
  gh release create "$standing" --repo "$GITHUB_REPOSITORY" --target "$GITHUB_SHA" \
    --title "$title" --notes-file "$notes" --latest=false
fi
# The file first and the words after it, so the notes never name a version
# whose file is not there yet.
gh release upload "$standing" "$asset" --repo "$GITHUB_REPOSITORY" --clobber
gh release edit "$standing" --repo "$GITHUB_REPOSITORY" \
  --title "$title" --notes-file "$notes" --latest=false
echo "$standing now carries $asset from $GITHUB_REF_NAME"
