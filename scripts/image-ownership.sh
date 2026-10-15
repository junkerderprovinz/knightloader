#!/usr/bin/env bash
# Starts the image the way a first `docker run` does and checks which account
# the server ends up running as and which folders it gave to that account
# (docs/decisions.md, "Ownership"). It runs as root, because the folders it
# sets up and inspects belong to root:
#
#   sudo scripts/image-ownership.sh knightloader:ci
set -euo pipefail

image=${1:?usage: image-ownership.sh <image>}
work=$(mktemp -d)
names=()

cleanup() {
  for n in "${names[@]}"; do docker rm -f "$n" >/dev/null 2>&1 || true; done
  rm -rf "$work"
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# start runs one container and leaves its name in c. It is not called in a
# command substitution, whose subshell would lose the entry in names and leave
# the container running after cleanup. JDownloader provisioning is off because
# it downloads for minutes and has nothing to do with ownership.
start() {
  c=kl-owner-$1-$$
  shift
  names+=("$c")
  docker run -d --name "$c" --cpus=2 --memory=2g --health-interval=2s \
    -e KL_PROVISION_JD=0 "$@" "$image" >/dev/null
}

healthy() {
  for _ in $(seq 60); do
    [ "$(docker inspect -f '{{.State.Health.Status}}' "$1")" = healthy ] && return 0
    [ "$(docker inspect -f '{{.State.Running}}' "$1")" = true ] || break
    sleep 2
  done
  docker logs "$1" >&2
  fail "$1 did not become healthy"
}

stopped() {
  for _ in $(seq 30); do
    [ "$(docker inspect -f '{{.State.Running}}' "$1")" = false ] && return 0
    sleep 1
  done
  fail "$1 is still running on a data directory it cannot write"
}

# expect_ids compares the real, effective, saved and file-system ids of the
# server process, and its supplementary groups, with the account given.
expect_ids() {
  local name=$1 uid=$2 gid=$3 groups=$4 status got
  status=$(docker exec "$name" sh -c 'cat /proc/$(pidof knightloader)/status')
  got=$(awk '/^Uid:/ {print $2, $3, $4, $5}' <<<"$status")
  [ "$got" = "$uid $uid $uid $uid" ] || fail "$name runs with Uid $got, want $uid"
  got=$(awk '/^Gid:/ {print $2, $3, $4, $5}' <<<"$status")
  [ "$got" = "$gid $gid $gid $gid" ] || fail "$name runs with Gid $got, want $gid"
  got=$(awk '/^Groups:/ {$1 = ""; print}' <<<"$status" | xargs)
  [ "$got" = "$groups" ] || fail "$name keeps the supplementary groups '$got', want '$groups'"
}

expect_owner() {
  local got
  got=$(stat -c %u:%g "$1")
  [ "$got" = "$2" ] || fail "$1 belongs to $got, want $2"
}

expect_log() {
  docker logs "$1" 2>&1 | grep -qF -- "$2" || {
    docker logs "$1" >&2
    fail "$1 never logged: $2"
  }
}

# A first start: Docker creates both bind-mount sources as root:root.
start fresh -v "$work/fresh/data:/data" -v "$work/fresh/dl:/data/downloads"
healthy "$c"
expect_ids "$c" 1000 1000 ""
expect_owner "$work/fresh/data" 1000:1000
expect_owner "$work/fresh/dl" 1000:1000
expect_log "$c" "started as root, gave /data, /data/downloads to 1000:1000, now running as 1000:1000 (knight)"
echo "ok: a fresh start runs as 1000:1000 and owns its new folders"

start puid -e PUID=1234 -e PGID=2345 -v "$work/puid/data:/data" -v "$work/puid/dl:/data/downloads"
healthy "$c"
expect_ids "$c" 1234 2345 ""
expect_owner "$work/puid/data" 1234:2345
expect_owner "$work/puid/dl" 1234:2345
echo "ok: PUID=1234 PGID=2345 runs as 1234:2345 and owns its new folders"

# Unraid's template: --user, with folders that already belong to that account.
# PUID is set to show it is not read.
mkdir -p "$work/user/data" "$work/user/dl"
chown 99:100 "$work/user/data" "$work/user/dl"
start user --user 99:100 -e PUID=1234 -v "$work/user/data:/data" -v "$work/user/dl:/data/downloads"
healthy "$c"
expect_ids "$c" 99 100 100
expect_owner "$work/user/data" 99:100
if docker logs "$c" 2>&1 | grep -q "started as root"; then
  fail "$c was started with --user and still reports a switch from root"
fi
echo "ok: --user 99:100 runs as 99:100, reads no PUID and changes no owner"

# A download share with something in it keeps its owner, while the fresh data
# directory beside it is handed over.
mkdir -p "$work/share/dl"
touch "$work/share/dl/film.mkv"
start share -v "$work/share/data:/data" -v "$work/share/dl:/data/downloads"
healthy "$c"
expect_owner "$work/share/data" 1000:1000
expect_owner "$work/share/dl" 0:0
expect_owner "$work/share/dl/film.mkv" 0:0
echo "ok: a download folder with a file in it keeps root as its owner"

# A data directory with something in it is not chowned. The server cannot
# write it, says how to fix it and stops.
mkdir -p "$work/used/data"
touch "$work/used/data/keep"
start used -v "$work/used/data:/data"
stopped "$c"
expect_owner "$work/used/data" 0:0
expect_owner "$work/used/data/keep" 0:0
expect_log "$c" "the data directory /data belongs to 0:0 (root:root) and this process runs as 1000:1000 (knight:knight)"
expect_log "$c" "chown -R 1000:1000 /data"
echo "ok: a data directory with a file in it keeps root as its owner and gets the chown line"
