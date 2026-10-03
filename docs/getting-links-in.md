# Getting links in

Pasting works, and so does dropping text onto the collector. Beyond that:

- **[Click'n'Load](clicknload.md)**: a site's own CnL button hands its links
  straight over. Every site addresses `127.0.0.1`, and a container's loopback is
  not the browser's, so when KnightLoader runs on a NAS there are two ways to
  bridge that gap: the [browser extension](browser-tools.md), which catches the
  submission in the page itself and routes it through the relay to whichever
  instance you pick, or the same binary run as a bridge on your desktop
  (`knightloader -bridge http://nas:8749`) for a browser with no extension in it.
- **Watched folder**: drop a `.txt`, a `.magnet` or a JDownloader `.crawljob`
  onto a share and the box picks it up, with its package name, destination and
  archive password. A `.torrent`, a container and an `.nzb` are taken too, and
  go where an upload of the same file would. A file that has been taken is
  renamed to `.done`. One this instance cannot open, such as an encrypted
  container with no JDownloader backend, stays where it is, and the log says
  why. An `.nzb` left there for want of an account is taken once you add one.
  Point Settings at the folder to switch it on.
- **A page**: paste one, and the files it links to are staged instead.
- **A container file**: upload a `.txt`, `.dlc`, `.ccf` or `.rsdf`. A link list is
  read on the spot. The encrypted formats cannot be opened by anyone offline,
  because their key is issued to registered clients. They are handed to the
  JDownloader backend, which has one. That backend is provisioned on first run
  by default (`KL_PROVISION_JD`), so this normally works with nothing set. With
  no backend at all, a container is recognised and refused, with the missing
  backend named as the reason.
- **An `.nzb`**: upload it the same way and it goes to Usenet, see below.
- **Your debrid account**: what you add on the service's own website can come
  in by itself. See below.
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

**A torrent's files** can be changed after it was added. Once its file list is
known, the torrent's row in the collector and in Downloads opens with the small
triangle in front of its name, the way a package does. Each file then gets a
row of its own with its path inside the torrent, its size, how much of it is
here and a switch. Switching a file on or off while the torrent downloads
changes what it fetches straight away, and nothing already downloaded is
fetched again. At least one file stays on. A magnet shows its files once the
swarm has sent the list, and a finished torrent shows them without switches.
The phone app lists them inside the torrent's card.

**Where a torrent lands**: one with several files gets a folder named after
it, and a single file goes straight into the download's folder. If that name
is already taken there, by a folder, a file or another torrent that is just
starting, the torrent goes into a new folder with `.1`, `.2` and so on after
the name, so two torrents never write into each other's files. A magnet link
can name the torrent differently, or not at all, and the torrent's real name
only comes with its file list. If that name is taken, the magnet is not
downloaded. A torrent that starts again, after a restart or by hand, carries
on where it was. Removing a torrent with its files deletes the files it
downloaded and any folders that end up empty. Anything else in its folder
stays, such as what an archive was unpacked to. Removing works the same after
a restart, since the download keeps a magnet's file list once the swarm has
sent it.

**Through a debrid service**: when TorBox, Real-Debrid, AllDebrid,
Premiumize.me, Debrid-Link or Linksnappy ranks above "Built-in torrent client" on the Accounts
page, that service fetches the torrent and the files come here over HTTP. The
file selection counts there as well: Real-Debrid and Debrid-Link are told which
files to fetch, and from the other services only those files come here. A
torrent from a private tracker stays with the built-in torrent client, because its
passkey would go to the service with it. A `.torrent` says whether it is
private. A magnet link counts as private when its own tracker address carries
a passkey, the same test the extra trackers below use.

A service that makes no progress on a torrent for 10 minutes, whether the
torrent waits in its queue or the service is still reading the magnet link,
has the torrent deleted there, and it goes to the next service or the
built-in torrent client. "Give up on a debrid service after" under Settings,
Torrents sets the minutes, and 0 turns it off. A download imported from the
account and a torrent pinned to the service are left alone.

**Seeding what a debrid service fetched**: with "Seed what the debrid service
fetched" on under Settings, Torrents, a torrent the service fetched is seeded
once all its files are here, which helps with cross-seeding. Nothing is
downloaded for it. KnightLoader first checks that every file is there at the
size the torrent gives, and for a `.torrent` file every piece as well. A magnet
link carries no pieces, so for a magnet only the sizes are checked. "Seed in"
picks the client. The built-in torrent client seeds under the seeding targets.
qBittorrent is handed the torrent through its Web UI with the files where they
are, so tools that watch it, such as cross-seed or qui, find it there. It needs
its address, the login and, if you like, a category. qBittorrent is told not to
check the files again, because KnightLoader already has, so it has to reach
them at the same path. When the two run in separate containers that mount the
download folder under different paths, set "Download folder in qBittorrent" to
where qBittorrent sees it, for example `/data/downloads` for a folder
KnightLoader has at `/downloads`. A torrent whose folder is outside the download
folder cannot be mapped that way and is not handed over. "Test connection" logs
in and shows qBittorrent's version. Once qBittorrent has a torrent, KnightLoader
does nothing more with it. A torrent that fails the check, or that qBittorrent
does not take, is not seeded anywhere, and its row says why.

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
rejected with the reason, next to the links the link filter rejects, and a ban
added later still stops it from starting. Restoring it lets it past the line
that caught it, not past one added afterwards. It is refused rather than
stripped of that tracker: the rest of the torrent would still announce the same
info hash, and a private torrent without its tracker finds no peers.

**Network interface** under Settings, Torrents ties the built-in torrent client
to one interface, such as `wg0` or `tun0` from a VPN, the way qBittorrent's
"Network interface" setting does. Peers, trackers and the DHT then use that
interface only. Every socket is bound to its address, and on Linux, macOS and
Windows to the interface itself as well, so the system cannot route it out
another way. The list offers the interfaces the system has, with their
addresses, and "Any interface", which is the default.

KnightLoader looks at the interface every two seconds. While it is missing,
down or has no address, the client sends and receives nothing: open
connections are closed, no peer is dialled and no tracker is asked. The
torrents keep their place and what they have, the Overview's Torrents card
says they are on hold, and a magnet link that waits for its file list does not
run out of time meanwhile. Once the interface is back, the peers each torrent
knew are dialled again, and trackers and the DHT bring the rest within about a
minute. A change applies to running torrents at once.

With an interface set, trackers and web seeds skip any proxy, because a proxy
would reach them outside the interface. A client that starts with an interface
set also leaves out UPnP port mapping and WebRTC peers, which open connections
of their own, and IPv4 or IPv6 if the interface has no address of that kind. If
you turn the setting on or pick another interface while torrents run, these
stay as they are until the client starts again. Host names of trackers and DHT
nodes are still looked up through the system's resolver. On Linux before 5.7
the container needs `CAP_NET_RAW` to tie a socket to the interface itself, and
without it sockets are bound to the interface's address only. Torrents a debrid
service fetches are not affected: they come over HTTP from the service.

## Usenet

KnightLoader has no newsreader of its own. An `.nzb` goes to a debrid service
that has one: your TorBox account first, or Premiumize.me when there is no
TorBox account, TorBox turns the file down, or TorBox is not taking new ones
for the moment. Add the account under Accounts; there is nothing else to set
up. The service downloads the articles, repairs and unpacks them, and hands
back ordinary files, which then download into the package's folder like any
other link. A release that unpacks into folders keeps them, so a subtitle in
`Subs/` lands in `Subs/` inside the package's folder. An `.nzb` can come from
the upload button, the watched folder, or Sonarr and Radarr, and may be up to
64 MB, which covers a release of about 450 GB.

TorBox takes at most 60 NZBs an hour per API key. When it is at that limit or
says it is busy, the next `.nzb` goes to your Premiumize.me account if you
have one, and otherwise waits and goes out once TorBox takes files again, so
nothing fails for being one too many. An `.nzb` also waits when Premiumize.me
says the account has used up its fair-use points or already runs as many
transfers as it may. A download TorBox queues because every slot of the
account is taken is followed until it starts. An account whose plan does not
include Usenet is passed over for an hour once it has said so. With no
account that can take it, a real `.nzb` is refused, with that as the reason.
A DDL indexer's "nzb" that is really a list of links is read for its links
either way.

While an `.nzb` waits for an account or is being fetched, the status strip
counts it under Usenet. One the service gives up on is listed with the links
that were not added, together with the service's reason.

## From your debrid account

Add a torrent on your debrid service's website and KnightLoader can pick it up
from there, as rdt-client does. Switch on **Import** in the account's row on the
Accounts page. Every account has a switch of its own, so a second account at
the same service can stay out while the first comes in. TorBox, Real-Debrid,
AllDebrid, Premiumize.me and Debrid-Link can do this. The other services have
no list of your downloads to read, and their rows show a dash.

KnightLoader reads the account's list once a minute, well inside every
service's rate limit. Anything new goes into the link collector like a pasted
link, so the link filter, the Packagizer and "Start added links immediately"
treat it as they treat any other. The task keeps the service's own id for the
download, in a link such as `debrid://realdebrid/ABC123`, so the files are
fetched from that account and the torrent is never added a second time. Only
the import writes such a link, and only the Usenet queue writes the links to the
files of an `.nzb`. KnightLoader refuses one that is pasted, sent through the
API or published in a feed, so nobody else can fetch or delete a download on
your account. From
TorBox and Premiumize.me, web downloads and usenet downloads come in as well as
torrents. When a file fails or KnightLoader restarts, the task carries on with
the files already here.

Only what is added after you switch the import on comes in. What was on the
account before stays where it is, and so does everything KnightLoader added
itself, an `.nzb` from the Usenet queue and a torrent from Sonarr included.
Every download that came in is noted in `debrid_imports.json` in the data
directory, so a restart does not bring it in twice. Switching the import
off and on again starts afresh.

The services do not say who added a download. What another app adds with the
same account, such as rdt-client or a second KnightLoader, comes in as well and
is deleted there like the rest, so leave the import off for an account another
app uses.

Once its files are here, the download is deleted on the service, as a torrent
KnightLoader added itself is. Removing an imported task before it has finished
deletes it there too, once the removal can no longer be undone, thirty seconds
after it went through. A restart within those thirty seconds does not stop it.
If the service refuses the delete, KnightLoader tries again, waiting longer
each time, for about two hours. "Keep downloads on the debrid service" under
Settings, Torrents keeps both on the account.

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
for Sonarr and Radarr**, on the Security page or the Modules page. It is off
on a fresh install.

Both need an API token of this instance that can add and read, which you
create with the **Add and read** preset on the Security page (see
[API tokens and their rights](connecting.md#api-tokens-and-their-rights)). With
a token for each app you can revoke one without cutting off the others.

The category Sonarr or Radarr sends is a KnightLoader category, through either
door. A grab is filed in the category of the same name, whatever the case.
When there is none, one is created with its own folder inside the download
folder, so `tv` lands in `/downloads/tv`, and the log says it was created.
You can change its folder and priority under Settings > Rules & categories.

Inside the category's folder every grab gets a folder of its own, named after
the release, so an episode sent under `tv` lands in
`/downloads/tv/Show.S01E01.1080p.WEB`. If that name is taken, `.1`, `.2` and so
on are added, as SABnzbd does. It works this way whether **Put each package in
its own subfolder** is on or off, and a Packagizer rule cannot move the files
out of that folder. A rule that files them in another category still changes
their category. Sonarr and Radarr need this, because once they have imported a
download they delete the folder its client named, with everything in it. When
they remove a download and ask for its files to go, KnightLoader deletes that
download's files and then the folder, as long as nothing else is left in it,
for example the files an archive was unpacked to.

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

When Sonarr tests the connection, it creates its category if this instance
does not have it yet, as described above, and finds it listed with its folder.
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
stops short of its indexer's ratio or seed time stays in the list until you
remove it.

When a link to a `.torrent` file arrives instead of a magnet, as it can from
Prowlarr, KnightLoader fetches the file first. A link that leads to no torrent,
such as an indexer's error or login page, is refused rather than downloaded.
Sonarr's Initial State "Stopped" leaves the torrent in the collector, even with
**Start added links immediately** switched on.

A login lasts an hour and ends as soon as you revoke its token. Sonarr logs in
again by itself.

### Usenet and DDL indexers through SABnzbd's API

Add SABnzbd instead, with this instance's address, the URL Base `api/sabnzbd`
and an API token as its API key. A real `.nzb` goes to Usenet as described
above. While the service fetches an `.nzb`, Sonarr's queue shows it
downloading at the service's own progress. Once the files are here it follows
them, and its history names the folder to import from. A download that fails
with a retry still to come stays in the queue, so Sonarr does not give up on
a release that is about to arrive.

Anything else Sonarr uploads is scanned for links, the way a paste is, so a
DDL indexer whose "NZB" is really a list of links works.

Sonarr accepts only a category it finds under exactly the name it has, so
this door offers each of this instance's categories under its name and its id,
plus the defaults Sonarr and Radarr come with: `tv` and `movies`, and
`tv-sonarr` and `radarr` from their qBittorrent settings.

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
