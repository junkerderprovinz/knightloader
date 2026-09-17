#!/usr/bin/env bash
# Publishes this tag's draft release once every file the README's download
# buttons point at is attached.
#
#   publish-when-complete.sh [<asset name>...]
#
# Without names it reads release-assets.txt beside it: one name per line,
# {tag} standing for the tag, # starting a comment. One list, however many
# jobs attach files.
#
# WHY A DRAFT. The download buttons point at /releases/latest/download/<name>,
# and a release becomes "latest" the moment it is published. release.yml used to
# publish it straight away, while the builds that attach its files take twenty
# minutes and more, so every release left the buttons answering 404 for that
# long (measured on v1.1.5). A draft is not "latest", so the previous release
# keeps serving the buttons until this one has everything.
#
# EVERY JOB THAT ATTACHES FILES CALLS THIS, after its own upload. Whichever
# upload finishes last sees all the files and publishes; an earlier caller finds
# something missing and leaves the draft alone. Only uploads that have finished
# count, so a file still on its way cannot publish the release early.
#
# "LATEST" ONLY FOR THE NEWEST PLAIN vX.Y.Z TAG, so re-cutting an older version
# publishes it without taking the badge, and the buttons, back to it.
#
# A JOB THAT FAILS LEAVES THE RELEASE A DRAFT. Nothing half-attached ever becomes
# the latest release; re-running the failed job attaches and publishes.
set -euo pipefail

tag=$GITHUB_REF_NAME
repo=$GITHUB_REPOSITORY

if [ "$#" -eq 0 ]; then
  list="$(dirname "$0")/release-assets.txt"
  # shellcheck disable=SC2046 # one name per word is the point
  set -- $(sed -e 's/#.*//' -e "s/{tag}/$tag/g" "$list")
  if [ "$#" -eq 0 ]; then
    echo "$list names no files, so nothing says when $tag is complete" >&2
    exit 1
  fi
fi

draft=$(gh release view "$tag" --repo "$repo" --json isDraft -q '.isDraft')
if [ "$draft" != "true" ]; then
  echo "$tag is already published"
  exit 0
fi

have=" $(gh release view "$tag" --repo "$repo" --json assets \
  -q '[.assets[] | select(.state == "uploaded") | .name] | join(" ")') "
missing=""
for name in "$@"; do
  case "$have" in
    *" $name "*) ;;
    *) missing="$missing $name" ;;
  esac
done
if [ -n "$missing" ]; then
  echo "$tag stays a draft, still waiting for:$missing"
  exit 0
fi

newest=$(gh api "repos/$repo/git/matching-refs/tags/v" --paginate -q '.[].ref' \
  | sed 's|^refs/tags/||' | grep -E '^v[0-9]+[.][0-9]+[.][0-9]+$' | sort -V | tail -1)
latest=false
if [ "$newest" = "$tag" ]; then
  latest=true
fi
gh release edit "$tag" --repo "$repo" --draft=false --latest="$latest"
echo "$tag published, latest=$latest"
