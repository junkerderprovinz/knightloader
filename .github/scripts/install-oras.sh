#!/usr/bin/env bash
# Puts oras on the PATH. It reads and writes the knightloader-nox package on
# GHCR, which keeps the qbittorrent-nox builds out of every release list.
set -euo pipefail

version=1.3.6
sum=66fb0ad7206f3f6fa77ed5b552049d364ccf4b50b1684554f731f76302b52ca0
dir="${RUNNER_TEMP:-/tmp}/oras"

mkdir -p "$dir"
curl -fsSL -o "$dir/oras.tar.gz" \
  "https://github.com/oras-project/oras/releases/download/v$version/oras_${version}_linux_amd64.tar.gz"
echo "$sum  $dir/oras.tar.gz" | sha256sum -c -
tar -xzf "$dir/oras.tar.gz" -C "$dir" oras
echo "$dir" >> "$GITHUB_PATH"
"$dir/oras" version
