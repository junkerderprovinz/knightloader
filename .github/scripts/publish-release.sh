#!/usr/bin/env bash
# Creates the GitHub release for the pushed tag, with every file at once.
#
#   publish-release.sh <file>...
#
# Called by release.yml's last job, after every build it needs is done. gh
# creates the release as a draft, uploads the files, and only then publishes
# it, so a release is never public without its downloads. If an upload or the
# publish fails, gh removes its own draft.
#
# BEFORE THAT, THREE CHECKS, each of which stops the run rather than guessing:
#
# - The tag still points at the commit that was built. GitHub ignores the
#   target of a release whose tag exists, so a tag moved to another commit
#   while this ran would get a release carrying the old binaries.
# - No published release exists for the tag. Re-cutting a published version is
#   a deliberate act: delete that release first. A DRAFT for the tag is what a
#   failed earlier attempt leaves; it is private and its files belong to that
#   attempt, so it is removed.
# - The list of published releases could be read, because "latest" depends on
#   it.
#
# "LATEST" only when no published release has a newer plain vX.Y.Z tag, so
# re-cutting an older version does not pull the badge and the download buttons
# (and in KnightLoader the in-app update) back to it. Counted over published
# releases rather than tags: a newer tag whose release never came out must not
# keep this one from being the newest that exists.
set -euo pipefail

tag=$GITHUB_REF_NAME
repo=$GITHUB_REPOSITORY

if [ "$#" -eq 0 ]; then
  echo "::error::no files to publish with $tag" >&2
  exit 1
fi

tagged=$(gh api "repos/$repo/commits/$tag" -q .sha)
if [ "$tagged" != "$GITHUB_SHA" ]; then
  echo "::error::$tag now points at $tagged, but this run built $GITHUB_SHA. The tag moved; the run for its new commit publishes it." >&2
  exit 1
fi

existing=$(gh api --paginate "repos/$repo/releases?per_page=100" \
  -q '.[] | select(.tag_name == env.GITHUB_REF_NAME) | "\(.id) \(.draft)"')
if printf '%s\n' "$existing" | grep -q ' false$'; then
  echo "::error::$tag is already published. To re-cut it, delete that release first." >&2
  exit 1
fi
printf '%s\n' "$existing" | while read -r id _draft; do
  [ -n "$id" ] || continue
  gh api -X DELETE "repos/$repo/releases/$id"
  echo "removed the draft $id an earlier attempt left"
done

published=$(gh release list --repo "$repo" --exclude-drafts --limit 1000 --json tagName -q '.[].tagName')
newest=$(printf '%s\n%s\n' "$published" "$tag" | grep -E '^v[0-9]+[.][0-9]+[.][0-9]+$' | sort -V | tail -1)
latest=false
if [ "$newest" = "$tag" ]; then
  latest=true
fi

# The title is the version alone: the repository name is already above it.
gh release create "$tag" \
  --repo "$repo" \
  --verify-tag \
  --title "$tag" \
  --notes-file ".github/release-notes/$tag.md" \
  --latest="$latest" \
  "$@"
echo "published $tag with $# files, latest=$latest"
