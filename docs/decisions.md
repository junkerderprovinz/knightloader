# Decisions that shaped what is not here

Every entry below is a feature, or a part of one, that was specified and then
deliberately not built the way the specification asked. They are written down for
one reason: a decision whose reasoning is lost looks exactly like an oversight,
and the next person to read the code fixes it back.

Each entry names what was chosen, and what was rejected and why. The rejected
half is the part worth keeping.

## Ownership: UMASK applies, PUID and PGID only report

**Built:** the readout. Which user and group downloaded files land as, under
which mask, per configured folder, plus the same figures in the diagnostics
bundle. And UMASK: the process takes it as its own umask at startup, the way
linuxserver.io images read it, and logs one line saying so.

**Not built:** an entrypoint that makes PUID and PGID take effect.

The two kinds of variable need different things. A umask belongs to the
process, any process may set its own, and the children it starts (yt-dlp,
ffmpeg, the headless JDownloader) inherit it, so honouring UMASK needs no
privilege at all. Changing the uid does. `Dockerfile` declares `USER knight`, on
purpose. Honouring PUID and PGID means starting the container as root and
dropping privileges in an entrypoint, which turns a non-root image into a
root-started one, and it needs a first-ever docker build job in CI to prove the
result. That is a separate decision on a separate day. On Unraid,
`--user 99:100` in Extra Parameters does what PUID and PGID do elsewhere.

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

Two consequences are load-bearing and must not be "tidied":

- The readout reports per variable. `umaskApplied` says whether UMASK took;
  a single `idsRead: false` says that nothing reads PUID or PGID. There is no
  per-id "did this take" flag, because this build could not answer one
  honestly: `applied.puid = true` could only ever mean "the value you set
  happens to equal the uid you already had", which reads on screen as "PUID
  works".
- No string in the interface may imply that setting PUID or PGID would change
  anything. `settings.owner.envIgnored` means: the variable is set, and nothing
  in this image reads it.

`routes_fileowner.go` carries a test that reads this repo's own `Dockerfile` and
fails in both directions: while `USER knight` is there and `idsRead` is true,
and when `USER knight` is gone and `idsRead` is still false. So the day somebody
does take the entrypoint decision, that test tells them the copy has to move
with it.

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
plain webhook are all the same shape and are all covered.

**Not built:** e-mail.

SMTP is a second transport with its own credential at rest, its own TLS-mode
question, its own test shape and its own German copy. Folding it in adds about a
third again on both sides plus a second credentials decision in the middle of the
first. `internal/notify`'s package comment says in as many words that the absent
seam is absent on purpose, so nobody adds one while they are in there.

## Settings export: no secrets by default

**Built:** the "include stored secrets" tick, unticked when the dialog opens.

The failure this avoids cannot be taken back: an export lands in a sync folder, a
mail attachment or a chat, and a router password goes with it. The cost of the
other direction is a proxy that dials with no password after an import, which is
visible, loud, and fixable in one field.

`TestSecretlessJudgesTheDocumentAndNotItsClaim` is the guard that matters here:
it reads what the file actually holds rather than what the file says about
itself, because the `secrets:` field is a string in a document anybody can edit.

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

## yt-dlp: fetch and verify, once a day unless switched off

**Built:** a button that fetches a newer yt-dlp, verifies its checksum, proves it
RUNS on this machine, and only then swaps it in. The same path runs once a day
on its own, a few minutes after start and every 24 hours, behind a switch that
ships on. Plus an opt-in "ask GitHub when this page opens" toggle that ships off
and only ever checks.

**Not built:** a daily run that installs yt-dlp where there is none, or that
replaces a version it cannot put in order against the release, such as a build
from git.

yt-dlp does ship regressions, and a new one changes what downloads produce.
The daily run is on anyway because the other failure is the common one: the
container carries Alpine's yt-dlp package, which lags weeks behind, and a site
that changed its page breaks every download from it until yt-dlp catches up. A
new release that fails its checksum or does not start here replaces nothing,
and the switch and "back to the system copy" stay on the Resolvers page for the
regression case.

A fetched copy DOES outrank an explicitly set `KL_YTDLP`, which is the one place
this feature overrides an operator's own setting. The reason is that the
Dockerfile pins `KL_YTDLP=/usr/bin/yt-dlp` on every container, so the other
answer would make the whole button a silent no-op in exactly the situation the
button exists for. It is visible and reversible: the card states plainly that
`KL_YTDLP` is not being started and offers "back to the system copy".

## The desktop update: on the next start, checked by its checksum

**Built:** a desktop app that downloads a newer release in the background once
a day, checks it against the release's `checksums.txt` and puts it in place of
the program for the next start. The switch is on from the start.

**Not built:** an Install now button that restarts into the new version, and a
code signature check.

A restart in the middle of the day would cut off whatever is downloading, and
the next start comes soon enough for a download manager that runs all day. The
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
