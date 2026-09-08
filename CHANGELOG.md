# Changelog

All notable changes to KnightLoader. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Versioning

Three things here are installed separately and upgrade separately, so they are
versioned and tagged separately. An APK on a phone does not change when a
container is pulled, and a browser extension does not change because a server
did - tying them to one number would mean either bumping it for changes it does
not contain, or not bumping it for changes it does.

| What | Version lives in | Tag |
| --- | --- | --- |
| KnightLoader itself (server, web UI, desktop) | the tag | `vX.Y.Z` |
| Android app | `mobile/app.json` (`expo.version` **and** `expo.android.versionCode`) | `mobile/vX.Y.Z` |
| Browser extension | `extension/src/manifest.json` | `extension/vX.Y.Z` |

Both are released at `mobile/v1.0.0` and `extension/v1.0.0`. KnightLoader
itself is released at `v1.0.0`.

The extension's earlier 1.1 and 1.2 were numbers in `manifest.json` that were
never tagged or published, so its first release folds them in rather than
starting at a version nobody ever had.

Each tag runs its own workflow and no other: a `*` in a GitHub ref filter does
not cross a `/`, so `mobile/v1.0.0` is invisible to the bare `v*.*.*` pattern
and the reverse. Each workflow refuses a tag whose version does not match the
file it claims to describe.

`versionCode` matters as much as the version string: Android decides upgrade
order by it, and it must go up on every build you hand anybody, even when the
version name is unchanged.

The copy of the extension most people run does not come from its tag. Settings
> Browser & App serves a zip built from the copy embedded in whatever server
binary is running, so that one tracks the server. The tag exists for a store
submission and for a fixed download.

## [Unreleased]

### Added

- **The log is readable, searchable and can be kept on disk.** The last 500
  lines have gone into the diagnostics bundle for a while and could be read
  nowhere else. The Diagnostics page now searches them, filters them by which
  part of the app wrote them, and follows new ones as they arrive, with a cursor
  rather than a refresh: a poll asks only for what is new, and when more lines
  arrived than memory holds the view says so instead of joining the two halves
  silently. Beside it, an optional log FILE on disk with a size cap and a few
  older generations to download. It is off, it stays off through an update, and
  switching it on writes the lines already in memory into the file first, so the
  boot that went wrong is in there rather than only what happened after somebody
  noticed. There is no path box, deliberately: the folder sits beside the
  database and `KL_LOG_DIR` moves it, because a mistyped path is the one way to
  stop a log with nothing on screen to say why.
- **Every download's own log lines, in its detail panel.** Honest rather than
  complete, and it says which: most of this app's log lines record no download
  at all, so an empty card there is a fact about the logging and not about the
  download. It reads a route under `/api/diagnostics`, which is forwarded to no
  peer and to no relay - a log line can carry the address of a feed with its key
  in it, which a task list never does.
- **The filter is by SOURCE and not by severity, and the card says so.** Nothing
  in this tree records a log level: every line is a bare `log.Printf`. A menu
  offering Info, Warn and Error would have to guess one out of the wording, and a
  guess dressed as a level hides lines from whoever trusted it. What the lines do
  carry is the name of the part of the app that wrote them, so that is what the
  picker offers, read off the server rather than copied into the browser.
- **A download can be opened and read.** Double-clicking one has shown its
  folder, its password and its connection count for a long time; beside that
  card there is now a read-only panel with the address, the page it came from,
  the hoster, the backend, every timestamp, how many attempts have been made,
  the current failure with its cause, the rules that matched it as links
  straight into the rule editor, and a small player for a finished file. Two
  things it deliberately does not show, because the server cannot answer them:
  a "started at" time, which is not stored anywhere, and a "2 of 5" retry
  ceiling, which depends on the per-host rule, the per-failure rule and the
  global maximum together and would be wrong the first time somebody adds a host
  rule.
- **A search over every settings page.** 22 pages whose set and order come from
  the server, and no way to find anything in them. The index is built from the
  page registry and the translation keys, so it is right in all 42 languages
  with no extra work, and two CI checks keep it from rotting: one fails when a
  page draws a caption the index does not carry, the other when the index points
  at a row no page draws any more.
- **Search, filters and facets survive leaving the page**, and can be saved
  under a name. The column layout and the sort order have been remembered for
  months while the search box was emptied by every trip to the settings. Named
  views go in the same document, with no new table.
- **Notifications, per event.** In the app, as a notification from your
  operating system, or nothing at all, decided one event kind at a time. The
  browser is asked for permission on the first switch somebody turns on and
  never on page load. Quiet mode still applies on top.
- **How much room is left on each target folder**, with three facts kept apart
  that are easy to blur: a folder that does not exist yet is measured at the
  nearest folder above it and says so, some systems cannot be asked at all and
  then the numbers mean nothing rather than zero, and what the queue still has
  to write is never subtracted from what is free.
- **A volume curve and a monthly cap**, aggregated per host and per backend out
  of the history table, which has carried the raw material since the first
  release and had nothing able to add it up. The cap has its own reset day and
  can report, pause the queue or throttle everything.
- **Your own request headers for one site, and your own sign-in cookies for
  yt-dlp, can finally be managed.** Both were built, encrypted and completely
  unreachable: they are sealed in the credential store on purpose, so that a
  cookie or a token can never land in a diagnostics bundle, and sealing them was
  the half that got done. Now there are routes and a page for each. **A stored
  value is never shown again** in either: a profile is its site plus the header
  names it holds, and a cookie jar is a site plus the word "stored". Editing a
  profile you already have keeps every value you do not touch, which is the one
  thing that had to be right, because an empty value means "delete this header".
- **A feed subscription says how it is doing, and can be tested before it is
  saved.** Whether it is being checked, when it was last checked, why the last
  check failed, whether the first run has happened and how many entries it
  recognises again. The test button fetches the address once and shows the
  feed's name and its first entries, each marked taken or left by your title
  filter. **It stages nothing and remembers nothing**, which is what makes it
  usable on an address you have not saved: a title filter is a pattern typed
  against titles nobody has seen, and this is the only way to see them.
- **A Packagizer rule can file links into a category.** The drawers were
  buildable last wave and nothing could put anything in one, because the rule
  editor had no such action. The grammar and the control had to land together: a
  grammar entry on its own would have put an accept/reject switch on the
  Packagizer tab wearing the word "Category", and there is now a test that reads
  the editor's source from Go and refuses to let that happen again.
- **A category's unpacking switch, collision rule and queue position are read.**
  Until now only its folder was. The queue position is written once, when the
  link arrives, and never again, so a download you have dragged up the list
  stays where you put it. The speed limit is still stored and still does
  nothing, and its own hint says so: this build has one limiter for the whole
  app, shared between the backends by measured demand, and there is no
  per-download allowance to write a number into.
- **Twelve features that were finished but unreachable now have controls.** Each
  of them worked, was tested and shipped, and could only be set by editing
  `settings.json` by hand or by calling the API, which is a feature nobody has.
  They are: the **working folder** (where bytes are written while a download is
  still arriving, so an Unraid mover or a library scanner never meets a half
  file), the **move-after-unpacking target**, the **three free-space floors**
  (a per-download reserve, a "start nothing below this" line and a "stop
  everything below this" line), **standing-still detection** with its timeout,
  automatic restart and restart cap, **per-hoster exceptions** to the connection
  counts and the retry backoff, **RSS and Atom subscriptions**, **categories** as
  a page of their own, the **name-conflict rule** for downloads and how many
  numbered names it may try, the **collector's** countdown and what it does with
  a link you already have or one already known dead, **when two addresses count
  as the same file** plus keeping and switching to the copy, **how much a file
  found on the disk has to prove** before it counts as the download, and the
  whole of **yt-dlp's** remaining configuration: audio format, bitrate and
  spoken language, what gets written into the file (metadata, thumbnail,
  chapters, subtitles, a Kodi NFO), the ffprobe pass over the finished file,
  livestream limits, the cookie switch, and per-hoster defaults.
- **A Kategorien page.** A category is a folder, a queue position, an unpacking
  switch, a speed limit and a collision rule under one name you pick once,
  instead of five answers given again for every batch. Every field left empty
  means the category has no opinion about it and the level above applies, so a
  half-filled category only changes what you filled in. Be aware of what is not
  built yet, and the page says so rather than implying otherwise: only the
  FOLDER is read by this build, and nothing but a Packagizer rule can put a
  download into a category, because the rule editor has no category action yet.

- **A playlist arrives as one row per video** instead of a single task. The
  entries are read from the playlist page alone, without touching any of the
  videos, so a fifty-video list costs one request rather than fifty; every video
  is its own row, with its own progress, its own tick box and its own failure,
  and they all land in one package named after the playlist. A video already in
  the list is folded away with a reason, the same as a link pasted twice. The
  switch is the one that was already there, "Download the whole playlist when a
  link points into one" under Settings > Resolvers: off still means the link is
  the one video it points at. At most 100 videos are staged from one playlist,
  because each of them brings the five variant rows of its own family with it,
  and a channel with ten thousand uploads would otherwise be fifty thousand rows;
  a longer list is cut and says so, with both numbers, in the skipped-links
  notice.
- **Sonarr and Radarr can use KnightLoader as their download client.** It
  answers at `/api/sabnzbd/api` in SABnzbd's own shape, because that is the one
  download-client protocol of the four the *arr apps ship whose credential is an
  API key rather than a session login, and it is six calls rather than fifteen.
  The key is one of this instance's own API tokens; set the client's URL Base to
  `api/sabnzbd`. **Off by default**, and switched on under Settings > Access: an
  interface that can create downloads and delete files does not stand open
  because a default said so, and it refuses every call without a token even on
  an instance with no password. What the *arr apps upload is scanned for links
  the way a paste is, so a DDL indexer works; a real `.nzb` is refused with that
  reason rather than accepted into a download that could never start, because
  there is no Usenet backend here to fetch articles with.
- **Your own servers as a source: FTP, FTPS, SFTP and WebDAV.** A seedbox, a NAS
  or your own Nextcloud is now a link like any other. `ftp://`, `ftps://`,
  `sftp://`, `webdav://` and `webdavs://` are staged with the name and size
  read off the server first, and a plain `https://` link is claimed as WebDAV
  only when an account exists for that exact host, so nothing ordinary is taken
  over. A link to a folder becomes one task per file inside it, subfolders
  included, the way a torrent's file list already did. Paused downloads
  continue where they stopped, by `REST` over FTP, by offset over SFTP and by
  HTTP range over WebDAV; a server that cannot do it fails loudly rather than
  quietly appending a second copy of the file to the first. Logins live in the
  encrypted account store under the server's hostname, never in the link, and a
  password written into a URL is refused rather than saved to the task list in
  plain text. An SFTP host key is remembered on first use and has to match after
  that.
- **A grip to drag the priority order by**, and the list moves under the pointer
  while you drag rather than jumping when you let go. The two arrow buttons are
  gone; the grip is a real button, so the arrow keys still move a row for anyone
  without a pointer.
- **An easter egg on the sidebar mark.** Press and hold: the blade draws out of
  the rail with a highlight running along its edge, and letting go swings it,
  with a wave that passes down the entries below. A short click still goes home.
  It follows the motion setting and falls back to a glint under reduced motion.
- **One speed limit instead of three.** The configured value used to be handed
  whole to each of the three things that move bytes, so somebody who set 10 MB/s
  and had the engine, JDownloader and yt-dlp all working got 30. It is now split
  between the ones that are actually downloading, by what each is pulling, and
  re-adjusted every few seconds; a meter using less than its share hands the rest
  to the ones that are saturated. A single working backend still gets the whole
  limit, and "unlimited" stays unlimited.
- **A queued download says why it is not running.** The dispatcher always knew:
  the slot count is full, this host is at its own ceiling, the account behind
  the only backend that claims the link is benched, the queue is stopped. It
  threw the answer away, so ten queued rows all read "waiting" and telling four
  completely different situations apart meant reasoning about the settings page.
  The reason is recomputed on every pass, so one that stops applying disappears
  by itself.
- **Multihosters are marked as such in the hoster picker.** Nineteen of the
  services JDownloader knows unlock other hosts rather than hosting files, and
  KnightLoader has no backend of its own for any of them, so JD is the only way
  to use them and the picker is the only place they can be set up. They are
  labelled rather than hidden. The list is kept by hand because JD's API cannot
  answer the question: `getAccountInfo` returns an empty `infoMap`, and nothing
  else distinguishes the two kinds.
- **A parity check for the settings pages** (`web/check-settings-pages.mjs`, run
  by CI). The rail and the command palette are two hand-kept lists of the same
  page set, and three pages had quietly drifted out of the palette.
- **The download list is a table you can arrange.** Every column is resizable,
  including the name; the last one stretches to the right edge, so no empty
  strip sits beside it. Progress moved to the far right and its bar is thicker
  and takes the shape setting's own corner.
- **Hoster logos** in the list's own Hoster column, beside the name, from the
  same self-cached icon the account picker draws.
- **A "do not ask again" switch on every confirmation** that can carry one, with
  a card under Settings > Appearance that lists what has been silenced and turns
  it back on.
- **Dragging a link into another package** moves it there instead of snapping
  back. Reordering inside a package was already possible.
- **Linksnappy and Offcloud** as debrid services too. Neither vendor publishes
  an API reference any more, so these two rest on what their live endpoints
  answer plus a working open-source client, and each file says so in place of a
  documentation link. Both decode loosely: an answer they do not recognise
  fails with a sentence, and Offcloud's undocumented site list simply claims no
  hosts rather than claiming hosts it cannot unlock.
- **Debrid-Link and Premiumize.me** as debrid services of their own, with the
  supported-host list, the direct link, the plan and the remaining allowance
  each of them publishes. Debrid-Link in particular could not be used through
  the JDownloader sidecar at all: its login is an OAuth device confirmation
  nobody can answer, because KnightLoader never shows JD's interface. A private
  API key has no such step.
- **An on/off switch on every hoster login**, beside the one the debrid rows
  already had. Off takes the account out of JDownloader's own list and keeps the
  credential sealed here, so switching it back on needs no password retyped.
- **"Move to package" in the right-click menu**, where JDownloader keeps it. The
  dialogue is the one the selection row's folder badge already opened.
- **The remaining allowance for services that meter in a percentage** rather
  than in bytes, so the column stops being blank for them.
- **The priority order is arrangeable.** Drag the services on the Accounts page
  into the order you want them asked in, or move a row with the two arrows
  beside it. A hand-made order beats every automatic one, including the boost a
  confirmed hoster login earns JDownloader; "Automatisch" throws it away again
  and follows the automatic ladder as it changes. The card also explains what
  the list IS - every road a link can take, which is why TorBox and yt-dlp are
  in it together - and it now shows the order the downloader actually walks
  rather than the registry's own registration-time one.
- **A "Linkeingang" card** in Settings > General for the two ways a link arrives
  without being pasted: Click'n'Load, and a clipboard watch that stages every
  link you copy. Both are also two badges in the collector's own button row.
  The clipboard watch is offered only where the browser will allow it - over
  HTTPS or on localhost - and says so where it will not; Ctrl+V into
  KnightLoader needs neither and works everywhere.
- **A plan column and a remaining-allowance bar on both account cards.** Free
  and premium are told apart from what JDownloader answers about the account
  (`validUntil`, `trafficMax`), which it could always say and was never asked.

### Changed

- **The Beschriftung setting reaches the whole app.** It used to draw the sidebar
  and the settings rail and nothing else, so it read as a sidebar option rather
  than as a rule. The head card's buttons, both list toolbars and the collector's
  own buttons now follow it too. Note that the default is "Symbol und Text", so
  those controls show their labels out of the box; "Nur Symbol" restores the
  square glyph badges everywhere.
- **Every dropdown is a GlimStone control.** The `.glim-select` rule had been in
  the stylesheet since the port and not one `<select>` in the app used it, so
  eight of them were still drawing the platform's own widget chrome.
- **The plan column is text, and the allowance is one line.** The plan was a
  filled, uppercased chip in a column of ordinary words; the allowance stacked a
  bar over its figure and cost every row in both tables a second line. Allowances
  are quoted in GB throughout, the unit the vendors themselves advertise.
- **A service on an unlimited plan says how much has gone through it** instead of
  showing an infinity symbol and nothing else.
- **The sign-out button is only in the sidebar now**, and above Settings rather
  than under it. The copy on the password card is gone.
- **"Skip the collector" and the watch folder moved to the Linkeingang card** on
  the General tab, beside Click'n'Load and the clipboard watch. Four independent
  proposals for restructuring the settings were weighed and this was the only one
  that survived; everything else cost more in search words and bookmarks than it
  bought.
- **Names that say what a tab is.** "Sammler" is the "Linksammler", "Zugang" is
  "Passwort & Fernzugriff", "Hoster-Logins" are "Hoster-Konten", and everything
  about corners, colours and motion moved out of the General tab into a new
  "Aussehen" tab of its own, with the About card taking the last place on
  General.
- **Debrid services no longer appear in the hoster list.** They have their own
  card, and appearing in both made the same account look like two.
- **Click'n'Load is on out of the box, in the container too.** The image set
  `KL_CNL=0`, which is why the switch read "off" on every container install with
  no way to tell a choice from a default. It still binds `127.0.0.1` only, and
  deliberately not the LAN: the protocol carries no authentication at all.
- **The schedule could undo a hard stop.** The timetable runner reads what the
  queue should be doing, lets go of the lock, works out the answer and only then
  applies it, so a "stop everything" that landed in that window was overwritten
  by a reading taken before it happened, and a waiting download was handed the
  slot the stop had just emptied. The button read as broken. It surfaced as a
  test failing about one run in twenty, on this commit and on every one before
  it; the test for it now writes the interleaving out by hand instead of racing
  for it.
- **A debrid service with `www.` in its catalogue link was still offered as a
  hoster login.** The filter compared `www.premiumize.me` against JDownloader's
  `premiumize.me` and never matched, so a service with its own card could be
  configured twice, two different ways.
- **A help link pointed at a settings page that never existed** (`/settings/general`),
  and an unknown page id routes silently to Downloads, so the link had always
  landed somewhere else with no error anywhere.
- **The command palette could not reach three settings pages.** Accounts,
  Instances and Appearance all had components and no command; the comment there
  still explained why one of them had none.
- **Dragging a folder onto a row of a different priority now moves it there.**
  It used to do nothing, silently, and only for that case - which on a real
  list, where folders rarely all share one priority, is indistinguishable from
  drag-and-drop being broken. The dropped rows take the priority of the row they
  land on, because a list ordered by priority cannot honour the drop otherwise,
  and a message says so. The drag preview also stops snapping the aim onto the
  nearest same-priority row several places away.
- **The head card is a reading, not a page of prose**: the counters strip, the
  account chip and both explanatory sentences are gone; the speed curve moved to
  the trailing edge and is as tall as the card. Nothing on it is clickable any
  more, which is what the hamburger beside it is for.
- **One row for every list action**, the way the collector already worked - quick
  filters, search, the selection verbs and the page-level badges, all above the
  card instead of stacked in three rows around it. The search opens as a popover
  under its badge.
- **The status column says what it means in each list**: "Verfügbarkeit" with a
  green or red dot in the collector (amber on a package whose links disagree),
  and the transfer state with its own glyph in the download list.
- **A selected row is visibly selected** - the mark is the theme accent and an
  edge, and it survives the rainbow wash that used to paint over it.
- **The quick settings read as sentences**: one field per row, each named after
  what it counts, each with its own bubble.
- **Relay and access texts rewritten**, including four distinct sentences for the
  connection state (project relay, own relay, no relay at all, and no contact
  with a relay that is configured).
- **A media site goes to yt-dlp even when a debrid service also covers it.**
  TorBox's host list includes streaming sites and TorBox outranks yt-dlp, so on
  an instance with a TorBox key a YouTube link was fetched as one nameless file
  instead of becoming the five rows with a quality to pick. TorBox keeps those
  sites only when yt-dlp is not running at all.
- **The site icon is looked for the way a browser looks for it**: the page's own
  `<link rel="icon">` declarations first, including ones on a different host,
  then the two well-known paths, and the largest image found wins. Sites whose
  icon sits on a CDN had no logo at all before, and a 16-pixel favicon.ico was
  taken over the 180-pixel icon beside it.
- **yt-dlp downloads in parallel fragments and chunked ranges**, which is what
  the speed difference against JDownloader on the same video was. A speed limit
  is divided across the fragments, so the cap still means what it says.
- **The Debrid and Hoster cards say what they are for**, including what a debrid
  account is and why it is the recommended way; the notes on Comment and Unpack
  archives were rewritten in plain words.
- **No explanation bubble on the collector and list card titles.** The one
  explanation the table still needs, how to sort and where the column menu is,
  moved from the header row onto the card's own title badge.
- **No "Enabled" column in the download list.** It is the collector's own "take
  this along when I press start"; once a link is in the queue the switch that
  means something is pause.

### Fixed

- **yt-dlp's own explanation was being thrown away before anything read it.**
  A failure kept the last line of its output and then the last 200 bytes of
  that line, and the line that says "sign in to confirm you are not a bot" is
  about 400 characters long. So the one piece of evidence worth having was
  destroyed on the way in, and every such failure arrived as a truncated
  fragment. The whole buffer is kept now and read for six causes that call for
  six different answers: a bot check, members only, geo-blocked, deleted, DRM,
  and an extractor that has stopped working.
- **A test double could take the whole test suite down with it.** It closed its
  channel on every call, so a second probe was not a failed test but a panic,
  which aborted every remaining test in the package and printed a stack naming
  neither the test nor the URL. It arrived exactly that way: green through three
  full runs here, red once on CI, with nothing in the output to say what had
  happened. The second call is now reported with its URL and the run continues.
  Why anything probes twice is still open; the evidence just stops being
  destroyed.
- **A refused folder named the wrong field.** One validator checks five
  different folders (the download folder, the working folder, a category's, a
  batch's and a single download's) and every one of them reported "the download
  folder must be an absolute path". So the field that failed was the one field
  the message did not name, and typing a relative path into the new working
  folder sent people off to fix a download folder that was fine.
- **A queue held back by the free-space floors said "all slots busy".** The disk
  guard's own reason was the only one of the nine with no word for it, so the
  list showed a different and wrong explanation, and the obvious thing to do
  about it, raising the concurrency limit, would have changed nothing.
- **A category you added and had not named yet vanished on save**, with the
  refusal shown in English on a German page. An unnamed row now waits on the
  page instead of being sent, and joins the list the moment it has a name, which
  is the moment the server would accept it.
- A link switched off in the collector is no longer started by "start
  everything"; it stays where it is and the toast says how many were passed over.
- Progress bars no longer animate while the queue is stopped. The looping bar
  now means "bytes are moving", not "this row has no size yet".
- A container's own crawl verdict is kept, so a freshly opened DLC shows online
  or offline per link immediately instead of staying grey until a manual check.
- The variant pickers are the app's own menu rather than the operating system's
  widget, which no stylesheet could reach.
- The audio menu offers AAC, ALAC and Vorbis where the source carries them, and
  the video ladder covers 144p to 4320p.
- The remote-access card follows the relay's state live instead of only after a
  reload, and the address field accepts a bare host name.
- Folders can be dragged in the download list. The queue accepted the move all
  along; the list drew its own order with a comparator that contradicted itself
  as soon as one link in a folder had failed, so the folder stayed anchored to
  its dead link.
- A media link no longer lands in a folder named after the URL's path while its
  title is being fetched. It stays ungrouped for those few seconds and then
  takes the video's own name, or the old guess if the probe fails.
- The "handed to JDownloader" bar in the collector stops when the container's
  links actually arrive, instead of sweeping until the handover expires, and it
  runs the full width of the card.
- The "new account" dialogue no longer offers the captcha solvers, which are
  configured on their own settings page and never appeared on this one.

## [1.0.0] - 2026-09-02

The first release of KnightLoader itself: server, web interface and desktop
build. The app and the extension are versioned separately, see Versioning
above.

### Added

- **Collector** that stages links before anything downloads, with name, size,
  availability and the backend that will take each one. A link no backend
  handles is still shown, with the reason attached, and can be rechecked
  without re-pasting.
- **Crawling**: a pasted page becomes the files it links to. Only a link no real
  backend recognised is opened, so a plain download costs no extra request.
- **Resolvers** for direct links, TorBox, AllDebrid and Real-Debrid, yt-dlp, and
  a headless JDownloader as the catch-all. When a backend says a link is not its
  business, the next one gets a turn.
- **Scheduler** with global and per-host concurrency, priorities, manual queue
  order, and automatic retries with a growing delay.
- **Speed limit** that applies to everything and takes effect on downloads
  already running. The embedded engine has no rate-limit hook, so its traffic
  goes through a loopback proxy where the bytes are metered.
- **Download folders**: a global folder, an optional per-package subfolder, a
  per-task override, and path templates such as
  `/downloads/<jd:date>/<jd:hoster>/<jd:packagename>`.
- **Extraction** of zip, rar including multi-volume, 7z including split volumes,
  tar, gz, bz2, xz and zst. A multi-part set waits for every part before it
  opens. Encrypted rar and 7z take passwords, tried per task first and then from
  a configured list.
- **Checksum verification** against an `.sfv`, `.md5` or `.sha*` that arrived
  with the batch, or a CRC in the file name.
- **Click'n'Load** from a site's own button, including the preflight modern
  browsers require before a page may reach a loopback address. The same binary
  runs as a bridge for instances that are not on the browser's own machine.
- **Watched folder** for `.txt` and JDownloader `.crawljob` drop files, carrying
  package name, destination and archive password.
- **Multi-instance federation**: register other KnightLoaders and drive them all
  from one dashboard. Instances on the same network announce themselves over
  multicast and are one click to add, with nothing configured. Two that cannot
  reach each other directly meet through a relay you host yourself.
- **A twelve-word connection phrase.** Read it off one instance, type it into
  the next, and they find each other across networks - no account, no login,
  no port forward, no domain, and no third-party site to visit. The words are
  BIP39's, the list hardware wallets use, chosen there for the properties that
  matter here too: no two words share their first four letters, none are
  near-homophones, none carry accents. So a phrase survives being read down a
  phone line and typed on a mobile keyboard, and its checksum refuses a
  mistyped or swapped word on the spot - naming the word and its position -
  instead of letting it become a connection that silently never finds its
  sibling. The relay is told `SHA-256(domain || secret)`, never the secret, so
  whoever runs one cannot reconstruct anybody's words; showing a phrase again
  needs the instance password re-entered, because a session opened hours ago
  is not evidence anybody is still sitting there. Run the relay yourself and
  the same phrase works against it.
- **The relay cannot read what it forwards.** A second key comes out of the
  same secret under its own domain, and every proxy frame is sealed with
  AES-256-GCM under it. A relay sees which instance a frame is for and which
  request it answers, because it routes on those - not the path, not the body,
  not the API token a phone attaches. The two domains are the point: a relay
  is handed the group key in every hello frame, so a frame key derived from
  that would be one it already holds. Routing fields are bound into the seal,
  so a frame cannot be redirected and still open. A relay configured by
  hand-entered key instead of by phrase seals against everything between the
  instances and the relay, but not against its operator, who holds that key.
- **The card explains itself before it asks anything.** Twelve words is an
  odd enough thing to be handed that "what am I looking at" comes before
  "what do I press", so connecting opens with three numbered steps and a
  paragraph on what actually happens when you press them. The button names
  inside the steps come from the buttons' own translation keys rather than
  being written into the sentence, so a step cannot end up quoting a label
  that says something else in that language.
- **Getting the app lives with getting the extension.** Both answer the same
  question - how do I reach this from somewhere that is not this browser tab -
  so the app card moved onto the tab that already held the bookmarklet and the
  extension, now called Browser & App. The store badges are the real artwork,
  with a direct APK download beside them, which is the one of the three routes
  that works before a store listing exists.
- **Instances can be hidden from the sidebar** the way Accounts already could,
  through a settings tab of the same shape - useful for anybody running the
  one instance, whose Instances page lists exactly itself, forever. Each
  instance's card now carries the app's mark down its left edge.
- **The unprotected-instance warning stopped being a banner.** It fired on
  every load of every container, because a container binds every interface in
  its own namespace by design, and it was saying what the password card three
  centimetres above it already said. It is now a second line on that card, in
  the warning colour, next to the field that fixes it.
- **Tailscale is gone.** It had been in this card since before the relay
  existed, when it was the only way in from outside, and merging the cards
  moved it rather than removing it - so the page whose whole point is not
  needing a third-party login kept offering one. Nobody ever had to use it;
  the phrase never touched it. Its one unique job was handing out a public
  address a stranger's browser could open, and the answer to that is now your
  own domain in front of a reverse proxy. Self-hosting the relay moves to
  Settings → Advanced (`relayUrl`, `relayServe`), which lists every setting
  this instance has.
- **The relay gets its own certificate.** Set `KL_RELAY_DOMAIN` and it
  terminates TLS itself over TLS-ALPN-01 - no reverse proxy, no certbot, no
  renewal cron, and no port 80, because the challenge completes inside a
  handshake on 443. Repeated failed handshakes from one address back off, so a
  relay on a public address is not a free guessing gallery.
- **Being in the group is the credential.** A request arriving over the relay
  came off a socket the relay only joins to connections presenting the same
  group key, so the sender has already proved it holds the phrase - and a
  password-protected instance accepts its own siblings instead of answering
  401 to all of them. A pairing code used to be what closed that gap, one peer
  at a time; it is gone, and nothing replaced it because nothing needs to.
  What a sibling may reach is an allowlist rather than a property each route
  happens to have: tasks, links and the queue, plus reading the auth state,
  the peer list and the instance's own accent. Not the settings, not the
  accounts, not the phrase.
- **The phone joins the group too.** Twelve words, and every instance appears
  at once - where it used to want a relay address, a relay key and then an API
  token per instance, and saved one instance per visit. It decodes the phrase
  itself, because the case the phrase exists for is the one where there is no
  server to ask.
- **The Android app asks for four fewer permissions**, and the four it dropped are the reason Play Protect blocked the install: `SYSTEM_ALERT_WINDOW` ("draw over other apps"), the microphone, biometrics and external storage. None of them came from this code. expo-camera brings the microphone along because it can also record video, React Native brings the overlay permission for its own developer overlay, and expo-secure-store brings biometrics for an option that is not used here. A download manager asking for the microphone and permission to draw over other apps looks like malware, and Play Protect was right to say so. What remains is camera, internet, and network and Wi-Fi state.
- **One instance can be the relay**, from a switch on the Access tab, instead of
  a second program on a second address. It answers under `/relay/connect` on the
  address that instance already uses, behind the same reverse proxy and the same
  certificate, and it admits only the relay key that instance stores - so
  turning it on does not make a published address a meeting place for whoever
  finds it. Off, the route answers 404, exactly as a build without the feature
  does. What it cannot change is the one thing a relay needs: it is the third
  point both sides dial out to, so the instance hosting it has to be reachable.
- **One card for connecting.** It used to be several, each showing a different
  piece of the plumbing to somebody who came for an answer. There is one now,
  named for the two things it does - connect your instances, and reach this one
  from anywhere - and it opens with the answer rather than the machinery: one
  sentence saying whether the group is up, then the phrase. Everything that was
  a third and fourth way to arrive at the same place is gone rather than folded
  away, because a fold is still a thing to wonder about.
- **The app and the extension follow GlimStone**, the design language the web
  UI already speaks: the same palette, the same corner shapes, the same
  Sunflower-gold accent before anyone touches a picker. The app takes its look
  from the instance it is connected to, so opening the app and that instance's
  web interface side by side shows one product rather than two opinions - with
  a local override for anyone who wants a different colour on their own phone.
- **Rainbow in the app**: a long list of downloads reads as distinct rows
  instead of one gold wall. Shown rather than set, because the palette offset
  lives on the instance - two clients of one server disagreeing about the
  colour of a download is a bug, not a preference.
- **Problems?** in the app's settings and the extension's options: the version,
  the platform and the shape of the configuration, copied in one tap or opened
  as a prefilled report. No address, no token and no relay key is in it - an
  address is somebody's home network and a token is a credential, and both
  would otherwise be pasted into a public issue by anyone who trusted the
  button.
- **Connect from anywhere**: the Android app finds servers on its own network
  and fills the address in; the browser extension can send to a peer that has
  no address of its own, by routing through an instance that does; the desktop
  build finds and adds instances even though nothing can dial it back. See
  [docs/connecting.md](docs/connecting.md).
- **Access control**: an optional password lock with signed session cookies,
  off by default.
- **The download list is a real table**: sortable columns you choose, packages
  drawn as rows of their own with a folder glyph and a triangle that collapses
  them, and a right-click menu on packages and files. Which packages are folded
  survives a reload.
- **Settings** as thirteen sub-pages behind one shell, with the set and the order
  coming from the server so the tab bar, the modules page and the index all read
  one list. Among them a **module registry** whose switches really do switch a
  subsystem off, a **connection manager**, and an **Advanced** page generated
  from the Go configuration struct by reflection, so a new setting cannot be
  added without becoming visible.
- **Rules**: the Packagizer and the link filter in one editor, and a holding area
  that keeps what a filter caught instead of discarding it, so a rule that was
  too broad can be seen and undone.
- **Reconnect** in four methods: run a program, replay recorded HTTP requests,
  UPnP (which needs no router details at all, because it asks the network where
  the gateway is), or run a script through a named interpreter. A recorded
  LiveHeader `[[[HSRC]]]` script imports directly, reporting which of its blocks
  mapped alongside which did not. The automatic reconnect fires only when a
  backend itself asked for a later retry, never while the queue is halted, and
  never more than one at a time.
- **Native desktop applications** for Windows, macOS and Linux alongside the
  container. The window is a webview served by the very same HTTP handler the
  container serves, so the engine, the resolvers, the API and the interface are
  not a second implementation that can drift from the first. Every release tag
  builds all three and attaches them to the release.
- **42 languages**, each fetched only when chosen, right-to-left included.
- **GlimStone**, the design language the interface is built on, documented in
  `docs/design-language.md`. Its CSS prefix is `glim-`. The palette is IBM
  Carbon, the same values the sibling apps already use: a shared design language
  has to share the ground first, so GlimStone contributes the system rather than
  a second set of greys.
- **Adjustable corners** — round, soft or square — driven by one token, so the
  whole interface changes together instead of arriving half converted.
- **Adjustable accent** with eight presets and a free colour. The text placed on
  the accent is derived from its luminance rather than configured separately.
- **Rainbow accent**: a palette of eight hues handed out by position, so a long
  download list reads as separate rows. Position, not a hash of the task id: the
  hash kept a row's colour when the rows above it finished, which sounds better
  until three rows share eight buckets and two neighbours come out the same
  colour, which is the one thing the mode exists to prevent. It has a reactive
  mode that rests neutral and colours only what is hovered or running, an
  optional rotation of the starting hue, and all eight colours are editable.
- **Info bubbles.** An explanation now sits behind a neutral `(i)` beside its
  label instead of as grey prose under the control. It opens on hover and on
  focus, closes on Escape, and is rendered at document level so no card or
  scroll container can clip it.
- **BitTorrent and magnet links** as a fourth resolver alongside direct links,
  yt-dlp and JDownloader: selective per-file download, a seed-to-ratio-or-
  duration target, port mapping, and a private torrent's own metadata
  switching off DHT and peer exchange for it automatically, no toggle
  required.

### Security

- Removing a peer now ends its credentials. It used to delete a line and
  nothing else, leaving that peer a live, full-power API token indefinitely,
  and leaving this instance's own credential for it to be inherited by whatever
  was registered under the same name next.
- Nothing announced over multicast is trusted: fields are length-capped on
  arrival and the peer list is bounded, so a device on the network cannot grow
  it without limit.
- Adding a discovered instance exchanges no credentials, and the interface now
  says so instead of claiming otherwise. A password-protected peer added that
  way is reported as having refused, rather than as offline.
- The API no longer sends a wildcard CORS header and the WebSocket no longer
  accepts any origin, which together stopped another website from driving an
  instance through the visitor's browser.
- The Click'n'Load endpoints accept POST only. Answering GET made them a browser
  simple request, which any page could have used to queue downloads and archive
  passwords without the user knowing.
- The JDownloader provisioner fetches over HTTPS and checks the downloaded bytes
  really are an archive before executing them.

### Fixed

- A peer that REFUSES this instance is no longer shown as simply offline. That
  happens on its own whenever the other side sets or changes its password, and
  reported as offline it reads as a machine somebody unplugged - so the thing
  that would actually fix it is the last thing anyone would try. The status dot
  has a third state now, and it names the connection phrase.
- A YouTube link no longer lands in a folder called "watch" when its title
  arrives quickly. The name and the folder were decided by two things racing,
  and the one that lost left the guessed folder in place - so the fix worked
  only when the title took its time.
- An instance whose name is not plain ASCII, or is longer than 32 characters,
  can be addressed at all. The name is folded into one that works as a URL path
  segment ("Bürglers Keller" becomes "Burglers Keller"); before, the far side
  refused it as invalid, about a name nobody had typed and nobody could see.
- The browser extension keeps peers it cannot open a connection to, instead of
  dropping them silently and reporting "No new instances found" - the sentence
  it also showed for an empty list, a sign-in problem, an unreachable host and
  a timeout. Each of those now says what actually happened.
- The Android app's network scan reaches the whole subnet. It probed every
  address at once, and Android's HTTP client queues past 64 concurrent
  requests, so everything past the first batch timed out while still waiting
  for a slot - never sent, never answered. A server anywhere in a typical DHCP
  range was simply never found.
- Renaming an instance reaches the network immediately, rather than leaving
  every other machine showing the old name until the process restarts.
- An instance bound to loopback no longer announces a network address nothing
  serves.
- Removing a task no longer deletes what was downloaded. That was data loss on
  the ordinary "clear finished" path.
- Task IDs are checked for collisions before entering the map, where a duplicate
  would have silently orphaned a running download.
- A single compressed file unpacks beside its archive instead of into a folder
  named after the file it produces.
- A bracketed run of eight digits is no longer read as a CRC32, which had been
  stamping intact downloads as corrupt.
- A byte-order mark no longer eats the first link of a dropped text file.
- One slow WebSocket viewer no longer delays progress updates for everybody
  else.
- The embedded UI carries an ETag and revalidates, so a redeploy cannot leave a
  browser on a stale bundle.
- A private torrent's DHT and peer exchange refusal, and the seed-ratio,
  seed-duration and port settings on the Torrents page, are now actually
  carried into a running download - all four were previously saved and
  validated but never reached the torrent engine.
- A script started at the moment of shutdown can no longer outlive it. Asking
  "is the host still open?" and signing up to be waited for were two separate
  steps, so a run that slipped between them went untracked: shutdown returned
  while the script kept calling into an app being torn down around it, and the
  same gap could trip Go's own guard against that pattern and take the process
  down. The two steps are now one.
- The same gap is closed in the two other places it existed: the background jobs
  the app itself starts - an availability probe, a checksum pass, a dropped job
  file, the update that publishes a finished task - and the desktop build's own
  window and tray helpers. A shutdown now either waits for a job or refuses it
  outright, with nothing in between, so none of them can still be writing to a
  database or a window that has just been closed underneath them.
