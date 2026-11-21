# Decisions that shaped what is not here

Every entry below is a feature, or a part of one, that was specified and then
deliberately not built the way the specification asked. They are written down for
one reason: a decision whose reasoning is lost looks exactly like an oversight,
and the next person to read the code fixes it back.

Each entry names what was chosen, and what was rejected and why. The rejected
half is the part worth keeping.

## Ownership: start as root, hand over what is empty, switch

**Built:** the readout. Which user and group downloaded files land as, under
which mask, per configured folder, plus the same figures in the diagnostics
bundle. UMASK, which the process takes as its own umask at startup, the way
linuxserver.io images read it. And PUID and PGID, for a container that starts as
root.

**Not built:** a recursive chown, a chown of a folder that already holds
something, and a shell entrypoint.

Docker creates a missing bind-mount source as root:root 0755. An image that
starts as an unprivileged user cannot write a data directory made that way, so
the plain `docker run` from the README would stop at its first start and restart
in a loop. `Dockerfile` therefore declares no `USER`. The binary starts as root
and, before it reads a setting or opens a socket, `internal/privdrop` does three
things. It gives each folder a first start mounts (the data directory, the
`downloads` folder inside it, `/watch` and every `KL_WATCH_DIRS` entry) to
PUID:PGID, provided root owns it and it holds nothing but other volumes' mount
points. It drops root's supplementary groups and switches gid, then uid. And it
checks that neither its real nor its effective uid is root, and stops the start
otherwise. PUID and PGID
default to 1000, the image's `knight`; 0, or a value that is not a number, stops
the start.

Started as any other account, none of this runs and PUID and PGID are not read.
That is Unraid, whose template passes `--user 99:100`, and any other `--user`,
`runAsUser` or rootless Docker. A server started as root outside a container
switches just the same.

Only an empty folder is handed over, and only the folder itself. A folder with
something in it is somebody's existing data or a mounted share, and chowning it
at boot would rewrite the ownership of their library. For a data directory like
that, `fileowner.Advise` logs who owns it, which account the server runs as and
the chown to run on the host.

The switch is Go, in `main`, rather than a shell script, so that root does
nothing but the hand-over and the switch: no listener, no file in the data
directory, no JDownloader. `--reset-2fa` and the bridge run after the switch as
well. `tini` stays PID 1 as root; it reaps children and forwards signals, and
starts nothing but the server.

The two kinds of variable need different things. A umask belongs to the
process, any process may set its own, and the children it starts (yt-dlp,
ffmpeg, the headless JDownloader) inherit it, so honouring UMASK needs no
privilege at all. Changing the uid does, which is why PUID and PGID depend on how
the container was started.

For the mask to decide anything, every file a download produces is requested as
0666 and every folder as 0777 (`internal/filemode`), extracted files included.
With `UMASK=000`, the Unraid convention, the account that reaches the share over
SMB can move and delete what KnightLoader wrote. The torrent library is the
exception: it creates files 0644 and folders 0755 whatever the umask, and makes
each file read-only once it is complete, with no option to change either. So a
finished torrent's own files and folders are set to what the umask asks for
when it completes, before the app moves them on.

The mask widens nothing the app keeps for itself. The database, settings, keys,
sessions and backups are written with explicit modes, and a umask only takes
bits away. JDownloader is the one child that writes secrets with modes of its
own choosing, the hoster logins it is handed, so its folder in the data
directory is kept at 0700.

Three consequences are load-bearing and must not be "tidied":

- The readout reports per variable. `umaskApplied` says whether UMASK took;
  `idsRead` says whether this process read PUID and PGID at all, which only a
  start as root does. There is no per-id flag: under `--user` neither is read,
  and `applied.puid = true` could then only mean "the value you set happens to
  equal the uid you already had", which reads on screen as "PUID works".
- `settings.owner.envIgnored` means: the variable is set, and this container
  was started as another account, so nothing read it. No string may imply that
  setting PUID or PGID changes anything while `--user` is in the run command.
- `TestTheImageStartsAsRootSoPUIDCanTakeEffect` in `internal/api` reads this
  repo's `Dockerfile` and fails on a `USER` line, or when the account it creates
  differs from the one the switch falls back to. The `image` job in
  `.github/workflows/ci.yml` builds the image and runs
  `scripts/image-ownership.sh`, which starts it as root on fresh bind mounts and
  checks the ids the server runs with, the owners of the folders it took over,
  `--user 99:100`, and that a folder with a file in it keeps root as its owner.

## Metrics: a guarded route and a switch that ships off

**Built:** the Prometheus readout behind the same authentication as everything
else, plus a settings switch that is off until somebody turns it on.

**Not built:** a second open endpoint.

The original item asked for one. `internal/api/routes_test.go`'s
`TestOnlyTheseRoutesAreOpen` pins the list of unlocked doors with a written
justification per entry, and an unauthenticated metrics route on a
password-locked instance is a hole somebody would have to have chosen on
purpose. Anybody who wants it scraped switches it on and knows why they did.

## Outbound notifications: HTTP now, SMTP as its own item

**Built:** one HTTP subscriber on the event bus. ntfy, Gotify, Matrix and any
plain webhook are all the same shape and are all covered. Discord is the same
shape too; a Discord address with no body of its own gets one written for it,
because a single template cannot colour a failure red or keep a long file name
inside Discord's limits.

**Not built:** e-mail.

SMTP is a second transport with its own credential at rest, its own TLS-mode
question, its own test shape and its own German copy. Folding it in adds about a
third again on both sides plus a second credentials decision in the middle of the
first. `internal/notify`'s package comment says in as many words that the absent
seam is absent on purpose, so nobody adds one while they are in there.

## Orphaned files: deferred until the history can point at one

**Not built at all**, and this is the one whole item on the list that waits.

It named three categories of file to offer for deletion. One of them, "files of
deleted tasks", cannot be built as worded: `store/history.go` deliberately does
not keep the folder a file went to ("a column that is right one time in twenty is
worse than no column"). The history can vouch for a name and a length, which is
exactly the use `reclaimWitness` already makes of it. Inverting that into "a
history match proves this file may be deleted" is the most expensive mistake
available in this feature, so rather than ship two thirds of a delete page, the
item waits for the history to record its destination. That is its own item, with
its own schema change.

Settled for whenever it is picked up: **one folder level, the app's own folders
only.** Never a recursive walk from the download root. `reclaim.go` already gives
the reason and it still holds: on a typical Unraid box `/downloads` is a share
the user browses, often the same tree as the media library, and a recursive walk
plus a delete button turns that library into a selection list.

## The log file: a source filter, not a level picker

**Built:** the optional log file, its rotation, the search, the follow switch,
the per-task chip, and a filter by SOURCE.

**Not built:** a filter by severity level.

There are no levels to filter by. This tree has zero uses of `log/slog` and 156
bare `log.Printf` call sites, and migrating them is its own item. The card says
so in its own words rather than offering an empty picker:
`settings.diagnostics.logSourceHint` ends "There are no severity levels to filter
by: nothing in this app records one."

## The self test: it does not ask a stranger

**Built:** the seven checks the instance can answer about itself, and the four
the browser can answer about the proxy in front of it.

**Not built:** "is the torrent port open from outside".

That question cannot be answered without contacting a machine the user does not
run. This repo has already ruled twice that it will not do that on its own
initiative (`internal/proxycfg/probe.go`, `internal/reconnect/config.go`). The
row reports that it is declining, names which question it is declining, and
points at the UPnP button, which is the thing that CAN be pressed.

## The components: fetched, verified and kept current behind one switch

**Built:** yt-dlp, ffmpeg, Deno, the desktop app's Java and JDownloader are kept
current by one switch, **Update components automatically**, which ships on.
While it is on, the app asks after yt-dlp once a day and after the others once
a week, and downloads only a version newer than the one in use. Every fetch
verifies its checksum, proves the program RUNS on this machine, and only then
swaps it in. **Update components** on the Components card runs the same updates at
once, whatever the switch says. When each component was last asked after is
kept in `tools/checks.json` in the data folder, so a restart does not ask again
before one is due.

**Not built:** a switch per component, and a switch that only checks. One
switch is what somebody looking for "keep this current" expects to find, and a
check that downloads nothing changes nothing either. An automatic run also
leaves alone a yt-dlp version it cannot put in order against the release, such
as a build from git, and in the container installs no yt-dlp where there is
none.

yt-dlp does ship regressions, and a new one changes what downloads produce.
The switch is on anyway because the other failure is the common one: the
container carries Alpine's yt-dlp package, which lags weeks behind, and a site
that changed its page breaks every download from it until yt-dlp catches up. A
new release that fails its checksum or does not start here replaces nothing.
yt-dlp is asked after daily for the same reason, and the others weekly because
they move slower. ffmpeg's rolling build changes every night, so its weekly
check fetches a new one most weeks and no more often.

A fetched copy DOES outrank an explicitly set `KL_YTDLP`, which is the one place
this feature overrides an operator's own setting. The reason is that the
Dockerfile pins `KL_YTDLP=/usr/bin/yt-dlp` on every container, so the other
answer would make updating yt-dlp a silent no-op in exactly the situation it
exists for. The card says plainly which yt-dlp runs.

The desktop app fetches yt-dlp rather than shipping it, like every other
component, because a video link that no yt-dlp reads falls through to
JDownloader and lands as one plain row, and the copy the app keeps current is
the fetched one anyway: the program's folder is read-only to the app. The
installer on Windows and Linux fetches it into the data folder, checked against
the release's `SHA2-256SUMS`, as the last step of the install. The macOS app, a
copy from the zip, the Flatpak and a silent install fetch it at the first
start, where the app finds none. The Windows and Linux zips stay the program
alone, because every installed copy updates itself from them and refuses a zip
that holds more. A yt-dlp beside the program, which older installs carry, ranks
below a fetched one and below `KL_YTDLP`.

ffmpeg is fetched rather than shipped. It is about 180 MB on Windows, more than
the program and yt-dlp together, and most of a download that is never needed
where ffmpeg is already installed. On Windows and Linux a desktop copy that
finds no ffmpeg on `PATH` fetches the static LGPL build of FFmpeg master from
FFmpeg-Builds/FFmpeg-Builds, verifies the archive against that release's
`checksums.sha256`, unpacks only ffmpeg and ffprobe beside the fetched yt-dlp,
and runs each with `-version` before putting it in place. yt-dlp's own fork of
those builds publishes GPL builds only, and macOS gets nothing from either, so
it stays with an ffmpeg on `PATH`. The rolling `latest` release is used and
not a pinned one, because the project prunes its dated releases after about
two weeks and an installed app has to find its build again months later. The
release keeps its tag, so the digest it publishes for the archive is what
tells a new build from the fetched one. An ffmpeg on `PATH` is never replaced
and always wins.

Deno is fetched the same way. yt-dlp runs YouTube's player code in a
JavaScript runtime, and Deno is the one it enables by itself. A desktop copy
that finds none fetches the newest release from denoland/deno for its
platform, macOS included, verifies the zip against the `.sha256sum` published
beside it (sha256sum's line on Linux and macOS, PowerShell's `Get-FileHash`
list on Windows), runs it with `--version` and puts it beside the fetched
yt-dlp. Every yt-dlp run gets it as `--js-runtimes deno:<path>`, the fetched
copy ahead of `KL_DENO` and `PATH` as for yt-dlp itself, and without a Deno
yt-dlp runs as before with fewer YouTube formats. A newer release replaces the
fetched copy. The container installs Alpine's deno package instead, which is
built for musl on both architectures, where the official builds need glibc.
The self-test does not take a Deno on disk as proof: it asks yt-dlp, whose
`-v` header names the runtimes it found, and warns on `JS runtimes: none`.

## The desktop update: on the next start or a restart asked for, checked by its checksum

**Built:** a desktop app that downloads a newer release in the background once
a day, checks it against the release's `checksums.txt` and puts it in place of
the program for the next start. The switch is on from the start. Once a new
version is in place, **Restart for** on the Version card ends the program and
starts the new one.

**Not built:** a restart nobody asked for, and a code signature check.

A restart in the middle of the day would cut off whatever is downloading, so
the program never restarts on its own: the new version waits for the next
start, or for the person who presses the button and knows what runs. The
checksum proves the file is the one the release workflow published, not that
the workflow was trustworthy, since both come out of the same job. Only a
signature tied to a key outside the build would close that gap, and the
releases are not signed yet.

## The end-of-queue command is redacted whole

The command line a person can have run when the queue empties is redacted
everywhere it can travel: the diagnostics bundle, the settings page, log lines,
and the program's own output before it reaches a log line.

The whole line, program and arguments both, not just the arguments.
`routes_diagnostics.go` already refuses to carry this instance's own store path,
because a desktop path contains a person's real name, and a half-redaction is the
"patched three sites, missed the fourth" shape that `reconnect.redact` warns
about. The consequence is deliberate: the settings page shows an empty box with a
placeholder saying a command is stored, exactly the arrangement `Reconnect.tsx`
already uses for a password, and `POST /api/idle-action/check` is what answers
"what would actually run".

## An event program follows its row by a random id

An event program's command line is redacted whole, like the end-of-queue
command, and a save puts the stored one back onto the incoming row with the
same id. The id alone decides which program a row runs, so it is random rather
than the lowest free number. With numbers, a row deleted in one tab and saved
again from another, or a row imported from another instance's export, would
carry an id that some other row holds by then and would run that row's program.
An id a client sends is kept only when it has the shape this instance gives
out, sixteen hex digits; an API client that numbers its rows gets random ids
instead. An imported row whose id this instance does not know arrives with no
program, and the import names it as incomplete.

## The start report only looks

The boot pass stats folders and writes nothing anywhere, the data directory
included. The write test happens only on a human press, and even then only into a
folder that already exists; nothing is ever created.

A boot-time probe file in every configured folder wakes a spun-down array disk on
every container restart. That is the whole reason "the folder is there" and "this
instance can write in it" are two separate claims in the report rather than one:
a row that said "ok" without saying which would be claiming a test it did not
run.

## The debrid drive: read-only first

**Built:** a WebDAV share at `/dav/` that lists what is on the debrid accounts
and streams a file from the service's download server, a range at a time.

**Not built:** writing to it. No upload into a folder to add a torrent, no
delete that removes a download on the service, no rename.

A share that can be written to is a second way to change an account, beside
the one the collector already guards. Deleting through a mount is easy to do by
accident: a media server's "empty trash", a file manager's sync, a cleanup
script pointed at the wrong folder. On a read-only share each of those is
refused; on a writable one each deletes a download the service may have taken
hours to fetch, with no undo. Read-only also keeps the credential small. The
drive takes a token with the read right, the same one a dashboard gets, so the
key sitting in `rclone.conf` on another machine cannot add, delete or change
anything. Writing can come later as its own decision, with its own right and
its own switch, once there is a reason that the website or the collector does
not already cover.

Two refusals belong to the same reasoning and must not be "tidied". A PROPFIND
with `Depth: infinity` gets a 403 instead of a walk of every download on every
account, which RFC 4918 allows and which would otherwise spend the account's
rate limit on one request. And the login password is not accepted as a Basic
password: it would end up in a config file on another machine and would get
past the second factor, so only an API token opens the drive.

## Usenet: pure Go, without the par2 binary

**Built:** an NNTP client, a yEnc decoder and an `.nzb` reader of KnightLoader's
own, and next a par2 check and repair in Go as well.

**Not built:** downloading through a library that needs cgo, or repairing with
the `par2` program.

The release is one static binary built with `CGO_ENABLED=0`, for every
platform the desktop app ships on, and the container has no package manager
to install anything into. The fastest Go NNTP pool, javi11/nntppool, decodes
yEnc through rapidyenc, which needs cgo, or Go 1.27 with an experiment switched
on; the other NNTP libraries have no pool. A protocol of a handful of commands
and an encoding of a page of rules cost less to own than that dependency.

par2cmdline-turbo is the fastest repair there is, but it is a C++ program under
the GPL that would have to ship for every platform and be kept current, the
way yt-dlp is, for a step most downloads never need. The Go libraries either
hold every block in memory (akalin/gopar, its issue 10) or are too young or
carry no licence, so the check and the repair are written here
(`internal/par2`), from the PAR 2.0 specification.

## Usenet: the par2 check trusts what it downloaded

**Built:** a check of each release from the own servers once all its files are
here. A file whose articles all arrived and matched their CRC is taken as
whole when its size and the hash of its first 16 KiB match; every other file
is read and checked slice by slice. A file under a random or a wrong name is
recognised by that hash, or by the checksum of one of its slices, and renamed.
The recovery volumes stay switched off until the check knows how many blocks
it needs, and then only the volumes that cover them are fetched. A file with
missing articles counts its missing bytes, and once the par2 index is here
the release fails as soon as those bytes alone need more blocks than every
volume holds.

**Not built:** par2cmdline's search for blocks that have moved inside a file,
and NZBGet's way of working out each block's CRC from the article CRCs.

Every article is written at the offset its yEnc header gives, so a block can
be missing but never moved: the search for shifted data that par2cmdline runs
over a damaged file would read the whole file to find what is always where it
belongs. Articles and par2 slices have different sizes, so NZBGet's quick check
has to combine article CRCs across slice boundaries and still read the slices
that straddle a gap. Trusting a download whose every article matched costs one
16 KiB read per file instead. When that trust turns out wrong, the repaired
file fails its MD5, and the check starts over and reads everything, so a wrong
guess costs time and never a broken release. The early verdict counts only
bytes that were certainly not written, rounded down to blocks, so it can fail
a release late but never one a repair could have saved.

## Usenet: the par2 repair streams, on a fixed budget

**Built:** Reed-Solomon over GF(2^16) as PAR 2.0 defines it. The matrix of the
recovery slices used is inverted once; then the files are read in stretches as
wide as the memory budget allows, the intact slices are multiplied into the
recovery slices, and the damaged slices come out and are written in place.
Each file it wrote is checked against its MD5 before the release goes on. It
works on half the processor cores and at most 256 MiB, which on a set with
few damaged blocks is a single pass over the files.

**Not built:** SIMD, a setting for the cores and the memory, and PAR 1 or PAR 3.

par2cmdline-turbo gets its speed from hand-written vector code that Go would
need assembly or cgo for, and a repair is rare enough that the plain table
lookups, about 3 GB a second per core on a current desktop processor, are
worth their simplicity. The
check runs beside the downloads and whatever else the server does, so it takes
half of the machine rather than all of it; the two numbers are constants until
somebody has a box where they are wrong. Usenet posts come with PAR 2.0
files, and PAR 3 is still a draft.

## DLC: the key arrives when a release is built

**Built:** `.dlc` files opened by KnightLoader itself, with the application key
the DLC service issued to KnightLoader. The key and its IV are two package
variables in `internal/container`, empty in the source, and `release.yml` fills
them with `-ldflags -X` from the repository secrets `KL_DLC_KEY` and
`KL_DLC_IV`: the image build receives them as BuildKit secrets, so they appear
in no layer, build argument or provenance record, and the desktop builds read
them from the environment.

**Not built:** the key in the source, a setting for it, or a variable that
supplies it at run time.

The key names the client to the service. Committed to a public repository it
would belong to every fork and every script that copies it, and whatever they
did with it would be done under KnightLoader's name, up to the service
withdrawing it from every installation at once. A setting or a variable would
only move the same problem to wherever people paste their configuration. The
cost is that a build from source opens no `.dlc` itself and hands every one to
the JDownloader backend; CI builds and tests without the secrets. The key does
end up in the shipped binary, and somebody determined can dig it out of there.
What keeping it out of the source prevents is publishing it next to the code.

## Torrents: qBittorrent, run by KnightLoader

**Built:** a qbittorrent-nox that KnightLoader fetches, starts on `127.0.0.1`
with a WebUI login it generates, watches and stops with itself, the way it
runs its JDownloader. The version is pinned in
`internal/qbittorrent/nox/versions.json`, with the SHA-256 of every build, and
moves only when somebody picks another one. An install where nobody picked one
runs the table's default, the newest build. Linux and the container fetch the
static build from userdocs/qbittorrent-nox-static at run time. Windows and
macOS fetch a build without a window that `qbittorrent-nox.yml` makes. Every
KnightLoader release carries those builds with their sources, and an app
fetches them from the release it is, so the files always match its table and
no release of their own is needed.

Every torrent runs in it. KnightLoader keeps the decisions it made before,
where a torrent lands, which files it fetches, which trackers it may use and
when its seeding ends, and qBittorrent moves the bytes. The engine reaches
it through `internal/torrents`, which has a client in memory beside the real
one for the tests.

**Not built:** private trackers with KnightLoader's own torrent engine, or
qBittorrent from Alpine's packages in the image.

Private trackers admit only the clients on their whitelist, by the client's
peer id and user agent, and KnightLoader's engine is on none of them.
qBittorrent is on all of them, but only at the versions each list names: OPS
lists 2.x to 4.5.x. Following the newest release, as the other components do,
would take a member off their tracker the day a new version came out, so the
version is data that a person changes, and 4.6.7 and 4.5.5 stay to pick
beside the default. Alpine ships one qBittorrent, the
newest, which misses the pin, and a GPL program in the image would oblige us
to offer its source with every image. Fetched at run time and driven over
HTTP, it is a separate program next to KnightLoader. 4.5.x does not check TLS
certificates on its own downloads (CVE-2024-51774), so KnightLoader turns off
everything that has qBittorrent fetch from the web by itself, RSS and the
peer country lookup, and hands it only .torrent files and magnet links.

qBittorrent starts the torrents it kept on its own, and 4.5 has no setting
that starts them stopped. So it starts tied to a network interface that does
not exist, where they reach nobody, and the first thing KnightLoader does
once it answers is stop every torrent of its tag and start those the engine
runs and the twins of tasks on the list. Only then does qBittorrent get its
settings with the real interface. A torrent whose task is gone is taken
out without its files, since KnightLoader deleted the files of a task
removed with them itself. Only a twin seeding from links keeps a note, in
`torrents-left.json`, when qBittorrent could not be reached as its task left,
so its links go at the next start.

The built-in VPN is a network stack inside KnightLoader, which another
program cannot use. qBittorrent reaches it through a SOCKS5 proxy that
KnightLoader runs on loopback, which dials into the tunnel only and refuses
while the tunnel is down. qBittorrent is tied to the loopback interface and
has the DHT, uTP, local peer discovery and port mapping off, since each of
those can go around a proxy: 4.5 resolves the DHT's first nodes with the
system's name servers. The proxy is in qBittorrent's settings file before it
starts, so the torrents it restores never go out directly. A SOCKS5 proxy
takes no incoming connections, so torrents through the VPN cannot be reached
by other peers, forwarded port or not.


## Cross-seeding: the twins stay in qBittorrent

**Built:** a twin found on another tracker is added to KnightLoader's
qBittorrent with the tags `knightloader` and `cross-seed`, its check skipped,
seeding from the task's own files or from links to them. It is not a task.
It leaves qBittorrent when its task leaves the list, and takes its linked
files along when the task's files go. While the task stays, the twin seeds
on, whatever the seeding targets say.

**Not built:** a task for each twin.

A twin holds no byte the task did not fetch. As a task it would stand in the
list twice over the same files, and a removal with files could delete what
the task still seeds from. The seeding targets are left out too: they exist
so a download gives back what it took, and a twin took nothing, while the
reason to seed it at all is to keep seeding on the other tracker. The totals
on the overview count what a twin sends, read from qBittorrent every few
seconds from its second reading on, so nothing it sent before is counted
twice; it downloads nothing.

The search asks each tracker about each torrent once and remembers the
answer in `cross-seed-asked.json`, a match, no match and an error alike, so a
restart or a second look never asks a private tracker again. An error is
asked again only once that tracker's settings change, since a wrong key
fails the same way every time.
