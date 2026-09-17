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
# WHY A STANDING RELEASE. GitHub serves a download link that survives the next
# release only as /releases/latest/download/<name>, and "latest" is the
# product's own release, which a surface release deliberately never is (see
# make_latest in the two workflows that call this).
#
# ONLY THE NEWEST TAG PUBLISHES, decided here at the end of the run and from
# tags fetched now. A build of an older tag that finishes after a newer one must
# not put the older file behind the button, and a tag list read at checkout,
# half an hour earlier, cannot know about the newer tag. Only plain X.Y.Z tags
# count, so a pre-release can neither take the button nor, sorted above its own
# final release, keep it.
#
# THE STANDING TAG IS CREATED ONCE AND NEVER MOVED. Moving it would be a ref
# update made with GITHUB_TOKEN, which GitHub refuses when the workflow files
# differ between the two commits. The file is what the button needs, and the
# title and notes say which version it is.
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
