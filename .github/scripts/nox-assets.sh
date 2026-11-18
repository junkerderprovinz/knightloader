#!/usr/bin/env bash
# Gathers the qbittorrent-nox builds this project makes, with their sources,
# for the KnightLoader release to carry.
#
#   nox-assets.sh <dir>
#
# The Windows and macOS apps fetch qBittorrent from the release of the
# KnightLoader that runs, so every release carries every build the table in
# internal/qbittorrent/nox/versions.json names without a repo of its own, and
# the source archives the GPL asks to go beside them. A file is taken from the
# first of two places that has it with the SHA-256 the table pins: the draft
# release qbittorrent-nox, then the latest release. The qbittorrent-nox
# workflow fills the draft, and builds kept from elsewhere go in by hand:
#
#   gh release create qbittorrent-nox --draft --title "qbittorrent-nox builds" --notes "..."
#   gh release upload qbittorrent-nox <files> --clobber
#
# A file neither has with that digest stops the run. SHA256SUMS lists them all.
set -euo pipefail

out=$1
repo=$GITHUB_REPOSITORY
store=qbittorrent-nox
table=internal/qbittorrent/nox/versions.json
tmp="${RUNNER_TEMP:-/tmp}/nox-asset"

mkdir -p "$out"
: > "$out/SHA256SUMS"

take() {
  local name=$1 sum=$2 from
  for from in "$store" ""; do
    rm -rf "$tmp" && mkdir -p "$tmp"
    local at=()
    [ -n "$from" ] && at=("$from")
    gh release download "${at[@]}" --repo "$repo" --pattern "$name" --dir "$tmp" </dev/null 2>/dev/null || continue
    if [ "$(sha256sum "$tmp/$name" | cut -d' ' -f1)" = "$sum" ]; then
      mv "$tmp/$name" "$out/$name"
      echo "$sum  $name" >> "$out/SHA256SUMS"
      echo "$name from ${from:-the latest release}"
      return 0
    fi
    echo "::warning::$name in ${from:-the latest release} is not the build versions.json pins"
  done
  echo "::error::neither the qbittorrent-nox draft nor the latest release holds $name with the SHA-256 versions.json pins, $sum" >&2
  return 1
}

# Two versions built against the same libtorrent share its source archive.
jq -r '.versions[] | ((.assets[] | select(.repo == null)), .sources[]) | "\(.name) \(.sha256)"' "$table" |
  sort -u > "$tmp.list"
[ -s "$tmp.list" ] || { echo "::error::$table names no build of this project's own" >&2; exit 1; }
while read -r name sum; do
  take "$name" "$sum"
done < "$tmp.list"
cat "$out/SHA256SUMS"
