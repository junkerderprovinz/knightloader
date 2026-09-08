# Decisions that shaped what is not here

Every entry below is a feature, or a part of one, that was specified and then
deliberately not built the way the specification asked. They are written down for
one reason: a decision whose reasoning is lost looks exactly like an oversight,
and the next person to read the code fixes it back.

Each entry names what was chosen, and what was rejected and why. The rejected
half is the part worth keeping.

## Ownership: PUID, PGID and UMASK report, they do not apply

**Built:** the readout. Which user and group downloaded files land as, under
which mask, per configured folder, plus the same figures in the diagnostics
bundle and one line at boot.

**Not built:** an entrypoint that makes those variables take effect.

`Dockerfile` declares `USER knight`, on purpose. Honouring PUID and PGID means
starting the container as root and dropping privileges in an entrypoint, which
turns a deliberately non-root image into a root-started one, and it needs a
first-ever docker build job in CI to prove the result. That is a separate
decision on a separate day.

Two consequences are load-bearing and must not be "tidied":

- There is no per-variable "did this take" flag, because this build could not
  answer one honestly. `applied.puid = true` could only ever mean "the value you
  set happens to equal the uid you already had", which reads on screen as "PUID
  works". One `envRead: false` says the true thing once.
- No string in the interface may imply that setting PUID would change anything.
  `settings.owner.envIgnored` means: the variable is set, and nothing in this
  image reads it.

`routes_fileowner.go` carries a test that reads this repo's own `Dockerfile` and
fails in BOTH directions: while `USER knight` is there and `envRead` is true, and
when `USER knight` is gone and `envRead` is still false. So the day somebody does
take the entrypoint decision, that test tells them the copy has to move with it.

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

## yt-dlp: fetch and verify, never install unattended

**Built:** a button that fetches a newer yt-dlp, verifies its checksum, proves it
RUNS on this machine, and only then swaps it in. Plus an opt-in "ask GitHub when
this page opens" toggle that ships off and only ever checks.

**Not built:** an automatic install.

Replacing the extractor unattended silently changes what downloads produce, and
yt-dlp does ship regressions. `internal/update/update.go` already refuses the
same step for KnightLoader's own binary, and the same line holds here.

A fetched copy DOES outrank an explicitly set `KL_YTDLP`, which is the one place
this feature overrides an operator's own setting. The reason is that the
Dockerfile pins `KL_YTDLP=/usr/bin/yt-dlp` on every container, so the other
answer would make the whole button a silent no-op in exactly the situation the
button exists for. It is visible and reversible: the card states plainly that
`KL_YTDLP` is not being started and offers "back to the system copy".

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

## The start report only looks

The boot pass stats folders and writes nothing anywhere, the data directory
included. The write test happens only on a human press, and even then only into a
folder that already exists; nothing is ever created.

A boot-time probe file in every configured folder wakes a spun-down array disk on
every container restart. That is the whole reason "the folder is there" and "this
instance can write in it" are two separate claims in the report rather than one:
a row that said "ok" without saying which would be claiming a test it did not
run.
