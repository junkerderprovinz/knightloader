# Debrid drive

The debrid drive shows what is on your debrid accounts as a read-only network
drive. Mount it with rclone, point Plex or Jellyfin at the mount, and they
stream straight from the service. Nothing is downloaded to your disk first.

TorBox, Real-Debrid, AllDebrid, Premiumize.me and Debrid-Link can be shown,
the same services whose downloads the [import](getting-links-in.md#from-your-debrid-account)
can read. The other services keep no list of your downloads, so they have
nothing to show.

## What it looks like

The drive answers WebDAV at `/dav/` on the address you open KnightLoader on,
for example `http://192.168.1.10:8080/dav/`. Behind a reverse proxy that mounts
KnightLoader under a path, the path comes first: `https://example.com/kl/dav/`.

A proxy that strips the path before it passes the request on has to name the
path in `X-Forwarded-Prefix`, as Traefik's StripPrefix does. Without it the
drive cannot tell that request from one that reached it directly, and answers
as it would at `/dav/`.

```text
dav/
  TorBox/
    Some Movie (2024)/
      Some.Movie.2024.1080p.mkv
    Some Show S01/
      Season 1/
        Some.Show.S01E01.mkv
  Real-Debrid/
    ...
  Real-Debrid (work)/
    ...
```

Each account is a folder named after its service, with the account's id in
brackets for a second account at the same service. A slash or backslash in the
id is written as `%2F` or `%5C`, and a percent sign as `%25`. That way every
account gets its own folder, and the folder keeps its name when another
account is added or removed. Each download on the
account is a folder named after the download, holding its files. When two
downloads share a name, the older one keeps it and the newer one has the
service's id added in square brackets, so a path never moves when something
new arrives. A download the service is still fetching shows as an empty folder
until its files are there.

## Switching it on

1. Open **Settings → Accounts**. The **Debrid drive** card is below the
   accounts. Switch it on there or on the Modules page; it is one switch.
2. Create an API token that can read under **Settings → Security → API
   tokens**. **Read only** is all the drive needs, and it is the right preset
   for it: the token goes into a config file on another machine.
3. Copy the rclone configuration from the card and put the token in it.

While the switch is off, `/dav/` answers 404 as if it did not exist. While it
is on, every request needs a token that can read, also on an instance with no
password. rclone sends the token as `bearer_token`. A WebDAV client that can
only send a user name and a password, such as a file manager or a player, uses
the token as the password; the user name can be anything. The login password
is not accepted here, because it would sit in a config file on another machine
and would get past a second factor.

## rclone

The card on the Accounts page shows the configuration with your address filled
in:

```ini
[knightloader]
type = webdav
url = http://192.168.1.10:8080/dav/
vendor = other
bearer_token = your-read-token
```

Add it to `rclone.conf`; `rclone config file` prints where that file is. Then
check that rclone sees the accounts:

```sh
rclone lsd knightloader:
```

and mount the drive:

```sh
rclone mount knightloader: /mnt/debrid --read-only --allow-other \
  --dir-cache-time 5m --vfs-cache-mode full --vfs-cache-max-size 20G
```

`--allow-other` lets Plex or Jellyfin, which run as another user, read the
mount. `--vfs-cache-mode full` keeps what was read on disk up to the size you
give it, so a player that jumps back does not fetch the same part again. The
mount works without it as well: rclone then asks for each part as the player
reads it.

### On Unraid

1. Install the **rclone** plugin from Community Applications.
2. Open the Unraid terminal, run `rclone config file` to see where the
   configuration lives, and paste the section above into that file.
3. Mount the drive from a script in the **User Scripts** plugin that runs at
   array start, with the `rclone mount` line above and a mount point such as
   `/mnt/remotes/debrid`. Add `--daemon` so the script returns.
4. In the Plex or Jellyfin template, add a path that maps
   `/mnt/remotes/debrid` into the container, and set its access mode to
   **Read Only - Slave**. Without the slave mode the container keeps seeing
   the empty folder that was there before the mount.
5. Add a library in Plex or Jellyfin that points at that path, or at one of
   the account folders in it.

### With Docker

rclone runs in a container of its own, which needs FUSE and has to share its
mount with the host:

```yaml
services:
  rclone:
    image: rclone/rclone
    command: >
      mount knightloader: /data --read-only --allow-other
      --dir-cache-time 5m --vfs-cache-mode full --vfs-cache-max-size 20G
    volumes:
      - ./rclone:/config/rclone
      - /mnt/debrid:/data:rshared
    devices:
      - /dev/fuse
    cap_add:
      - SYS_ADMIN
    security_opt:
      - apparmor:unconfined
    restart: unless-stopped
```

`rclone.conf` goes into `./rclone`. Plex or Jellyfin then mounts
`/mnt/debrid` with `:rslave` on the end of the volume, for the same reason as
the slave mode on Unraid.

## How it behaves

A listing read from a service is kept for the refresh interval on the card,
five minutes unless you change it, and every account's API is asked at most a
few times a second, well inside every service's limit. A download that is
complete on the service is read once and then kept while it stays on the
account, since its files do not change. If a service cannot be reached, the
drive keeps showing the last listing it read.

A file is unlocked on the service only when something reads it, never when a
folder is listed. The unlocked link is used for an hour. A link the service's
download server refuses before then is unlocked again once. Every read asks
the download server for the part it needs, so seeking in a film fetches from
the new position instead of from the start.

When the download server cannot deliver a file, the drive answers 502 with the
account and the reason, and writes the same reason to the log with the file's
path. Neither contains the link, since an unlocked link is a credential in
itself.

A browser that opens a file on the drive gets it as a download and does not
show it as a page. Otherwise a web page or SVG picture on one of your accounts
could run its script on KnightLoader's address, where you may be logged in.

## Limits

- The drive is read-only. Deleting or renaming a file is refused; delete a
  download on the service's website.
- Real-Debrid lists the newest 100 torrents, so older ones do not show.
  TorBox lists up to 1000 of each kind of download.
- Real-Debrid sometimes packs the chosen files of a torrent into fewer
  archives. Those archives have no name until they are unlocked, so the drive
  leaves them out.
- Unlocking a Real-Debrid file adds it to the Downloads list on Real-Debrid's
  website, as any unlock does.
- A media server that reads files to build previews, detect intros or analyse
  audio streams the whole file to do it. Switch those jobs off for a library
  on the drive if your service counts traffic.
- A WebDAV client that asks for a whole tree at once (`Depth: infinity`) is
  refused, since that would read every download on every account. rclone and
  the common file managers list one folder at a time.
