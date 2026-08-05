# Running the preview instance

KnightLoader ships as a single static binary with the UI embedded, so a
container is just that binary plus the two tools its media path shells out to
(`yt-dlp`, `ffmpeg`). The repository is private, so the image is built where it
runs instead of being pulled from a registry.

## Deploy (or redeploy after a change)

From a machine that can reach the server over SSH:

```sh
# 1. package the working tree (dist is committed, so no Node step is needed)
tar czf /tmp/kl-src.tgz -C /path/to/knightloader \
    --exclude=.git --exclude=node_modules --exclude=bin .

# 2. ship it
scp -P <ssh-port> /tmp/kl-src.tgz root@<host>:/tmp/kl-src.tgz

# 3. build and (re)start — data and downloads survive, they live on volumes
ssh -p <ssh-port> root@<host> '
  rm -rf /tmp/klbuild && mkdir -p /tmp/klbuild &&
  tar xzf /tmp/kl-src.tgz -C /tmp/klbuild &&
  cd /tmp/klbuild &&
  docker build --build-arg VERSION=preview -t knightloader:preview . &&
  docker rm -f knightloader;
  docker run -d --name knightloader \
    --restart unless-stopped \
    --user 99:100 \
    --cpus 2 --memory 1g \
    -p 8749:8749 \
    -v /mnt/user/appdata/knightloader:/data \
    -v /mnt/user/downloads/knightloader:/data/downloads \
    -e TZ=Europe/Berlin \
    knightloader:preview'
```

`--user 99:100` makes downloaded files land as `nobody:users`, which is what the
rest of an Unraid box expects. `VERSION` is stamped into the binary and shown
under the wordmark in the sidebar.

## Where things live

| Path | Holds |
|---|---|
| `/mnt/user/appdata/knightloader` | SQLite database, settings, the encrypted account store, the instance list |
| `/mnt/user/downloads/knightloader` | finished downloads (mounted at `/data/downloads`) |

## Environment

Everything from the README applies. Two notes specific to the container:

- `KL_CNL=0` by default — Click'n'Load binds `127.0.0.1`, which inside a
  container is not the browser's localhost. To use it, run with
  `-e KL_CNL=9666 -p 9666:9666` and `--network host` (or publish it and point
  the extension at the server).
- `KL_TORBOX` / `KL_ALLDEBRID` / `KL_REALDEBRID` are optional: keys entered on
  the Accounts page are stored encrypted in the data volume and survive
  restarts, so the environment does not need them.

## Health

`GET /api/health` returns `{"status":"ok","version":"…"}`; the image's
HEALTHCHECK uses it, so `docker ps` shows `healthy` once the server is up.
