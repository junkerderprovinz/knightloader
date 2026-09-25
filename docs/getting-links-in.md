# Getting links in

Pasting works, and so does dropping text onto the collector. Beyond that:

- **[Click'n'Load](clicknload.md)**: a site's own CnL button hands its links
  straight over. Every site addresses `127.0.0.1`, and a container's loopback is
  not the browser's, so when KnightLoader runs on a NAS there are two ways to
  bridge that gap: the [browser extension](browser-tools.md), which catches the
  submission in the page itself and routes it through the relay to whichever
  instance you pick, or the same binary run as a bridge on your desktop
  (`knightloader -bridge http://nas:8749`) for a browser with no extension in it.
- **Watched folder**: drop a `.txt` or a JDownloader `.crawljob` onto a share and
  the box picks it up, with its package name, destination and archive password.
  Point Settings at the folder to switch it on.
- **A page**: paste one, and the files it links to are staged instead.
- **A container file**: upload a `.txt`, `.dlc`, `.ccf` or `.rsdf`. A link list is
  read on the spot. The encrypted formats cannot be opened by anyone offline,
  because their key is issued to registered clients. They are handed to the
  JDownloader backend, which has one. That backend is provisioned on first run
  by default (`KL_PROVISION_JD`), so this normally works with nothing set. With
  no backend at all, a container is recognised and refused, with the missing
  backend named as the reason.
- **Your own server**: see below.
- **Sonarr and Radarr**: they hand their grabs over as if KnightLoader were
  qBittorrent or SABnzbd. See below.

## Torrents

Paste a magnet link like any other link, or drop a `.torrent` file onto the
collector. A `.torrent` with more than one file opens its file list first, so
you can untick what you do not want.

**File selection** on the Torrents page picks the files when nobody has picked
them by hand: a minimum size, and regular expressions for files to fetch and
files to skip. They are matched against a file's path inside the torrent, the
way "matches pattern" works in a Packagizer rule, so `(?i)sample` catches a
sample folder as well as a sample file. The choice is made when a torrent
starts. For a magnet that is once the swarm has sent its file list; a
`.torrent` opens its file list with the choice already ticked. Whatever you
tick yourself wins, and if the selection would leave nothing, every file is
fetched. A category can have a file selection of its own instead, for example
a music category where the small files are the album. It applies to every
torrent filed there, whether you picked the category or a Packagizer rule did.

**Extra trackers** help a torrent with few peers. Type addresses in, or give
the address of a public list such as
[ngosang/trackerslist](https://github.com/ngosang/trackerslist), which is
fetched at most once a day. If a fetch fails, the last good list stays in use.
A `.torrent` marked private never gets them. A magnet link cannot say it is
private before its metadata arrives, and by then its trackers are set, so a
magnet whose own tracker address carries a passkey counts as private too. A
private tracker that knows its members by their IP address instead of a
passkey cannot be told apart this way: leave the extra trackers empty if you
take magnet links from one.

**Banned trackers** keep a torrent out. One that announces to a banned host is
held back with the reason, next to the links the link filter holds, and a ban
added later still stops it from starting. Restoring it lets it past the line
that caught it, not past one added afterwards. It is refused rather than
stripped of that tracker: the rest of the torrent would still announce the same
info hash, and a private torrent without its tracker finds no peers.

## Own servers (FTP, SFTP, WebDAV)

A seedbox, a NAS or your own Nextcloud is a source like any other. Paste
`ftp://`, `ftps://`, `sftp://`, `webdav://` or `webdavs://` and the file is
staged, named and sized before it starts.

**Credentials live in Accounts, never in the link.** Add an account with the
service *Own server (FTP, SFTP, WebDAV)* and give it the **hostname** as its
account name, for example `seedbox.example.net`. That name is what a pasted link
is looked up by, so a login stored under anything else is never found. A password
written into a URL is refused rather than quietly stripped, because it would be
saved to the task list in plain text.

A plain `https://` link is claimed as WebDAV only when an account exists for that
exact host, so no ordinary download is ever taken over. Public FTP archives need
no account at all.

A link to a **folder** stages one task per file inside it, subfolders included,
the way a torrent's file list does. Paused downloads continue where they stopped:
FTP restarts at an offset with `REST`, SFTP reads at an offset directly, and
WebDAV uses HTTP byte ranges. A server that cannot do it says so and the download
fails loudly instead of quietly writing a corrupt file.

The first time an SFTP server is seen, its host key is written to
`known_hosts` in the data directory and has to match on every connection after
that, the same rule `ssh` follows once you have answered its prompt.

## Sonarr and Radarr

Sonarr, Radarr and Prowlarr can use KnightLoader as a download client. It speaks
two protocols they already know: qBittorrent's for torrents, and SABnzbd's for
what a Usenet or DDL indexer hands over. One switch opens both, **Download client
for Sonarr and Radarr**, on the Remote access page or the Modules page. It is off
on a fresh install.

Both need an API token of this instance that can add and read, which you
create with the **Add and read** preset on the Remote access page (see
[API tokens and their rights](connecting.md#api-tokens-and-their-rights)). With
a token for each app you can revoke one without cutting off the others. Also
switch on **Put each package in its own subfolder** under Settings >
Downloads. Without it every grab lands in the same folder, and the importer
cannot tell one release from the next.

### Torrents through qBittorrent's API

In Sonarr or Radarr, open Settings > Download Clients, add qBittorrent and fill
it in like this:

| Field | Value |
|---|---|
| Host and Port | KnightLoader's address and port, 8749 by default |
| URL Base | `api/qbittorrent` |
| Username | any name, it is not checked, but Sonarr skips the login when it is empty |
| Password | an API token |
| Category | what the app suggests: `tv-sonarr` in Sonarr, `radarr` in Radarr |

If your Sonarr shows an API Key field for qBittorrent, you can put the token
there instead and leave Username and Password empty.

When Sonarr tests the connection, its category becomes one of KnightLoader's own
categories (Settings > Rules & categories), so you can give it a folder and a
priority there. A category you already have by that name is used as it is.
Torrents go through the normal intake, the same as a magnet you paste,
and KnightLoader reports each one under its info hash, which is how Sonarr
recognises its own grabs. Sonarr only ever sees the torrents it handed over. A
torrent that is already in your list is refused, as qBittorrent refuses it, so
Sonarr never takes over a download of yours.

Sonarr imports a download once all of its files are on disk and unpacked. With
Remove Completed switched on, it removes the download afterwards, once
KnightLoader has stopped seeding it. When an indexer in Sonarr has a seed ratio
or a seed time, Sonarr sends it along and waits until the torrent has reached
it. KnightLoader itself seeds every torrent to the targets under Settings >
Torrents, so set those at least as high as your trackers ask. A torrent that
stops short of its indexer's ratio stays in the list until you remove it.

When a link to a `.torrent` file arrives instead of a magnet, as it can from
Prowlarr, KnightLoader fetches the file first. A link that leads to no torrent,
such as an indexer's error or login page, is refused rather than downloaded.
Sonarr's Initial State "Stopped" leaves the torrent in the collector, even with
**Start added links immediately** switched on.

A login lasts an hour and ends as soon as you revoke its token. Sonarr logs in
again by itself.

### Usenet and DDL indexers through SABnzbd's API

Add SABnzbd instead, with URL Base `api/sabnzbd` and an API token as its API
key. KnightLoader scans what Sonarr uploads for links, the way it scans a paste,
so a DDL indexer whose "NZB" is really a list of links works. A real `.nzb` is
refused with that reason, because this build has no Usenet backend to fetch it
with.

## Sites that want their own headers

Some links only work with something extra on the request: a forum that checks
the referrer, a private Nextcloud behind basic auth, a page that needs the
cookie your browser already has. A **header profile** stores that per host, in
the same encrypted store as the account credentials, and you can paste a cookie
block or a whole `curl` line in and have the headers pulled out of it.

Two rules make this safe to use, and the code enforces both. A
stored header **never appears anywhere it could be read back**: not in a log,
not in an error message, not in a diagnostic bundle, and not in
`settings.json`, where a Packagizer rule only ever names the profile it wants.
And a header **never follows a redirect off its own site**: a forum that hands
you on to a CDN does not get to pass your session token along with you.
