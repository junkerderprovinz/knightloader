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

Each tag runs its own workflow and no other: a `*` in a GitHub ref filter does
not cross a `/`, so `mobile/v1.0.0` is invisible to the bare `v*.*.*` pattern
and the reverse. Each workflow refuses a tag whose version does not match the
file it claims to describe.

`versionCode` matters as much as the version string: Android decides upgrade
order by it, and it must go up on every build you hand anybody, even when the
version name is unchanged.

The copy of the extension most people run does not come from its tag. The App
page in Settings serves a zip built from the copy embedded in whatever server
binary is running, so that one tracks the server. The tag exists for a store
submission and for a fixed download.

## [Unreleased]

### Added

- **A manual at [junkerderprovinz.github.io/knightloader](https://junkerderprovinz.github.io/knightloader/).**
  Installing, what it does, configuration, getting links in, Click'n'Load,
  connecting instances and where files land, built from `docs/` with MkDocs
  and published on every change. The README keeps the overview, the
  comparison, the screenshots and a quick start, and its download row gains a
  Docs button after Source. The comparison table marks each cell with ✅, ⚠️ or
  ❌ like the other repositories' tables.
- **Browser extension 1.0.3: a build Mozilla has signed, for Firefox.** Firefox
  installs only signed add-ons, so the zip loaded there only until the next
  restart. Each extension tag has Mozilla sign the package on the unlisted
  channel, and the release carries the `.xpi` beside the zip. The README's
  Firefox button installs it with one click.
- **A download's backend can be chosen in its properties.** The Properties
  panel has a Backend dropdown with the services that can take every selected
  link, and Automatic, which leaves the choice to the priority order on the
  Accounts page. A paused download pinned to another service moves there when
  you resume it and starts from the beginning.
- **Every module this build runs has a switch on the Modules page.** JDownloader,
  yt-dlp, torrents, captchas, event scripts, outbound connections, peer
  instances, the Packagizer and the link filter can be switched off like the
  modules that already could. Most modules' own settings pages carry the same
  switch, and flipping either one moves the other straight away, in other open
  tabs too; JDownloader is switched on the Modules page only. A link whose
  backend is switched off waits in the queue as "Module switched off" instead
  of going out as a plain download, which would save the hoster's web page. If a debrid service carries the host, it takes the link
  over. A link that points straight at a file still goes to the direct
  download. Each row also links to the page its module is set up on.
- **Twelve more debrid services, spoken to directly**: BestDebrid, CocoLeech,
  CoolDebrid, DebridItalia, Deepbrid, FakirDebrid, Mega-Debrid, MultiUp,
  NeoDebrid, ProLeech, RPNet and Zevera. They used to work only as a login
  handed to JDownloader. Each has its host list, its account details on the
  accounts page and a key check before saving, and the form names the fields
  the service asks for, such as ProLeech's API user and key. Video sites such
  as YouTube stay with yt-dlp even where a service lists them.
- **Each connected hoster login is a row of its own on the priority card.**
  Where it sits against a debrid service that carries the same host decides
  which of the two gets those links. A login you add after arranging the order
  starts right below JDownloader, which is where its links go.
- **The video and audio rows of a yt-dlp link pick with two dropdowns each.**
  The video row picks a format, a container and codec the site offers, and
  beside it a quality, the resolution and the frame rate where tracks differ
  in it. Together they name one track, which keeps its own container and is
  not converted, where it used to be a height merged into mkv. The audio row
  lists the formats the source has, plus Auto for its best track, then under
  "Convert to" the formats it lacks, which yt-dlp converts to with ffmpeg.
  Beside it are the bitrates of the chosen format. The variant defaults on the
  yt-dlp page and the gear on a package in the collector offer the same two
  pairs, and a new link from that host starts with them.
- **hCaptcha challenges can be solved in KnightLoader.** JDownloader's
  hCaptcha challenges showed "This captcha cannot be shown here." and waited
  until they timed out. They now open hCaptcha's own widget, and the answer
  goes back to JDownloader the same way a reCAPTCHA answer does. A plain
  reCAPTCHA v2 got the same message unless it was an Enterprise or invisible
  one; it opens now too, and an invisible one starts by itself. Both widgets
  use the interface language. When a widget cannot load, because a blocker
  stops it or because the hoster tied its key to its own website, the captcha
  window says so instead of showing an empty box.
- **The folder picker can create folders.** "New folder" under the list makes
  an empty folder inside the one shown and opens it, so "Use this folder" takes
  it. A name no folder can have, such as one with a slash in it or a Windows
  device name like CON, is refused with the reason. Every folder field has the
  picker now: the working folder, the folder in a download's options and
  properties, a Packagizer rule's "Download folder" and the folder settings on
  the Advanced page joined the fields that already had it. For a download on a peer
  instance the field stays a plain box, since the picker would show this
  machine's disk.
- **Schedules can be suspended for a while** without changing them: for an
  hour, for three hours, until midnight or until you switch them back on.
  Meanwhile the queue follows your own settings, and when the time is up the
  schedules apply again by themselves. A restart does not end the suspension.
  The schedule status card shows it and can end it.
- **More in the quick settings**: the end-of-queue action, quiet mode, "Start
  added links immediately", suspending the schedules and "Reconnect now". Each
  row is the control from its own settings page and changes the same value, so
  both places show the same thing. "Reconnect now" is greyed out while no
  reconnect is set up or the module is switched off; its (i) says which, and a
  badge below leads to the page that changes it. The button on the Network page
  is called "Reconnect now" as well.
- **A Packagizer rule can say where the unpacked files go.** The rule editor
  has a "Move the unpacked files to" field with the folder chooser, next to
  "Extract automatically". For the links the rule matches it takes the place
  of the setting of that name on the Archives page, and variables work in it.
  Rules could carry this before, but only an imported rule set could set it.
- **Settings with a fixed set of values are a menu on the Advanced page.**
  What happens when unpacked files collide, what becomes of an archive, file
  name collisions, mirrors, duplicates, offline links, the trust level for
  files already on disk and what a restart resumes each offer their values by
  the names their own page uses. They were free text fields, and a typo was
  saved as the default without a word.
- **A field in the quick settings sets how far back the head bar's speed curve
  reaches**, from 10 seconds to an hour. It carries the name the Overview gives
  its own curve's window, shows seconds below a minute and minutes from there,
  and both speed curves label their time axis the same way. Up to two minutes
  the curve draws a sample a second, beyond that one every ten seconds, which
  is how the server records them.
- **Links and packages can be renamed from the right-click menu or with F2.**
  A finished file on this machine is renamed on disk at once, and a running
  download takes the new name when it finishes. A renamed package's folder
  follows the name only while nothing in the package has started. The window
  says so beforehand, as it does for a file that cannot be renamed at all: a
  torrent, a JDownloader download, one part of a multi-volume archive, or a
  file being unpacked. A refusal it cannot see coming, such as a part of an
  archive nothing has unpacked yet, is shown in your language as well.
- **A package can be paused and switched off from its right-click menu.**
  Pause, Resume, Switch off, Switch on, Hold and Release on a package header
  act on every link in the package, including rows a filter hides.
- **The README compares KnightLoader with JDownloader 2, pyLoad and
  rdt-client.** A table under the Overview sets the four side by side, one row
  for each thing people choose a download manager by, such as debrid services,
  torrents, captchas or a phone app. The text below it says where the others
  are ahead, pyLoad's own hoster plugins and JDownloader's Usenet support among
  them. JDownloader's entry in the License row says that a few of its parts
  are closed source and names its sources.
- **The desktop app for ARM64 on Windows and Linux.** Every release has
  Windows and Linux builds for ARM64 next to the x64 ones; the macOS build is
  still universal. The desktop card on the App tab gives the build that fits
  the computer when the browser says which one that is, and x64 when it does
  not. Buttons next to the tiles give the other architecture. In the README
  the Windows and Linux buttons say x64 and each has an ARM64 part of its own.
  An installed ARM64 app updates to the ARM64 build of the next release, and
  on Windows on ARM it fetches yt-dlp's own ARM64 build.
- **API tokens carry rights.** A token can read, add, control or administer,
  in any mix, picked as Full access, Add and read, Read only or Custom when it
  is made, and the token list shows what each one may do. Add and read is
  enough for Sonarr and Radarr, whose key travels in the address. A call a
  token has no right to gets a 403 that names the missing right, and every
  route in `GET /api/help` lists the right it needs. Picking the folder a
  download goes to needs Admin, and the Modules page warns when no token can do
  what the Sonarr bridge or `/api/metrics` needs. Tokens made before this keep
  every right, and so do browser sessions, siblings on the relay and paired
  instances.

### Fixed

- **Unpacking an archive again clears its last failure from the row.** The red
  error under its first file used to stay until the new attempt ended, next to
  a status that said it was unpacking.
- **A debrid download reaches the speed limit within seconds.** With a speed
  limit set, a download from a debrid service or TorBox started at 16 KB/s and
  took two to three minutes to reach full speed. It fell back to 16 KB/s
  whenever the next file was still being unlocked. A download running on its
  own now gets the whole limit straight away. When the built-in engine and
  JDownloader download at the same time, each gets its share within a few
  seconds. One that waits for a captcha hands its share back on the next
  update, three seconds later. One whose server sends less than its share
  leaves the rest to the other, and at most a tenth of the limit goes unused.

- **A low speed limit no longer makes downloads crawl.** The limiter charged
  every read for 32 KB, however little arrived, so under a low limit a
  connection could wait longer for its turn than the download engine waits for
  an encrypted connection to be set up. Downloads then kept reconnecting and
  moved a few KB/s for minutes. The limiter now charges for what arrives and
  passes a new connection's first bytes on at once.

- **A server that stops answering no longer holds a download at 0 B/s.** A
  download server that took a request and then sent nothing kept the download
  waiting until somebody paused it. After two minutes of silence the
  connection is now closed, and the download asks for the rest of the file
  again, keeping what it already has. A plain HTTP download whose server went
  quiet right after its headers is asked for again as well.

- **Links no longer wait in JDownloader's free mode while a debrid service can
  fetch them.** A link kept the backend it was given when it was added or on an
  earlier try, even when that was JDownloader at a moment no debrid service
  could take the host. It then sat at a captcha in free mode, across restarts,
  while TorBox or Debrid-Link would have fetched it at once. A link that has not
  loaded any bytes now goes to the highest-ranked debrid service that can take
  it when it starts, and JDownloader drops the package it still held for it, so
  the file is not fetched twice. A link a service has just turned down only
  moves further down the priority order. If the accounts below it are benched,
  it waits for them rather than going back to the service that turned it down
  and looping. yt-dlp's rows and torrents keep their backend.
- **A download that runs again shows no old retry date.** A restarted link, or
  one that started again after a failure, kept the date its retry had been due
  on its row, weeks later in some cases.
- **JDownloader takes back a link an earlier try left in its list.** Before a
  download goes to JDownloader again, the package the earlier try left in
  JDownloader's download list is removed, since JDownloader's duplicate check
  could hold the new link back while it was there. A package that already
  loaded bytes is continued instead of added a second time. The JDownloader
  that KnightLoader starts itself no longer asks what to do with a link it
  already has, a link it found offline, or a part of an archive whose other
  parts it has not seen. Nobody can answer those questions in a JDownloader
  without a window. It now adds the duplicate and the archive part to its
  download list and keeps the offline link back.
- **A link JDownloader finds offline fails at once.** The download says the
  hoster reports the file offline, and a mirror of the same file, if you kept
  one, takes over straight away. Before, it waited 15 minutes and then
  reported that the link never reached JDownloader's download list.
- **TorBox switching off one site no longer takes it away from the others.**
  When TorBox answers that a site is temporarily disabled, as it did for
  rapidgator, links to that site go to the next service for 15 minutes, and
  TorBox keeps fetching every other site. A link no other service carries
  waits and goes back to TorBox after those 15 minutes. Before, TorBox was
  benched for every site, for 15 minutes up to six hours.
- **TorBox errors no longer show your API key.** When TorBox refused to hand
  out a download link, or never answered the request for one, the task row and
  the log quoted the full request address, key included. They now name the
  request without it.
- **A retried debrid or TorBox download is written under its own name again.**
  After KnightLoader restarted, an interrupted download left its half-written
  file behind, and the next attempt was saved beside it as "name (1).rar",
  then "(2)" and "(3)". Unpacking then read the leftover and failed with "bad
  block header". A download now remembers the file it writes, and the next
  attempt deletes that file first, as long as no other download claims it and
  it is still the size the download was writing. The same goes for direct
  downloads. When a download moves from one debrid service to the next, the
  first service lets go of it before the second starts, so the new transfer
  is no longer stopped, or saved as "name (1)", because of the old one.

- **A download with part of its file missing is no longer marked finished.**
  When a debrid or TorBox link stopped working part way through, the download
  engine skipped the part it could not fetch and reported the download as
  finished, and the file only showed up as a damaged archive when it was
  unpacked. KnightLoader now checks a finished download against its size and
  fetches the missing part again, from a fresh link where the service can hand
  one out. If that does not work either, the download fails and says how much
  of the file is missing.

- **Debrid, TorBox and WebDAV downloads land in the folder the list shows.**
  They were saved to the downloads folder in KnightLoader's data folder,
  whatever the category, the per-package subfolder or the download folder
  setting said, and never went through the working folder. They now go where
  a direct download goes, and the collision policy applies to them as well.

- **A download saved under another name is used from where it was saved.**
  When a file that belongs to something else already had the name, the
  download is still saved beside it. Unpacking, the checksum, a rename rule,
  the move out of the working folder and playing or saving the file from the
  list now take the downloaded file instead of the one that was in the way. The
  download's log says which name it got. If you rename such a file back to the
  download's own name by hand, it is found there as well.

- **Removing a download with its files works after a restart.** The file
  stayed on disk when the download had started before KnightLoader last
  restarted.

- **A multi-volume archive with a part in the wrong place is not unpacked from
  the wrong file.** When a part had to be saved under another name or into
  another folder, unpacking stops and names the file it would have read and
  the file that was downloaded, instead of failing as if the archive were
  damaged. Once the part is moved to where the message says, unpacking works.

- **An unpacking error names the part it failed in.** "bad block header" and
  the other errors from inside a RAR set start with the name of the volume
  that was open, so a broken part among forty can be found.

- **RAR archives with encrypted contents try the saved passwords.** An archive
  packed with `rar -p` keeps its file names readable, and it failed with
  "archived files encrypted, password required" without trying a single
  password or showing "Needs a password". It goes through the passwords now
  like any other encrypted archive. A wrong password for an archive whose names
  are encrypted too no longer ends the list before the right one, and a file a
  wrong password had started is removed before the next one is tried.

- **A failed unpacking takes its folder with it.** The folder named after the
  archive stayed behind with empty subfolders in it, which looked like an
  extraction that had worked.

- **A failed unpacking shows how far it got.** Its row kept the byte count of
  the last progress update before the failure.

- **"Delete" and "Move to trash" for the archive reach RAR sets.** The volumes
  of a RAR set stayed where they were after unpacking, whatever the setting
  said. They are deleted or moved to the trash now, as zip and 7z archives
  were.

- **Click'n'Load works in the desktop app.** It never started a listener
  there, while the Modules page said it listened on 127.0.0.1:9666. It listens
  the way the server does now, and `KL_CNL` means the same in both. Where
  nothing listens, the Modules page says so instead of naming an address.

- **A new script starts with what it can use.** Its first lines named a
  sandbox API that was "still being finished" and a Help section that does not
  exist. They list `log`, `notify`, `trigger` and `queue` now, and the
  objects an event brings along, such as `task` or `pkg`.

- **A folder field keeps what you typed.** A watch folder, "Unpack to" folder
  or "Move the unpacked files to" folder that was not a full path was emptied
  on save, so typing "C:" and pausing cleared the field. Such a folder is
  refused instead: the reason shows under the field, the stored folder stays,
  and your other changes still save. A save that comes back while you are
  still typing no longer replaces the text in the field. This holds for every
  text field in the settings, a category's name and the reconnect password
  included. A password or command that comes back masked no longer empties its
  field while you type.

- **A refused setting is no longer sent again and again.** Picking a reconnect
  method before the IP check URL was filled in, or adding a connection before
  its host, was refused and then sent again about every 600 ms, each time with
  an error message. A refused change now waits for your next edit, and the
  reason shows once, under the field it is about: the IP check URL, the
  reconnect program, a connection, a category, a feed, an event target, a rule
  or a row on the Advanced page. The rest of your changes still save. A stored
  setting that no longer passes the check, such as a reconnect switched back on
  after its check URL was cleared, no longer blocks saves on other pages.

- **A module switch no longer brings back a value you replaced or deleted.**
  After the watch folder, feed subscriptions, event targets or schedules had
  been switched off and on once, the switch still kept its old copy. If you
  later emptied the list or the folder by hand, the Add button or the folder
  field disappeared and only the switch was left, and it put the old value
  back. Event targets that were all switched off could even be replaced by the
  old list. The switch now drops its copy once it has brought the value back,
  and it never replaces anything set up since. A value set while the module was
  switched off, on the Advanced page, by a settings import or through the API,
  takes the copy's place too, so clearing it again leaves nothing to bring back.

- **Escape closes only the window on top.** In the folder chooser opened from a
  download's options, it closed the options too. A captcha that arrived while
  the chooser was open was skipped by the Escape meant for the chooser. Escape
  on an (i) closes its bubble first and the window only on the next press, in
  the web interface and in the extension's windows.

- **A captcha no longer takes the focus from the window you are typing in.**
  Its answer box took the focus the moment it arrived, even under the folder
  chooser or the command palette. It takes it only when the captcha window is
  the top one.

- **A refused button keeps the keyboard focus.** A button that shook after a
  refusal, such as Save in a download's properties, the passkey and second
  factor buttons or a link's on/off switch, was drawn anew to shake again, and
  the focus fell back to the top of the page.

- **An instance that refuses this one says "Refused"** next to its name, with
  the reason and what to do about it in its (i), instead of the whole paragraph
  in the badge.

- **The folder chooser opens for a folder made only of placeholders**, such as
  `<jd:packagename>/x`. On Windows it refused such a folder as not a full path.
  It opens where an empty field would, as on Linux, and puts the placeholders
  back after the folder you pick.

- **Dragging a rainbow colour in the extension keeps every step.** Two steps of
  the drag could overlap, and the later one wrote the earlier colour back.

- **A name with a dollar sign shows as typed in messages.** A saved view,
  passkey, script, package or file called "AT$&T" or "$$$ deals" came out as
  "AT{name}T" or "$$ deals" in the sentence around it. This affected the web
  UI, the phone app and the browser extension.

- **A database upgrade cut short no longer keeps KnightLoader from starting.**
  Each upgrade step and the note of how far the upgrade got are written
  together, so a crash between them cannot make a step run a second time on the
  next start, where it failed and the app stopped. A new install sets up its
  database in one go.

- **Alias domains count as the filehoster they belong to.** A link to rg.to,
  ul.to, k2s.cc, ddl.to or one of 1fichier's other domains was not recognised
  as a filehoster, so the direct download could take it and save the landing
  page. These links go where rapidgator.net and the others go, and a hoster
  login covers its alias domains too.

- **Links to a filehoster go to JDownloader's free mode, however the priority
  order is arranged.** Any drag on the card saved the direct download above
  JDownloader, and from then on a filehoster link without an account went out
  as a plain download, which usually saves the hoster's landing page. The
  direct download and the HTTP fallback leave every host that JDownloader, one
  of your debrid accounts or the built-in hoster list knows as a filehoster,
  and every video site while yt-dlp is switched on, wherever they stand in the
  order. yt-dlp leaves
  the filehosters too. Header profiles and your own FTP, SFTP and WebDAV servers
  left the card, since each takes only the links it was set up for, and they
  go ahead of every row.

- **"Wait before confirming automatically" waits.** The delay was saved and
  never read, so every batch confirmed the moment it arrived. A batch now counts
  down in the status strip, where the stop button leaves its links in the
  collector. Each batch has its own countdown, links you confirm or remove in
  the meantime are left alone, and a new delay also applies to countdowns that
  are already running. A countdown cut short by a restart carries on after it.

- **A watch-folder job with `enabled=false` stays parked.** It used to be
  confirmed before it was parked, and the folder's options could arrive after
  the download had started.

- **The audio row of a video link reaches the download list.** Only the video
  row moved when a batch was confirmed, and the duplicate check took the other
  rows of the same link for copies of it. Now the whole family moves together,
  and the download list shows which variant, quality and format each row
  fetches.

- **YouTube links show their size in the collector.** A merged download counts
  the video and the audio together, a track only on HLS counts its estimate,
  and the size follows every change of format or quality, also after a
  restart.

- **Unticking a variant in a host's preset hides its rows at once**, for links
  already in the collector as well, and ticking it again brings them back with
  their own picks. The phone app hides them too.

- **Far more hosters show their icon.** The fetch gave up after four tries,
  so a site that declares several large icons it does not have never got to
  its working favicon. It also read encoded paths literally, skipped the
  `www.` address and plain http, refused SVG icons and trusted a wrong content
  type. An icon fetched once is also read back from disk after a restart. The
  check that keeps icon requests out of the local network runs on the address
  being connected, so a DNS answer that changes in between cannot get past it.

- **A header profile takes the links of its own site before a debrid service
  does.** Since header profiles left the priority card, a debrid service that
  carries the same host came first, and nothing on the card could change that.

- **A debrid unlock that runs out of time shows as an error.** After two
  minutes without an answer the link stayed at "unlocking via …" for good.

- **A debrid link paused while it is being unlocked stays paused.** Pausing,
  resuming and pausing again in quick succession could leave the second unlock
  out of reach, and a pause that came just as the service answered still
  started the download. The same holds for TorBox, where a link paused or
  removed while TorBox was preparing it also ended as a failed download.

- **Saving the proxy or torrent settings while downloads run is safe.** Both
  wrote to the configuration the download engine was reading at that moment.

- **The action for an idle queue arms when it is switched on while nothing is
  downloading**, also when the save arrives during one of the controller's
  checks. Such a save could be spent on the settings from before it.

- **A new schedule stays open for editing.** The page saved it a moment after
  it was added and closed the row while reloading.

- **Dragging a settings tile no longer opens its page on release**, and Escape
  puts the old order back on screen as well as in storage.

- **Dragging a package that holds a finished file shows where it will land.**
  The preview kept the package where it was and moved the finished file to the
  top of it, while the drop put the package where the pointer was.

- **Browser extension 1.0.1: a tooltip no longer stays up after a click.**
  Focus opens a tooltip only after keyboard input now, so Cancel in the
  "leave the group" window, which hands focus back to the bin, no longer leaves
  the bin's tooltip standing where the pointer is not. The tooltip engine
  follows GlimStone 2.6.0.

- **A schedule's day presets and times look like controls again.** Inside the
  grey editor the presets' track and the start and end fields had the editor's
  own colour, so the presets read as plain words and the times as plain text.
  Every selector's track now has a colour of its own on any ground, and the
  time fields are fields, with a clock.

- **The image build no longer takes `latest` before the release exists.**
  `docker/metadata-action` adds `latest` by itself unless told not to, so the
  guard added in 1.1.6 decided nothing: on v1.1.6 the build moved `latest` half
  an hour before the release was created, and the job meant to decide it only
  corrected it afterwards. The build sets version tags only now.
- **The APK tile on Settings > App downloads the app version the
  card shows.** The number came from `mobile/app.json`, but the tile opened the
  list of all releases, where the newest app can be another version. The tile
  now fetches the APK of exactly that release. A check ties the number and the
  file to `app.json`, CI checks the built page whenever `app.json` changes, and
  a test ties the extension's number to the manifest in the zip the server
  hands out.
- **The speed graph's time axis reads the right way round in Arabic, Hebrew and
  Persian.** "-60s" and "0s" had swapped ends while the curve had not, and the
  top of the scale showed its unit before the number.
- **More of the interface mirrors in Arabic, Hebrew and Persian.** The badges on
  "Connect instances and remote access" no longer cover the sentence above the
  steps. The total download speed, a download's size, an instance's speed and
  the top of the downloaded volume scale keep the number before its unit.
  Switches, number and password fields, menus, toasts and the other notices
  sit on the correct side.
- **Every figure keeps its order in Arabic, Hebrew and Persian.** A size,
  speed, bitrate, duration, date, percentage or count inside a sentence could
  trade places with its unit or with the words around it: the Accounts page
  showed "GB 0,1 >", a bitrate read "kbit/s 128" and a size "GiB 2.7". They
  all keep their order now, in the web interface and in the phone app.
  The smallest allowance reads "< 0.1 GB" in every language, where it had a
  decimal comma.
- **A portrait video offers the qualities it has.** yt-dlp names a video
  filmed upright by its shorter side, so a 1080x1920 track is 1080p. The
  quality menu went by the height instead: it offered 1440p down to 144p for
  such a video, and "Up to 1080p" downloaded a 480p track. Qualities, caps and
  tracks follow yt-dlp's names now, so the menu lists 1080p down to 144p and a
  cap downloads the quality it names. A track picked earlier takes its new
  name when the link is checked again, and until then the menu lists it in
  order of resolution instead of at the end.
- **A video row set to "Custom format string" no longer shows another file's
  size.** The row showed mkv and the size of the best video, while yt-dlp
  downloaded whatever the string asked for. The extension and size now stay
  empty until the download reports them. With the string left empty, the row
  downloads what "Best available" does and shows the same.
- **"Unpack to" and "Move the unpacked files to" keep their caption level with
  the box** when the box shows an error under it. The caption dropped by about
  9 pixels.
- **Disabled buttons in the queue and status bar show their name** under the
  pointer, such as Stop while nothing runs.
- **The Schedules card keeps each row's name and times readable.** With
  labelled buttons, a row's buttons move to a line of their own instead of
  squeezing the name to nothing, and they wrap in a narrow window.
- **The figures on an instance card no longer overlap.** "Tasks" and "Speed"
  ran into each other on a narrow card. Every card has one width, on the
  Instances page and on the Instances settings tab alike, with the state badge
  in the name's row and the three figures on one line. A narrow window puts
  fewer cards side by side.
- **Links you add together start in the order you added them.** On Windows,
  several links from one paste could get the same timestamp. The queue starts
  the oldest link first, so it then took those links in any order.
- **Typing a folder no longer leaves half-typed folders behind.** The settings
  page saves while you type, and every save created the download or working
  folder it was given, so typing "D:\Downloads" could leave "D:\Down" on the
  disk. The watch folder did the same. A save now only checks the folder: one
  that exists has to be writable, and for one that does not exist yet the
  nearest folder above it has to allow a new folder. The first download
  creates it, and so does New folder in the folder chooser. Nothing creates a
  watch folder that is not there yet: it is watched anyway, the Modules page
  and the folder chooser say so, and files dropped into it are taken once it
  exists.
- **A folder template that starts at a drive root works on Windows.** In a
  folder such as `E:\<jd:packagename>`, the part before the variable was read
  as `E:`, which Windows takes as the current folder on that drive rather than
  its root. "Unpack to" lost such a folder on save, the folder chooser would
  not open on it, and a download folder like `D:\<jd:date>` was checked in
  whatever folder KnightLoader had been started from. The root is kept now,
  and a template there only has to be able to create its folder, which the
  root of the system drive allows even where it refuses files.
- **A setting unrelated to categories saves even when a category's folder is
  unusable.** Every save checked, and created, the folder of every category,
  so a category on a share that was offline refused a change of theme. The
  category folders are checked when the categories are saved.
- **The folder chooser stays inside KL_BROWSE_ROOTS through Windows
  junctions.** A junction inside the allowed folders led out of them, both for
  browsing and for New folder, and the same held for fetching a finished
  file. Junctions and mounted folders are followed to where they really point
  before the boundary is checked.
- **A hoster login saved under an alias domain is matched to JDownloader's
  account.** JDownloader files an rg.to login as rapidgator.net, so the login
  looked missing and was added again on every pass.
- **Server messages call pages and settings by the names the interface
  uses.** The metrics line spoke of "the Access page" and the update check of
  "the General tab", and the download client's warning said "this page" while
  it is shown on the Modules page. A module switch that has nothing to switch
  back on, and an encrypted container while JDownloader is off or missing,
  are now explained in your language.
- **reCAPTCHA Enterprise and v3 captchas can be solved.** They loaded the
  classic script and showed a checkbox that never worked. An Enterprise
  captcha loads Google's Enterprise script, and a v3 check fetches its answer
  by itself under the action the hoster asked for. Where a captcha cannot be
  solved in KnightLoader, the captcha window says so, and its (i) says why.
- **More refusals are in your language.** The folder chooser says why it
  cannot list a folder: a path that does not start at the top, a folder
  outside the allowed roots, one KnightLoader may not open, or a
  KL_BROWSE_ROOTS that names no usable folder. The same goes for a hoster
  login that JDownloader has not accepted or confirmed yet, adding a found
  instance while "Peer instances" is switched off (in the web UI and in the
  Android app), a wrong password when showing the connection phrase again, a
  header profile or media hook the server turns down, and a passkey setup
  that took too long or was started on another address. The server sends
  each of these with a code next to its English sentence, and
  `GET /api/folders` answers a refusal as JSON, like creating a folder does.
- **Error messages no longer start with "ApiError:".** A diagnostics bundle
  that could not be built or a folder check that could not run showed the
  error's type in front of the server's words.
- **Error lines read the right way round in right-to-left languages.** The
  line under a folder field, a refused save, a failed test and the card shown
  when a page cannot load set their own direction, so an English sentence on
  an Arabic page keeps its full stop at the end.
- **The paste and reveal buttons in the extension's phrase field sit side by
  side.** Both were drawn on the same spot, which looked like a grey blob. The
  eye is the one the web UI uses.

- **Links out of the desktop app open in the default browser.** On macOS and
  Linux the GitHub and email buttons, the version numbers, the "where to find
  it" links on the Accounts and Captcha pages, the store and APK tiles and the
  update link did nothing, because the app's window cannot open a second one.
  On Windows they opened a bare window with no address bar. They now go to the
  default browser or mail program.
- **A rename reaches a file still in the working folder.** With a working
  folder set, a file name from a Packagizer rule or from a rename made while
  the download ran was never applied, and neither was a rename of a finished
  file not yet moved on, because the file was looked for in the destination.
- **A rename that could never be applied is refused at once.** A file being
  unpacked, a JDownloader download that had not finished and one part of a
  multi-volume archive took the new name on the row, but the file never got
  it. The rename window now says why before anything is sent.
- **A window opened from a page dims and blurs the whole screen.** The rename
  window, the question before removing links and the other windows a page
  opens covered only the page, so the sidebar and the Downloads head bar stayed
  bright and sharp beside it.
- **The variant defaults on the yt-dlp page fit their card.** At 1440 pixels
  the table ran past the card and scrolled sideways. In a card too narrow for
  its columns, each site is now a block of its own with a name beside each
  switch. In a wider card, a format or quality too long for its button ends in
  "…", and the open menu shows it in full.
- **A variant default for youtube.com covers youtu.be and m.youtube.com.**
  Defaults were looked up by a link's exact host, so short links and the
  mobile site started with every variant switched on. For the big video sites
  a default now covers every address KnightLoader knows for the site. The
  gear on such a link's package edits that default instead of starting a
  second one, and where there is none yet, it saves one for the whole site.
- **Moving files into another package leaves them where they are on disk.**
  With the download folder named after the package, "Move to a package" sent
  a finished file's row to the new package's folder, where the file was not,
  and a part still waiting landed apart from the parts already there. Links
  moved out of a folder that already holds one of their files keep that
  folder, as they do when their package is renamed. Links nothing has been
  downloaded for follow the move.
- **The diagnostics page no longer says the desktop app brings its own Java.**
  The desktop builds come without one, so there the JDownloader backend needs
  a Java installed on the machine, on PATH or under JAVA_HOME. The advice for
  a missing Java now says so.
- **No coloured lines at the edges of the README's buttons.** They are all cut
  from one image in which they stood edge to edge, and Firefox, or Chrome at
  some zoom levels, drew a sliver of the neighbour down the side of a button.
  PayPal's dark blue had a yellow line on its left and an orange one on its
  right. The buttons in that image have space between them.

### Changed

- **The selection bar fits on one line.** Clearing the selection is the ×
  beside the count, the search is a glyph, and moving into a package, the
  order actions and removing with the files sit in a More menu at the end of
  the row. Plain removal still acts at once with its undo toast. The link
  collector has the same bar.
- **The right-click menu is as tall as its entries.** It scrolls only when
  the window is shorter than the menu, and near the bottom edge it opens
  upwards.
- **Marks on the rows.** The stop mark, Start now, hold and switched off show
  as small glyphs in the name cell, each the glyph of its menu entry, and on
  a package row when every link in it carries the mark. The stop mark
  follows the queue live.
- **AAC is one audio format, called AAC (M4A).** The audio format menus
  offered aac and m4a, two names for AAC, and aac wrote a bare AAC stream
  under an .m4a name rather than an MP4 file. The menus name it after the
  container it is written in, which is what YouTube calls it. Picking it
  copies a link's AAC track, or converts to AAC where the link has none, and
  writes a real .m4a file either way. Settings, variant defaults and rows that
  chose m4a carry on as AAC.
- **The variant defaults offer the formats a site serves first.** For YouTube
  that means mp4 and webm video in YouTube's own codecs and AAC (M4A) or opus
  audio, where the menus used to list every format there is. Below the
  site's own audio formats, under "Convert to", come the others, such as MP3
  and FLAC, and yt-dlp converts to them with ffmpeg. KnightLoader knows what a
  site serves from a built-in list of the big sites and from the links of that
  site it has already checked; a site it knows nothing about still offers
  everything, and an (i) beside it says why. A default that already names a
  video format the site lacks keeps it.
- **The status column shows unpacking the way JDownloader does.** While an
  archive unpacks, every file of its set reads "Unpacking 45%", and afterwards
  "Unpacked" with a tick. A failed one reads "Needs a password", or "Not
  unpacked" with the cause beside it, and the package row sums up its archives
  with a count such as "1/2" when it holds more than one. This replaces the
  archive card under the list, and "Stop unpacking" is in the right-click menu
  of every file of the archive. Each file still says how its last unpacking
  ended after KnightLoader restarts, and forgets it when the download is
  started over.
- **The download list reaches the bottom of the window and scrolls inside its
  card**, so the queue controls and the filters stay in view. With nothing to
  show, the empty card fills the same space. A window too short for a few rows
  scrolls the whole page instead.
- **A download that stands still gets new connections, and the mark is on by
  default.** A download that moves no bytes for two minutes is marked as
  standing still, and the built-in engine closes its connections and asks for
  the rest of the file on new ones. It keeps its download slot and what it has
  fetched, unless the server can only send the whole file, and this repeats
  every two minutes while it stands still. The new switch "Open new connections
  for a stalled download" in Settings under Downloads, on the "Standing still"
  card, turns it off. Existing installs get the mark as well, also where it was
  off, because off was the default until now. Importing a settings file
  exported by an earlier version turns it on the same way, and the list you
  pick the settings from shows the value that is taken over. Switch it off
  again and it stays off. JDownloader, yt-dlp and torrent downloads are only
  marked. With "Start a stalled download over" on, a download is started over
  only once new connections have not helped.
- **The watch folder has its own switch on the Link collector tile**, the same
  switch as on the Modules page, and each place links to the other. The link on
  the Modules page lands on the switch itself. Switched off, the folder is kept
  and comes back with the switch; the field has the folder picker, and the
  module is called "Watch folder" in both places.
- **Every module can be switched on its own settings page as well as on the
  Modules page.** Archive extraction, the page crawler, checksum verification,
  feed subscriptions, event targets, schedules, reconnect, the Packagizer, the
  link filter and the metrics address now have a switch on their own page,
  like the modules that already did. Both switches are one switch, and the one
  on the page takes its card's colour. A badge beside each of them leads to the
  other and lands on it. The card, the switch
  and the Modules row share one name: "Extraction" is now "Archive extraction",
  "Page crawl" is "Page crawler", "Monitoring" is "Metrics address for a
  monitoring system", "Your scripts" is "Event scripts", "Scheduler" is
  "Schedules", and "RSS and Atom subscriptions" is "Feed subscriptions".
  Switched off, the feed, event target and schedule cards show the switch alone
  instead of claiming there are none yet. Reconnect's method strip no longer
  has an "Off" tab, because the switch turns it off and keeps the method for
  later.
- **The Link intake card keeps its explanations in (i) bubbles.** The address
  Click'n'Load listens on is in its bubble and in your language, also on the
  Modules page and in the tooltip of the collector's Click'n'Load button.
  "Start added links immediately" lost its "(skip the collector)" to a bubble of
  its own. `GET /api/features` sends a row's reading as a code with its values
  too, next to the English sentence.
- **Buy Me a Coffee and PayPal open in a window inside the app** instead of a
  browser tab. The coffee window shows Buy Me a Coffee's own donation page.
  The PayPal window asks how often and how much, then offers PayPal's own
  button and a card button, so a donation can be one-off, monthly or yearly,
  with or without a PayPal account. Nothing from either service loads before
  its window is opened. The browser extension opens Buy Me a Coffee in a
  window too and keeps PayPal as a link, and the phone app keeps both links.
  On macOS and Linux the desktop app's PayPal button opens PayPal's
  donation page in the browser instead, because the window's login needs a
  popup the app cannot open there.
- **The speed graph is a filled area that glides** from one sample to the next,
  where it used to step once a second and rescale on every value. With motion
  switched off it moves without the glide.
- **A downloading row says "Downloading"**, and a waiting row no longer repeats
  "queue stopped" or "all slots busy", which the toolbar already says for every
  row at once.
- **A sorted list says so in a second badge beside the card title**, with the
  way back in its info bubble, instead of an extra row above the list with its
  own button.
- **The name column takes the rest of the list's width.** In the download list
  and the collector the other columns are as wide as what they show, and the
  name gets what is left. The list fits its card in a window 1280 pixels wide
  or wider and scrolls sideways only when columns you widened or switched on
  need more. In a narrow window the name gives way first, then the other
  columns. A column you dragged keeps its width, and a double-click on the
  name's edge lets the name fill again. The collector's variant column is
  narrower, and an expanded package shows an open folder.
- **The debrid account picker lost its search field** and is titled "Choose a
  debrid account", after the card it opens from.
- **The windows on the Accounts page are named after the card and the button
  that open them**: "Choose a hoster account", "Add your 1fichier.com account",
  "Edit the credential for TorBox". A window opened from the debrid card wears
  that card's colour, as the hoster card's windows already did.
- **The tour's last button reads "Done".**
- **Every row of the priority card can be dragged**, JDownloader, yt-dlp and
  the direct download included, where they used to sit in a section of their
  own. What a row is for sits in an (i) beside its name instead of a grey line
  under it.
- **JDownloader's switch lives on the Modules page only**, not on the Accounts
  tab as well.
- **The schedule page speaks of schedules.** The card is "Schedules", its
  button "Create schedule", each row has an edit button, and the status card's
  title is translated. "Custom" under days is a real choice that opens the
  weekday strip.
- **The language picker sits under Appearance.**
- **The settings have 21 tiles instead of 24, grouped by task.** "Link
  collector" holds link intake, the collector, page crawling, copies of the
  same file and links that are already dead. "Automation" holds schedules,
  the idle action, the media library call, event targets and scripts.
  "Network" holds outgoing connections, reconnect, header profiles and the
  per-hoster exceptions. Categories sit on "Rules & categories", quiet mode is
  the first switch of the notifications card, and "Files already on disk"
  moved to Downloads. Old addresses, a remembered page, a saved tile order
  and a shortcut bound to an old page all lead to the new tile.
- **Every debrid service is on the debrid card.** The multihosters
  KnightLoader reaches through JDownloader, LeechAll and MyDebrid, are picked
  and listed there, marked "through JDownloader", and no longer mixed into the
  hoster accounts. DebridPlanet, Simply-Debrid, MultiVIP and DailyLeech are
  gone from the list: the first two have closed, MultiVIP's site no longer
  answers, and the DailyLeech pages JDownloader logs in through are gone.
  put.io stays with the hoster accounts, since it stores your own files
  rather than unlocking other hosters.
- **A settings tile being dragged floats under the pointer** with a shadow,
  the other tiles slide aside while it passes, and on release it slides into
  its place. Escape puts everything back.
- **Rows in the download list and the collector float under the pointer while
  you move them**, like the settings tiles. A marking, or a package with its
  files, travels as one block, the other rows slide aside at the pace of the
  motion setting, and on release the block slides into its gap and stays there
  until the server confirms the order. Escape puts everything back. On a touch
  screen, holding a finger on a row picks it up, and lifting the finger
  without moving opens the row's menu. A row that cannot be moved, such as a
  finished download or any row while the list is sorted, opens its menu the
  same way and says why it stays put only once you try to drag it.
- **The priority card's rows follow the pointer too.** The row you drag lifts,
  the others make room as it passes, and on release it slides into place.
  Escape puts it back. On a touch screen you pick a row up by holding its grip
  for a moment. A row taller than the others no longer throws off where a drop
  lands.
- **The browser tab shows only the name**, without counts, percent and speed.
  The ring on the tab's icon still shows the progress.
- **The settings tiles sit closer to the sidebar.**
- **The logo in the sidebar no longer fades under the pointer.**
- **The web UI and the browser extension (1.0.2) are set in Noto Sans**,
  shipped with them, so they look the same on every system instead of taking
  whatever font the system has. A page loads only the alphabets it shows, the
  Latin one 35 KB. Chinese, Japanese and Korean use the system's own font,
  since those fonts are several megabytes each. The extension also takes the
  web UI's letter spacing, so the font sets the same way in both.
- **The phone app is set in Noto Sans too**, shipped with it, so its labels
  match the web UI and the extension instead of taking the phone's own font.
  Arabic, Hebrew, Thai, Chinese, Japanese and Korean use the phone's own font
  for those alphabets.
- **The "Browser & App" page is called "App" and offers every other way to get
  KnightLoader**, laid out as GlimStone 2.9.0's App tab. The phone card comes
  first: Google Play, marked "Soon" until the listing is live, and the APK,
  with a Download button and a QR code button beside it. QR code turns the tile
  into a code to scan on a white ground, and pressed again it turns back. The
  app's version stands in the card's corner, linked to its release. There is
  no App Store tile, because there is no iPhone build. In a container the
  second card offers the desktop app for Windows, macOS and Linux. In the
  desktop app it offers a server instead: Unraid's Community Applications,
  marked "Soon", a Docker tile that copies the command that starts the
  container, with your time zone in it and the command in its (i), and
  "Source code.zip" for the version you are running. The bookmarklet and the
  browser extension follow below them, unchanged.
- **The web UI, the browser extension and the phone app follow GlimStone
  2.9.0.** Every window has its way out as a button in its bottom row, tooltips
  open on focus only after keyboard input and close when their control changes,
  the default motion level is "subtle", and the About card of the extension and
  the app offers PayPal and crypto beside the coffee. There a coin tile lights
  up in the coin's own colour under the pointer or a finger, as in the web UI,
  and the name beside the logo is bold at 20 pixels. In the app's download
  list a dragged row lands in its place instead of jumping, the rows it passes
  slide aside, and it keeps its new place until the server confirms the order.
  An open package takes its files along, and a drop the server would not
  apply slides straight back. In the round shape every button, tab, badge,
  selector segment and switch is a true pill while fields keep a corner, and
  cards and fields are a little rounder than before. A fresh install starts on
  soft corners, called Abgerundet in German; a shape you already chose stays.
  With square chosen, five more taps on it reveal a fourth shape, the leaf,
  with two opposite corners rounded and the other two sharp.
- **The phone app moves like the other apps of the family.** At Wild the cards
  of the overview, the download list, the settings and the language list fly
  in from both sides and bounce into place, each time you come back to the
  screen and when you switch between downloads and collector. At Subtle they
  rise a little. Rows you scroll to appear without flying in. Buttons and cards
  give way under your finger, and at Wild a list flung against its top or
  bottom runs on a little and springs back instead of Android's glow. Off, like
  the phone's own reduce motion setting, keeps it all still, and the colour
  picker and the QR scanner now open without sliding or fading there too.
- **The interface texts were reworked** in the web UI, the extension, the app,
  the README and the user docs: plainer hints, and no dashes as punctuation.
- **A release waits for its images too**, not only for the desktop bundles, so
  a published release always has both.
- **The moving image tags follow the release.** `latest`, `1.1` and `1` are set
  once the release for that version is published, and only for the newest one.
  The build pushes the exact version alone, so a tag whose release never came
  out cannot leave anyone pinned to `:1` on an unreleased build.
- **"Latest" is decided once**, by the script that publishes the release, and
  the job that moves the image tag takes that answer instead of working it out
  a second time.
- **Tidied the code comments and log messages.**
- **Row actions are visible without hovering.** The edit, delete and move
  badges on settings rows, accounts, peer instances and list rows are there at
  rest instead of appearing under the pointer, which a touch screen does not
  have. In the download and collector lists they sit in a column of their own
  at the row's end rather than floating over the size, speed and status cells,
  and in a narrow window the name column gives way before the list scrolls.
- **Coin, browser and app tiles light up in their brand's colour under the
  pointer**, the same colour on both themes. The name and logo on the tile turn
  white, or near-black where white would not read. Bitcoin turns orange and
  Linux yellow, and a coin's symbol and Tux's beak and feet stay cut out in the
  tile's colour. The coin you picked keeps the accent colour. On the light
  theme the Android, Docker and Unraid logos keep their own colours.
- **The logo at the top of the sidebar is 104 pixels tall**, 44 in the narrow
  rail, the size the sibling apps use, and the mark on an instance card
  matches it.
- **The APK tile shows Android's logo and the word "APK"**, where it showed
  KnightLoader's shield and "Download the APK". The logo keeps Android's green
  on both themes.
- **The project relay card shows its address in the bubble behind "What can it
  see?"** instead of in a field of its own. The button sits at the bottom right
  of the card, where it no longer pushes the switch down.
- **The Downloads head bar is one row and about 40 percent lower.** Play, Pause
  and Stop sit side by side as large square buttons that show their names in
  the tooltip, and a fourth square of the same size opens the quick settings.
  The speed curve fills the rest of the row up to the bar's right edge and most
  of its height. The newest second sits on that edge with the current speed
  above it, and the top of the scale is shown above the other end. In a narrow
  window the curve moves under the buttons.
- **The speed curves end without a dot.** The dot on the newest sample, and the
  ring it threw on the Overview page, were stretched into an oval along with the
  plot. With the limit at 1337 KiB/s the line itself breathes instead.
- **Quick settings open as a small panel under their button** instead of a
  window. The speed limit field is no longer in the head bar. It comes first in
  the panel, with simultaneous downloads, downloads per hoster and connections
  per download under it, one field per line.
- **Every dropdown is the app's own.** A schedule's action, a rule's field and
  comparison, a connection's type, a reconnect request's method, a script's
  trigger, a category's media server address, a host preset's quality and audio
  format, the log source, the speed unit and the search box's field open the
  app's menu with a check mark on the current choice, like the quality picker
  in the collector, instead of the browser's own list. The mouse wheel steps
  through the choices once you have clicked or tabbed into the dropdown, so
  scrolling the page past a link's variant or "Suspend schedules" changes
  nothing. The extension's language picker works the same way.
- **Text fields, dropdowns, the time fields and the search boxes are one
  height**, the height of the buttons beside them, and share one look.
- **A selector that needs more than one line fills them evenly.** "When two
  links count as the same file" and the idle action used to leave an empty
  stretch at the end of a line; now the options share each line and reach its
  end, and the lines hold as nearly the same number of options as they can.
  They are shared out again when the window changes size. A selector that fits
  on one line keeps its width. Where an option's name would break onto a
  second line while the options still fit side by side, the selector spans its
  card and gives each option the width of its name, so „Ein Eintrag, dass
  dieser Download sie geschrieben hat“ under "When a file already on the disk
  counts as the download" reads on one line. GlimStone 2.10.0 made this its
  rule. Options with an icon, such as the reconnect methods, make room for it,
  so „Anfragen“ no longer runs into the edge of its option.
- **A button's icon is the size of its label**: 14 pixels instead of 20, and 16
  instead of 22 in the taller buttons, so a row of buttons no longer looks like
  a row of icons. A button that shows only its icon keeps it at half the
  button's height. The extension and the phone app use the same sizes.
- **Buttons carry their (i) inside them**, such as Import and Export on the
  rules card, "Check integrity", "Check again", "Play here" and "Connect an
  instance". The (i) stays readable on a button that is switched off, and where
  a button shows only its icon, the explanation joins its name in the bubble.
- **Settings tile names wrap onto a second line** instead of being cut off, so
  "Rules & categories" reads in full. The tile stays as tall as every other
  tile at any window size, and no letter is cut off at the top or the bottom,
  the marks in Arabic, Persian and Hindi included.
- **A search box shows one focus ring.** Clicking into the list search or the
  settings search ringed the box and the text field inside it; the box alone
  shows the ring while you type, and the picker beside the text shows its own
  once you tab to it.
- **Every line on the Modules page is in your language**, not only
  Click'n'Load's. `GET /api/features` sends each module's status and reason as
  a code with its values, next to the English sentence. A value in a line reads
  the way its own page names it: the reconnect method "Requests" rather than
  "http", the quality "Best available" rather than "best".
- **The web interface fits a phone.** In a window narrower than 768 pixels the
  sidebar becomes a bar along the bottom with the same entries in the same
  colours, so every page keeps the full width. The bar has its own setting,
  "Bottom bar labels" under Appearance, which uses "Navigation labels" until you
  pick something else, so the bar can show icons alone while the sidebar keeps
  its words. Toasts and notices stand above the bar. The settings tabs show
  their icons only there.
- **The direct download has one name**: "Direct download" on the priority
  card, on a download's backend badge and in the "By backend" view of the
  downloaded volume, where it read "Direct link" in one place and "Direct" in
  the others. The plain HTTP fallback and "Torrent and magnet" are named the
  same way in all three.
- **The JDownloader backend has one name**, the one its switch on the Modules
  page has. The Accounts card, the Health page, the self-test and the hoster
  password hint called it "JDownloader sidecar", and in German also
  "Beiwagen". The route list of `GET /api/help` and the errors about a missing
  backend use the name too, and a captcha answer or skip refused for that
  reason carries the code `noJD`.
- **Rainbow mode has one name**, the one on its switch in the web interface.
  The phone app's switch said "Rainbow", and German texts wrote
  „Regenbogenmodus“ beside the switch's „Regenbogen-Modus“.
- **The end-of-queue action, the theme and the reconnect method follow
  "Navigation labels"** like the badges do: set to "Icon only" or "On hover",
  each choice shows its icon alone, with its name in the bubble.
- **Hints name the settings pages the way their tabs do**: "Remote access"
  where they said "Settings → Access" or "the Access page", and "Rules &
  categories" where they said "the Rules page".
- **The package gear in the collector and the table on the yt-dlp page have
  one name, "Variant defaults"**, since they change the same settings. The
  gear's tooltip and its window said "Variant settings" and the table
  "Per-host defaults".
- **"Move to a package" offers the existing package names in the app's own
  menu**, which narrows as you type, instead of the browser's list.
- **The account windows' links are buttons.** "Choose a different account"
  goes back to the list, and "Where do I get this?" opens the service's page in
  a new tab.
- **Explanations sit behind (i) bubbles across the app.** The grey sentences
  next to or under a control moved into the (i) of the card, field or button
  they explain: in the captcha window, the hoster login window, the second
  factor step of the sign-in page, the setup tour, the crypto window and the
  quick add page, on the connection phrase, relay, second factor and settings
  transfer cards, and on the Modules, Rules, Torrents, Network, Diagnostics,
  Automation and App pages. A window's title badge can carry an (i) too. What
  stays on the page: notes about what this build or this machine cannot do,
  status and error lines, and empty lists. The crypto window in the Android app
  and in the browser extension keeps its introduction in an (i) as well.
- **The Collector's filters and a torrent's file list are switches** instead of
  tick boxes, and a click anywhere on a row flips its switch.
- **Links look like buttons.** "Get a key" on the captcha page is a badge
  called "Where do I get this?", as in the account windows. The update card
  names the new version and opens its release notes from a badge. "Open
  Collector" on the quick add page, "Set the disk limits" on the Overview, "Open
  the whole log" on a download, the page links under each help topic, "Open the
  setting" on a failed idle action, a log file's download, "Select all" and
  "Select none" in a torrent's file list, the captcha window's "More options"
  and the two entries it opens, and the reason on a failed download are
  buttons or badges.
- **Below 1024 pixels the settings tabs show their icons only**, as they
  already did on a phone, with each name in its bubble. Beside the full
  sidebar their names left the page a card about 220 pixels wide in an
  800 pixel window, so a selector put one option on each line; the card is
  about 370 pixels wide now.
- **The Downloads head bar's buttons are larger.** Play, Pause, Stop and the
  quick settings button are 48 pixels square with a 24 pixel symbol, up from 40
  and 20. The bar grows by the same 8 pixels, so the speed curve still reaches
  close to its top and bottom edges.
- **The speed limit field shows its unit and picks it by itself.** Below
  1 MiB/s it counts in KiB/s and from there in MiB/s, the units the rest of the
  app uses for speeds. A number you type counts in the unit on show, and the
  field changes unit when you press Enter or leave it; the arrows and the mouse
  wheel change it at once. The limit is still stored in bytes per second.
- **"Reconnect now" and the badge beside it sit side by side** at the bottom of
  the quick settings instead of one above the other.
- **"Hide the phrase" and "Copy" sit under the connection phrase**, in its
  column beside the QR code, instead of under the code. On a narrow screen they
  follow the words.
- **The sidebar mark's easter egg shakes the entries at the foot of the rail
  too.** The wave after the blade lands runs on through Events, Sign out and
  Settings. At the Subtle motion level every entry moves half as far and
  settles sooner, as the blade already did.
- **"Start now" replaces "Force to the front" in the right-click menu.** It
  starts the chosen links at once, ahead of the queue and past the limit of
  concurrent downloads, three at a time on top of that limit. As with
  JDownloader's forced start, it works while the queue is stopped too: the
  chosen links start and everything else keeps waiting. It is offered
  only for links still waiting their turn; failed and running links no longer
  show an entry that did nothing for them.
- **Pausing a selection pauses its waiting links too**, not only the running
  ones, so the slots the running downloads free are not handed straight to
  the rest of the selection. The whole selection goes in one request.
- **A name with / or \ in it is refused rather than cut.** The properties
  panel turned "Season 1/Episode 2" into "Season 1-Episode 2" without a word;
  it now says why the name cannot be used.
- **The page behind a window is blurred and darker**, so the window stands out
  from it. This goes for every window in the web UI and the browser extension,
  the quick settings included, which a click on the page or Escape closes; the
  phone app darkens the page without blurring it. When the system is set to
  reduce transparency, the page is darkened but not blurred. The web UI and
  the extension follow GlimStone 2.11.0.
- **The link collector reaches the bottom of the window and scrolls inside its
  card**, as the download list does, so the paste box, the filters and the
  list's buttons stay in view. A window too short for a few rows scrolls the
  whole page instead.
- **Every speed field works like the speed limit field**: a category's speed
  limit, a schedule's limit, the speed once the volume cap is reached and the
  torrent upload limit. Each shows its unit inside the field, switches between
  KiB/s, MiB/s and GiB/s by itself and steps by the same amounts. The
  schedule's unit menu is gone.
- **A typed number can name its unit.** "500k", "2m", "1.5 MiB" or "640 KB/s"
  in a speed field and "90s" or "2 min" in the speed curve's window count in
  that unit, in upper or lower case and with or without a space. The unit
  beside the number changes to match.
- **Minutes are written "min" wherever a time is shown**: a download's time
  left, the speed curve's time axis, the uptime on the Health page, the clock
  check in the self-test and the wait after dropping a container file. The
  time-left column starts a little wider so "123h 45min" fits.
- **The README's download buttons come right after the description**, and the
  donation appeal follows. The notice that KnightLoader is not ready to install
  stays where it was, under the donation row. The band crosses the download
  rows first and the donation row after them.
- **The README's screenshots are new, and there are nine of them.** They show
  the overview, the download list, the quick settings, the link collector with
  a YouTube video's variants, the Appearance page, the accounts, the Rules &
  categories page, the App tab and the instances, each in the theme your system
  uses. The downloads, hosts and accounts in them are made up, and
  `scripts/screenshots/shoot.mjs` draws them again from that sample data.

### Removed

- **`/api/controls`.** Nothing calls it any more: the quick settings save
  through `PATCH /api/settings`, and the Android app and the browser extension
  never used it.

## [1.1.6] - 2026-09-18

### Changed

- **A release goes public only with its desktop zips attached.** The README's
  desktop buttons lead to `/releases/latest/download/`, and a release used to be
  "latest" from the moment it was created, twenty minutes and more before
  `desktop.yml` attached the zips, so the buttons answered 404 for that long and
  the in-app update check met a release without files. `release.yml` now runs
  the desktop build itself and creates the release once it is done, with the
  zips and `checksums.txt` in the same command. A failed build leaves no
  half-finished release behind.
- **"Latest" goes only to the newest published version**, so re-cutting an
  older one does not pull the badge, the buttons and the update check back.
- **A tag without release notes stops before the builds** instead of publishing
  a generated list of commit subjects, the same as the app and the extension.
- **`latest` on both images moves only after the GitHub release is out**, by the
  same rule as the "Latest" badge, so the badge, the download buttons and
  `docker pull …:latest` always name the same version. The image jobs also
  wait for the notes check now.

### Fixed

- **The Linux and macOS desktop zips hold a program that runs.** The bundles
  went through a build artifact before they were zipped, and an artifact drops
  file modes, so `KnightLoader` on Linux and the executable inside
  `KnightLoader.app` on macOS came out as mode 644 (checked on v1.1.4 and
  v1.1.5). They are zipped on the machine that built them now, where the modes
  are still right.

## [1.1.5] - 2026-09-17

### Changed

- **Settings > Browser & App serves the browser extension 1.0.0**, the version
  that goes to the browser stores, and the version link on that card leads to
  its release.
- **The popup's send button says what it sends**: the page, a link, an image,
  a selection, or the links a Click'n'Load button handed over.
- **The extension reads the current tab's address only when you press send**,
  not when the popup opens, and no longer asks for the `activeTab` permission.
- **On Firefox the extension declares at install which data it sends**, and it
  needs Firefox 140 or later on desktop.

### Fixed

- **A send from the toolbar popup could get lost.** The popup closed before the
  extension's sleeping background had the message. It now waits for the
  background to confirm.
- **Switching Click'n'Load off left the `jdcheck.js` answer on**, so sites still
  saw a receiver. The rule follows the switch now and is set again after every
  update and browser start.
- **Leaving the group kept the browser's random member ID.** It is deleted with
  the phrase now.
- **Danish and Swedish called a text selection a committee and a sample.** Both
  say "markering" now.
- **The relay's backoff for failed handshakes stopped growing at 16 minutes**
  for a caller that tries one connection at a time. It grows to 50 minutes now.

### Security

- **The relay no longer writes client IP addresses to its log.** Failed TLS
  handshakes and other errors from the web server carried them; they read
  `[address]` now, and a failed certificate renewal still shows up.
- **A failed address's rate-limit record is deleted within 61 minutes of its
  last failed attempt.** Before, it stayed until the relay restarted. The relay
  image also clears records on a timer; an instance's own relay clears them as
  requests come in.

## [1.1.4] - 2026-09-17

### Added

- **Download buttons in the README, in two rows.** Windows, macOS and Linux
  first, then Docker, the Android app and the browser extension. Each points at
  the newest build, so a release does not need a README edit.
- **A container image of KnightLoader itself.** Every release tag publishes
  `ghcr.io/junkerderprovinz/knightloader` for amd64 and arm64, beside the relay
  image, and only the newest release tag moves `latest`.
- **The desktop zips also carry a name without the version**, because
  `/releases/latest/download/` needs a name that stays the same from one release
  to the next. `checksums.txt` lists both names.
- **A standing release each for the app and the extension.** A new app or
  extension release also copies its file to `mobile/latest` as
  `knightloader-android.apk`, or to `extension/latest` as
  `knightloader-extension.zip`, which is where the README buttons lead.

### Changed

- **The notice at the top of the README says what exists now.** The releases,
  the image and the downloads are there so the builds can be tested; there is
  still no Community Applications entry, and the advice not to install it yet
  stays. The donation row moves up under the paragraph that announces it.
- **One band crosses all three rows of buttons in turn**: the donation row, the
  desktop row, then the row below it. Three rows need slightly more than seven
  seconds of travel, so this page's loop is 8.2 seconds, at the house speed and
  with the house pause.
- **The release workflow can be dispatched.** It then builds both images for
  both architectures and pushes neither, and releases nothing.
- **Both images move `latest` only after their build**, from the tags as they
  stand at that moment. An older tag's build that finishes last can no longer
  drag `latest` back onto itself.

## [1.1.3] - 2026-09-16

### Changed

- **The crypto window closes from a button in its footer, not from a corner X.**
  It carries its word and its glyph and follows the labelling setting like
  every other button: the word with its glyph, the word alone, or the glyph
  alone, whichever that one setting says.
- **The defect the corner was introduced for is still fixed.** This window had
  no visible way out at all: it has no footer of its own, and Modal draws its
  corner X only for a caller that asks for one, so this window fell between the
  two and left Escape and a click on the dimmed ground as the only exits.

## [1.1.2] - 2026-09-16

### Removed

- **The crest on the About card.** It was an easter egg: a small coat of arms
  beside the version line that turned when you pressed and held it, with the
  seven characters of this build's revision on its back. Gone with everything
  it touched - the component, the mark in the icon set, the turn in the
  stylesheet, the state that fed it, and its entry in the easter-egg list.
- **With it, the revision leaves the interface.** It still reaches the browser
  and `/api/health` still answers with it, but the back of that crest was the
  only place it was ever drawn. That matters on a `preview` build, where the
  version line reads the same in every build and the revision is what tells two
  of them apart; `docs/preview-deploy.md` points at /api/health for it now.

## [1.1.1] - 2026-09-16

### Fixed

- **The crypto window had no visible way out at all.** Escape closed it and so
  did a click on the dimmed ground, and neither of those is something a reader
  can see. It fell through a rule that is right everywhere else: a window only
  draws the X in its corner when it asks for one, because seventeen of this
  app's windows carry a Cancel button in their footer and an X above that
  offers the same answer twice. This window has no footer, because nothing in
  it is a decision, so it was the one window the rule left without an exit.
- **`common.close` exists in all forty-two languages now.** The word was
  missing as a shared string, which is part of why that corner control had
  never been asked for.

### Changed

- **The PayPal button opens a donation page rather than a handle.** It says who
  is being paid, carries a sentence about what the money does, offers three
  amounts and a free one, takes a card without a PayPal account, and has a box
  for making it monthly.

## [1.1.0] - 2026-09-16

### Added

- **Twelve finished features that nobody could reach.** Each was built, tested
  and wired to nothing: a volume cap with no card to set it on, a retry counter
  with no denominator on screen, an event list behind a bell that led nowhere, a
  disk report no page drew, plain-language failure advice with no dialogue to
  show it in. They are reachable now, and the largest piece of that is the
  download list from the keyboard.
- **The download list can be walked, selected and opened from the keyboard.**
  Arrows move a cursor, Shift extends the selection from the same anchor a
  Shift-click uses, Space picks a row out the way Ctrl-click does, left and
  right close and open a folder, Enter opens the properties panel and moves the
  focus there. It is a real tree with one tab stop rather than one per row, so
  Tab lands on the list and not on the four hundredth badge inside it. The list
  is windowed, so "focus row 3000" is not a call to `.focus()` at all: the row
  is not in the document, and a one-pixel probe is scrolled to its offset first.
  See `web/src/components/listKeyboard.ts`, which explains every awkward-looking
  line as one half of that single problem.
- **The selection says how much of itself is off screen.** A selection survives
  a search, a filter and a folded package on purpose, so somebody can hold
  eighteen rows while six are visible, and then Delete takes all eighteen. The
  action row now reads "18 selected, 12 of them not visible", the number is a
  button that drops the hidden ones, and the removal dialogue says the same
  before the press rather than the toast saying it afterwards. The dropped rows
  come back from the toast, because twelve rows picked one at a time are real
  work to lose.
- **The speed curve survives a reload.** The instance records it itself now, one
  sample a second for the last two minutes and one every ten seconds for the
  last hour, so the Overview curve draws filled instead of starting flat. It is
  held in memory only and is empty again after a restart, which the card says.
  A suspended machine or a moved clock fills the gap with idle rather than
  drawing one straight line across four missing hours.
- **The database can be checked, compacted and analysed.** An integrity check
  reads every page and reports what SQLite finds; compacting rewrites the file
  at its real size and gives back what deleted rows left inside it; analysing
  lets SQLite measure the tables again. On demand, or on an interval that ships
  off. The card names the file's size, what is free INSIDE it, and where the
  scratch copy of a compaction goes, which on a container is the temporary
  volume and not the data volume: without that, "database or disk is full" names
  the wrong disk.
- **Settings alone can be exported and taken back in, key by key.** Beside the
  full archive, and deliberately a different thing: the archive moves an
  INSTALL and applies at the next start, this moves a CONFIGURATION and applies
  live. The import previews every key and has to be confirmed. Stored passwords
  are NOT included unless a box that starts unticked is ticked, because an
  export that lands in a sync folder takes whatever it holds with it.
- **Events can be sent outward.** Operator-defined targets over plain HTTP:
  ntfy, Gotify, Matrix, or any webhook, with the address, the method, the
  headers and the body under the operator's control, a placeholder vocabulary
  and a test button. The bus had one subscriber and no way to add a second.
  There is no e-mail here and no half-built seam for one; SMTP is a second
  transport with its own credential and its own decisions, and it gets its own
  item.
- **A category can tell a media library to rescan.** One stored address per
  category drawer, called once, after the last file of a package has finished
  AND been moved into place. The one header value such an address may carry is
  sealed in the credential store rather than kept on the settings row, so it
  cannot reach the diagnostics bundle.
- **The end-of-queue action grew three more answers.** Besides doing nothing and
  pausing, an empty queue can now run one external program, quit KnightLoader
  cleanly, or suspend the machine. The command is a program and its arguments,
  never a shell line, and it is checked before it is armed rather than failing
  at three in the morning. It is redacted everywhere it could travel: the
  settings page, the diagnostics bundle, the log, and the program's own output
  before that reaches a log line.
- **yt-dlp can be kept current.** The Resolvers page shows which yt-dlp and
  which ffmpeg are actually being run and where they came from, and a button
  fetches a newer yt-dlp, verifies its checksum, proves it RUNS on this machine
  and only then swaps it in. Checking GitHub on page open is opt-in and ships
  off. There is no automatic install: replacing the extractor unattended
  silently changes what downloads produce, and the same line already stops
  KnightLoader from updating its own binary.
- **A start report at boot, and a self test on demand.** The boot pass names
  every tool it found with its version, every configured folder, and the clock
  and time zone, into the log and the diagnostics bundle. It only LOOKS: no
  probe file is written anywhere, because that would wake a spun-down array disk
  on every container restart. The write test happens when somebody presses the
  button, and only into a folder that already exists. Beside it, a self test
  that walks the instance's own checks, and a browser-side card for the reverse
  proxy in front of it, which is the only place a rewritten header can be seen
  at all.
- **A detailed health readout, per subsystem.** What is running, what is
  waiting, what failed, with a remedy for each, plus a Prometheus rendering of
  the same figures. Behind the same authentication as everything else and behind
  a switch that ships off: the list of routes that answer without a session is
  pinned in a test with a written justification per entry, and an unauthenticated
  metrics endpoint on a password-locked instance would have to be a decision
  somebody took on purpose.
- **All forty-two languages carry every one of the new strings.** The fourteen
  features above brought 661 new keys, and a key present only in English renders
  as English everywhere: `lib/i18n.tsx` resolves `dict[key] ?? en[key]`, so a
  missing translation is invisible rather than loud. Every catalogue now holds
  all 2566 keys with a translation behind each. What is still English is
  deliberate and written down, 638 values: program and file names (`yt-dlp`,
  `ffmpeg`, `settings.json`, `KL_YTDLP`), format strings holding no words at all
  (`{n}/{max} · {countdown}`), the legends printed on keys in the languages whose
  keyboards carry the Latin ones, and loanwords a language has taken over
  unchanged. Each of those was looked at in its own language rather than waved
  through in bulk, which is why the counts differ per language: thirty-four
  catalogues keep `Home`, twenty-nine keep `Del`.
- **Placeholders and dashes are guarded per language.** The extension's 120 keys
  have been checked this way since they were written and the web UI's 2566 had
  nothing, and the failure is quiet and one-language-deep: drop `{n}` and that
  language renders "Removed download(s)." with no number in it for ever, while
  `tsc` is perfectly happy because the type is `string`. Nobody reports that as a
  missing placeholder. The dash half caught 39 real ones in 32 catalogues,
  because the rule is usually written "no em dashes" and in German the
  Gedankenstrich is the EN dash: the source string carried one, and 36
  translators faithfully copied it past a green check. A dash between digits
  (`2020–2024`, `10–20 MB`) is correct typography and is left alone, because a
  check that flags those is one people learn to skip.
- **The files' owner and mask are on screen.** Which user and group downloads
  land as, per configured folder, and under which umask. It REPORTS and does not
  apply: this image runs as a fixed user, so PUID and PGID are not read, and the
  page says exactly that rather than implying a setting would take effect. A
  test reads the repository's own Dockerfile and fails in both directions, so
  the day the image changes, the wording has to change with it.

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
- **The login can be given a second factor, a passkey, or both** (GlimStone
  1.15.0, "The second way in"). Two cards on the Access page rather than one,
  because they are two decisions somebody can want separately: one makes the
  password harder to abuse, the other replaces typing it. A login gains a way
  IN and never a way INSTEAD - the password keeps working in both cases, and
  removing every passkey locks nobody out.
  - **The second factor** is a six-digit code from an authenticator app
    (RFC 6238, written out in `internal/secret` rather than pulled in as a
    dependency). The enrolment renders the step it is on rather than every
    control at once: scan or type the secret, prove it with a code, then eight
    single-use recovery codes shown exactly once - which the card says BEFORE
    it shows them, and which are acknowledged rather than dismissed. The status
    line reads the server's answer, never which screen the card is on, so a
    half-finished enrolment still says off. Turning it off costs the same proof
    as using it, which is stricter than an ordinary "are you sure" and for a
    different reason: not regret, but a session somebody walked away from.
  - **And the way back in, because there is no second person here.** An
    instance has one password and no user accounts, so nobody can unlock it for
    you. `knightloader -reset-2fa` turns the factor off from the command line
    and leaves the password alone; with the app stopped, deleting the `totp`
    and `recovery` entries from `auth.json` does the same by hand. Both need
    write access to the data directory, which is the access that could already
    delete `auth.json` outright and take the password with it - so this grants
    nothing new, and it is documented rather than hidden, because a way back
    nobody knows about is not a way back.
  - **Passkeys**, with the refusal that is most of the feature. WebAuthn binds
    a credential to a DOMAIN, so a browser refuses the exchange on a bare IP
    address and again on a certificate it does not trust - which is the default
    KnightLoader install, reached at `http://[LAN IP]:8749`. That gets no
    control at all plus the paragraph saying what is wrong, in the reader's own
    language: the server's verdict crosses over as a boolean and its English
    sentence stays a diagnostic for the log. A key records the address it was
    registered for, and one that cannot answer here is MARKED rather than
    hidden, because a key somebody deliberately created must never look lost.
  - **The login route grew a throttle**, and it had to: a password is long and
    slow to check, a six-digit code is a million possibilities and an HMAC. It
    sits in front of the whole route rather than only the code half, so failing
    the uncounted half cannot be the way around it.

### Changed

- **Two things that look wrong on a fresh install and are not.** Worth reading
  before filing either as a bug. First: the health card reports "running, with
  one fault" out of the box, because the download folder does not exist until
  something has been downloaded into it. That is deliberate and not a special
  case: the identical state is how a bind mount that did not come up presents,
  which is the expensive one, and the remedy sentence explains it in plain
  words. It clears itself the moment anything downloads. Second: anybody who
  has ever dragged a settings tab into their own order finds the new Zustand
  page at the BOTTOM of their rail rather than beside Diagnose, because the
  stored order is honoured first and whatever it does not name is appended.
- **The translation ledger has two lists now, and they mean opposite things.**
  `untranslated.json` tracked one thing: values still carrying English because no
  wave had reached them. That list is empty. What it could not express is a value
  that is English on purpose, so those 638 sat in it as debt, and the next seeding
  wave would have re-opened all of them and sent forty translators to answer the
  same question twice. They live in a second list with their reasoning written
  above them. Both are held against the catalogues the same three ways, and the
  checker refuses a key that appears in both. One rule is new and hard: a value of
  some length carrying several words may not be byte-identical to English unless
  one of the two lists says why. `de.ts` is the calibration for it, and the reason
  it can be trusted: that file is written by hand by a native speaker and 114 of
  its values equal the English one, because Downloads is Downloads and so are
  Status, Import and Online. A check counting those would fire on a correct file,
  and a check that fires on correct files gets switched off. Sentences are
  different: `de.ts` has none, the rule found eleven elsewhere, and all eleven
  turned out to be right to be English and are now written down rather than
  tolerated by a threshold.
- **The Wails CLI version is read out of `desktop/go.mod` rather than pinned in
  the workflow.** The two had drifted twice: `go.mod` on v2.13.0 while CI
  installed v2.10.2, then `go.mod` on v2.15.0 while CI still installed v2.13.0.
  Neither time did anything fail loudly, the CLI simply built the module with an
  older toolchain, and the comment saying "bump these together" was a rule with
  nothing enforcing it. The step now reads the require line and fails outright
  when it cannot, because a silent fallback to `@latest` is the failure it
  replaces. The documentation guard moved with it: it used to compare the README
  against the workflow, which is why it stayed green through the second drift,
  both files carrying the same wrong number. It reads `desktop/go.mod` now, which
  is the one place the answer actually comes from.
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

### Security

- **An event target's headers no longer follow a redirect to somebody else.** A
  target row's header values are treated as secrets throughout, and the comment
  above them says so in capitals, because that is where a ntfy token or a Matrix
  access token lives. The HTTP client sending those events followed redirects,
  and a redirect is a reply from the address the operator typed telling the
  client to repeat the request somewhere else, headers included. So an operator
  who mistyped a host, or whose ntfy instance moved, could hand a Matrix token
  to whatever answered at the new address, and nothing in the UI would have said
  anything went anywhere unusual. The client now refuses redirects outright and
  reports a 3xx as its own problem kind rather than folding it in with "the
  server said no", so the test button names what happened instead of showing a
  status code. `internal/mediahook` closed the same hole first; this was its
  sibling, and the fix names it so the next transport gets checked before it
  ships rather than after. The test was written before the fix and watched to
  fail: it stood up two servers, redirected from one to the other, and read
  `gotify-secret-value` out of `X-Gotify-Key` on the second.

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
