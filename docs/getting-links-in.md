# Getting links in

Pasting works, and so does dropping text onto the collector. The text need not
be tidy: a link is found inside a sentence, beside others on one line, and
where a mail client broke it across two lines. Beyond that:

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
  renamed to `.done`. Until then a `.torrent`, a container or an `.nzb` carries
  an `.opening` suffix, since JDownloader can take minutes to crawl a
  container, and if the box restarts before it is done, the file is taken
  again. One this instance cannot open stays where it is, and the log says
  why. An `.nzb` left there for want of an account is taken once you add one.
  Point Settings at the folder to switch it on.
- **A page**: paste one with the Page crawler on under Settings, Link
  collector, and the files it links to are staged instead of the page.
- **A container file**: upload a `.txt`, `.dlc`, `.ccf` or `.rsdf`. A link
  list, a DLC, an RSDF and a CCF are read on the spot. One that will not open
  here is handed to the JDownloader backend, which is provisioned on first run
  by default (`KL_PROVISION_JD`). With no backend at all, such a file is
  refused, with the missing backend named as the reason.
- **An `.nzb`**: upload it the same way and it goes to Usenet, see below.
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
Torrents sets the minutes, and 0 turns it off. A torrent pinned to the
service is left alone.

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

An `.nzb` is fetched from your own Usenet servers, or by a debrid service that
has Usenet access: TorBox or Premiumize.me. Which goes first is the order of
their rows under Accounts, Download priority order. Your own servers rank
first until you move them.

### Your own Usenet servers

Add your provider's server under Accounts, Usenet servers: the address, the
port, whether to use TLS, your username and password, and how many connections
your plan allows. Test, in the form and in the row's menu, logs in and asks
the server for an article, so a wrong password shows up before the first
download. The
password is stored encrypted on the instance and never shown again.

Each file of the `.nzb` becomes a download of its own in the release's package
straight away. Its articles are fetched over all connections at once and
written into place, so the file grows from the middle as well as from the
start, and a `.klpart` file with a `.segments` map beside it records which
articles are already there. A paused or interrupted download, a restart
included, asks only for the rest. The speed limit counts these downloads like
any other.

Servers on level 0 are asked first. When one says it does not have an article,
the next server on the same level is asked, then the servers on level 1, and
so on, which is how a block account fills the gaps of the main one. An article
that arrives damaged is fetched again from the same server a few times before
the next one is asked. A server marked optional is skipped while it cannot be
reached; for any other server the article waits and is asked for again, since
that server might have it. A server with a retention in days is not asked for
older articles.

When some articles are on none of your servers and the release comes with
par2 files, the file is finished with gaps where those articles belong, and the
par2 check below fills them in. Without par2 files the release goes to the next
account in the priority order, TorBox or Premiumize.me, if you have one. Files
your servers have already finished stay where they are and are not fetched
again. The other downloads from your servers are removed, and the service
fetches those files instead. Without such an account the file fails, and the
error names how many articles are missing.

### Checking and repairing with par2

Once every file of a release is here, KnightLoader checks it against its par2
files. A file posted under a random or a wrong name is recognised by its
content and renamed to the name the par2 set gives it. A file whose articles
all arrived intact is not read again; the others are checked block by block.
Until the release has passed, the list shows its files as Verifying or
Repairing, Sonarr sees the same, and nothing of the release is unpacked or
moved out of the working folder.

The par2 recovery files of a release are listed but switched off, with a note
that they load only when needed, and they do not count as unfinished. When the
check finds damaged blocks, just enough of them are switched on and downloaded
to rebuild those blocks, and the repair writes them back into the files. A
file whose download failed, for example because a server stayed unreachable,
is rebuilt too when the recovery files hold enough, and its row then counts as
done. A repair uses half of the processor cores and at most 256 MB of memory
at a time.

When more blocks are damaged than all the recovery files of the release can
rebuild, the release goes to the next account, or fails with both numbers in
the error. Once the par2 index is here this shows while the files are still
downloading, and the rest of the release is not fetched for nothing.

### Through TorBox or Premiumize.me

Add the account under Accounts; there is nothing else to set up. TorBox goes
before Premiumize.me unless you move them. The next account also takes over
when TorBox turns the file down or is not taking new ones for the moment. The
service downloads the articles, repairs and unpacks them, and hands back
ordinary files, which then download into the package's folder like any other
link. A release that unpacks into folders keeps them, so a subtitle in
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

While an `.nzb` waits for an account or is being fetched by a service, the
status strip counts it under Usenet. One the service gives up on is listed
with the links that were not added, together with the service's reason.

## Own servers (FTP, SFTP, WebDAV)

A seedbox, a NAS or your own Nextcloud is a source like any other. Paste
`ftp://`, `ftps://`, `sftp://`, `webdav://` or `webdavs://` and the file is
staged, named and sized before it starts.

**Credentials live in Accounts, never in the link.** Open **Add a server** on the
**Own servers** card of the Accounts page and enter the server's **hostname**,
for example `seedbox.example.net`, with the username and password. A pasted link
is looked up by that name, so a login stored under anything else is never found.
If you paste a whole link into the field, only the hostname is kept. A password
written into a URL is refused rather than quietly stripped, because it would be
saved to the task list in plain text. KnightLoader logs in only when it
downloads, so a wrong password shows up on that download and not when you save
the login.

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

## MEGA

A `mega.nz` file or folder link needs no account and no JDownloader.
KnightLoader asks MEGA for the file, decrypts it as it arrives and checks it
against the checksum in the link's key before keeping it. A file that fails the
check is deleted, and the next try starts over. A folder link stages one task per file,
named and sized, and each subfolder becomes a package of its own. The older
`#!` and `#F!` links and `mega.co.nz` work too.

Without an account, MEGA allows each address a limited amount of downloading.
Once that is used up, the download waits as long as MEGA says and then carries
on where it stopped, sooner if a reconnect is set up and brings a new address.
A debrid service that carries MEGA has no such limit: drag it above **MEGA** on
the priority card on the Accounts page to use it first. A MEGA login in
JDownloader goes first anyway. With **Allow free downloads** off, a MEGA link
waits for one of those, since MEGA's own client downloads for free.

## 1fichier

A free `1fichier.com` link needs no JDownloader either, and neither do the
other domains 1fichier answers under, such as `dl4free.com` or `megadl.fr`.
KnightLoader reads the file page, so the link collector shows the name, the
size and whether the file is still there, and it fetches the file over one
connection, which is all a free download gets.

Between two free downloads 1fichier makes each address wait, often for an hour
or more. The task keeps running through that wait and its row shows the time
left, and once it is over the file starts by itself. Pausing or removing the
task calls the wait off. When 1fichier is holding back free downloads as a
whole, KnightLoader asks again every five minutes, five times, before it
gives up. A link protected by a password needs the download password set on it,
which goes into 1fichier's password field. A folder link goes to
JDownloader.

A debrid service that carries 1fichier and a 1fichier login in JDownloader
have no such waits, so both go first. With **Allow free downloads** off, a
1fichier link waits for one of them.

## When KnightLoader cannot use a login

Rapidgator, Keep2Share, FileBoom, TezFiles, Nitroflare, filer.net, ddownload,
KatFile, Clicknupload, FileSpace and Uploadrar logins stay with KnightLoader,
which signs in to these hosters itself, and JDownloader never sees them. If
KnightLoader's own client cannot use one of them, because the hoster answers
in a way the client does not understand or keeps failing three times in a row
without saying why, the login goes back to JDownloader, which signs in through
its own plugin. The login's
row then shows what JDownloader says about it, with a note that JDownloader
has taken over, and the hoster's links go to JDownloader again. A download
that was on its way moves on to the next backend on the priority card instead
of failing, and its row says why.

A hoster that turns the login down, for a wrong password for instance, keeps
the login with KnightLoader and shows it on the row. JDownloader would be
turned down too, and a hoster blocks an address that keeps trying. A hoster
that cannot be reached at all changes nothing either.

While JDownloader has the login, KnightLoader's free client for the hoster
stands aside too, so the links go out on the login rather than for free.

To give KnightLoader another try, save the login again or switch it off and on.
KnightLoader checks it once more and takes it back if its client works with it.

## Rapidgator

With your Rapidgator premium login saved under **Hoster accounts** on the
Accounts page, a Rapidgator link needs no JDownloader. KnightLoader signs in
through Rapidgator's own API, asks it for the download link and fetches the
file over up to five connections, one for a small file. The domains
`rapidgator.net`, `rg.to` and `rapidgator.asia` all work, and the login may be
saved under any of them. The login stays with KnightLoader and is not handed to
JDownloader, so Rapidgator sees one sign-in for the account rather than two.
The login's row shows what Rapidgator says about it: premium or free, when the
plan runs out and how much traffic is left.

The link collector names every Rapidgator link as soon as it arrives, with its
size and whether it is still online. With the login it asks the API, which
knows the size to the byte. Without one it reads the public file page, which
rounds the size.

A download fails with a clear reason when Rapidgator turns the login down, when
the account has no premium, when its traffic has run out or does not cover the
file, and when the file is gone. An account that asks for a two-factor code
cannot be used, since KnightLoader has no way to enter one. Once the traffic
has run out, Rapidgator links go to the next backend on the priority card for
an hour, and then the login is tried again. When Rapidgator asks for a pause
between sign-ins, the task waits five minutes and shows it.

Against a debrid service that carries Rapidgator, the login's row on the
priority card decides which goes first.

Without a premium login a Rapidgator link downloads for free, still without
JDownloader. KnightLoader opens the file page, sits out the countdown it
starts, which shows on the task, and then asks for the Cloudflare Turnstile
captcha on Rapidgator's captcha page. The phone app or one of your captcha
accounts can answer it, since a Turnstile captcha only runs on Rapidgator's
own address. The file then comes over one connection. When Rapidgator makes
the address wait between free downloads, the task keeps running, shows the
time left and starts again once it is over. A file too large for a free
download, or one kept for premium accounts, fails with that reason. The free
client ranks below every debrid service and above JDownloader's free mode, and
with **Allow free downloads** off a Rapidgator link waits for a login or a
debrid service.

## Keep2Share, FileBoom and TezFiles

Keep2Share, FileBoom and TezFiles are three sites on one API, and each keeps
its own accounts. With your premium login for one of them saved under
**Hoster accounts** on the Accounts page, that site's links need no
JDownloader. KnightLoader signs in through the site's own API, asks it for the
download link and fetches the file over up to ten connections. Keep2Share
links work under `k2s.cc`, `keep2share.cc`, `keep2s.cc`, `k2share.cc` and
`keep2.cc`, FileBoom links under `fileboom.me` and `fboom.me`, and TezFiles
links under `tezfiles.com` and `publish2.me`. The login may be saved under any
of a site's domains. It stays with KnightLoader and is not handed to
JDownloader, so the site sees one sign-in for the account. KnightLoader keeps
the session for as long as the site accepts it, because signing in too often
makes the site ask for a captcha. The login's row shows what the site says
about it: premium or free, when the plan runs out and how much traffic is left
for today.

The link collector names every link of these sites as soon as it arrives, with
its size to the byte and whether it is still online, with or without a login.
A whole paste goes out as one question.

A download fails with a clear reason when the site turns the login down, when
the account has no premium, when today's traffic or download limit is used up,
when only some accounts may fetch the file, such as a private one, and when the
file is gone or blocked. Once the traffic is used up, that site's links go to
the next backend on the priority card for an hour, and then the login is tried
again. When the site has counted too many sign-ins, the task waits half an
hour and shows it.

Now and then a site asks for a captcha at sign-in, mostly from a new network.
KnightLoader cannot answer it, and the login's row says so. Sign in to the site
once in a browser on the same network and solve the captcha there, then switch
the login off and on again.

Against a debrid service that carries the site, the login's row on the
priority card decides which goes first.

Without a premium login a link of these sites downloads for free through the
same API. The site shows an image captcha, which the phone app or one of your
captcha accounts can answer, then makes the download wait, mostly half a
minute, and the task shows the time left. The file comes over one connection.
When the site makes the address wait between free downloads, the task sits
that out as well. A file only premium accounts may download, or one too large
for a free download, fails with that reason. The free client ranks below every
debrid service and above JDownloader's free mode.

## Nitroflare

With your Nitroflare premium login saved under **Hoster accounts**, a
Nitroflare link needs no JDownloader. KnightLoader asks Nitroflare's own API
for the download link and fetches the file over as many connections as you
have set, since Nitroflare puts no limit on a premium download. Links under
`nitroflare.com`, `nitroflare.net` and `nitro.download` all work, and the login
may be saved under any of them. The login stays with KnightLoader and is not
handed to JDownloader. Nitroflare's API keeps no session and wants the email
address and password with every call. They go over HTTPS, and KnightLoader
keeps them out of its logs and error messages. The login's row shows what
Nitroflare says about it: premium or free, when the plan runs out and how much
traffic is left.

The link collector names every Nitroflare link as soon as it arrives, with its
size and whether it is still online, with or without a login.

A download fails with a clear reason when Nitroflare turns the login down, when
the account has no premium, when its traffic has run out and when the file is
gone. Once the traffic has run out, Nitroflare links go to the next backend on
the priority card for an hour, and then the login is tried again.

Nitroflare's API sometimes asks for a captcha before it takes a login.
KnightLoader cannot answer it, and the login's row and the failed download say
so. Open `https://nitroflare.com/api/v2/solveCaptcha?user=` followed by the
account's email address in a browser, solve the captcha there, then switch the
login off and on again.

Free Nitroflare links stay with JDownloader, which waits out Nitroflare's
countdown and answers its captcha. Against a debrid service that carries
Nitroflare, the login's row on the priority card decides which goes first.

## filer.net

With your filer.net premium login saved under **Hoster accounts**, a
`filer.net/get/…` link needs no JDownloader. KnightLoader asks filer.net's own
API for the download link and fetches the file over one connection, as
JDownloader does for filer.net. The login stays with KnightLoader and is not
handed to JDownloader. filer.net's API keeps no session and takes the email
address and password with every call, over HTTPS. The login's row shows what
filer.net says about it: premium or free, when the plan runs out and how much
traffic is left.

The link collector names every filer.net link as soon as it arrives, with its
size to the byte and whether it is still online, with or without a login. A
whole paste goes out as one question.

A download fails with a clear reason when filer.net turns the login down, when
the account has no premium, when its traffic has run out or does not cover the
file, when the file sits in a folder locked with a password and when the file
is gone. Once the traffic has run out, filer.net links go to the next backend
on the priority card for an hour, and then the login is tried again. A folder
link goes to JDownloader.

Without a premium login a filer.net link downloads for free, the way
filer.net's own download page does it: an hCaptcha, which the phone app or one
of your captcha accounts can answer, then the wait filer.net sets, shown on the
task, then the file over one connection. When filer.net lets an address have
only so many free downloads an hour, the task waits out the hour. The free
client ranks below every debrid service and above JDownloader's free mode.

FileFactory, Fikper and Upstore logins still go to JDownloader. FileFactory
has no public API any more, Fikper's API did not answer while KnightLoader's
client was being written, and Upstore has no API at all.

## ddownload

A `ddownload.com` or `ddl.to` link needs no JDownloader. KnightLoader reads
the file page, so the link collector shows the name, the size and whether the
file is still there, and it fetches the file over one connection, which is all
ddownload serves.

With a ddownload login saved under hoster accounts on the Accounts page,
KnightLoader signs in itself and downloads on your premium. The login's row
shows the plan, the day it runs out and the traffic left. ddownload asks for a
Cloudflare Turnstile captcha at every sign-in, which the phone app or one of
your captcha accounts can answer, since a Turnstile captcha only runs on
ddownload's own address. KnightLoader keeps the session, so a restart needs
no new sign-in and no new captcha. The login stays with KnightLoader and does
not go to JDownloader.

Without premium the download is free, and ddownload shows the same captcha
before each file and a countdown of about a minute. Between two free
downloads ddownload makes each address wait. The task keeps running through
that wait and its row shows the time left, and once it is over the file
starts by itself. Pausing or removing the task calls the wait off. A link
protected by a password needs the download password set on it.

A wrong login, a premium that ran out, traffic used up for the day, a deleted
file and a file kept for premium accounts each stop the download with their
own message. A premium login ranks where its row sits on the priority card,
right where a ddownload login in JDownloader would. A debrid service that
carries ddownload goes before a free download. With **Allow free downloads**
off, a ddownload link waits for a premium login or a debrid service.

## KatFile

A `katfile.com` link needs no JDownloader, and neither does one of KatFile's
other domains such as `katfile.cloud` or `katfile.online`. The link collector
shows the name, the size and whether the file is still there.

With a KatFile login saved under hoster accounts, KnightLoader signs in itself
and downloads on your premium over two connections. KatFile asks for a
Turnstile captcha at every sign-in and sometimes before a premium download,
and the phone app or one of your captcha accounts can answer it. Without
premium the download is free over one connection: the file page counts down
for about half a minute, the next page asks for a captcha and may count down
again, and the wait KatFile sets between free downloads shows on the task.
Everything else works as for ddownload.

## Clicknupload

Clicknupload moves from domain to domain, and KnightLoader takes links of all
the ones JDownloader knows, such as `clicknupload.click`, `clicknupload.co`,
`clicknupload.org` and `clicknupload.name`. It reads the name, the size and
whether the file is still there from the file page, as for KatFile.

A saved Clicknupload login is signed in without a captcha, and a premium
download uses up to five connections. A free download goes over one
connection, with the countdown and the captcha Clicknupload shows before it
hands out the file. Everything else works as for ddownload.

## FileSpace

A `filespace.com` or `spaceforfiles.com` link needs no JDownloader either,
and the link collector names and sizes it like the others. FileSpace serves every download over one connection, free or premium. A saved
FileSpace login is signed in without a captcha. A free download sits out
FileSpace's countdown and answers its captcha first. Everything else works as
for ddownload.

## Uploadrar

An `uploadrar.com` link needs no JDownloader either, and the link collector
names and sizes it like the others. Uploadrar's file page carries an advert
dressed up as the download button, which KnightLoader leaves alone. A
saved Uploadrar login is signed in without a captcha, and a premium download
uses up to four connections. A free download goes over one connection, with
Uploadrar's countdown and captcha. Everything else works as for ddownload.

## Send.now

A `send.now` link needs no JDownloader, and neither does one of the domains
Send.now went by before, such as `send.cm`, `tusfiles.com` or
`userscloud.com`. The link collector shows the name, the exact size and
whether the file is still there. Send.now's file page goes straight to its
download form, and KnightLoader downloads for free over one connection. When
Send.now says an address has used up its free downloads for the day, the task
keeps running, waits an hour with the reason on its row and then tries again.

A Send.now login stays with JDownloader, whose plugin signs in with Send.now's
API key, so with a premium login there JDownloader goes first, and so does a
debrid service that carries Send.now. A short link (`send.now/d/…`) and a
folder go to JDownloader as well.

## Pixeldrain

A Pixeldrain file link (`pixeldrain.com/u/…`) needs no JDownloader, and
neither does a list (`pixeldrain.com/l/…`), which stages one task per file
under the list's title. The other domains Pixeldrain answers under, such as
`pixeldrain.net` or `pixeldra.in`, work the same. KnightLoader asks
Pixeldrain's API about all the files of a paste in one go, so the link
collector shows their names, sizes and whether they are still there right
away. A link into Pixeldrain's file system (`/d/…`) goes to JDownloader.

A free download gets one connection. After 6 GB in a day Pixeldrain slows a
free address down to 1 MiB/s, and the download carries on at that pace. When
Pixeldrain says an address has used up its downloads for the moment or has too many
open at once, the task keeps running, its row says why and how long it waits,
and then it tries again. A file Pixeldrain hands out only after a captcha on
its own page, or a download it takes for hotlinking, fails and says so.

To use a Pixeldrain account, add a Pixeldrain login under **Hoster accounts**
on the Accounts page with an API key from Pixeldrain's API keys page as the
password. The user name does not matter. The key stays with KnightLoader and
is not handed to JDownloader. KnightLoader's own client sends it along, and
with a paid plan the download opens as many connections as the task allows.
With a key stored, Pixeldrain links rank where the login's row sits on the
priority card, as a Pixeldrain login in JDownloader would, so a debrid service
that carries Pixeldrain comes after it unless the card says otherwise.
Without a key, such a debrid service goes first. With **Allow free
downloads** off, a Pixeldrain link waits for a debrid service.

## Gofile

A `gofile.io/d/…` link needs no JDownloader. A folder stages one task per
file, subfolders included, and each subfolder becomes a package of its own. A
link to a single file stages that file. Gofile lets its documented API list a
folder for premium accounts only, so KnightLoader reads a folder the way
Gofile's own website does: it opens a guest account and signs each request
with the script Gofile's pages use. It fetches that script again every hour,
since Gofile changes it from time to time. A file comes over up to three
connections.

A folder locked with a password opens with the **Link password** given in the
add-links form, and every file of it downloads with the same password. When
Gofile turns away a burst of requests, KnightLoader pauses a few seconds and
asks again, and if that does not help the task waits a minute with the reason
on its row.

A Gofile login under **Hoster accounts** takes the account token from the
profile page on Gofile as the password, and KnightLoader then uses it in place
of a guest account, which also opens content shared with premium accounts
only. As with Pixeldrain, the token stays with KnightLoader, and the login's
row on the priority card decides whether Gofile links go before a debrid
service that carries Gofile. With **Allow free downloads** off, a Gofile link
waits for such a debrid service.

## Mediafire

A `mediafire.com` file or folder link needs no JDownloader, and neither does
`mfi.re`. KnightLoader asks Mediafire's public API for the names, sizes and
state of a paste's files in one go and opens a folder through the same API,
one task per file with each subfolder as a package of its own. When the
download starts it reads the file's page for the link behind its download
button. A free download may open up to 15 connections, and the task's own
setting decides how many it uses.

When Mediafire limits an address for a while, or has a download unavailable
for the moment, the task keeps running, its row says how long it waits, and
then it reads the page again. A file Mediafire removed or blocked fails as
gone. A file locked with a password, one Mediafire wants a captcha for and one
it flagged as malware fail and say so, since only JDownloader's plugin or
Mediafire's own website get past those.

A debrid service that carries Mediafire and a Mediafire login in JDownloader
go first. With **Allow free downloads** off, a Mediafire link waits for one of
them.

## Krakenfiles

A `krakenfiles.com/view/…/file.html` link needs no JDownloader. KnightLoader
asks Krakenfiles for the name, the size and the state of each file as soon as
it arrives. Before a free download Krakenfiles wants a Cloudflare Turnstile
captcha, and the phone app or one of your captcha accounts can answer it while
the task's row says what it is waiting for. The download then goes over one
connection. When an address has used up the 5 GB of free transfer Krakenfiles
gives it a day, the task waits an hour at a time with the reason on its row.

A profile's file list (`krakenfiles.com/profiles/…`) and a Krakenfiles login
stay with JDownloader. A Krakenfiles login there and a debrid service that
carries Krakenfiles go first. With **Allow free downloads** off, a Krakenfiles
link waits for one of them.

## Workupload

A `workupload.com/file/…` link needs no JDownloader. Workupload puts a robot
check in front of its pages, a small puzzle KnightLoader solves itself, so
nobody has to answer anything. The link collector shows the name, the size
and whether the file is still there. A file locked with a password opens with
the **Link password** given in the add-links form. The download goes over one
connection, since Workupload's servers do not resume. When Workupload has a
download unavailable for the moment, the task waits half an hour with the
reason on its row and then tries again.

An archive link (`workupload.com/archive/…`) goes to JDownloader, and a debrid
service that carries Workupload goes first. With **Allow free downloads** off,
a Workupload link waits for one.

## Google Drive

A Google Drive file link needs no JDownloader, in any of its forms:
`drive.google.com/file/d/…`, `drive.google.com/uc?id=…`, `drive.google.com/open?id=…`
and `drive.usercontent.google.com/download?id=…`. KnightLoader asks Drive's
download server for the first byte of each file, so the link collector shows
its name and size right away. A file too large for Google to scan for viruses
comes with a warning page first, and KnightLoader goes past it the way the
**Download anyway** button does. A download opens up to 6 connections.

A folder link (`drive.google.com/drive/folders/…`) stages one task per file,
subfolders included, and each subfolder becomes a package of its own.
KnightLoader reads the folder through the files API that Drive's own folder
page calls, with the key that page carries, so no Google API key is needed. A
Google document, spreadsheet or presentation downloads as a Word, Excel or
PowerPoint file, in a folder as well as linked on its own from
`docs.google.com`. Forms, maps and sites have no file and are left out. An
older link that names a `resourcekey` keeps it.

When Drive says a file has been downloaded too often lately, the task keeps
running, its row says so, and it tries again after an hour. A file Drive no
longer has fails as gone. A file or folder shared with some Google accounts
only, and a file Drive found a virus in, fail and say so.

A debrid service that carries Google Drive and a Google login in JDownloader go
first. With **Allow free downloads** off, a Google Drive link waits for one of
them.

## Dropbox

A Dropbox file link (`dropbox.com/scl/fi/…`, or the older `dropbox.com/s/…`)
needs no JDownloader, whether it ends in `dl=0`, `dl=1` or `raw=1`.
KnightLoader follows the link with `dl=1` to Dropbox's content server and reads
the file's name and size from its first byte. The download then comes from
there over one connection, as it does in JDownloader.

A folder link (`dropbox.com/scl/fo/…` or `dropbox.com/sh/…`) stages one task
per file, subfolders included, each subfolder as a package of its own.
KnightLoader lists it the way Dropbox's own folder page does. If Dropbox does
not list the folder, the link stays one task and downloads as the zip Dropbox
makes of the whole folder.

A link locked with a password opens with the **Link password** given in the
add-links form. When Dropbox pauses a link for drawing too much traffic, the
task keeps running, its row says so, and it tries again later. A link Dropbox
no longer has fails as gone.

A debrid service that carries Dropbox and a Dropbox login in JDownloader go
first. With **Allow free downloads** off, a Dropbox link waits for one of them.

## OneDrive and SharePoint

Share links from OneDrive (`1drv.ms/…`, `onedrive.live.com/…`) and from any
SharePoint site (`….sharepoint.com/:u:/…`, `/:f:/…` and the other kinds) need
no JDownloader. KnightLoader opens a share the way a browser does, which gives
it a guest session on the site, and downloads with that session. A shared file
is named and sized in the link collector as soon as it arrives. A shared folder
stages one task per file, subfolders included, each subfolder as a package of
its own, and KnightLoader lists it through SharePoint's REST API, which answers
the guest too. Personal OneDrive shares go the same way, since Microsoft keeps
them on SharePoint as well.

When OneDrive throttles requests, the task keeps running and waits as long as
OneDrive asks. A share that was removed fails as gone. A share for some
Microsoft accounts only fails and says so, and so does a share locked with a
password, which only OneDrive's website takes.

A debrid service that carries OneDrive goes first. With **Allow free
downloads** off, a OneDrive link waits for one.

## WeTransfer

A WeTransfer link (`wetransfer.com/downloads/…`, also on a company's own
`….wetransfer.com`) and a short link (`we.tl/…`) need no JDownloader. A
transfer stages one task per file, and the folders it was sent in become
packages. KnightLoader asks WeTransfer's public download API, the one its own
download page uses, for the transfer's files and for each file's download
link. A file comes over one connection, as it does in JDownloader. A transfer
link that stays one task downloads as the zip WeTransfer makes of the whole
transfer.

A transfer locked with a password opens with the **Link password** given in
the add-links form. An expired or deleted transfer fails as gone. A transfer
whose sender wants every recipient to confirm their email first fails and says
so, since only WeTransfer's website can do that.

A debrid service that carries WeTransfer goes first. With **Allow free
downloads** off, a WeTransfer link waits for one.

## SwissTransfer

A SwissTransfer link (`swisstransfer.com/dl/…`) needs no JDownloader. A
transfer stages one task per file, read from the transfer's own page, and the
folders it was sent in become packages. When a download starts, KnightLoader
asks SwissTransfer's API for the file's signed link, which works for an hour,
and asks for a fresh one if the download outlasts it.

A transfer locked with a password opens with the **Link password** given in
the add-links form. An expired transfer, and one downloaded as often as its
sender allowed, fail as gone.

A debrid service that carries SwissTransfer goes first. With **Allow free
downloads** off, a SwissTransfer link waits for one.

## Sonarr and Radarr

Sonarr, Radarr and Prowlarr can use KnightLoader as a download client. It speaks
two protocols they already know: qBittorrent's for torrents, and SABnzbd's for
what a Usenet or DDL indexer hands over. One switch opens both, **Download client
for Sonarr and Radarr**, on the Security page. It is off on a fresh install.

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
above. While your own servers or a service fetch an `.nzb`, Sonarr's queue
shows it downloading, and verifying or repairing during the par2 check. A
release your servers cannot complete or repair, with no debrid account to take
it, lands in Sonarr's history as failed with the reason, so Sonarr can block
it and search for another. The par2 recovery
files that stay switched off do not hold a release back. Once the files are here it follows
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
