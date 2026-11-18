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
# first of two places that has it with the SHA-256 the table pins: the
# knightloader-nox package on GHCR, one tag per file name, then the latest
# release. The qbittorrent-nox workflow fills the package, and a build kept
# from elsewhere goes in by hand, from the folder that holds it:
#
#   oras push ghcr.io/junkerderprovinz/knightloader-nox:<file> <file>
#
# A file neither has with that digest stops the run. SHA256SUMS lists them all.
# Needs oras on the PATH (install-oras.sh) and GH_TOKEN with packages: read.
set -euo pipefail

out=$1
repo=$GITHUB_REPOSITORY
store=ghcr.io/${GITHUB_REPOSITORY_OWNER:-junkerderprovinz}/knightloader-nox
table=internal/qbittorrent/nox/versions.json
tmp="${RUNNER_TEMP:-/tmp}/nox-asset"

mkdir -p "$out"
: > "$out/SHA256SUMS"
echo "$GH_TOKEN" | oras login ghcr.io -u "${GITHUB_ACTOR:-junkerderprovinz}" --password-stdin >/dev/null

take() {
  local name=$1 sum=$2 from
  for from in "$store" ""; do
    rm -rf "$tmp" && mkdir -p "$tmp"
    if [ -n "$from" ]; then
      oras pull "$from:$name" -o "$tmp" </dev/null >/dev/null 2>&1 || continue
    else
      gh release download --repo "$repo" --pattern "$name" --dir "$tmp" </dev/null 2>/dev/null || continue
    fi
    [ -f "$tmp/$name" ] || continue
    if [ "$(sha256sum "$tmp/$name" | cut -d' ' -f1)" = "$sum" ]; then
      mv "$tmp/$name" "$out/$name"
      echo "$sum  $name" >> "$out/SHA256SUMS"
      echo "$name from ${from:-the latest release}"
      return 0
    fi
    echo "::warning::$name in ${from:-the latest release} is not the build versions.json pins"
  done
  echo "::error::neither $store nor the latest release holds $name with the SHA-256 versions.json pins, $sum" >&2
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
