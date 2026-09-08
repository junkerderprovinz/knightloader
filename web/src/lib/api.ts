// The only imports in this file, and both type-only: the nav-label mode is
// owned by the store that hands it out (lib/navLabels.ts), not by the wire
// shape, so the Settings type below names that type rather than restating its
// four strings and drifting from it.
import type { NavLabelMode } from './navLabels';
// Second type-only import, same rule as the first: the event target row is
// owned by lib/eventtargets.ts, which also holds its fetches and its
// helpers, and restating its ten fields here is the copy that goes stale.
import type { EventTargetRow } from './eventtargets';

export type TaskStatus =
  | 'collected'
  | 'queued'
  | 'running'
  | 'paused'
  | 'extracting'
  | 'done'
  | 'error';

// Availability is what a check said about the link itself, which is separate
// from whether a download has been attempted.
//
// 'uncheckable' is the host being asked and refusing to say — a 429, a 503, a
// transport error, a resolver with no way to probe at all. It is not a synonym
// for '': a link nobody has checked and a link the host would not talk about
// need different words on screen, and folding the second into 'offline' is how
// one flaky minute gets a live link deleted.
export type Availability = '' | 'online' | 'offline' | 'uncheckable';

// Reason is the typed cause of a failure, as opposed to Task.error, which is the
// sentence beside it. The server sends 'gone' | 'auth' | 'limit' | 'unavailable'
// | 'network' | 'diskFull' | 'unsupported' | 'captcha' | 'cancelled' | 'botCheck'
// | 'membersOnly' | 'geoBlocked' | 'drm' | 'extractorBroken', or '' when nothing
// recognised the failure — a value it declines to guess at rather than one it
// forgot to set.
//
// The last five are named by the backend that hit them rather than by the shared
// classifier (core.Update.Reason): only the process that read the whole of
// yt-dlp's own output can tell "the site thinks we are a bot" from "the site said
// 403", because by the time a sentence reaches the classifier the distinguishing
// words have been truncated away.
//
// It stays an open string, not a union: the taxonomy grows on the server, and a
// union here would make every new value a compile error in a build that is
// otherwise perfectly able to show it. Anything reading it maps the values it
// knows and shows nothing for the rest (see reasonKey in components/columns.tsx).
export type Reason = string;

// Origin is the intake path a link arrived by — the paste box, the watch folder,
// Click'n'Load, a container upload. Open for the same reason as Reason: the
// values are named by the wave that starts writing them.
export type Origin = string;

export interface Task {
  id: string;
  url: string;
  name: string;
  package: string;
  resolver: string;
  /** Whether this goes out on an account or anonymously. Only set for a link a
   *  hoster is on the other end of; absent means the question does not apply. */
  mode?: 'free' | 'premium';
  /** What the backend is doing right now, in its own words, for a task that is
   *  running but not moving bytes: "Captcha recognition", "Waiting for
   *  reconnect". Not a failure - see core.Update.Note. */
  note?: string;
  size: number;
  loaded: number;
  speed: number;
  status: TaskStatus;
  error?: string;
  createdAt: string;
  dir?: string;
  password?: string;
  online?: Availability;
  retries?: number;
  /**
   * How many attempts this row is allowed, as the SERVER resolved it.
   *
   * It is the denominator and nothing else. `retries` above counts the ones
   * already spent, so a row is at "retry {retries} of {maxTries}" and never at
   * "attempt N": the first run is not a retry.
   *
   * ZERO IS "NOBODY HAS SAID", NOT "NO ATTEMPTS ALLOWED". Nothing has failed
   * yet, the failure settled on a branch that decides instead of counting (see
   * `gaveUp`), or the instance predates this field. All three drop the
   * denominator; a row rendering "of 0" is inventing one.
   *
   * NEVER RECOMPUTED HERE from settings.maxRetries, which is only the last link
   * of the chain: a host rule picked by the longest dot-boundary match, merged
   * over the per-reason table, merged over the global count, with "never" OR-ed
   * rather than fallen back. A second copy of that policy drifts on the first
   * change to it, and it could not even be fetched for the right box, because
   * the list routinely shows a PEER instance's tasks while fetchSettings is
   * hard-wired to this one.
   *
   * A SNAPSHOT AND NOT A LIVE READING. Raise the retry count from three to five
   * and a row that has already failed keeps saying "of 3" until it fails again:
   * the timer already armed was armed against the old ceiling, so the old
   * ceiling is the true answer for that row.
   */
  maxTries?: number;
  nextTry?: string;
  priority: number;
  position: number;
  checksum?: 'ok' | 'failed';

  /** What a Packagizer rule attached; nothing in the app acts on it. */
  comment?: string;
  /**
   * Connections this one download opens. 0 is "no opinion" and hands the count
   * to the global setting, not "no connections" and not "whatever the resolver
   * says" - a resolver's number is a ceiling on this one, never a replacement.
   */
  chunks?: number;
  /**
   * Per-task override of the global extraction switch.
   *
   * Deliberately tri-state, and `undefined` is the third state, not a missing
   * value: it means no rule had an opinion and the global decides. A rule that
   * switches unpacking off has to survive a global that is on, so a control
   * bound to this must offer inherit / on / off — rendering `undefined` as an
   * unchecked box silently turns "inherit" into "never".
   */
  autoExtract?: boolean;
  /** The Packagizer rules that shaped this task, in the order they fired. */
  matchedRules?: string[];

  /**
   * When the download settled as done. Always present in the JSON, because Go's
   * omitempty does not drop a zero time: an unfinished task carries the zero
   * timestamp "0001-01-01T00:00:00Z", not an absent field. fmtDate in
   * ./format.ts is what turns that back into an empty cell.
   */
  finishedAt?: string;
  /** The user's own switch for one link. Always sent, and true unless switched off. */
  enabled: boolean;
  /** Parked without failing: not started, not an error either. */
  skipped?: boolean;
  skipReason?: string;
  /** A link the user deliberately parked; "resume everything" must not start it. */
  hold?: boolean;
  /** Runs now, past the concurrency and per-host limits. */
  forced?: boolean;
  /**
   * The password a hoster asks for before handing over the file. NOT `password`,
   * which is the archive password tried when unpacking — two secrets, two
   * parties, and one label for both is how the wrong one gets typed.
   */
  downloadPassword?: string;
  /** A checksum supplied with the link rather than found beside the file. */
  expectedHash?: string;
  /** The outbound connection this download is routed over; empty = the machine's own. */
  connection?: string;
  /** The file host, which is not the resolver: through a debrid service every download would otherwise claim the same origin. */
  host?: string;
  /** The page a crawl found this link on. */
  source?: string;
  /** The task this one is a second copy of, when the mirror policy staged it. */
  mirrorOf?: string;
  /**
   * Whether an interrupted transfer can be picked up where it stopped.
   * `undefined` is a genuine third answer — nobody has asked yet — and must not
   * be shown as "no": warning about losing 4.2 GB of a transfer that resumes
   * fine is how people learn to click through the dialog.
   */
  resumable?: boolean;
  /** The name to write the file under when it is not the one the backend would choose. */
  filename?: string;
  /** Which form of the same resource was picked — a yt-dlp format, a quality. */
  variant?: string;
  /** A display-only best-effort file extension, shown next to name before a
   *  download has started — set only where it is genuinely certain ahead of
   *  time (see core.Task.Ext's own doc comment for exactly which variant
   *  kinds qualify and why the rest are deliberately left blank rather than
   *  guessed). */
  ext?: string;
  /** For a yt-dlp "Variante" video row, which quality presets the probed
   *  source genuinely offers — empty/absent means "no opinion yet" and the
   *  Variante column falls back to the full static menu. */
  availableQualities?: string[];
  /** For a yt-dlp "Variante" audio row, which of AudioFormats() the probed
   *  source's own audio-only tracks genuinely offer natively — empty/absent
   *  falls back to the full static menu, same convention as
   *  availableQualities above. */
  availableAudioFormats?: string[];
  /** For a yt-dlp "Variante" audio row, which of ApiOptions.ytdlpAudioBitrates
   *  the probed source's own best audio track can honestly support — same
   *  convention as availableAudioFormats above. */
  availableAudioBitrates?: string[];
  /** The audio row's own bitrate pick (yt-dlp's --audio-quality, e.g. "192"
   *  for 192 kbit/s) — meaningful only once the row's own format asks for
   *  an actual transcode, not a "best" extract. */
  audioBitrate?: string;
  /** A package the user chose by hand; automatic re-packaging leaves it alone. */
  manualPackage?: boolean;
  reason?: Reason;
  /**
   * Why a QUEUED task has not started - the counterpart to `reason`, which says
   * why a stopped one failed.
   *
   * Absent means nothing is holding it back: it is next, or it is not queued at
   * all. The server recomputes it on every dispatch pass, so a limit somebody
   * just raised stops being the answer by itself; nothing here has to clear it.
   *
   * Typed as a union but read defensively at the call site: a newer server may
   * send a value this build has never heard of, and a row that prints a raw
   * enum is worse than one that falls back.
   */
  // 'disk' was missing here while core.WaitingDisk was already being sent, so
  // the one reason the new disk guard produces was a value this side did not
  // believe in: the status cell fell through to "all slots busy", which is a
  // different and wrong explanation for a queue that is not moving. Found by
  // reading the Go constants against this union rather than by a test, because
  // an absent member of a string union is not an error anywhere - it just makes
  // the lookup miss and the fallback win.
  waiting?: 'slot' | 'host' | 'forced' | 'disabled' | 'hold' | 'captcha' | 'account' | 'halted' | 'disk' | 'volumeCap';
  /**
   * When the bytes STOPPED, not when the standstill was noticed - so the age of
   * a download dead since midnight reads as hours in the morning rather than as
   * the five seconds since the watcher last looked. The duration is computed
   * from this on every render; a stored one is stale in the second it is sent.
   *
   * Not persisted, on purpose: it describes a connection this process is
   * holding open, and a standstill restored from the database would describe
   * one that no longer exists.
   */
  stalledSince?: string;
  /** How often the watcher has restarted this task for standing still. */
  stallRestarts?: number;
  /**
   * Set when nothing will be tried again by itself: a never rule, a captcha, a
   * full disk. Deliberately NOT set for "attempts exhausted", which is curable
   * by raising the retry count - a flag that is set everywhere says nothing.
   */
  gaveUp?: boolean;
  origin?: Origin;
  /** When this task last changed. Zero-timestamp caveat as for finishedAt. */
  changedAt?: string;
  /** Volume number inside a multi-volume set, 0 for a file that is not in one. */
  archivePart?: number;

  /**
   * The multi-file selection tree for a torrent task - absent for every other
   * task, and absent for a single-file torrent, which never shows one. See
   * TorrentFile below and components/FileDrop.tsx for where it is built.
   */
  torrentFiles?: TorrentFile[];

  /**
   * The torrent swarm fields (11.5E: Peers/Seeds/Ratio columns, the row
   * tooltip's fuller detail - components/columns.tsx), all omitempty and all
   * absent for every non-torrent task. Mirrors core.Task field for field
   * (internal/core/task.go) - see that struct's own doc comment for why
   * these five specifically are never persisted to internal/store despite
   * arriving on every live task update: a peer count is true for the second
   * it was read, and writing it to disk only to show it stale after a
   * restart would be worse than not showing it at all.
   */
  /** How many peers the swarm has shown us, seeding or not. */
  peers?: number;
  /** How many of those are connected and complete. */
  seeds?: number;
  /** Uploaded over downloaded - what a seed target is measured against. */
  ratio?: number;
  /** Bytes sent to the swarm. */
  uploaded?: number;
  /**
   * A finished torrent still giving bytes back - a FLAG beside
   * `status === 'done'`, never a status of its own (build-plan.md section 4,
   * conflict 2: a new status value breaks every exhaustive mapping of the
   * seven this app already has).
   */
  seeding?: boolean;

  /**
   * Which torrent this is, not what its swarm is doing right now - set once
   * at stage time (app_torrents.go's AddTorrent, app_links.go's stage) and
   * never re-derived, unlike the five swarm fields above. UNLIKE those five
   * these two ARE persisted (internal/store/store.go's info_hash/trackers
   * columns, migration 13) - see core.Task.InfoHash's own comment for why.
   */
  infoHash?: string;
  trackers?: string[];
}

/** One file inside a multi-file torrent, and the tick beside it - mirrors
 *  core.TorrentFile field for field. Path is the file's path INSIDE the
 *  torrent, forward-slashed, never a path on this machine. */
export interface TorrentFile {
  path: string;
  size: number;
  selected: boolean;
}

/**
 * One RSS or Atom subscription. Mirrors feed.Subscription
 * (internal/feed/subscription.go) field for field.
 */
export interface FeedSubscription {
  /**
   * The feed document's address, http or https only. It is ALSO the
   * subscription's identity: the poller is keyed on it and so is the memory of
   * which entries have already been added, so a changed address is a different
   * subscription that starts over.
   */
  url: string;
  /**
   * Minutes between two fetches. 0 is "no opinion" and means the server's own
   * 15 (feed.DefaultIntervalMinutes); any other value is pulled into 1..10080
   * by feed.Sanitize rather than refused.
   */
  intervalMinutes: number;
  /**
   * A Go (RE2) regular expression an entry's title has to match before it is
   * staged. Absent or empty takes everything. It decides what is DOWNLOADED and
   * never what is remembered, so widening it later does not dump the feed's
   * current window into the collector. A pattern that will not compile is
   * REFUSED at save, never dropped.
   */
  titleFilter?: string;
  /** Where this subscription's downloads land, absent for the app's own
   *  download folder. May be a pathvars template, so a folder chooser has to
   *  keep the tail the way the download folder's does. */
  dir?: string;
  /**
   * One of the seven queue priorities (-3..3), absent when the subscription
   * named none. Absent and 0 are DIFFERENT: 0 is a real priority, which is why
   * the Go side holds a *int. Clamped into range by feed.Sanitize.
   */
  priority?: number;
}

/**
 * One level of the retry chain - mirrors settings.RetryRule
 * (internal/settings/settings_hostrules.go). Every number is SECONDS or a
 * count, never a Duration: this is what settings.json holds.
 *
 * ZERO IS "NO OPINION", not "none", on every field here. It hands the question
 * down to the next level (host rule, then the reason table, then the
 * instance-wide backoff, then the built-in 15s/10min pair), field by field, so
 * a row that sets only `delay` changes only the delay.
 */
export interface RetryRule {
  /** Wait before the first retry, in seconds. 0 takes the level below. */
  delay?: number;
  /** Where the doubling stops, in seconds. 0 takes the level below. */
  max?: number;
  /** How many attempts this gets at all. 0 takes the global maxRetries. */
  tries?: number;
  /**
   * Settles the task without arming any retry - its own end state, not "failed
   * after N attempts". Merged with OR rather than as a fallback, so `false`
   * here never clears a `true` set by the reason table.
   */
  never?: boolean;
}

/**
 * The backoff itself: the instance-wide delay and ceiling, plus the per-failure
 * table layered over them. Both halves may be absent on a document written by
 * an older server, so every read goes through `?.`.
 */
export interface RetryPolicy {
  /** Instance-wide backoff in seconds. 0 on either keeps the built-in
   *  fifteen-seconds-to-ten-minutes pair. */
  delay: number;
  max: number;
  /** Keyed by the server's own failure reason ("limit", "network", "gone",
   *  ...). May arrive as null: Go writes an empty map as JSON null. */
  byReason: Record<string, RetryRule> | null;
}

/**
 * What one host pattern may differ in - mirrors settings.HostRule
 * (internal/settings/settings_hostrules.go). One table and not three, because
 * connections, chunk count and backoff are all answers to "what does THIS
 * hoster tolerate".
 */
export interface HostRule {
  /** This host's own simultaneous-download ceiling. 0 takes global maxPerHost. */
  maxPerHost?: number;
  /**
   * Connections ONE download from this host opens. 0 takes global chunks.
   * An OVERRIDE and not a ceiling: it may be higher than the global number,
   * unlike a limit a resolver reports about the host, which can only lower.
   */
  chunks?: number;
  /** This host's own backoff, layered over the per-reason table. `omitzero` on
   *  the Go side, so it is absent rather than `{}` when nothing is set. */
  retry?: RetryRule;
}

/**
 * One named drawer, mirroring settings.Category. Every field except id and name
 * is an override of something that already has an answer a level up, and every
 * ABSENT value means "no opinion, use the level above" - which is why priority
 * and extract are optional rather than 0 and false: 0 is the middle priority
 * and false is a drawer that deliberately does not unpack.
 */
export interface Category {
  /**
   * The stable key Task.category points at. Send it empty on a row the client
   * has just created: the server derives it from the name once, on save, and
   * never re-derives it afterwards. Never change it to rename a category.
   */
  id: string;
  name?: string;
  /** Absolute, and may be a pathvars template. Empty = the global download folder. */
  dir?: string;
  /** -3..3. Absent is "no opinion" and is NOT the same as 0, the middle position. */
  priority?: number;
  /** Absent is "no opinion"; false is a drawer that deliberately keeps archives packed. */
  extract?: boolean;
  /** Bytes per second, 0 = no opinion. Stored and resolved; nothing enforces it yet. */
  speedLimit?: number;
  /**
   * 'rename' | 'skip' | 'overwrite', empty = the instance's own policy. A plain
   * string, not a union: the menu comes from GET /api/options.collisionPolicies,
   * so a value the server adds must render rather than fail to compile.
   */
  collision?: string;
  /**
   * The stored address called when a package filed in this drawer finishes and
   * its files are in place. Absent or empty means this drawer calls nothing,
   * which is a decision and not an unset field. An id no stored address matches
   * is REFUSED by the server on save, unlike every other field on this type.
   */
  notify?: string;
}

export interface Settings {
  maxConcurrent: number;
  maxPerHost: number;
  /**
   * Connections ONE download opens, when neither the task nor a rule named a
   * number. 0 is "no opinion" and not "none": the server holds the fallback, so
   * a client that helpfully filled in a 4 here would be inventing a second copy
   * of a number it does not own.
   */
  chunks: number;
  speedLimit: number; // bytes/s, 0 = unlimited
  extract: boolean;
  /**
   * Skips the collector entirely: an added batch is confirmed the instant it
   * stages, as if a person had clicked Confirm right away. This is what the
   * settings page's own "start added links immediately" toggle controls -
   * `autoStart` below answers a narrower, later question (does a CONFIRMED
   * batch start immediately or wait), not this one. See settings.go's own
   * three-way split doc comment on the Go side.
   */
  autoConfirm: boolean;
  /** Seconds the collector waits before an unconfirmed batch auto-confirms on
   *  its own - 0 disables the countdown. */
  autoConfirmDelay: number;
  /** What a CONFIRMED batch does next: start immediately (true, the default -
   *  preserves this app's behaviour from before autoConfirm/autoStart were
   *  split apart) or wait on Hold for a person to start it by hand. */
  autoStart: boolean;
  /** confirm.Policy value ("include"|"exclude"|"exclude-and-remove"|"ask") for
   *  a link that duplicates one already in the list at confirm time. */
  onDupes: string;
  /** Same shape as onDupes, for a link already known offline at confirm time. */
  onOffline: string;
  /** A newly-confirmed batch is placed at the front of the queue rather than
   *  the back. */
  addAtTop: boolean;
  downloadDir: string;
  subfolderByPackage: boolean;
  /**
   * Where a download's bytes are written while it is still arriving. The
   * finished file is moved into downloadDir once nothing is owed on it any
   * more, after the checksum and after any extraction. '' writes straight to
   * the destination, which is what every install had before this field existed
   * and the only safe default. Always an absolute path and never a pathvars
   * template: several downloads heading for one destination share this one
   * folder, so a template would split a multi-volume archive's parts across
   * four of them - see sanitizeStaging (internal/settings/settings_staging.go).
   */
  workDir: string;
  /**
   * The named drawers: a folder, a queue position, an unpacking switch, a speed
   * limit and a collision rule under one word, referred to by Task.category.
   * Mirrors settings.Settings.Categories (internal/settings/settings_categories.go).
   *
   * The server always sends the key (no omitempty, deliberately - see the Go
   * field), but read it through `?? []` anyway: an older server predates it.
   */
  categories: Category[];
  /**
   * The stored addresses called once a package has finished and its files have
   * been moved into place. Mirrors settings.MediaHooks.
   *
   * The server always sends the key (no omitempty, deliberately - see the Go
   * field), but read it through `?? []` anyway: an older server predates it.
   * Note that the card that edits these does NOT ride the settings draft, so
   * this array can be one save behind what GET /api/mediahooks answers.
   */
  mediaHooks: MediaHook[];
  archivePasswords: string[];

  /**
   * Where extractions are collected. Empty means beside the archive, which is
   * what every install did before the setting existed. May be a pathvars
   * template, so the folder chooser has to keep the tail - see FolderPicker.
   */
  extractTo: string;
  /** Each package in its own folder below extractTo. Does nothing without one. */
  extractSubfolder: boolean;
  /**
   * Where the CONTENT of a finished extraction is moved once it has finished
   * unpacking. '' leaves it where it unpacked. Not a second extractTo:
   * extractTo is where the unpacking WRITES, this moves the finished files
   * afterwards, and it moves the files rather than the release folder. May be a
   * pathvars template, so the folder chooser has to keep the tail - see
   * FolderPicker. A Packagizer rule that named a folder for the link wins over
   * it (app.extractMoveTarget).
   */
  extractMoveTo: string;
  /** What an extraction does when its destination folder is already there. */
  extractCollision: string;
  /**
   * What becomes of an archive that unpacked cleanly: 'keep' | 'trash' |
   * 'delete'.
   *
   * This key REPLACED the boolean `deleteArchive`, and nothing here may send
   * the old spelling again. The server maps an old settings file once, at load,
   * and a client that kept writing the boolean would undo that migration on
   * every save - which is the whole failure a field that changes type causes.
   * Typed as a plain string rather than a union because the menu comes from
   * GET /api/options: a value the server adds must render, not fail to compile.
   */
  archiveDisposal: string;
  /** How long a trashed archive stays before the sweep takes it. 0 never sweeps. */
  trashRetentionDays: number;
  /** Sweep the .nfo/.sfv/.diz/.url that came with the same package. */
  deleteInfoFiles: boolean;
  maxRetries: number;

  /**
   * Per-host exceptions to maxPerHost, chunks and the retry backoff, keyed by
   * host pattern - mirrors settings.Settings.HostRules. A pattern matches the
   * host and any subdomain of it on a dot boundary, longest match wins.
   *
   * `null` and `{}` mean the same thing (no rows, every host keeps the global
   * numbers) and BOTH occur on the wire: the Go field has no `omitempty`, so a
   * nil map encodes as `null` on any install whose settings.json predates the
   * key, while Defaults() writes `{}`. Same pairing rainbowPalette already uses
   * - read it as `cfg.hostRules ?? {}` everywhere.
   */
  hostRules: Record<string, HostRule> | null;
  /** The instance-wide backoff and its per-failure table - the level a host
   *  row's own retry values fall through to. */
  retry: RetryPolicy;

  /**
   * How long a RUNNING download may move no bytes before it is marked as
   * standing still, in seconds. 0 (the default) never marks anything, which is
   * how this app behaved before the mark existed.
   *
   * Never send 1..59. sanitizeStall raises anything in that range to
   * MinStallTimeout (60) - see internal/settings/settings_stall.go for why a
   * shorter timeout would mark healthy downloads rather than find dead ones -
   * so a spinner that offers 30 is a control that lies about what saving it
   * did. 86400 (one day) is the server's ceiling.
   */
  stallTimeout: number;
  /**
   * Hands a marked download back to the wait queue and starts it from the top.
   * Off by default and deliberately a switch of its own: the mark costs
   * nothing, while the restart throws away the bytes the stalled attempt had
   * already fetched. Torrents are exempt server-side whatever this says
   * (app.stallRestartDueLocked).
   */
  stallRestart: boolean;
  /**
   * How many of those automatic restarts one download gets. 0 means
   * DefaultStallRestarts (3), NOT unlimited, and the server caps at 20.
   */
  stallMaxRestarts: number;

  /**
   * Bytes kept free BEYOND what a download still has to fetch, measured against
   * that download's own remaining bytes (app_diskguard.go's admit). The only
   * one of the three that ships on: 536870912 (0.5 GiB,
   * settings.DefaultDiskReserve). 0 is off. Negative saves as 0 and anything
   * over 1 PiB clamps to 1 PiB - see sanitizeDiskSpace, which never refuses.
   */
  diskReserve: number;
  /**
   * Free bytes under which NO new download starts, whatever its size and even
   * when its size is unknown. 0 is off, which is the default. Raised to
   * diskCriticalSpace on save whenever that one is higher - the repair goes
   * this way round on purpose, see settings_diskspace.go.
   */
  diskLowSpace: number;
  /**
   * Free bytes under which everything already RUNNING is stopped and put back
   * in the wait queue, checked every 15 seconds. 0 is off, which is the
   * default. Costs more than diskLowSpace: a non-resumable transfer loses the
   * bytes it had fetched.
   */
  diskCriticalSpace: number;

  /**
   * How many bytes may finish downloading in one period. 0 is NO CAP: the
   * counter keeps running and nothing is ever held back. That is also why there
   * is no master switch for this feature and no "off" among the actions below,
   * one off state in one field, the way historyMax and the three disk floors
   * already work. Decimal gigabytes on screen, raw bytes here.
   */
  volumeCap: number;
  /**
   * Day of the month the counter goes back to zero, 1..31. In a month shorter
   * than this the server uses that month's last day; it never rolls into the
   * next month.
   */
  volumeCapResetDay: number;
  /**
   * What happens once the cap is reached. There is deliberately no "off" value:
   * the off state is volumeCap === 0, and an action here with no cap set does
   * nothing at all.
   */
  volumeCapAction: VolumeCapAction;
  /**
   * Bytes per second while capped, read only for the 'throttle' action. It is a
   * ceiling BESIDE the other limits and never instead of them: a schedule
   * window or quiet mode asking for less still wins.
   */
  volumeCapThrottle: number;

  /**
   * When two DIFFERENT URLs count as the same file - dedupe.Policy ("off" |
   * "filename-only" | "size-only" | "filename-and-size" | "filename-or-hash" |
   * "hash-only"). A plain string, not a union: the menu comes from
   * GET /api/options.mirrorPolicies, so a policy the server adds must render
   * rather than fail to compile. The same URL twice is a fact and is refused
   * whatever this says.
   */
  mirrorPolicy: string;
  /** Keeps a folded-away copy as a parked sibling row instead of dropping it.
   *  On its own it starts nothing. */
  keepMirrors: boolean;
  /** Releases that parked sibling when the download it copies has finished
   *  failing. Does nothing without keepMirrors. */
  mirrorFailover: boolean;
  /**
   * How much the "already on the disk" pass may believe about a file it did not
   * watch arrive: "checksum" | "record" | "size". A plain string for the same
   * reason as mirrorPolicy above - the menu comes from
   * GET /api/options.reclaimTrustModes.
   */
  reclaimTrust: string;

  /**
   * What a restart does with the downloads that were in flight: 'never' |
   * 'running' | 'all'. A plain string, not a union - the menu comes from
   * GET /api/options, so a mode the server adds must render rather than fail to
   * compile.
   *
   * The default is 'never' and the reason belongs next to the control: no
   * backend handle survives the process, so a resumed transfer starts from the
   * beginning, and the partial already on disk meets the collision policy.
   */
  resumeOnStart: string;
  /**
   * What a download does when the destination name is already taken: 'rename' |
   * 'skip' | 'overwrite'. A plain string and not a union, matching
   * archiveDisposal and resumeOnStart: the menu comes from
   * ApiOptions.collisionPolicies, so a value the server adds must render rather
   * than fail to compile. Only the built-in engine can be told a name, so a
   * link handed to JDownloader, TorBox or yt-dlp honours skip and nothing else
   * (app.HonoursCollisionPolicy).
   */
  collisionPolicy: string;
  /**
   * How many counted names "rename" tries before it gives up. 0 means the
   * package's own cap of 1000 and never "unlimited". Marked omitempty on the Go
   * side, so it is simply absent from the JSON whenever it is 0 - read it as
   * `cfg.collisionMaxAttempts ?? 0` and do not trust the non-optional type
   * here, the same way Archives.tsx reads trashRetentionDays.
   */
  collisionMaxAttempts: number;
  /** Days a finished download stays in the LIST. 0 keeps it forever. */
  keepFinishedDays: number;
  /** How many entries the history keeps. 0 keeps every one. */
  historyMax: number;
  /**
   * How often the database looks after itself without being asked, in days.
   * 0 - the shipped value, and what every existing install already has -
   * means only when somebody presses a button on the diagnostics page.
   * Offered as 0/30/90/180; the server clamps anything above 365.
   */
  maintenanceIntervalDays: number;
  /**
   * Whether the scheduled run also rewrites the file, or only reads it and
   * reports. False, because compacting needs room for a full second copy of
   * the database on the TEMPORARY volume, which on a container is not the
   * data volume. The Compact button always compacts, whatever this says.
   */
  maintenanceCompactOnSchedule: boolean;

  /**
   * The optional copy of the server's own log on disk. Off on every install
   * that upgrades into it: an update must not start writing files nobody asked
   * for, and on an Unraid box the data folder is usually on the array. maxMb is
   * clamped to 1..1024 and keep to 0..20 by the server; keep = 0 is a real
   * answer, meaning "only the file being written", and not a way to switch the
   * log off. There is deliberately no path: it sits beside the database and
   * KL_LOG_DIR moves it, because a mistyped path is the one way to stop a log
   * with nothing on screen to say why.
   */
  logFile: { enabled: boolean; maxMb: number; keep: number };

  crawl: boolean;
  /**
   * How many pages deep a crawl goes: 1 is the pasted page alone, 2 also
   * follows the pages it links to, 3 follows theirs. Capped server-side at
   * internal/crawler.MaxDepth.
   *
   * 1 by default and deliberately so - nobody may get a three-level crawl of
   * somebody else's forum out of an update they did not read.
   */
  crawlDepth: number;
  /** How many pages one crawl may FETCH. It counts requests, not links found. */
  crawlMaxPages: number;
  /**
   * Keeps a deep crawl on the pasted page's own host. Host exactly, so a
   * subdomain is a different site, and it never restricts the FILES that come
   * back - see crawler.Options.SameHost for both halves of the reasoning.
   */
  crawlSameHost: boolean;
  /**
   * Regular expressions matched against the URLs a crawl meets. They are not
   * symmetric: exclude keeps the crawl away from pages as well as files, while
   * include only ever narrows what is staged. An include applied to pages too
   * would cut the walk off at the first hop and make crawlDepth do nothing.
   */
  crawlInclude: string[];
  crawlExclude: string[];
  watchDir: string;
  /** The RSS and Atom subscriptions this instance follows. `null` and never an
   *  empty array on the wire (feed.Sanitize returns nil for an empty list) is
   *  the off state a fresh install has, the same pairing captchaSolverOrder
   *  already uses. */
  feeds: FeedSubscription[] | null;
  /**
   * Where this instance reports to when something happens: one row per
   * address, with the method, headers, body template and events the
   * operator chose. See lib/eventtargets.ts for the row itself.
   *
   * OPTIONAL as well as nullable, UNLIKE feeds directly above, and the
   * difference is the whole upgrade story: the Go field is omitempty, so a
   * settings.json written before this feature existed carries no key at all
   * and this instance sends nothing. A page reading it does `?? []`.
   *
   * Every header value in here arrives as eight stars and is sent back
   * untouched; the server merges the real one in, and only while the row
   * still points at the same address.
   */
  eventTargets?: EventTargetRow[] | null;
  verifyChecksums: boolean;
  /**
   * Scans a paste or drop for links wherever they sit in it, instead of
   * reading one line as one link verbatim. JDownloader's own
   * AddLinksPreParserEnabled by another name - off is that older, literal
   * reading, kept reachable as an escape hatch.
   */
  preParserEnabled: boolean;
  shape: 'round' | 'soft' | 'square';
  accent: string;
  rainbow: boolean;
  rainbowReactive: boolean;
  rainbowRotate: boolean;
  rainbowSeed: number;
  rainbowPalette: string[] | null;

  /** Hides the sidebar's own "Konten" nav item - the settings tab and the
   *  nav item render the same page either way, so this only ever removes a
   *  second entry point to it, never the page itself. */
  hideAccountsFromSidebar: boolean;
  /** The same for the "Instanzen" nav item, and the same relationship to its
   *  settings tab - see the server field's own doc comment. */
  hideInstancesFromSidebar: boolean;
  /** How much of a navigation entry is drawn, in the sidebar AND the settings
   *  rail at once - see lib/navLabels.ts for what each of the four means and
   *  internal/settings/settings_appearance.go for why "hover" is not the
   *  collapsing rail it sounds like. */
  navLabels: NavLabelMode;
  autoUpdateCheck: boolean;
  /** Meaningless without autoUpdateCheck also being on, and only ever acted
   *  on by the desktop build - see internal/settings/settings.go's own doc
   *  comment on the mirrored server field. */
  autoUpdateInstall: boolean;

  /** One request to api.github.com when the Resolvers page loads, and
   *  nothing beyond it: it never downloads and never replaces anything.
   *  Off by default. There is deliberately no auto-INSTALL companion for
   *  yt-dlp - replacing the extractor unattended silently changes what
   *  every download produces. */
  ytdlpVersionCheck: boolean;

  /**
   * Which automatic captcha solvers to try, and in what order, before a
   * captcha ever reaches a human - catalogue ids ('2captcha' | 'anticaptcha',
   * see CatalogueService, group 'captchaSolver') present in this list in try
   * order; an id absent from it is never tried. Mirrors
   * settings.Settings.CaptchaSolverOrder (internal/settings/settings.go) -
   * membership and order are the one fact, so there is no separate `enabled`
   * flag to disagree with where an id sits in the list. `null` (never an
   * empty array on the wire - see sanitizeCaptcha) is what a fresh install
   * has, the same pairing `rainbowPalette` already uses for "nothing chosen
   * yet, behave as if this setting did not exist".
   */
  captchaSolverOrder: string[] | null;

  /**
   * What happens once the wait queue has nothing enabled left to run, start
   * or finish, after a cancellable countdown - mirrors
   * settings.Settings.IdleAction (internal/settings/settings.go). See
   * IdleActionConfig below for the shape, and fetchIdleAction/cancelIdleAction
   * for the live state this field alone cannot answer (whether the queue is
   * idle right now, whether a countdown is actually running).
   */
  idleAction: IdleActionConfig;

  /**
   * The yt-dlp backend's own configuration - mirrors settings.Settings.Ytdlp
   * / ytdlp.Options (internal/resolver/ytdlp/options.go). See YtdlpOptions
   * below for the shape and for why every field's zero value changes
   * nothing about how this backend downloads.
   */
  ytdlp: YtdlpOptions;
  /** Per-host "Variante" defaults - mirrors settings.Settings.YtdlpPresets
   *  (internal/settings/settings.go). Keyed by the lower-cased, www-stripped
   *  host a task carries. A host with no entry gets all five variants on, best
   *  quality, best audio. YtdlpHosterPreset is already declared below. */
  ytdlpPresets: Record<string, YtdlpHosterPreset>;

  /**
   * This instance's own identity - mirrors settings.Settings.InstanceName /
   * KnownDomains (internal/settings/settings_identity.go). Both are optional
   * and only ever change what this instance offers when pairing or building
   * its own QR code (routes_pairing.go's pairingSelf, routes_remote.go's
   * preferredAddress) - neither is required for anything else to work.
   */
  instanceName: string;
  /** Full URLs (scheme included, e.g. "https://kl.example.com"), remembered
   *  automatically the first time a request arrives on one, or added by hand
   *  for a domain that is configured but has not been visited through yet. */
  knownDomains: string[];
}

/**
 * One configured account row, mirroring app.AccountState (internal/app/app_accounts.go) -
 * one per stored or container-supplied credential, never one per catalogue
 * entry. The secret itself is never part of this shape: `configured` is all
 * the page ever learns about whether one is set.
 */
export interface Account {
  /** What every account/label/enabled call sends back - see app.metaKey. */
  id: string;
  /** Catalogue id - accounts.Lookup(service) in CatalogueService[]. */
  service: string;
  /** "" for a service's default (only) account, a caller-chosen id otherwise. */
  account: string;
  label: string;
  enabled: boolean;
  configured: boolean;
  /** fromEnv + envVar together are the reason a credential is read-only on the
   *  page - see app.AccountState. */
  fromEnv: boolean;
  envVar?: string;
  ok: boolean;
  detail: string;
  hosts: number;
  /**
   * When this service's ROUTING host list (the set links are actually
   * matched against, refreshed on a timer and on demand - see
   * app.fetchDebridHosts) was last actually obtained. RFC3339, or absent
   * before the very first successful fetch - format with fmtDate. A
   * different question from `hosts`: that is a count from the last live
   * "Refresh" this row ran; this is when the number ROUTING is using was
   * last confirmed current, which a failing service can leave older than
   * that without ever going empty.
   */
  hostsFetchedAt?: string;
  /**
   * The account-health ticker's cached reading - never the result of a call
   * made while answering this request (see app.AccountHealth). "unknown" is
   * the default until the ticker's first successful read, and it must never
   * be treated as "free": those are different facts, and conflating them is
   * how a paying user ends up watching this column call their account Free.
   */
  tier: string;
  /** {used, limit, unlimited, resetsAt} - see TrafficState. Check `unlimited`
   *  before ever computing a percentage from `used`/`limit`: both are the zero
   *  value while it is true, and a bar fed a zero maximum reads as "out of
   *  traffic", the opposite of what unlimited means. */
  traffic: TrafficState;
  /** Filled in by the account-health refresher; empty means "not fetched yet",
   *  rendered as a dash rather than treated as a real answer. RFC3339 when
   *  present - format with fmtDate. */
  expiry?: string;
  trafficLeft?: string;
}

/** One account's traffic allowance - app.TrafficState (internal/app/app_accounts.go). */
export interface TrafficState {
  used: number;
  limit: number;
  unlimited: boolean;
  /** RFC3339, or absent when the service does not say when this resets. */
  resetsAt?: string;
}

/** Which shape of secret a service needs - accounts.Kind (internal/accounts/catalogue.go). */
export type ServiceKind = 'apiKey' | 'usernamePassword';

/**
 * Which section of the accounts page a service belongs to - accounts.Group.
 * 'captchaSolver' (accounts.GroupCaptchaSolver) is deliberately not rendered
 * by Accounts.tsx at all - see that group's own Go doc comment - a solver's
 * credential is configured on settings/Captcha.tsx instead.
 *
 * 'remoteServer' (accounts.GroupRemoteServer) has no section on this page yet
 * either, and unlike the solver group it has no other page to go to: a login
 * for the user's own seedbox, NAS or Nextcloud is stored through
 * POST /api/accounts today, with the SERVER'S HOSTNAME as the account id.
 * The value is listed here so the type still matches what the server sends,
 * which is the point of this union - a group missing from it makes every
 * catalogue row a lie about its own type.
 */
export type ServiceGroup = 'debrid' | 'hoster' | 'captchaSolver' | 'remoteServer';

/**
 * One entry in the service catalogue GET /api/accounts/catalogue returns -
 * mirrors accounts.Service (internal/accounts/catalogue.go). This is the
 * single source the accounts page reads for what services exist, which
 * section each belongs to and what form its credential takes; never a
 * hardcoded list here.
 */
export interface CatalogueService {
  id: string;
  label: string;
  kind: ServiceKind;
  group: ServiceGroup;
  /** Set when a container env var can supply this credential instead of the
   *  encrypted store - absent for a service with no such override. */
  env?: string;
  whereUrl: string;
}

/** What a credential POST/verify body carries - accounts.Credential's wire shape. */
export interface AccountCredential {
  apiKey?: string;
  username?: string;
  password?: string;
}

export interface VerifyResult {
  ok: boolean;
  hosts: number;
  detail: string;
}

export interface QueueState {
  halted: boolean;
  stopMark?: string;
  running: number;
}

export interface AuthState {
  enabled: boolean;
  authenticated: boolean;
}

export interface Instance {
  /**
   * The address every proxied call is built from (`/api/instances/${name}`)
   * - for a relay peer this is always its InstanceID, never the name it
   * announced, so it never changes on its own when something else about the
   * reachable set changes (federation.Manager.reachable's own doc comment
   * has the full reasoning). Never render this for a relay peer; render
   * displayName instead.
   */
  name: string;
  url: string;
  /**
   * What a relay peer calls itself, present only when it differs from
   * `name` (i.e. only for a relay peer that has announced one). Purely a
   * label - nothing addresses a peer by it, which is what lets two peers
   * safely share one. Fall back to `name` when absent.
   */
  displayName?: string;
  /**
   * Set only for a peer that is reachable through the relay right now -
   * federation.Instance.RelayID, the instance ID a call to it is addressed
   * by. A stored peer never carries one, so this is also how the UI tells
   * the two apart: a relay peer exists exactly as long as the relay says so
   * and has no address of its own to show, which is why `url` is empty for
   * it rather than guessed at.
   */
  relayId?: string;
}

/**
 * What GET /api/diagnostics answers: everything a bug report needs, and
 * everything the downloadable bundle contains - the diagnostics page's live
 * preview and its download button read this one shape, so there is nothing
 * the page shows that the saved file does not also carry.
 *
 * `settings` is deliberately untyped further than "an object": it is the same
 * redacted document GET /api/settings sends (see that route's own comment),
 * carried along for whoever reads the saved file rather than rendered field
 * by field here - Settings above is itself only a subset of what is really in
 * it, for the same reason (see settings/context.tsx's SettingsDraft comment).
 */
export interface Diagnostics {
  generatedAt: string;
  version: string;
  /** "container" or "desktop" (internal/buildinfo.Deployment) - which binary produced this bundle. */
  deployment: string;
  goVersion: string;
  os: string;
  arch: string;
  goroutines: number;
  /** Which yt-dlp and ffmpeg this instance runs, where each came from and
   *  what version it reports - the first question every "this video link
   *  stopped working" report has to answer. */
  mediaTools: MediaToolsStatus;
  settings: Record<string, unknown>;
  /** How many archive passwords are configured - the values themselves are never in this bundle. */
  archivePasswordCount: number;
  logLines: string[];
  logCapacity: number;
  /**
   * Whether those lines are also being kept on disk, and how much of them.
   * `path` is always EMPTY here: the bundle is a file people attach to public
   * bug reports and a desktop data directory carries somebody's real name, so
   * the server strips it (logring.FileState.Redacted). The real path comes from
   * fetchLogFileState below, which only ever reaches somebody already looking
   * at their own settings pages.
   */
  logFile: LogFileState;
  /**
   * How many samples the speed record holds right now, out of 120 (one a
   * second, two minutes) and 360 (one per ten seconds, an hour). Zero on both
   * means nothing is recording and the Overview curve will stay flat; a full
   * record with an old recordingSince means the sampler is fine and the box was
   * quiet. Held in memory only, so it starts empty after every restart.
   */
  speedSamplesRecent: number;
  speedSamplesHour: number;
  speedRecordingSince?: string;
  /** The store file plus any journal beside it. Paths are deliberately NOT in this bundle. */
  storeBytes: number;
  /** Space deleted rows left inside the file. A floor: compaction usually gives back more. */
  storeReclaimableBytes: number;
  settingsBytes: number;
  /** False on an install that has never saved a settings page - settings.Load never writes. */
  settingsPresent: boolean;
  /**
   * Who this instance writes files as - the same document GET /api/fileowner
   * answers. Carried here so a bug report has the uid without anybody being
   * asked for it.
   */
  ownership: FileOwnerIdentity;
  /**
   * Every configured folder as a stat saw it. NO PATH, deliberately: this
   * bundle is attached to public bug reports and the desktop build's default
   * download folder sits inside the user's own home directory. The role and
   * the numbers are the whole finding.
   */
  ownedFolders: FolderOwnership[];
  /**
   * The reading the start checks took once, just after this instance came up.
   *
   * NULL WHEN NOTHING EVER STARTED ONE - a test, or a build that does not run
   * it - which is not the same claim as "everything passed". An empty check
   * list drawn as a clean bill of health is the worst version of that mistake,
   * so the two are kept apart on the wire.
   *
   * It is the BOOT reading and stays the boot reading: runStartupCheck answers
   * with a fresh one and deliberately does not replace this, because what was
   * true at start is what a bug report needs.
   */
  startup: StartupReport | null;
}

/**
 * What every operation on a whole selection answers with: the ids actually
 * touched, so the interface can say "12 removed" without re-fetching the list to
 * work out which twelve.
 */
export interface BulkResult {
  ids: string[];
  count: number;
  /**
   * The token that puts a removal back, set by deleteTasks alone and only when
   * the files were left where they are. Absent means there is nothing to undo -
   * either nothing was removed, or the files went with it and the row alone is
   * not what anybody would be restoring.
   */
  undo?: string;
  /**
   * How long that token stays good, in milliseconds.
   *
   * Read from the answer rather than assumed here: the server owns the window
   * (app.UndoWindow), and an "Undo" button that outlives it is a button that
   * answers "nothing to undo" to a press that looked perfectly in time.
   */
  undoMs?: number;
}

/**
 * The "clean up…" entries. The union exists so a caller cannot mistype a class
 * into a request that answers 400 at runtime — but the *menu* must be built from
 * fetchOptions().cleanupClasses, not from this array: the server owns which
 * classes it implements, and a menu entry it does not recognise is a button that
 * fails when pressed.
 */
export const CLEANUP_CLASSES = [
  'finished',
  'offline',
  'disabled',
  'duplicates',
  'incompleteArchives',
] as const;

export type CleanupClass = (typeof CLEANUP_CLASSES)[number];

/** A link that never became a task, kept so the collector can say what happened to it. */
export interface SkippedLink {
  url: string;
  /** What the mirror set decided: "duplicate" or "mirror". */
  kind: string;
  /** The sentence to show, which names what the match rests on. */
  reason: string;
  /** The task it was folded into. */
  ofId?: string;
  /** The signal the match rests on (file name, byte count, …). */
  signal?: string;
  at: string;
}

/** The fixed choices the settings form offers, so no dropdown is hard-coded here. */
export interface ApiOptions {
  mirrorPolicies: string[];
  collisionPolicies: string[];
  /**
   * confirm.Policy values that are valid as an INSTANCE default: include,
   * exclude, exclude-and-remove, ask. confirm.UseGlobal is withheld by the
   * server (routes_settings.go's confirmPoliciesForAGlobalDefault) because a
   * global default cannot defer to itself. Served since the confirm split
   * landed; this type simply never declared it.
   */
  confirmPolicies: string[];
  /** reclaim.Trust tiers, strictest first: "checksum" | "record" | "size". */
  reclaimTrustModes: string[];
  /** The ceiling on the category table (settings.MaxCategories). A number and
   *  not a list: the table's own limit belongs with the menus, because a 64
   *  hardcoded in the browser is a second copy of a Go constant and the copy is
   *  the one that goes stale silently. */
  maxCategories: number;
  /**
   * The archive page's own three lists, and deliberately not the download ones
   * above. An extraction honours a different set of collision policies from a
   * download - it has nobody to ask, and it decides per folder rather than per
   * file - and the formats are whatever readers this build was compiled with.
   * A list typed into this file instead would go on promising a format the
   * server stopped opening, with nothing anywhere to catch it.
   */
  archiveCollisions: string[];
  archiveDisposals: string[];
  archiveFormats: string[];
  /**
   * What a restart may do with what was in flight. Served rather than typed
   * here for the same reason as the three above, and it was the one that went
   * missing: resumeOnStart was honoured at boot with no control anywhere, so it
   * could only be set by editing settings.json by hand.
   */
  resumeModes: string[];
  /** The folder "trash" really means, so the help text can name it. */
  archiveTrashFolder: string;
  proxyKinds: string[];
  // No rule vocabulary here. The rule editor builds its form from
  // GET /api/rules/grammar, which the engine generates, so that an operator this
  // build refuses can never appear in a dropdown.
  scheduleActions: string[];
  cleanupClasses: CleanupClass[];
  /** The resolver options page's own quality menu, and the "Variante"
   *  preset editor's own audio-format menu - ytdlp.Qualities()/
   *  AudioFormats() (internal/resolver/ytdlp/options.go), served for the
   *  same reason as every list above it: a value this build cannot honour
   *  must never be selectable. */
  ytdlpQualities: string[];
  ytdlpAudioFormats: string[];
  /** ytdlp.AudioBitrates() (internal/resolver/ytdlp/options.go) - the
   *  audio row's own --audio-quality menu, "" meaning no opinion. */
  ytdlpAudioBitrates: string[];
  /** The methods a media library call may be sent with ('GET', 'POST'), from
   *  the package that sends them. Optional: an older server does not serve it,
   *  and the method strip stays out rather than guessing. */
  mediaHookMethods?: string[];
}

/** A container that was a plain link list: parsed here and staged like any paste. */
export interface ContainerStaged {
  kind: string;
  links: number;
  created: Task[];
  handedTo?: undefined;
}

/**
 * A container that was encrypted. It is not decrypted here and never will be —
 * the key is issued by a service to registered clients — so it goes to the
 * headless JDownloader backend, which has its own. Nothing has been staged yet
 * when this comes back: the links appear when JD gets round to fetching it.
 */
export interface ContainerHandedOver {
  kind: string;
  handedTo: 'jd';
  /** Seconds the handover address stays fetchable. */
  expiresIn: number;
}

export type ContainerResult = ContainerStaged | ContainerHandedOver;

/**
 * ApiError is a refusal the server explained, carrying the typed half when it
 * sent one.
 *
 * `code` is what makes the message translatable. Without it a caller can only
 * show the server's sentence, which is English, and the alternative - having the
 * server translate - would need the reader's language on every request and would
 * write the log in whichever language asked last.
 */
export class ApiError extends Error {
  code?: string;
  params?: Record<string, string | number>;
  /**
   * The HTTP status the request failed with, when there was one.
   *
   * Carried because some failures need telling apart and the sentence alone
   * cannot do it: a peer that REFUSED a call and a peer that could not be
   * reached both surface as an exception, and they need completely different
   * things from the user - a pairing code in one case, a look at the network in
   * the other.
   */
  status?: number;

  constructor(message: string, code?: string, params?: Record<string, string | number>, status?: number) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.params = params;
    this.status = status;
  }
}

/**
 * json decodes a response, and refuses to decode one the server said no to.
 *
 * The check belongs here rather than at each call site, and it was missing:
 * `saveSettings` handed a 400 straight to `r.json()`, so refusing to save a
 * half-filled reconnect showed the user `SyntaxError: Unexpected token 'r',
 * "reconnect:"... is not valid JSON`. The server had written a perfectly clear
 * sentence and the client turned it into a parser error - for every validated
 * row on the settings page, not only that one.
 */
async function json<T>(r: Response): Promise<T> {
  if (!r.ok) {
    const body = (await r.text()).trim();
    // Validation failures send a JSON envelope so the message can be
    // translated; everything else sends the sentence as text. Both are
    // understood here, because a route that has not been taught the envelope
    // must still be able to explain itself.
    try {
      const p = JSON.parse(body) as { error?: string; code?: string; params?: Record<string, string | number> };
      if (p && typeof p.error === 'string') throw new ApiError(p.error, p.code, p.params);
    } catch (e) {
      if (e instanceof ApiError) throw e;
      // Not JSON at all, which is the ordinary case.
    }
    throw new ApiError(body || String(r.status), undefined, undefined, r.status);
  }
  return (await r.json()) as T;
}

/**
 * ok throws with the server's own words when a request failed.
 *
 * Used on the routes whose refusal is the feature rather than an accident: "no
 * JD backend is configured, which is the only thing that can open this
 * container", "offline is not a cleanup class, the app knows finished, …". A
 * caller that swallows those and shows a generic failure leaves the user with no
 * way to find out what to change.
 */
async function ok(r: Response): Promise<Response> {
  if (!r.ok) throw new Error((await r.text()).trim() || `${r.status}`);
  return r;
}

// post is the shape every command endpoint takes: JSON in, status out.
const post = (path: string, body: unknown) =>
  fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });

// apiBase returns the API prefix for an instance ('' = this instance).
export const apiBase = (instance: string): string =>
  instance ? `/api/instances/${encodeURIComponent(instance)}` : '/api';

export async function fetchTasks(base = '/api'): Promise<Task[]> {
  return (await json<Task[]>(await fetch(`${base}/tasks`))) ?? [];
}

/**
 * taskFileURL is where a task's own file streams from - inline for an
 * allowlisted type, a download prompt for everything else, decided entirely
 * server-side (see internal/api/routes_files.go). Not fetched through this
 * client: it is opened directly (a new tab, an <a href>), the same as any
 * other link, so the browser's own download/viewer handling applies.
 */
export const taskFileURL = (id: string, base = '/api'): string => `${base}/tasks/${encodeURIComponent(id)}/file`;

/**
 * Whether a base points at THIS instance rather than at a federated peer.
 *
 * It exists because the peer path is not merely slower, it is wrong: the
 * federation proxy forwards everything under "tasks/", reads at most 32 MB of
 * the answer into memory, forwards no Range header at all, and relabels
 * whatever comes back as application/json. A player pointed at that gets a
 * truncated body with a lying content type. Anything that streams bytes asks
 * this first.
 */
export const isLocalBase = (base: string): boolean => base === '/api';

/**
 * What GET /api/tasks/{id}/file would answer, asked with HEAD so nothing is
 * transferred to find out.
 *
 * Go's ServeMux answers a "GET" pattern for HEAD as well, so this is the same
 * handler, the same safety check and the same three refusals: 404 is "nothing
 * on disk yet", 400 is "not this app's file to serve" (a task the JD sidecar
 * fetched), 403 is a stored path that would leave its own folder. Kept as a
 * number rather than folded into a boolean, because the three call for three
 * different sentences.
 *
 * `bytes` is Content-Length, which http.ServeContent measures by seeking the
 * open file, so it is what is on disk THIS second and not the total the hoster
 * announced. A running download grows between two calls.
 *
 * `contentType` is the server's own answer from its extension allowlist. It is
 * never sniffed from the file, so it is safe to reason about.
 */
export interface TaskFileHead {
  ok: boolean;
  status: number;
  /** Bytes on disk right now; 0 when the server sent no length. */
  bytes: number;
  /** Lower-cased, parameters stripped: "video/mp4", not "text/plain; charset=utf-8". */
  contentType: string;
  /** Whether byte ranges were offered, which is what a player seeks with. */
  ranges: boolean;
}

export async function taskFileHead(id: string, base = '/api'): Promise<TaskFileHead> {
  const r = await fetch(taskFileURL(id, base), { method: 'HEAD' });
  const len = Number(r.headers.get('Content-Length') ?? '');
  return {
    ok: r.ok,
    status: r.status,
    bytes: Number.isFinite(len) ? len : 0,
    contentType: (r.headers.get('Content-Type') ?? '').split(';')[0].trim().toLowerCase(),
    ranges: (r.headers.get('Accept-Ranges') ?? '').toLowerCase() === 'bytes',
  };
}

/**
 * hosterIconURL is one host's own site icon, fetched and cached by the server
 * (internal/app/app_hostericons.go). Opened as an <img src>, not through this
 * client: a 404 is the ordinary answer for a host with no favicon, and an
 * <img> already has the right way to say so - its onError, which the component
 * turns into a monogram.
 */
export const hosterIconURL = (host: string, base = '/api'): string =>
  `${base}/hosters/icon?host=${encodeURIComponent(host)}`;

export async function addLinks(links: string, pkg: string, base = '/api'): Promise<Task[]> {
  const r = await fetch(`${base}/links`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ links, package: pkg }),
  });
  return (await json<Task[]>(r)) ?? [];
}

/**
 * The add-links form's own per-batch options (build-plan.md §8A): every field
 * is optional, and an empty object behaves exactly like the plain `addLinks`
 * above. `dir`, `password` (the archive password) and `downloadPassword` (what
 * a hoster's own page asks for - NOT the archive password) apply to the whole
 * batch. `priority`, `autoExtract` and `comment` apply too, but a matching
 * Packagizer rule wins over them UNLESS `overrule` is set - see the server's
 * own comment on app.LinkBatchOptions for the full precedence and why the
 * destination is never part of that bargain.
 */
export interface AddLinksOptions {
  package?: string;
  origin?: string;
  dir?: string;
  password?: string;
  downloadPassword?: string;
  comment?: string;
  priority?: number;
  autoExtract?: boolean;
  overrule?: boolean;
}

/**
 * addLinksWithOptions is `addLinks` plus the form's own per-batch fields. A
 * destination that cannot be used refuses the WHOLE batch with the server's
 * own sentence - `json` below throws an `ApiError` carrying it - rather than
 * staging every link to the wrong folder in silence, so callers must let that
 * throw reach the person who typed the path.
 */
export async function addLinksWithOptions(
  links: string,
  opts: AddLinksOptions,
  base = '/api',
): Promise<Task[]> {
  const r = await fetch(`${base}/links`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ links, ...opts }),
  });
  return (await json<Task[]>(r)) ?? [];
}

/**
 * What a start actually did.
 *
 * The route answered 204 to everything, and "nothing happened" had three
 * different causes behind it: the queue was halted, a link filter was holding
 * the named tasks, or the ids matched nothing. The shape is the server's - see
 * App.StartTasks in internal/app/app_queue.go - named the same on both sides so
 * a field cannot mean one thing there and another here.
 */
export interface StartResult {
  started: number;
  skipped: number;
  /** Passed over because their own switch is off - see app.StartResult. */
  disabled?: number;
  /** The queue was taken off a halt the user had set by hand. */
  released: boolean;
  /** A schedule window is holding the queue; the tasks are queued and waiting. */
  blocked: boolean;
}

// startTasks moves collected tasks into the download queue (empty = start all).
export const startTasks = async (ids: string[], base = '/api'): Promise<StartResult> => {
  const r = await fetch(`${base}/tasks/start`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids }),
  });
  // An instance older than this answer replies 204 with no body. Read as "it
  // started something and had nothing to report", which is what a 204 meant
  // here before there was anything to say.
  return (
    (await json<StartResult>(r)) ?? { started: ids.length, skipped: 0, released: false, blocked: false }
  );
};

// setPackage moves tasks into a package (empty name = ungrouped).
export const setPackage = (ids: string[], pkg: string, base = '/api') =>
  fetch(`${base}/tasks/package`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids, package: pkg }),
  });

/**
 * restartTasks re-runs finished/errored tasks (empty ids = all errored).
 *
 * `reasons` narrows that to the causes named, using core.Reason's own values -
 * '' included, which is the group nothing classified. Left out, it means every
 * cause, which is what this call has always meant. Given ids AND reasons, the
 * server intersects them; see app.RestartTasksIn for why that, and not a union.
 */
export const restartTasks = (ids: string[], base = '/api', reasons?: Reason[]) =>
  fetch(`${base}/tasks/restart`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    // Omitted rather than sent as an empty array, so an older instance sees the
    // exact body it saw before this field existed.
    body: JSON.stringify(reasons && reasons.length > 0 ? { ids, reasons } : { ids }),
  });

export const pause = (id: string, base = '/api') =>
  fetch(`${base}/tasks/${id}/pause`, { method: 'POST' });
export const resume = (id: string, base = '/api') =>
  fetch(`${base}/tasks/${id}/resume`, { method: 'POST' });
// remove drops a task from the list. withFiles additionally deletes what was
// downloaded, which is never the default.
export const remove = (id: string, base = '/api', withFiles = false) =>
  fetch(`${base}/tasks/${id}${withFiles ? '?files=1' : ''}`, { method: 'DELETE' });

// recheckTasks re-resolves collected links and refreshes their online state
// (empty = every collected link).
export const recheckTasks = (ids: string[], base = '/api') =>
  post(`${base}/tasks/recheck`, { ids });

/**
 * ExtractJob is one unpacking, as work in its own right rather than a status a
 * download wears for a while.
 *
 * `status` stays an open string for the same reason `Reason` does: the server
 * names the values, and a union here would make a new one a compile error in a
 * build that could perfectly well show it. `taskId` is the volume the job was
 * started on, which for a multi-volume set is the FIRST part and not whichever
 * one finished last.
 */
export interface ExtractJob {
  id: string;
  taskId: string;
  name: string;
  dir: string;
  package?: string;
  status: string;
  /** The file open right now, which at depth is one found inside the output. */
  archive?: string;
  depth?: number;
  files: number;
  bytes: number;
  volumes: number;
  nested?: number;
  error?: string;
  /** The failure was a missing password, which is the one with an obvious remedy. */
  password?: boolean;
  queuedAt: string;
  startedAt?: string;
  endedAt?: string;
}

export async function fetchExtractJobs(base = '/api'): Promise<ExtractJob[]> {
  return (await json<ExtractJob[]>(await fetch(`${base}/extract`))) ?? [];
}

/**
 * startExtraction unpacks finished downloads now, whatever the unpacking switch
 * says - pressing it IS the answer to that question.
 *
 * A selection where some rows cannot be unpacked answers 207 with both halves:
 * the jobs that did start, and a sentence naming what was refused. Throwing that
 * sentence is deliberate, because the jobs are already running and the caller
 * reads them off the stream like any other.
 */
export async function startExtraction(ids: string[], base = '/api'): Promise<ExtractJob[]> {
  const r = await fetch(`${base}/extract/start`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids }),
  });
  if (r.status === 207) {
    const p = (await r.json()) as { refused?: string };
    throw new ApiError(p.refused || String(r.status));
  }
  return (await json<ExtractJob[]>(r)) ?? [];
}

/**
 * abortExtraction calls one unpacking off and removes its half-written output.
 *
 * The refusal is thrown rather than returned as a response nobody reads: the one
 * way to get here wrongly is to press stop on a job that has just finished, and
 * a button that silently does nothing about that is how somebody presses it four
 * more times.
 */
export async function abortExtraction(id: string, base = '/api'): Promise<void> {
  const r = await post(`${base}/extract/${id}/abort`, {});
  if (!r.ok) throw new ApiError((await r.text()).trim() || String(r.status));
}

// setPriority lifts or drops tasks in the wait queue (-2..2, higher runs first).
export const setPriority = (ids: string[], priority: number, base = '/api') =>
  post(`${base}/tasks/priority`, { ids, priority });

// moveTasks reorders the queue by hand.
export const moveTasks = (ids: string[], where: 'top' | 'bottom', base = '/api') =>
  post(`${base}/tasks/move`, { ids, where });

/**
 * The per-task overrides, as the properties panel sends them.
 *
 * Every field is optional and the omission is the point: the server leaves a
 * field it was not sent exactly as it was, so a panel editing forty rows must
 * send only what the user actually changed. A key present with an empty string
 * is a deliberate clearing and is treated as one.
 *
 * `autoExtract` is the one field where `null` is a value rather than an absence:
 * it means "inherit the global switch", which is a different answer from `false`.
 * Spread into the body it survives JSON.stringify, whereas `undefined` does not -
 * which is exactly how the two stay apart on the wire.
 */
export interface TaskOptionsPatch {
  name?: string;
  dir?: string;
  password?: string;
  /**
   * What a hoster's own page asks for before it hands over the file. NOT
   * `password` above, which is the archive password tried when unpacking -
   * two secrets, two parties, one label for both is how the wrong one gets
   * typed into the wrong prompt.
   */
  downloadPassword?: string;
  comment?: string;
  priority?: number;
  /**
   * Connections this one download opens. 0 is a real value and not an omission:
   * it takes the override off again and hands the count back to the rule and the
   * global setting. So it may only be sent when the user actually typed it - a 0
   * filled in as a default would silently clear what a rule had set.
   */
  chunks?: number;
  autoExtract?: boolean | null;
  /**
   * The "Variante" column's own edit: a video row's resolution preset, or
   * an audio row's format - the sub-value half of the task's own `variant`
   * string (see Task.variant's own doc comment). '' is a real answer ("no
   * opinion"), not an omission - send this key only when the user actually
   * changed the row's own picker.
   */
  variantQuality?: string;
  /**
   * The audio row's own second, independent picker (core.TaskOptions.
   * AudioBitrate's own doc comment, app_tasks.go) - a bitrate on top of the
   * format above, not a replacement for it. '' is a real answer ("no
   * opinion, ffmpeg's own default"), same convention as variantQuality.
   */
  audioBitrate?: string;
}

// setTaskOptions applies per-task overrides; omitted fields stay as they are.
export const setTaskOptions = (ids: string[], opts: TaskOptionsPatch, base = '/api') =>
  post(`${base}/tasks/options`, { ids, ...opts });

// --- Operations on a whole selection -------------------------------------
//
// These take the selection in one request because the interface acts on a
// selection: a route per id turns a hundred-row selection into a hundred
// requests, a hundred store writes and a hundred broadcasts, which is slow
// enough to look broken and can fail halfway. They all answer with the ids
// actually touched, so nothing has to re-fetch the list to find out what
// happened. Everything under /api/tasks/ is forwarded to a peer instance, so
// they take a base; the routes below this block are not, and do not.

/** setEnabled switches a selection of links on or off. */
export const setEnabled = async (ids: string[], enabled: boolean, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/tasks/enabled`, { ids, enabled })));

/** setHold parks a selection, or lets it go again. */
export const setHold = async (ids: string[], hold: boolean, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/tasks/hold`, { ids, hold })));

/** setForced marks a selection to run ahead of the concurrency limits. */
export const setForced = async (ids: string[], forced: boolean, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/tasks/force`, { ids, forced })));

/**
 * deleteTasks removes a selection from the list. `withFiles` additionally erases
 * what was downloaded — a separate argument rather than a variant of the same
 * one, because it is never implied by removing a row and the confirmation that
 * precedes it has to name the file count and the bytes.
 */
export const deleteTasks = async (ids: string[], withFiles = false, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/tasks/delete`, { ids, files: withFiles })));

/**
 * undoDelete puts back the rows one removal took.
 *
 * A token that has expired, or one that was already spent, restores nothing and
 * is NOT an error: the window closing is the ordinary end of a token's life, and
 * the answer's own count is what the caller reports. Anything that throws here
 * is a real failure - the instance is unreachable, or the request never arrived.
 */
export const undoDelete = async (token: string, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/tasks/undo-delete`, { token })));

// --- Cleanup classes ------------------------------------------------------
//
// Not forwarded to a peer instance (the proxy carries only the task and link
// routes), so these act on this instance and take no base.

/**
 * cleanupPreview reports which tasks a class would take, and takes none of them.
 * Every class can select more than the user pictured, and a confirmation that
 * can only say "12 downloads" is a confirmation nobody reads.
 */
export const cleanupPreview = async (cls: CleanupClass) =>
  json<BulkResult>(await ok(await fetch(`/api/cleanup/${encodeURIComponent(cls)}`)));

/** runCleanup removes everything in a class and reports what it removed. */
export const runCleanup = async (cls: CleanupClass, withFiles = false) =>
  json<BulkResult>(
    await ok(
      await fetch(`/api/cleanup/${encodeURIComponent(cls)}${withFiles ? '?files=1' : ''}`, {
        method: 'POST',
      }),
    ),
  );

// --- The trace of links that never became tasks ---------------------------

/**
 * fetchSkipped lists the links that were folded into one already in the list,
 * oldest first. A link that disappears with nothing to show for it looks exactly
 * like a bug in the paste box, and gets reported as one.
 */
export async function fetchSkipped(): Promise<SkippedLink[]> {
  return (await json<SkippedLink[]>(await fetch('/api/collector/skipped'))) ?? [];
}

/** clearSkipped empties that trace. */
export const clearSkipped = () => fetch('/api/collector/skipped', { method: 'DELETE' });

// --- Link containers ------------------------------------------------------

/**
 * uploadContainer sends a .txt/.dlc/.ccf/.rsdf file.
 *
 * Two outcomes, and the caller has to tell them apart: a plain link list comes
 * back staged (`created`), while an encrypted container is handed to the JD
 * backend and *nothing exists yet* — the links appear later, over the websocket,
 * when JD has fetched it. Reporting "0 links added" for the second is what makes
 * people upload the same file four times.
 *
 * A failure throws with the server's sentence, which is the whole point on this
 * route: "this container is encrypted and only the JD backend can open it, none
 * is configured" is an instruction, and a generic error is not.
 */
export async function uploadContainer(file: File, pkg = ''): Promise<ContainerResult> {
  const form = new FormData();
  form.append('file', file);
  if (pkg) form.append('package', pkg);
  // No Content-Type header: the browser has to set the multipart boundary, and
  // setting it by hand produces a body the server cannot parse.
  return json<ContainerResult>(await ok(await fetch('/api/containers', { method: 'POST', body: form })));
}

// --- Torrent upload and the file-tree step ---------------------------------

/** What POST /api/torrents/parse hands back: enough to draw the file tree,
 *  and the `uri` the follow-up stageTorrent call needs. Nothing is staged by
 *  this call - it is a preview, matching the collector's own new step
 *  (components/FileDrop.tsx) that shows a tree before staging continues. */
export interface TorrentTree {
  uri: string;
  infoHash: string;
  name: string;
  private: boolean;
  totalSize: number;
  pieceLength: number;
  pieces: number;
  files: TorrentFile[];
  trackers: string[];
  droppedTrackers: number;
}

/**
 * parseTorrentUpload sends a .torrent file and gets back its file tree.
 *
 * A failure throws with the server's own sentence - "this .torrent's piece
 * layout does not match the data it describes" is an explanation, and "invalid
 * file" is what sends somebody re-uploading the same broken one, the same
 * reasoning uploadContainer's own doc comment gives.
 */
export async function parseTorrentUpload(file: File): Promise<TorrentTree> {
  const form = new FormData();
  form.append('file', file);
  return json<TorrentTree>(await fetch('/api/torrents/parse', { method: 'POST', body: form }));
}

/**
 * stageTorrent is the confirm step: the `uri` parseTorrentUpload returned,
 * with a file selection, becomes a task. `selectedPaths` names the files to
 * KEEP (not the ones to drop) - omit it to keep every file selected, which is
 * what a single-file torrent that never showed a tree wants, and what Parse
 * itself defaults to.
 *
 * The server re-derives the real file list from its own fresh parse of `uri`
 * and only ever narrows it against `selectedPaths` - a path that was never in
 * the torrent has no effect, so this cannot be used to smuggle a fabricated
 * entry onto the task. See routes_torrents.go's own comment on stageTorrent.
 *
 * Returns `null` when the mirror set folded this into a task already in the
 * list - the same "nothing new to show" outcome addLinks's own duplicate
 * handling already has, just for a single result instead of an array.
 */
export async function stageTorrent(
  uri: string,
  pkg: string,
  selectedPaths?: string[],
): Promise<Task | null> {
  return json<Task | null>(
    await fetch('/api/torrents', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ uri, package: pkg, selectedPaths }),
    }),
  );
}

/**
 * fetchOptions is every fixed choice the settings and cleanup menus offer, taken
 * from the packages that implement them so a menu can never offer a value the
 * server does not know.
 */
export async function fetchOptions(): Promise<ApiOptions> {
  return json<ApiOptions>(await fetch('/api/options'));
}

// --- Room on the target folders -------------------------------------------
//
// GET /api/diskspace, and deliberately with NO `base` parameter: the route is
// not on the federation forwarder's list and not on the relay allowlist, so
// asking a peer for it answers 403. A row built from this describes THIS
// machine's disks whatever list is on screen, which is why the shell strip only
// draws it while the scope is local, the same rule SpeedLimitField obeys.

/** One target folder as the server measured it. */
export interface DiskVolume {
  /** The folder the app would write into. It may not exist yet, and for a path
   *  template it is the fixed head of that template. */
  dir: string;
  /**
   * The folder the figures actually describe: `dir` itself when it is there,
   * otherwise the nearest existing folder above it. The two differ harmlessly
   * for a download folder nobody has written into yet, and they differ
   * ALARMINGLY when a mount did not come up - this is then the volume root and
   * the numbers describe a completely different disk. Show it whenever it is
   * not `dir`; hiding it is how a confident wrong number gets in front of
   * somebody.
   */
  measured: string;
  /** Whether `dir` itself is a folder today. */
  exists: boolean;
  /**
   * Whether this platform could be asked at all. False is a real third answer
   * and not a zero: internal/diskspace has no call on some kernels and every
   * guard then holds nothing back. When this is false, free, used and total are
   * all 0 and mean NOTHING - draw no figure and no bar, not even an empty one.
   */
  known: boolean;
  /**
   * Bytes this process may still write here. 0 with `known: true` is a
   * genuinely exhausted volume and must print as "0 B", never through fmtBytes,
   * which answers the no-data dash for 0.
   */
  free: number;
  /** Bytes somebody's files occupy. */
  used: number;
  /**
   * The volume's size. `free + used` can be LESS than this, by the root reserve
   * or by a quota. Draw the bar from used/total and print free on its own;
   * never derive any one of the three from the other two.
   */
  total: number;
  /**
   * What the downloads still owed would add here: announced size minus what has
   * arrived, per task, clamped at zero. A task whose size nobody knows adds
   * nothing, so this is a floor.
   *
   * AND IT IS NEVER SUBTRACTED FROM `free`. A transfer already running has
   * usually had its room taken out of the volume when it started, so its bytes
   * are missing from free rather than sitting on top of it. The two figures
   * stand side by side.
   */
  queued: number;
  /** How many downloads are aimed here, INCLUDING the ones whose size nobody
   *  knows and which therefore add nothing to `queued`. */
  tasks: number;
  /** Why this folder is in the list: 'downloads' | 'category' | 'work' | 'task'.
   *  A plain string, because the server names the roles and a value it adds
   *  must render rather than fail to compile. */
  role: string;
}

export interface DiskReport {
  /** Never null: the server sends an empty array. One row per FOLDER and not
   *  per disk - two folders on one volume repeat that volume's figures and
   *  nothing here can tell that they do, so never sum `free` across rows. */
  volumes: DiskVolume[];
  /** The queue named more destinations outside the configured folders than are
   *  worth a syscall each on a route every tab polls, and the ones owed least
   *  were left out. Every configured folder is always present. */
  truncated: boolean;
  /** When the reading was taken. Cached for a few seconds on purpose, so this
   *  is older than "now" and the interface should say so rather than imply a
   *  live gauge. */
  sampledAt: string;
}

export async function fetchDiskSpace(): Promise<DiskReport> {
  return json<DiskReport>(await fetch('/api/diskspace'));
}

// --- The detailed health readout -------------------------------------------

/**
 * What one part of this instance is doing.
 *
 * OPEN ON PURPOSE, the same shape FeatureVerdict in pages/settings/features.ts
 * already uses and for the same reason: the server may learn a state before
 * this build has a word for it, and every lookup goes through `key in en`
 * (lib/useHealthReport.ts) so an unknown one renders as its raw id rather than
 * as a blank cell. Never `as`-cast a server string into a closed union here -
 * that freezes an assumption a newer server breaks silently.
 *
 * "unused" is not set up on this instance and "unknown" cannot be asked on this
 * build or platform. Neither is a fault, and neither ever makes `status` worse
 * than ok.
 */
export type HealthState = 'ok' | 'degraded' | 'failed' | 'unused' | 'unknown' | (string & {});

export interface HealthSubsystem {
  /** store, queue, disk, jd, ytdlp, accounts, feeds, relay, captcha. A stable
   *  id the interface looks a label up by, never a word from the server - which
   *  has no idea which of the 42 locales is reading. */
  id: string;
  state: HealthState;
  /** The failing service's OWN words, in whatever language it speaks. Draw it
   *  beside a translated state, never instead of one. */
  detail?: string;
  /** A stable id a sentence is looked up from (health.remedy.<id>). Absent for
   *  a row nothing can be done about. */
  remedy?: string;
  /** When this row last CHANGED state, RFC3339. Absent until a state has been
   *  seen twice: the first reading knows the state and cannot know when it
   *  started, and stamping "now" would claim a fault began when the page was
   *  opened. */
  since?: string;
}

export interface HealthTaskCounts {
  running: number;
  waiting: number;
  paused: number;
  extracting: number;
  collected: number;
  /** Counted ACROSS the states above rather than instead of them, so the
   *  buckets still add up to the list on screen. */
  disabled: number;
  /**
   * How many rows are sitting in the list with an error on them RIGHT NOW.
   *
   * A GAUGE AND NOT A TALLY. It falls when somebody clears a row and when the
   * retention sweep trims the list, and it says nothing at all about how often
   * anything has failed. Do not draw it as a total, and do not chart it as one.
   */
  failed: number;
  /** core.Waiting id -> count and core.Reason id -> count, with zero-valued
   *  keys left out. Never null: the server always sends an object. The ids are
   *  the ones task.waiting.* / task.reason.* already label for the download
   *  list, so a breakdown needs no strings of its own. */
  waitingBy: Record<string, number>;
  failedBy: Record<string, number>;
}

export interface HealthReport {
  /** The worst row, where "not in use here" and "cannot be checked here" never
   *  make it worse than ok. */
  status: HealthState;
  version: string;
  deployment: string;
  startedAt: string;
  /** Computed on the server against its own clock, so a browser in another
   *  timezone or with a clock a few minutes out cannot print an uptime that is
   *  wrong by exactly that much. */
  uptimeSeconds: number;
  /** Every part, always all of them, in a fixed order. Never null - a row that
   *  vanished when it had nothing to say would be a row nobody can find. */
  subsystems: HealthSubsystem[];
  tasks: HealthTaskCounts;
  /** GET /api/diskspace's own rows verbatim, so this page and the Downloads
   *  page can never disagree about a folder. Never null - and read DiskVolume's
   *  own `known` before drawing any figure out of it. */
  volumes: DiskVolume[];
  halted: boolean;
  quiet: boolean;
  /** When the PROBED rows were taken. Shared for half a minute on purpose, so
   *  this is older than "now" and whatever draws it should say so rather than
   *  imply a live gauge. */
  sampledAt: string;
}

/**
 * The detailed readout. It is NOT /api/health, which answers two fields and the
 * literal "ok" for as long as the process is up because a container health
 * check, the Click'n'Load bridge and the phone app's instance discovery all
 * read it - see internal/api/routes_health.go.
 *
 * No `base`: the route is on neither forwarding allowlist, so a peer answers
 * 403, and that refusal is the point. This describes THIS machine.
 */
export async function fetchHealthReport(): Promise<HealthReport> {
  return json<HealthReport>(await fetch('/api/health/detail'));
}

// --- The volume curve and the monthly allowance ----------------------------

export type VolumeCapAction = 'report' | 'pause' | 'throttle';

/**
 * One bucket of the volume curve.
 *
 * `key` is already bucketed BY THE SERVER, in the server's own local calendar:
 * 'YYYY-MM-DD' for a day, 'YYYY-MM' for a month. Render it as the string it is,
 * or build a label from its digits. Feeding it to `new Date()` parses it as UTC
 * midnight, which draws every bar a day early anywhere west of Greenwich, and
 * the cap goes on being charged against the server's day either way.
 */
export interface VolumeBucket {
  key: string;
  /** Announced size of everything that finished in this bucket. */
  bytes: number;
  /** How many downloads that was. */
  count: number;
  /** How many of those had no size at all. They add nothing to `bytes`, so this
   *  is the one number that says whether the total understates itself. */
  unsized: number;
  /**
   * Bytes per file host, and per backend.
   *
   * NULLABLE, and not for tidiness: Go marshals an empty map as JSON `null`
   * rather than `{}`, so a gap-filled bucket arrives with nothing to index
   * into. Guard before reading.
   */
  byHost: Record<string, number> | null;
  byResolver: Record<string, number> | null;
}

/** GET /api/stats/volume */
export interface VolumeStats {
  /** Newest last, one entry per day, gaps filled with a zero bucket. */
  days: VolumeBucket[];
  /** Newest last, one entry per month, gaps filled with a zero bucket. */
  months: VolumeBucket[];
  /** The zone the server bucketed by, so the chart can say whose calendar it is. */
  timeZone: string;
  /** The oldest finish time the history still holds. Anything before it is not
   *  "zero downloaded", it is "no longer recorded". */
  oldest?: string;
  /** The history is at settings.historyMax, so the oldest months have been cut. */
  trimmed: boolean;
}

/** GET /api/stats/volume/usage, and the payload of the 'volume' websocket kind. */
export interface VolumeUsage {
  /** Bytes finished since periodStart. Moves when a download finishes, never
   *  while one runs. */
  used: number;
  /** 0 means no cap is set; `reached` is then always false. */
  cap: number;
  action: VolumeCapAction;
  throttle: number;
  periodStart: string;
  periodEnd: string;
  reached: boolean;
}

export async function fetchVolumeStats(): Promise<VolumeStats> {
  return json<VolumeStats>(await fetch('/api/stats/volume'));
}

export async function fetchVolumeUsage(): Promise<VolumeUsage> {
  return json<VolumeUsage>(await fetch('/api/stats/volume/usage'));
}

export async function fetchQueue(base = '/api'): Promise<QueueState> {
  return json<QueueState>(await fetch(`${base}/queue`));
}

/** setQueue toggles the master switch and/or arms the stop mark. */
export async function setQueue(
  patch: { halted?: boolean; stopMark?: string },
  base = '/api',
): Promise<QueueState> {
  return json<QueueState>(await post(`${base}/queue`, patch));
}

// --- The queue's own vocabulary -------------------------------------------
//
// Everything here takes a QueueSelection rather than a single id, and everything
// takes a base: the queue is the task list's own master switch, so it is
// forwarded to a peer alongside the list it belongs to. Acting on the machine
// you are looking at is the whole point.

/**
 * Who a queue action is about.
 *
 * `ids` is what a selection on screen produces. `package` reaches the rows a
 * filter hid, which is why "send this package to the top" names the package and
 * not the forty ids that happen to be visible — a package that arrives at the
 * top in pieces is worse than one that did not move. `all` has to be asked for:
 * the server refuses a request that names nothing rather than reading it as
 * "the whole list".
 */
export interface QueueSelection {
  ids?: string[];
  package?: string;
  all?: boolean;
}

/** The four steps the manual order understands. Anything finer is a drag. */
export type QueueMove = 'top' | 'up' | 'down' | 'bottom';

/**
 * One of the seven priorities, as the server offers them.
 *
 * There is no label: the server does not know which of the shipped locales this
 * browser is showing, and two clients of one instance routinely differ. The id
 * is what `priority.<id>` translates.
 */
export interface PriorityChoice {
  id: string;
  value: number;
}

/**
 * The priority ladder, fetched once per session and shared by everything that
 * offers it.
 *
 * Memoised here rather than in one component because it was in one component,
 * and the properties panel then grew a hardcoded ladder of its own: five steps
 * against the server's seven, with its own key set, so the right-click menu and
 * the panel disagreed about how many priorities the app has. That is the exact
 * failure the "build the menu from the server" rule exists to prevent, and one
 * private cache is how it happened - a second consumer could not reach the
 * first one's copy, so it made another.
 */
let prioritiesOnce: Promise<PriorityChoice[]> | null = null;

export function priorityChoices(): Promise<PriorityChoice[]> {
  if (!prioritiesOnce) prioritiesOnce = fetchPriorities();
  return prioritiesOnce.then(
    (p) => p,
    (e) => {
      prioritiesOnce = null; // a failed load must not poison the next attempt
      throw e;
    },
  );
}

/** What the figures under the list say. `eta` is seconds, null when nothing is moving. */
export interface QueueCounters {
  files: number;
  disabled: number;
  running: number;
  remaining: number;
  speed: number;
  eta: number | null;
}

/**
 * What stopping every transfer right now would throw away.
 *
 * `unknown` is deliberately apart from `losing`: nobody has asked those whether
 * they resume, and "we do not know" is a different sentence from "you will lose
 * 4.2 GB". Showing the second when the first is true is how people learn to
 * click straight through the dialog.
 */
export interface StopCost {
  running: number;
  losing: string[];
  bytes: number;
  unknown: number;
  unknownBytes: number;
}

/** What the hard stop answers with: what it stopped, and the switch it left behind. */
export interface StopResult {
  ids: string[];
  count: number;
  queue: QueueState;
}

/** queueMove changes where a selection or a whole package sits in the wait order. */
export const queueMove = async (sel: QueueSelection, where: QueueMove, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/queue/move`, { ...sel, where })));

/**
 * reorderTasks writes the full drag-and-drop order for one "band" - every
 * task sharing both `priority` and `forced`, which is as far as a single
 * request reaches: the server groups the queue the same way and refuses a
 * list that mixes bands rather than guessing which one the drop belongs to.
 * `ids` may be a SUBSET of the band - the server reads it as "these tasks, in this order, in the slots they already hold", top to bottom, not a diff -
 * see TaskList.tsx's own row drag for how that list is built and why a drag
 * that would leave its band is never sent at all.
 */
export const reorderTasks = async (ids: string[], base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/tasks/reorder`, { ids })));

/** queuePriority puts a selection at one of the seven. */
export const queuePriority = async (sel: QueueSelection, priority: number, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/queue/priority`, { ...sel, priority })));

/** queueForce starts a selection now: front of the queue, switched on, released. */
export const queueForce = async (sel: QueueSelection, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/queue/force`, sel)));

/** queueEnabled is the bulk switch: a selection, a package, or `all` disabled links. */
export const queueEnabled = async (sel: QueueSelection, enabled: boolean, base = '/api') =>
  json<BulkResult>(await ok(await post(`${base}/queue/enabled`, { ...sel, enabled })));

/** fetchPriorities is the menu's source of truth, so it cannot offer a value the server clamps away. */
export const fetchPriorities = async (base = '/api') =>
  json<PriorityChoice[]>(await fetch(`${base}/queue/priorities`));

export const fetchCounters = async (base = '/api') =>
  json<QueueCounters>(await fetch(`${base}/queue/counters`));

/** fetchStopCost weighs the hard stop and stops nothing. */
export const fetchStopCost = async (base = '/api') =>
  json<StopCost>(await fetch(`${base}/queue/stop`));

/** stopAll stops every transfer in flight and halts the queue behind them. */
export const stopAll = async (base = '/api') =>
  json<StopResult>(await ok(await post(`${base}/queue/stop`, {})));

export async function fetchSettings(): Promise<Settings> {
  return json<Settings>(await fetch('/api/settings'));
}

/**
 * patchSettings updates only the named top-level fields. Every field left
 * out, and any edit a different client made concurrently to a field this
 * one did not touch, is left exactly as stored - see PATCH /api/settings's
 * own doc comment (routes_settings.go) for why that is not something a PUT,
 * which always sends and replaces the whole document, can promise. A nested
 * object field (reconnect, idleAction, ...) is still replaced whole when
 * named, the same as PUT already does for it; only fields omitted from
 * patch are protected.
 *
 * This is the save path both real callers use - pages/Settings.tsx's own
 * save bar (a diff of the draft against what it was seeded from) and
 * QueueBar.tsx's speed-limit field (always exactly one field) - so there is
 * no client-side saveSettings(PUT) wrapper here for either to fall back to;
 * PUT /api/settings itself is still served, for whatever else wants to
 * replace the whole document in one call.
 */
export async function patchSettings(patch: Partial<Settings>): Promise<Settings> {
  const r = await fetch('/api/settings', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch),
  });
  return json<Settings>(r);
}

export async function fetchAccounts(): Promise<Account[]> {
  return (await json<Account[]>(await fetch('/api/accounts'))) ?? [];
}

// fetchAccountCatalogue is every service KnightLoader can store a credential
// for, configured or not - what the "new account" picker searches.
export async function fetchAccountCatalogue(): Promise<CatalogueService[]> {
  return (await json<CatalogueService[]>(await fetch('/api/accounts/catalogue'))) ?? [];
}

// verifyAccountCredential checks a credential against its service without
// storing it - called before Save, so a typo shows up before it is persisted.
export async function verifyAccountCredential(
  service: string,
  account: string,
  cred: AccountCredential,
): Promise<VerifyResult> {
  return json<VerifyResult>(await post('/api/accounts/verify', { service, account, ...cred }));
}

// saveAccountCredential stores one account's credential.
export async function saveAccountCredential(service: string, account: string, cred: AccountCredential): Promise<void> {
  await ok(await post('/api/accounts', { service, account, ...cred }));
}

// removeAccountCredential clears one account's credential - a zero credential
// is what the server reads as "delete this entry" (accounts.Credential.IsZero).
export async function removeAccountCredential(service: string, account: string): Promise<void> {
  await ok(await post('/api/accounts', { service, account }));
}

export async function setAccountLabel(service: string, account: string, label: string): Promise<void> {
  await ok(await post('/api/accounts/label', { service, account, label }));
}

// setAccountEnabled gates rewireBackends exactly as a missing credential does
// - see app.SetAccountEnabled.
export async function setAccountEnabled(service: string, account: string, enabled: boolean): Promise<void> {
  await ok(await post('/api/accounts/enabled', { service, account, enabled }));
}

// testAccount re-checks an already-stored account - the per-row "Refresh".
export async function testAccount(service: string, account: string): Promise<Account> {
  return json<Account>(await post('/api/accounts/test', { service, account }));
}

// --- The end-of-queue action -------------------------------------------
//
// What happens once the wait queue has nothing enabled left to run, start or
// finish, after a cancellable countdown - internal/idleaction. The
// configuration itself (which action, how long the countdown runs) is the
// `idleAction` field on Settings above, saved the ordinary way through the
// Settings page's own save bar; what follows here is what that document
// alone cannot answer: whether the queue is idle right now, whether a
// countdown is actually running, and cancelling one.

/**
 * The one external program the "command" end-of-queue action runs
 * (internal/idleaction.CommandSpec).
 *
 * A PROGRAM AND ARGUMENTS, NEVER A SHELL COMMAND LINE. The server runs it
 * directly, so a pipe, a redirect or two commands in a row belong in a script
 * file this points at - the same split reconnect's own Command/Args makes, for
 * the same reason.
 *
 * IT COMES BACK REDACTED. `program` is '********' and every argument is
 * '********' whenever anything is stored, because GET /api/settings is also
 * what the diagnostics bundle is built from and that file gets attached to
 * public bug reports. Send the mask back untouched to keep what is stored
 * (settings.Store.setLocked merges it back), send a new string to replace it,
 * send an empty one to clear it. Ask checkIdleCommand what would actually run.
 */
export interface IdleCommandSpec {
  program: string;
  args?: string[];
  timeoutSeconds: number;
}

/** settings.Settings.IdleAction (internal/settings/settings.go) on the wire. */
export interface IdleActionConfig {
  /** 'none' | 'pause' | 'quit' | 'command' | 'suspend', and whatever a later
   *  build adds - open on purpose, see fetchIdleActions. */
  action: string;
  delaySeconds: number;
  command: IdleCommandSpec;
}

/**
 * What POST /api/idle-action/check answers.
 *
 * It resolves the STORED command and never runs it, and it answers 200 even
 * when `problem` is set: a preflight that failed the request could not be read
 * by the page that asked for it.
 */
export interface IdleCommandCheck {
  problem?: 'empty' | 'notFound' | 'notExecutable' | 'permission' | 'timeout' | 'exit' | 'notSupported';
  /** What the program name resolves to - the same lookup the run itself does,
   *  so a bare name that would work resolves here too. */
  resolvedPath?: string;
  /** The exact argument vector, resolved program first. */
  argv?: string[];
  /** 'container' or 'desktop'. The interface picks between two explanations of
   *  one problem code with it: "not in this image" reads differently from
   *  "not on this machine". */
  deployment: string;
}

/**
 * What the last end-of-queue action did (internal/app.IdleRun), and what POST
 * /api/idle-action/run answers with.
 *
 * IT DOES NOT SURVIVE A RESTART, so the only run a 'quit' can ever leave
 * behind is a failed one: a successful quit takes the record with it.
 */
export interface IdleRun {
  action: string;
  /** RFC3339. */
  at: string;
  ok: boolean;
  /** Empty exactly when `ok` is true. A code, never a sentence - translate it. */
  problem?: string;
  exitCode?: number;
  /** The program's own output, capped, with the stored command line taken back
   *  out of it. */
  output?: string;
  /** The program as configured, NOT redacted: this document goes to the
   *  browser and never into the diagnostics bundle. */
  program?: string;
}

/**
 * GET /api/idle-action, and what POST .../cancel answers with too, so a
 * cancel button can repaint itself from the same response that confirms the
 * cancel took effect rather than firing a second request to find out.
 */
export interface IdleActionState {
  config: IdleActionConfig;
  /** Whether the queue has nothing enabled left to do, read fresh on every
   *  request - not merely "a countdown happens to be armed". */
  idle: boolean;
  armed: boolean;
  /** Which action is armed. Absent when `armed` is false. */
  action?: string;
  /**
   * The absolute instant the action fires, RFC3339, absent when `armed` is
   * false. Absolute rather than a duration, so a reloaded page - or one that
   * was simply asleep for a few seconds - draws the same deadline the server
   * is counting down to instead of restarting its own clock from a number
   * that was already stale on arrival; the same reason ScheduleState.Next
   * and the captcha modal's own ExpiresAt are both instants, not durations.
   */
  fireAt?: string;
  /** What the last action actually did, absent until one has run since this
   *  process started. It is the ONLY surface a fired action has once the
   *  countdown is over. */
  lastRun?: IdleRun;
}

export async function fetchIdleAction(): Promise<IdleActionState> {
  return json<IdleActionState>(await fetch('/api/idle-action'));
}

/** cancelIdleAction calls off a countdown in progress, without disarming the
 *  feature for the next time the queue goes idle. */
export async function cancelIdleAction(): Promise<IdleActionState> {
  return json<IdleActionState>(await ok(await post('/api/idle-action/cancel', {})));
}

/** fetchIdleActions is the menu's source of truth, so it cannot offer an
 *  action this build does not implement. */
export async function fetchIdleActions(): Promise<string[]> {
  return (await json<string[]>(await fetch('/api/idle-action/actions'))) ?? [];
}

/** checkIdleCommand resolves the stored command and reports what it found. It
 *  never runs anything, which is exactly what makes it safe to press on an
 *  action that would otherwise put the machine to sleep. */
export async function checkIdleCommand(): Promise<IdleCommandCheck> {
  return json<IdleCommandCheck>(await ok(await post('/api/idle-action/check', {})));
}

/** runIdleCommand runs the stored command once, now, exactly as the countdown
 *  would. The server refuses with 409 unless the configured action IS the
 *  command one: a test button that quits the process or suspends the machine
 *  because that is what happened to be configured is not a test. */
export async function runIdleCommand(): Promise<IdleRun> {
  return json<IdleRun>(await ok(await post('/api/idle-action/run', {})));
}

/**
 * abortActivity calls off the running background work of one kind and answers
 * how many runs that was.
 *
 * Zero is a normal answer, not a failure: the run may have finished between the
 * status strip drawing the button and somebody pressing it, which is the
 * ordinary race for a control that only exists while work is in flight. The
 * caller shows nothing for a zero - the row it was pressed on is already gone.
 */
export async function abortActivity(kind: string): Promise<number> {
  const r = await ok(await post(`/api/activity/${encodeURIComponent(kind)}/abort`, {}));
  return (await json<{ cancelled: number }>(r))?.cancelled ?? 0;
}

// ---- resolver routing facts (internal/resolver, GET /api/resolvers/*) -----

/** One resolver's identity and priority - resolver.Info (internal/resolver/resolver.go). */
export interface ResolverInfo {
  id: string;
  prio: number;
}

/**
 * fetchResolverPriority is the order configured services are actually asked
 * in, highest first - app.ResolverPriority, which is the registry's own order
 * AFTER the hand-arranged settings.resolverOrder and JD's per-host boost have
 * had their say. With `host` it is narrowed to the chain that host walks.
 */
export async function fetchResolverPriority(host?: string): Promise<ResolverInfo[]> {
  const q = host ? `?host=${encodeURIComponent(host)}` : '';
  return (await json<ResolverInfo[]>(await fetch(`/api/resolvers/priority${q}`))) ?? [];
}

/**
 * saveResolverPriority stores a hand-arranged order and answers with the
 * order that is now in force. An empty list is the reset: it puts the ladder
 * back to the automatic one.
 *
 * The answer is the server's own re-read, not an echo - blanks and repeats are
 * dropped on the way in, so a card redrawing from what it SENT could show an
 * order the downloader is not using.
 */
export async function saveResolverPriority(order: string[]): Promise<ResolverInfo[]> {
  return (
    (await json<ResolverInfo[]>(
      await fetch('/api/resolvers/priority', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ order }),
      }),
    )) ?? []
  );
}

/** The headless-JD sidecar's own status - app.JDStatus (internal/app/app_accounts.go). */
export interface JDStatus {
  configured: boolean;
  reachable: boolean;
  /** JDownloader's own revision number - a plain integer, not vX.Y.Z; absent
   *  while `reachable` is false. */
  version?: number;
  detail?: string;
}

export async function fetchJDStatus(): Promise<JDStatus> {
  return json<JDStatus>(await fetch('/api/resolvers/jd'));
}

// ---- yt-dlp resolver options (internal/resolver/ytdlp) --------------------
//
// Which service handles a given link at all lives on the Accounts page's own
// routing section (fetchResolverPriority/fetchJDStatus above) - this is the
// other half, what the ONE resolver with anything configurable
// (docs/jd-feature-census.md's "(per-plugin option list)" row) actually does
// once it has a link. Read and written through Settings.ytdlp like every
// other settings field, not a route of its own.

/**
 * Mirrors ytdlp.Options (internal/resolver/ytdlp/options.go), minus its own
 * Variant field: Variant is decided per-task (core.Task.Variant, one of the
 * five "Variante" rows), never a global default, so it has nothing to save
 * here. Every value is a plain string rather than a TS union, matching
 * every other server-sourced menu in this file (archiveDisposal,
 * collisionPolicy, resumeOnStart): the choices come from
 * ApiOptions.ytdlpQualities, so a value this build adds later still
 * round-trips instead of failing to compile.
 *
 * These are the INSTANCE-WIDE defaults a yt-dlp-routed link's "Variante"
 * rows are built from (app_ytdlp_variants.go's ytdlpOptionsForTask); which
 * variant rows exist at all, and whether each starts enabled, is a
 * per-hoster HosterPreset instead (settings.YtdlpPresets), not a field here.
 *
 * Every field's zero value ('' / false) reproduces exactly what this
 * backend did before any of them existed - see the Go type's own doc
 * comment. An install that never opens the resolver options page downloads
 * exactly as it always has.
 */
export interface YtdlpOptions {
  /** 'best' | '2160p' | '1440p' | '1080p' | '720p' | '480p' | '360p' |
   *  'custom' - see ApiOptions.ytdlpQualities. Read only on a video row. */
  quality: string;
  /** yt-dlp's own -f selector, used verbatim when quality is 'custom' and
   *  ignored otherwise. */
  customFormat: string;
  /** yt-dlp's own --audio-format value (e.g. "mp3", "m4a", "opus"), or
   *  "best" for no opinion. Read only on an audio row. */
  audioFormat: string;
  /** yt-dlp's own --sub-langs value (e.g. "en,de"); empty defaults to "en".
   *  Read only on a subtitle row. */
  subtitleLangs: string;
  /** Also fetch auto-generated captions when no manual track exists. Read
   *  only on a subtitle row. */
  subtitleAuto: boolean;
  /** A playlist URL fetches every entry instead of only the one link
   *  pointed at - off is what every install had before this existed. */
  playlist: boolean;
  /** yt-dlp's own -o template syntax; empty uses the built-in
   *  "%(title)s.%(ext)s". Server-sanitized against path traversal on save -
   *  see ytdlp.sanitizeTemplate's own doc comment. */
  outputTemplate: string;
  /** yt-dlp's own --audio-quality target in kbit/s ("192"), or "" for no
   *  opinion. Read only on an audio row, and only meaningful once audioFormat
   *  names a real transcode target. */
  audioBitrate: string;
  /** One spoken language, applied as a [language^=xx] filter inside the audio
   *  row's -f selector. "" passes no filter. */
  audioLang: string;
  /** Fail a subtitle row that wrote no file instead of settling it green. */
  subtitleStrict: boolean;
  /** Allow a stored cookies.txt to be handed to yt-dlp. The jar itself is never
   *  part of this document. */
  cookies: boolean;
  /** Tag and sort an audio row as music. */
  music: boolean;
  embed: YtdlpEmbed;
  measure: YtdlpMeasure;
  live: YtdlpLive;
}

/** Mirrors ytdlp.Embed (internal/resolver/ytdlp/options.go): what gets written
 *  INTO the finished file rather than beside it. */
export interface YtdlpEmbed {
  metadata: boolean;
  thumbnail: boolean;
  chapters: boolean;
  /** Video rows only. */
  subs: boolean;
  splitChapters: boolean;
  /** Not a yt-dlp flag: KnightLoader writes the NFO itself. */
  nfo: boolean;
}

/** Mirrors ytdlp.Measure: the ffprobe pass over a finished media file. */
export interface YtdlpMeasure {
  enabled: boolean;
  /** 1..100. Anything outside that, 0 included, is stored as 90. */
  shortPercent: number;
  failOnShort: boolean;
}

/** Mirrors ytdlp.Live: what a stream that has no end is allowed to do. */
export interface YtdlpLive {
  enabled: boolean;
  fromStart: boolean;
  /** Minutes of recording, 0 for no limit. */
  maxMinutes: number;
  /** MiB, 0 for no limit. */
  maxMB: number;
}

/** Every "Variante" row kind a yt-dlp-routed link stages, in the fixed
 *  order expandYtdlpVariants (app_ytdlp_variants.go) creates them - a
 *  closed, stable set fixed in that Go code, so unlike quality/audioFormat
 *  it is not fetched from /api/options. */
export const YTDLP_VARIANT_KINDS = ['video', 'audio', 'thumbnail', 'subtitle', 'description'] as const;
export type YtdlpVariantKind = (typeof YTDLP_VARIANT_KINDS)[number];

/**
 * Mirrors ytdlp.HosterPreset (internal/resolver/ytdlp/options.go): which of
 * the five "Variante" rows a hoster's own links start with enabled, and the
 * default quality/audioFormat those rows start on. Read/written through
 * GET/POST /api/ytdlp/preset, one host at a time - not part of the general
 * settings draft (see that route's own comment in routes_resolvers.go for
 * why).
 */
export interface YtdlpHosterPreset {
  variants: YtdlpVariantKind[];
  quality: string;
  audioFormat: string;
}

export async function fetchHosterPreset(host: string, base = '/api'): Promise<YtdlpHosterPreset> {
  return json<YtdlpHosterPreset>(await fetch(`${base}/ytdlp/preset?host=${encodeURIComponent(host)}`));
}

export async function saveHosterPreset(host: string, preset: YtdlpHosterPreset, base = '/api'): Promise<void> {
  const r = await post(`${base}/ytdlp/preset`, { host, ...preset });
  if (!r.ok) throw new ApiError((await r.text()).trim() || String(r.status));
}

// ---- yt-dlp sign-in cookie jars (internal/api/routes_ytdlpcookies.go) ------
//
// One cookies.txt per site, sealed in the encrypted credential store. The jar
// travels ONE way only: into the server. Nothing here can read one back, and
// the list route answers names and nothing else, because a cookies.txt is a
// live session. The Go side holds itself to that with its own guard test.

/**
 * The sites that have a stored jar, keyed the way every lookup keys them
 * (lower-cased, "www." stripped). What comes back can therefore differ from
 * what was sent, so render the answer rather than the request.
 */
export type YtdlpCookieHosts = string[];

export async function fetchYtdlpCookieHosts(base = '/api'): Promise<YtdlpCookieHosts> {
  return (await json<YtdlpCookieHosts>(await fetch(`${base}/ytdlp/cookies`))) ?? [];
}

/**
 * saveYtdlpCookieJar stores or replaces one site's cookies.txt and answers the
 * list that is now stored - the server's own re-read, not an echo.
 *
 * `text` is always sent. An empty string is the deliberate clear; leaving the
 * field out is refused with a 400 rather than read as "keep the stored one",
 * because a jar is never sent back to the page and a form re-saved without the
 * textarea refilled would otherwise delete a working session in silence.
 *
 * `host` takes the address the jar was exported from
 * ("https://www.youtube.com/watch?v=x") or the bare site name ("youtube.com").
 * Something that is not a host at all throws an ApiError carrying the server's
 * own sentence, which names the field and says what to send.
 */
export async function saveYtdlpCookieJar(
  host: string,
  text: string,
  base = '/api',
): Promise<YtdlpCookieHosts> {
  return (await json<YtdlpCookieHosts>(await post(`${base}/ytdlp/cookies`, { host, text }))) ?? [];
}

/**
 * removeYtdlpCookieJar deletes one site's jar and answers the remaining list.
 * A host with nothing stored throws with status 404 rather than succeeding
 * quietly: removing a jar is removing a logged-in session from this machine,
 * and a green tick on a typo would leave somebody believing a live session is
 * gone while it is still sealed under the name they meant to type.
 */
export async function removeYtdlpCookieJar(host: string, base = '/api'): Promise<YtdlpCookieHosts> {
  return (await json<YtdlpCookieHosts>(await post(`${base}/ytdlp/cookies/remove`, { host }))) ?? [];
}

// ---- header profiles (internal/resolver/hostheaders) -----------------------
//
// A user's own request headers for one origin: the cookie from a logged-in
// browser session, the Referer a forum insists on, the Basic auth a seedbox
// sits behind. The values are sealed in the credential store and never come
// back over this API, so a profile is its origin plus the header NAMES it
// holds. That is why an editing form re-sends REDACTED_HEADER for every line it
// is not changing: an empty value means "clear this header", here as it does
// for every other secret in this app, so a form that re-sent what it was given
// would delete the headers it was opened to edit.

/** One stored header profile - hostheaders.Listing. Names only, never a value. */
export interface HeaderProfile {
  /** What the profile is stored under, and the name a Packagizer rule uses to
   *  send a link through it. Letters, digits and - _ or . only, at most 64
   *  characters. */
  id: string;
  /** scheme://host:port, with the port always spelled out. A different port,
   *  and http instead of https, are different origins and need their own
   *  profile: a sub-domain is not covered by its parent's profile either. */
  origin: string;
  /** The header names this profile holds, in net/http's capitalisation. */
  headers: string[];
}

/** One header line on the way IN. */
export interface HeaderProfileLine {
  name: string;
  /** A new value replaces what is stored, REDACTED_HEADER keeps it, and an
   *  empty string clears that header. */
  value: string;
}

/** The placeholder a stored value is re-sent as - hostheaders.Redacted, the
 *  same string accounts.Redacted uses. A form rendering a stored profile must
 *  send this back for every header it did not touch. */
export const REDACTED_HEADER = '********';

/** fetchHeaderProfiles is every stored profile: origin and header names. */
export async function fetchHeaderProfiles(): Promise<HeaderProfile[]> {
  return (await json<HeaderProfile[]>(await fetch('/api/hostheaders'))) ?? [];
}

/** saveHeaderProfile stores or replaces the profile for one origin and answers
 *  the whole listing as the store now holds it. Leave id empty to edit the
 *  profile that origin already has; creating one has to name it. */
export async function saveHeaderProfile(
  origin: string,
  headers: HeaderProfileLine[],
  id = '',
): Promise<HeaderProfile[]> {
  return json<HeaderProfile[]>(await post('/api/hostheaders', { id, origin, headers }));
}

/** deleteHeaderProfile removes one profile. An unknown name answers 404 rather
 *  than a cheerful 204, for the same reason removeYtdlpCookieJar does. */
export async function deleteHeaderProfile(id: string): Promise<void> {
  await ok(await fetch(`/api/hostheaders/${encodeURIComponent(id)}`, { method: 'DELETE' }));
}

// --- The media library call, beside the header profiles ---------------------
//
// The address a drawer calls once a package has finished AND its files have
// been moved into place. The rows live in settings.json; the one header value
// each may carry is sealed in the credential store, so the listing answers
// whether one is stored and never what it is - which is why an editing form has
// to send REDACTED_HEADER back for a value it is not changing. Empty means
// "clear this" here as it does for every other secret in this app, so a form
// that re-sent what it was given would delete the token it was opened to edit.

/**
 * One stored address. Mirrors mediahook.Hook plus the five read-only fields the
 * listing route adds.
 *
 * THERE IS NO VALUE FIELD, and that is a property of the type rather than of
 * whichever component renders it: the value is sealed in the credential store
 * and the listing route has nothing to send. `hasValue` is the only thing this
 * side ever learns about it.
 */
export interface MediaHook {
  /** Stable key a drawer points at (Category.notify). Letters, digits and - _
   *  or . Never changes: renaming it would leave every drawer pointing at an
   *  address that no longer answers. */
  id: string;
  name?: string;
  /** Absolute http or https address, host and port included. */
  url: string;
  /** 'GET' or 'POST'. A plain string, not a union: the menu comes from
   *  GET /api/options.mediaHookMethods, so a method the server adds renders. */
  method: string;
  /** The one header sent with the call. Empty sends no extra header. */
  headerName?: string;
  /** Seconds this address is left alone after a package finishes, so a batch
   *  becomes one call. 0 calls as soon as a package's files are in place, which
   *  is a real answer and not "unset". */
  waitSeconds: number;
  /** Whether a header value is stored. Never the value. */
  hasValue: boolean;
  /** host[:port] the call resolves to, for the line that says where it goes. */
  host: string;
  /** The target is loopback or on a private range. Answered WITHOUT a DNS
   *  lookup, so a host name reads as false: the sentence that gets withheld
   *  wrongly is the one nobody can see. */
  private: boolean;
  /** Category ids currently pointing at this address. Never null. Empty means
   *  it is stored and nothing calls it. */
  usedBy: string[];
  /** The last call, test calls included. In memory only, so null after a
   *  restart even for an address that has worked for a year. */
  last: MediaHookResult | null;
}

/** What one call did. `code` keys the sentence that says what to try next. */
export interface MediaHookResult {
  at: string;
  /** The package that armed the call, or the first of several by name. Absent
   *  for a test call. */
  package?: string;
  /** How many packages were folded into this one call. Absent or 1 means one. */
  packages?: number;
  test?: boolean;
  status?: number;
  durationMs: number;
  ok: boolean;
  /** 'dns'|'refused'|'timeout'|'tls'|'auth'|'notFound'|'method'|'redirect'|
   *  'server'|'proxy'|'unknown'. A plain string: a code this build has no key
   *  for falls back to settings.mediahook.problem.unknown with `error`. */
  code?: string;
  params?: Record<string, string | number>;
  error?: string;
}

/** One save. `headerValue` carries the three meanings every secret in this app
 *  carries: a new value replaces, REDACTED_HEADER keeps, empty clears. */
export interface MediaHookSave {
  id: string;
  name?: string;
  url: string;
  method: string;
  headerName?: string;
  headerValue: string;
  waitSeconds: number;
}

/** fetchMediaHooks is every stored address, in the order they are stored -
 *  which is the order the picker on the Categories page offers. */
export async function fetchMediaHooks(): Promise<MediaHook[]> {
  return (await json<MediaHook[]>(await fetch('/api/mediahooks'))) ?? [];
}

/** saveMediaHook stores or replaces one address and answers the whole listing
 *  as the server now holds it. Send REDACTED_HEADER as headerValue to keep the
 *  stored value, '' to clear it, anything else to replace it. */
export async function saveMediaHook(h: MediaHookSave): Promise<MediaHook[]> {
  return json<MediaHook[]>(await post('/api/mediahooks', h));
}

/** deleteMediaHook removes one address and the header value sealed for it.
 *  Refused with 409 while a category drawer still points at it. */
export async function deleteMediaHook(id: string): Promise<void> {
  await ok(await fetch(`/api/mediahooks/${encodeURIComponent(id)}`, { method: 'DELETE' }));
}

/** testMediaHook calls one stored address once, with its sealed header, and
 *  reports what came back. It answers 200 even when the call failed - the
 *  request was fine, it was the far end that was not - so read `ok` on the
 *  result rather than catching. */
export async function testMediaHook(id: string): Promise<MediaHookResult> {
  return json<MediaHookResult>(await post(`/api/mediahooks/${encodeURIComponent(id)}/test`, {}));
}

// --- Feed subscriptions: health and the test fetch --------------------------

/**
 * FeedStatus is one configured subscription as GET /api/feeds reports it.
 *
 * `lastPolledAt` absent is the flag for "nothing to report yet": the health
 * table lives in the server's memory and a restart blanks it, while the
 * subscription's memory of what it has already added survives. While it is
 * absent, `seeded` and `remembered` are not yet known and must not be drawn as
 * "has not seeded" and "remembers nothing".
 */
export interface FeedStatus {
  url: string;
  /** Whether this address is actually being polled. False with an `error` is a
   *  row the server refused; false with no error is the moment at startup
   *  before the runner has taken the list. */
  polling: boolean;
  /** RFC3339. Absent when no poll has run since the server started. */
  lastPolledAt?: string;
  /** Why the last poll produced nothing, or why the row is not polled at all. */
  error?: string;
  seeded: boolean;
  remembered: number;
}

/** One entry of a test fetch, as a subscription would see it. */
export interface FeedTestEntry {
  title: string;
  /** What would actually be staged, which for an entry carrying an enclosure is
   *  the enclosure and not the article beside it. */
  link: string;
  /** Whether the title filter takes it. True for everything when there is none. */
  matches: boolean;
}

/**
 * FeedTest is what POST /api/feeds/test found. `error` set means the address
 * could not be read; `entries` is then an empty list rather than null, so the
 * panel has one shape to draw however the attempt ended.
 */
export interface FeedTest {
  title: string;
  entries: FeedTestEntry[];
  /** Entries in the whole document; `entries` above is only the first few. */
  total: number;
  /** How many of `total` the filter takes. Equals `total` with no filter. */
  matched: number;
  error?: string;
}

/** fetchFeeds is the status table under the subscription rows. */
export async function fetchFeeds(): Promise<FeedStatus[]> {
  return (await json<FeedStatus[]>(await ok(await fetch('/api/feeds')))) ?? [];
}

/**
 * testFeed fetches one feed once and reports what is in it. Nothing is staged
 * and nothing is remembered, so it is safe to press on an address that has not
 * been saved yet - which is the whole point, because a title filter is a
 * pattern typed against titles nobody has seen.
 *
 * It throws with the server's own sentence when the row itself is wrong (an
 * address that is not http or https, a pattern that will not compile): those
 * are 400s and the sentence names the field. A feed that simply could not be
 * read comes back as a normal result with `error` set.
 */
export async function testFeed(url: string, titleFilter = ''): Promise<FeedTest> {
  const r = await ok(await post('/api/feeds/test', { url, titleFilter }));
  return (await json<FeedTest>(r)) ?? { title: '', entries: [], total: 0, matched: 0 };
}

// ---- native hoster logins (internal/hosterauth) ----------------------------
//
// A per-host login rendered entirely in KL's own UI (see
// components/HosterLoginSection.tsx), never JD's own web interface. Saving one
// writes the credential into the headless-JD sidecar's OWN account config
// through JD's Remote API; JD's existing plugin performs the actual login.
// This is a different store from accounts.Catalogue's - one row per host from
// a list that can run into the hundreds, not a short hand-maintained one - so
// it gets its own small API surface instead of overloading /api/accounts.

/** One host the "add a login" picker offers - hosterauth.Host. */
export interface HosterHost {
  id: string;
  label: string;
  /**
   * A service that unlocks OTHER hosts rather than hosting files itself.
   *
   * Absent for the ordinary ones (the server omits a false), so read it as a
   * flag and never as a tri-state. It comes from a list kept by hand on the
   * server - JDownloader's own API cannot answer the question, which
   * internal/app/app_multihoster.go explains at length.
   */
  multihoster?: boolean;
}

/**
 * The three-way sync status one stored login can be in against JD -
 * hosterauth.LoginStatus. 'queued' and 'rejected' are deliberately distinct:
 * a login JD has not validated yet reads as "still checking", not as "wrong
 * password" - collapsing the two is how a user gives up on a login seconds
 * from working.
 */
export type HosterLoginStatus = 'queued' | 'active' | 'rejected' | 'off';

/** One stored native hoster login and its status - hosterauth.LoginState. Never the password. */
export interface HosterLogin {
  host: string;
  username: string;
  status: HosterLoginStatus;
  detail?: string;
  /** The user's own switch, beside rather than inside `status`: status is what
   *  JDownloader currently thinks of the login, enabled is whether JD was ever
   *  given it. The row needs both - one draws the toggle, the other the badge. */
  enabled: boolean;
  /** What JD says about the account itself, so this card can show the same
   *  columns the debrid card does. All optional: JD answers nothing at all for
   *  an account it has nothing to say about, and that has to stay
   *  distinguishable from a zero. */
  tier?: string;
  expiry?: string;
  trafficLeft?: number;
  trafficMax?: number;
}

/** fetchHosterHosts is the "add a login" picker's host list. */
export async function fetchHosterHosts(): Promise<HosterHost[]> {
  return (await json<HosterHost[]>(await fetch('/api/hosterauth/hosts'))) ?? [];
}

/** fetchHosterLogins is every stored native hoster login and its sync status. */
export async function fetchHosterLogins(): Promise<HosterLogin[]> {
  return (await json<HosterLogin[]>(await fetch('/api/hosterauth/logins'))) ?? [];
}

/** saveHosterLogin stores (or updates) one host's native login. */
export async function saveHosterLogin(host: string, username: string, password: string): Promise<void> {
  await ok(await post('/api/hosterauth/logins', { host, username, password }));
}

/** setHosterLoginEnabled switches one host's stored login on or off without
 *  deleting it - off takes the account out of JDownloader's own list and
 *  leaves the credential sealed here, so switching it back on needs no
 *  password retyped. */
export async function setHosterLoginEnabled(host: string, enabled: boolean): Promise<void> {
  await ok(await post('/api/hosterauth/logins/enabled', { host, enabled }));
}

/** removeHosterLogin clears one host's stored native login. */
export async function removeHosterLogin(host: string): Promise<void> {
  await ok(await post('/api/hosterauth/logins/remove', { host }));
}

// ---- captcha (internal/captcha) --------------------------------------------
//
// A hoster (or an account's own login gate) asking a human something before a
// download can continue. Challenge mirrors captcha.Challenge
// (internal/captcha/challenge.go) verbatim; CaptchaResolution mirrors
// app.CaptchaResolution (internal/app/app_captcha.go). Rendering a 'widget'
// challenge and giving up on one (skip/blacklist) are their own routes -
// internal/api/routes_captcha_widget.go and routes_captcha_skip.go, built by
// other agents this wave - see components/CaptchaModal.tsx for how these
// types reach both without this file importing anything from them.

export type CaptchaKind = 'image' | 'click' | 'widget' | 'unsupported';

/** Challenge.Payload for 'image' and 'click' - captcha.ImagePayload /
 *  ClickPayload are the identical Go type, and so is this: Kind alone is
 *  what tells a renderer to offer a click surface instead of a text box. */
export interface CaptchaImagePayload {
  /** Always a complete "data:image/...;base64,..." string, ready for <img src>. */
  dataUrl: string;
}

/**
 * Challenge.Payload for 'widget' - captcha.WidgetPayload. The sitekey data a
 * hosted reCAPTCHA v2 or hCaptcha widget needs to render and solve itself,
 * never a screenshot. Passed straight through as query parameters to
 * routes_captcha_widget.go's own contract - see captchaWidgetUrl.
 */
export interface CaptchaWidgetPayload {
  siteKey: string;
  siteUrl: string;
  contextUrl: string;
  type?: string;
  enterprise?: boolean;
  v3Action?: string;
  secureToken?: string;
}

/** Challenge.Payload for 'unsupported' - captcha.UnsupportedPayload. */
export interface CaptchaUnsupportedPayload {
  /** JD's own challenge class name - the real origin, never a guess. */
  vendor: string;
}

/**
 * One captcha instance blocking a download until a human answers it,
 * dismisses it, or it expires on its own - captcha.Challenge
 * (internal/captcha/challenge.go) verbatim.
 */
export interface CaptchaChallenge {
  /** Opaque: pass back to answerCaptcha/skipCaptcha unchanged, never parsed. */
  id: string;
  source: string;
  host: string;
  /** The KnightLoader task this challenge blocks, when the server could work
   *  that out. Empty is a real, expected answer, not a bug. */
  taskId?: string;
  kind: CaptchaKind;
  /** Instructions a human reads, in whatever language the hoster wrote them. */
  prompt?: string;
  payload?: CaptchaImagePayload | CaptchaWidgetPayload | CaptchaUnsupportedPayload;
  /**
   * When this challenge stops being answerable. Always present in the JSON -
   * Go's omitempty does not drop a zero time.Time, the same caveat
   * finishedAt/changedAt carry on Task - so "0001-01-01T00:00:00Z" means the
   * source could not say, never "already expired". See CaptchaModal.tsx's own
   * zero-year check before treating this as a real deadline.
   */
  expiresAt: string;
}

/**
 * How far a skipped challenge's effect reaches - captcha.AbortScope
 * (internal/captcha/challenge.go), the exact three names
 * routes_captcha_skip.go's own {"scope": ...} body expects.
 */
export type CaptchaAbortScope = 'skip-once' | 'blacklist-hoster' | 'blacklist-everywhere';

/** One challenge's end, as broadcast over the hub - app.CaptchaResolution
 *  (internal/app/app_captcha.go). */
export interface CaptchaResolution {
  id: string;
  taskId?: string;
  host: string;
  reason: 'solved' | 'expired' | 'aborted' | 'timedOut' | 'resolved';
}

/**
 * fetchCaptchas is every challenge this instance currently knows about - a
 * cache read, never a live JD call (see app.CaptchaChallenges' own doc
 * comment); the WebSocket "captcha"/"captchaResolved" events are what keep it
 * live afterwards without polling this again.
 */
export async function fetchCaptchas(): Promise<CaptchaChallenge[]> {
  return (await json<CaptchaChallenge[]>(await fetch('/api/captcha'))) ?? [];
}

/** refreshCaptchas polls the source right now instead of waiting for the next
 *  automatic check, and returns what it found. */
export async function refreshCaptchas(): Promise<CaptchaChallenge[]> {
  return (await json<CaptchaChallenge[]>(await post('/api/captcha/refresh', {}))) ?? [];
}

/**
 * answerCaptcha submits text as id's solution. stillValid is the direct,
 * authoritative answer to "did this arrive too late" from the server, which
 * itself has it from JD - trust this over any local countdown, and never
 * re-derive it from one.
 */
export async function answerCaptcha(id: string, text: string): Promise<{ stillValid: boolean }> {
  return json<{ stillValid: boolean }>(await post(`/api/captcha/${encodeURIComponent(id)}/answer`, { text }));
}

/**
 * skipCaptcha gives up on one challenge through routes_captcha_skip.go's own
 * route, not this file's /answer. scope decides how far the effect reaches;
 * JD keeps its own blacklist for blacklist-hoster/blacklist-everywhere, so
 * nothing here has to remember it or re-send it per link.
 */
export async function skipCaptcha(id: string, scope: CaptchaAbortScope): Promise<void> {
  await ok(await post(`/api/captcha/${encodeURIComponent(id)}/skip`, { scope }));
}

/**
 * captchaWidgetUrl is routes_captcha_widget.go's own query-string contract
 * (internal/api/routes_captcha_widget.go's captchaWidgetRequest): every
 * rendering parameter passed as a query parameter rather than looked up by id
 * server-side, because the caller already holds the only copy of that data
 * that exists (from fetchCaptchas or the WS stream) - a server-side lookup
 * would only be a second, redundant captcha/get?format=rawtoken call for data
 * already on screen. Parameter names match that file's own
 * parseCaptchaWidgetRequest exactly: siteKey/type/enterprise/v3Action/
 * secureToken/host/prompt.
 */
export function captchaWidgetUrl(ch: CaptchaChallenge): string {
  const p = (ch.payload ?? {}) as CaptchaWidgetPayload;
  const q = new URLSearchParams();
  if (p.siteKey) q.set('siteKey', p.siteKey);
  if (p.type) q.set('type', p.type);
  if (p.enterprise) q.set('enterprise', '1');
  if (p.v3Action) q.set('v3Action', p.v3Action);
  if (p.secureToken) q.set('secureToken', p.secureToken);
  if (ch.host) q.set('host', ch.host);
  if (ch.prompt) q.set('prompt', ch.prompt);
  return `/api/captcha/${encodeURIComponent(ch.id)}/widget?${q.toString()}`;
}

export async function fetchAuth(): Promise<AuthState> {
  return json<AuthState>(await fetch('/api/auth'));
}

// login exchanges the password for a session cookie.
export async function login(password: string): Promise<AuthState> {
  const r = await post('/api/auth/login', { password });
  if (!r.ok) throw new Error(await r.text());
  return json<AuthState>(r);
}

// logout ends the current session. Throws on a non-2xx response the same
// way login() above does, rather than only on a network-level failure - a
// caller's try/catch (Sidebar.tsx, Access.tsx's PasswordCard) needs both to
// actually mean "sign-out failed", not just the network case.
export async function logout(): Promise<void> {
  const r = await fetch('/api/auth/logout', { method: 'POST' });
  if (!r.ok) throw new Error(await r.text());
}

// setPassword sets, changes or (with an empty next) removes the password lock.
export async function setPassword(current: string, next: string): Promise<AuthState> {
  const r = await fetch('/api/auth/password', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ current, new: next }),
  });
  if (!r.ok) throw new Error(await r.text());
  return json<AuthState>(r);
}

// ---- API tokens (internal/apitoken) ----------------------------------------
//
// Named, individually revocable credentials: the answer to "a phone gets its
// own", so losing one device means revoking that one token rather than
// rotating the shared password for every other client. See
// internal/apitoken's own package comment for the full reasoning, and
// bearerToken in internal/api/api.go for how a stored one is presented
// (Authorization: Bearer <secret>) - this file has no helper for that half,
// because it is a non-browser client, a script or another device, that
// presents a token, never this same web UI to itself.

/** One token's metadata - apitoken.Token. Never the secret itself. */
export interface ApiToken {
  id: string;
  name: string;
  createdAt: string;
  /** Absent until this token's first successful use. */
  lastUsed?: string;
}

/**
 * What POST /api/tokens answers with: the same metadata GET /api/tokens
 * lists forever after, plus the plaintext secret this instance will never
 * be able to show again once this response has been read. The caller has to
 * put it somewhere on this one screen, because asking again means issuing a
 * new token.
 */
export interface NewApiToken extends ApiToken {
  secret: string;
}

export async function fetchTokens(): Promise<ApiToken[]> {
  return (await json<ApiToken[]>(await fetch('/api/tokens'))) ?? [];
}

export async function createToken(name: string): Promise<NewApiToken> {
  const r = await fetch('/api/tokens', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  });
  return json<NewApiToken>(r);
}

/** revokeToken pulls one device's credential without touching the shared
 *  password or any other token. */
export const revokeToken = (id: string) => fetch(`/api/tokens/${encodeURIComponent(id)}`, { method: 'DELETE' });

// ---- Remote access (internal/api/routes_remote.go) -------------------------
//
// What the Remote access settings page is built from: which addresses this
// instance actually answers requests on, whether a password protects them,
// and the loud warning for when it does not and can. There is no route here
// (and none anywhere in this build) that pairs this instance with a hosted
// relay or reaches off its own LAN by itself - see GET /api/help, whose
// `remoteAccess` field states that plainly, for why.

/** One URL this instance might answer on - api.ReachableAddress. */
export interface ReachableAddress {
  /** "this connection" for the address the request that fetched this data
   *  itself arrived on (proven, not guessed), otherwise a local interface's
   *  own IP. */
  label: string;
  url: string;
  /** 127.0.0.1/localhost/::1: reachable only from this same machine, never
   *  a phone on the LAN, and never what the QR code encodes. */
  loopback: boolean;
  /** A real hostname behind a reverse proxy or VPN, not a bare LAN IP -
   *  api.ReachableAddress.Domain. The one kind of address that still works
   *  once whatever is scanning the QR code has left this network. */
  domain: boolean;
}

/**
 * A QR code as the plain module grid the server computed - api.QRMatrix.
 * Rendered client-side as inline SVG (components/QRCode.tsx), never sent as
 * an image - see that component's own comment for why encoding stays on the
 * server (a solved, easy-to-get-subtly-wrong problem) while drawing stays on
 * the client (trivial, and never out of sync with the address list this
 * same response already carries).
 */
export interface QRMatrix {
  size: number;
  /** One string per row, '1' for a dark module, '0' for a light one. */
  bits: string[];
}

/** What GET /api/remote-access answers with - api.RemoteAccessInfo. */
export interface RemoteAccessInfo {
  /** "container" or "desktop" - fetchDeploymentInfo's own fuller version of
   *  the same fact. The desktop build never opens a TCP port at all, so
   *  every field below is empty/false for it rather than guessed at. */
  deployment: string;
  passwordSet: boolean;
  /** Every address this build can name for this instance, the one the
   *  request itself arrived on always first. */
  addresses: ReachableAddress[];
  /**
   * The loud warning's own condition: no password is set, AND this very
   * request just proved this instance is reachable from somewhere other
   * than this machine itself. Proof, not a forecast built from how the
   * server is configured to listen - see the Go route's own comment on
   * requestIsNonLoopback for why a configured-address forecast was tried
   * and rejected (it reads "exposed" for nearly every ordinary container
   * install, whether or not the host actually forwards the port anywhere).
   */
  exposed: boolean;
  /** The primary address (addresses[0]) as a scannable code; absent when
   *  there is nothing to encode (the desktop build, or no address at all). */
  qr?: QRMatrix;
}

export async function fetchRemoteAccess(): Promise<RemoteAccessInfo> {
  return json<RemoteAccessInfo>(await fetch('/api/remote-access'));
}

export async function fetchHealth(): Promise<{ status: string; version: string }> {
  return json(await fetch('/api/health'));
}

export async function fetchExtensionVersion(): Promise<{ version: string }> {
  return json(await fetch('/api/browser-extension/version'));
}

export interface UpdateCheck {
  checked: boolean;
  available: boolean;
  current: string;
  latest?: string;
  url?: string;
}

export async function fetchUpdateCheck(): Promise<UpdateCheck> {
  return json(await fetch('/api/system/update-check'));
}

/** One external program a media download runs, as it stands on this machine. */
export interface MediaTool {
  found: boolean;
  path?: string;
  version?: string;
  /** "managed" | "env" | "path" | "none". yt-dlp only: ffmpeg and ffprobe are
   *  whatever is on PATH, so a source word on their rows would never vary. */
  source?: string;
  /** Why it was not found, or why a recorded copy is unusable. English, and a
   *  fact about this machine rather than a translated phrase - the same
   *  convention Feature.reason follows, for the same reason: it names a path,
   *  an errno or a program's own output, none of which survive translation. */
  detail?: string;
}

/** What was fetched, when, and what it turned out to be. */
export interface YtdlpManagedRecord {
  tag: string;
  asset: string;
  sha256: string;
  /** What the staged file printed at its smoke test, not what the tag claims. */
  version: string;
  fetchedAt: string;
}

export interface MediaToolsStatus {
  ytdlp: MediaTool;
  ffmpeg: MediaTool;
  ffprobe: MediaTool;
  managed?: YtdlpManagedRecord;
  /** Where a fetched copy lives, sent alongside `managed`. Its own field
   *  because of the one state where ytdlp.path is NOT that file: a recorded
   *  copy that no longer starts, where ytdlp.path names the fallback that took
   *  over instead. */
  managedPath?: string;
  /** What would run if the fetched copy were removed; absent unless one is in
   *  force. The "your own copy has fallen behind the system's" warning is built
   *  from this, and without it this feature makes the long run worse rather
   *  than better. */
  shadowed?: MediaTool;
}

export interface YtdlpLatest {
  checked: boolean;
  tag?: string;
  url?: string;
  /** "newer" | "same" | "older" | "unknown". Four states and never a bool:
   *  yt-dlp versions are dates and a same-day rerelease adds a fourth segment,
   *  so "these two cannot be ordered" has to be sayable. Rendering that as
   *  "you are current" is the one wrong answer available here. */
  compare: string;
  /** GitHub's own refusal, verbatim, when checked is false - "API rate limit
   *  exceeded" and "Not Found" send somebody to two different places. */
  detail?: string;
}

/** Which yt-dlp, ffmpeg and ffprobe this instance runs. Never calls out: it is
 *  read on every settings-page load and by every diagnostics bundle. */
export async function fetchMediaTools(): Promise<MediaToolsStatus> {
  return json(await fetch('/api/mediatools'));
}

/** The one call in this feature that leaves the box. Downloads nothing and
 *  replaces nothing. */
export async function fetchYtdlpLatest(): Promise<YtdlpLatest> {
  return json(await fetch('/api/mediatools/ytdlp/latest'));
}

/** Slow: downloads yt-dlp's newest release, verifies it against the release's
 *  own SHA2-256SUMS, runs the staged file once to prove it starts on this
 *  machine, and only then swaps it in. Nothing is replaced unless every step
 *  passed, so a rejection leaves the copy that was working exactly as it was. */
export async function updateYtdlp(): Promise<{ tag: string; version: string; path: string; asset: string; sha256: string }> {
  return json(await fetch('/api/mediatools/ytdlp/update', { method: 'POST' }));
}

/** Deletes the fetched copy and its record, and answers with the status that
 *  results - so the card can say which yt-dlp is running now without a second
 *  request drawing a gap in between. */
export async function revertYtdlp(): Promise<MediaToolsStatus> {
  return json(await fetch('/api/mediatools/ytdlp/revert', { method: 'POST' }));
}

/**
 * Downloads and applies the latest release, then relaunches - desktop only
 * (internal/api/routes_lifecycle.go's own POST /api/system/update-install
 * refuses with 501 on the container build, where App.RequestUpdateInstall
 * is nil). Slow: the request does not resolve until the download and swap
 * finish, at which point the process is already on its way out to
 * relaunch - the caller races that exit the same way requestRestart's own
 * "shutting down" response already does, and should treat any network
 * error here (a fetch that never resolves, a reset connection) as "it
 * probably worked" rather than a real failure.
 */
export async function installUpdate(): Promise<{ status: string }> {
  return json(await fetch('/api/system/update-install', { method: 'POST' }));
}

// fetchDiagnostics is called both to render the diagnostics page's live
// preview and, again, right before a download - the bundle is meant to
// reflect the moment it was pulled, not whatever the page happened to load
// with (log lines and the goroutine count move constantly, and both are the
// point of the bundle).
export async function fetchDiagnostics(): Promise<Diagnostics> {
  return json<Diagnostics>(await fetch('/api/diagnostics'));
}

// --- The start report, the log, the file owners and the self-test -----------
//
// Four separate readings, landed here together because each of their authors
// asked for the same anchor: they are all the second half of the sentence the
// diagnostics bundle above starts. The bundle is a snapshot to attach to a bug
// report; these are the questions somebody asks while they still have the page
// open. Kept in four labelled blocks rather than interleaved, so each one still
// reads as the argument its author wrote.

// ----- The start checks (internal/startupcheck) -----

/**
 * One thing the start check looked at - startupcheck.Check
 * (internal/startupcheck). `id`, `role`, `verdict` and `code` are stable server
 * ids and never prose: the server has no idea which of the forty-two languages
 * is reading, which is the same reason Feature.ID travels rather than a label.
 */
export interface StartupCheck {
  /** "data" | "java" | "ytdlp" | "ffmpeg" | "ffprobe" | "folder" | "clock" -
   *  open, so an id this build has never heard of still draws as a row. */
  id: string;
  /** Folder rows only: "downloads" | "work" | "category" | "extract" | "extractMove" | "watch". */
  role?: string;
  /** "ok" | "warn" | "fail" | "skipped". "skipped" is "not needed on this
   *  install" and never "switched off". */
  verdict: string;
  /**
   * The concrete thing that was checked: an absolute folder, the binary that
   * was found, the raw TZ value. The data directory is masked to "<data>"
   * before it leaves the server, because this document is a file people attach
   * to public bug reports and a desktop data directory carries somebody's name.
   */
  subject?: string;
  /** The fact worth reading: a version line, the zone abbreviation and its
   *  offset. Already clamped server-side, because `ffmpeg -version` answers
   *  with several hundred bytes of banner. */
  detail?: string;
  /**
   * Folder rows: the deepest folder above `subject` that does exist. When it
   * differs from `subject` the folder is not there, and on a box that mounts a
   * share that usually means the share is not mounted - the same distinction
   * DiskReport.Measured draws (internal/app/app_diskreport.go).
   */
  measured?: string;
  /**
   * Folder rows: a test file really was written into this folder and removed
   * again.
   *
   * FALSE IS THE NORMAL CASE and does not mean a write failed. A pass started
   * by the boot writes nothing anywhere, so "ok" then means the folder is
   * there, not that this instance can write in it. See StartupReport.probed.
   */
  probed?: boolean;
  /** WHICH failure, so startupAdvice can offer the one remedy that helps. Open string. */
  code?: string;
  /** The system's own message, verbatim and clamped. Shown raw, never
   *  translated: a translated errno is neither searchable nor quotable. */
  err?: string;
}

/** One whole pass of the start checks - startupcheck.Report. */
export interface StartupReport {
  /** "running" | "done" | "off". "off" means KL_STARTUP_CHECK=0, and the page
   *  says so rather than showing an empty list as a clean result. */
  state: string;
  startedAt: string;
  finishedAt?: string;
  /**
   * Whether this pass was allowed to write its test file at all: false for
   * every boot pass, true only for one somebody pressed the button for.
   *
   * A property of the PASS and not of a row, so "nothing was written anywhere"
   * is one sentence at the top of the card instead of a qualifier repeated on
   * every line. The boot deliberately only looks, because writing into a folder
   * on an Unraid array wakes the disk that share sits on, and probing every
   * configured folder would do that at every container restart.
   */
  probed: boolean;
  /** Never null: the server initialises the slice, because a nil slice encodes
   *  as JSON null and the page walking it would throw. */
  checks: StartupCheck[];
}

/**
 * Runs the start checks again NOW and answers with a fresh report.
 *
 * POST AND NOT GET BECAUSE IT WRITES. This is the pass that drops a small test
 * file into every configured folder that exists and deletes it again, and a GET
 * that writes is a route any browser prefetch, any speculative navigation and
 * any link scanner fires on its own.
 *
 * The answer does NOT become the bundle's own reading. The server keeps the one
 * taken at start (Diagnostics.startup), because that is the one a bug report
 * needs and pressing this is exactly when somebody would destroy it.
 */
export async function runStartupCheck(): Promise<StartupReport> {
  return json<StartupReport>(await fetch('/api/diagnostics/startup', { method: 'POST' }));
}

// ----- The log: the tail, the optional file on disk, and one download's lines -----

/**
 * One line as the server read it: the text, which part of the app it came from,
 * and which download it names, if any.
 *
 * SOURCE AND TASKID ARE DECIDED ON THE SERVER, and that is the point. Both are
 * rules about lines this tree writes - a table of the prefixes they start with,
 * and the literal word "task" followed by a sixteen-character id - so a second
 * copy on this side would drift the first time somebody reworded a log call,
 * and the drift would show up as a filter that quietly matches nothing.
 */
export interface LogLine {
  seq: number;
  line: string;
  source?: string;
  taskId?: string;
}

export interface LogTail {
  entries: LogLine[];
  /**
   * Lines the server's memory buffer threw away between two polls. Non-zero
   * means the follow view has a hole in it and must SAY so rather than joining
   * the two halves silently: a busy instance can log more than the buffer holds
   * in two seconds, and a log that reads as continuous and is not is the one
   * failure a diagnostic view may never have.
   */
  dropped: number;
  /** The cursor for the next poll. Resets on restart; the server handles a
   *  cursor from a process that is gone by starting over. */
  newest: number;
  capacity: number;
  /** The source buckets the server offers, in its own order. Never levels:
   *  nothing in this tree records one. */
  sources: string[];
}

export interface LogGeneration {
  /** 0 is the file being written; 1 the newest renamed one. */
  index: number;
  bytes: number;
  modifiedAt: string;
}

export interface LogFileState {
  /** Whether lines are reaching a file RIGHT NOW - not the settings switch. A
   *  sink that was armed and then failed is false here with a sentence in
   *  `problem`, which is the distinction the card is built on. */
  enabled: boolean;
  /** Where it is, or would be: the server answers this even with nothing armed,
   *  because that is exactly when somebody is deciding whether to switch it on.
   *  Always empty in the diagnostics bundle - see the Diagnostics field above. */
  path: string;
  bytes: number;
  maxBytes: number;
  keep: number;
  /** Never null. */
  generations: LogGeneration[];
  /** Empty while the file is being written. Non-empty is the sentence the card
   *  shows, and freeKnown/freeBytes is what turns it into advice. */
  problem?: string;
  /** Only measured while `problem` is set. */
  freeBytes?: number;
  /** False means the platform could not be asked, which is NOT the same as zero
   *  bytes free - drawing an empty disk there sends somebody hunting for a
   *  problem they do not have. */
  freeKnown: boolean;
}

export interface TaskLog {
  lines: LogLine[];
  /** Where the lines were looked for. 'memory' today, always: the alternative
   *  is reading up to a gigabyte of rotated files off an array volume every
   *  time somebody double-clicks a row. */
  scanned: 'memory' | 'memory+file';
  /** Always true, and the card says so: seven of this app's log call sites
   *  record which download they are about and the rest do not. */
  partial: boolean;
}

/**
 * The log lines newer than `since`, plus what fell out of memory in between.
 * Pass 0 for everything the buffer holds. `limit` takes the OLDEST matching
 * lines, so a limited answer advances the cursor instead of skipping the
 * middle.
 */
export async function fetchLogTail(since: number, limit?: number): Promise<LogTail> {
  const q = new URLSearchParams({ since: String(since) });
  if (limit !== undefined) q.set('limit', String(limit));
  return json<LogTail>(await fetch(`/api/diagnostics/log?${q.toString()}`));
}

/** Whether the log is being written to disk, where, how big, and what to try
 *  when it is not. */
export async function fetchLogFileState(): Promise<LogFileState> {
  return json<LogFileState>(await fetch('/api/diagnostics/logfile'));
}

/**
 * The log lines that name one download.
 *
 * LOCAL ONLY. It sits under /api/diagnostics, which neither the relay nor the
 * federation proxy forwards - a log line can carry a feed URL with an indexer's
 * key in its query string, which a task list never does, so the reasoning those
 * two allowlists rest on does not cover this. Ask isLocalBase first.
 */
export async function fetchTaskLog(id: string): Promise<TaskLog> {
  return json<TaskLog>(await fetch(`/api/diagnostics/task/${encodeURIComponent(id)}`));
}

/**
 * The address one log file is downloaded from. A plain href and not a fetch:
 * the route answers text/plain with a Content-Disposition, so the browser does
 * the whole job, and a file that can be a gigabyte has no business being read
 * into a Blob in a tab first.
 */
export function logFileHref(gen: number): string {
  return `/api/diagnostics/logfile/${gen}`;
}

// ----- Who this instance writes files as (internal/fileowner) -----

/**
 * Who this instance writes files as, and what the environment asked for.
 *
 * `known` false is a real third answer and not a zero: the desktop build on
 * Windows has no unix file owners at all, and every number below then means
 * NOTHING - not uid 0, not root. Same rule as VolumeReport.known, and whatever
 * draws this has to say so in words rather than print zeroes.
 */
export interface FileOwnerIdentity {
  known: boolean;
  /** "container" | "desktop" (internal/buildinfo.Deployment). The two need
   *  different sentences: PUID is a container idea, and the desktop app runs as
   *  the person sitting in front of it. */
  deployment: string;
  /** The EFFECTIVE ids, which is what the kernel stamps on a file at creation. */
  uid: number;
  gid: number;
  /** "" when the id has no passwd or group entry, which is normal under
   *  --user 99:100 and not an error - the number alone is a usable answer. */
  user: string;
  group: string;
  /** Four octal digits, "0022". Empty when umaskKnown is false. */
  umask: string;
  /** False on a kernel with no Umask: line in /proc/self/status (it arrived in
   *  Linux 4.7 and exists nowhere else). There is no portable read-only
   *  umask(2), and the read-then-restore trick is a race in a process that
   *  creates files on a dozen goroutines, so it is not attempted. */
  umaskKnown: boolean;
  /** What the operator set, verbatim. "" means unset, which is NOT 0: unset
   *  PUID means the image's own uid 1000, unset UMASK means the runtime's mask. */
  env: { puid: string; pgid: string; umask: string };
  /**
   * Whether anything in this build ACTS on those three. It is false today, and
   * that is the finding rather than an omission: the image declares USER
   * knight, so the process starts as uid 1000 and an unprivileged process
   * cannot become another uid. A page that showed only the effective uid would
   * leave an operator staring at a PUID they set, that is plainly there in
   * `docker inspect`, and that did nothing - with no way to tell that from
   * having typed it wrong. Never render this as "PUID would work if you set it".
   */
  envRead: boolean;
}

/** One folder as the probe MEASURED it: a real file and a real sub-folder were
 *  created inside it, stat-ed, and removed again. Nothing here is computed from
 *  the process uid and the umask, because that answer is wrong on a set-group-id
 *  folder, on an NFS export with root squash, and on any mount carrying its own
 *  umask option - which is three of the situations this exists for. */
export interface FolderOwnerProbe {
  dir: string;
  /** "downloads" | "work" | "category" | "watch" - a stable id, looked up
   *  locally. The server has no idea which of the 42 locales is reading. */
  role: string;
  exists: boolean;
  /** False where files have no owner. Every number below then means nothing. */
  known: boolean;
  dirUid: number;
  dirGid: number;
  dirUser: string;
  dirGroup: string;
  /** Four octal digits, so a set-group-id folder reads "2775". */
  dirMode: string;
  /** The probe file, which is what a finished download will look like. */
  fileUid: number;
  fileGid: number;
  fileUser: string;
  fileGroup: string;
  fileMode: string;
  /** What a new per-package folder comes out as. The field a file-only probe
   *  would not have: with a tight umask the files can be fine while the folder
   *  containing them cannot be entered, and every download lands in one. */
  subdirMode: string;
  /** A stable id, never prose:
   *  ok | ownerMismatch | groupUnreadable | dirUnreadable | notWritable | missing | unknown */
  verdict: string;
  /** The raw OS error, present only for the verdicts that have one. */
  detail?: string;
}

export interface FolderOwnerReport {
  checkedAt: string;
  /** Never null - a nil slice would encode as JSON null and the map() throws. */
  folders: FolderOwnerProbe[];
}

/** One configured folder as a STAT saw it, as the diagnostics bundle carries it.
 *  NO PATH and no raw error, deliberately: that bundle is attached to public bug
 *  reports and the desktop build's default download folder sits inside the
 *  user's own home directory. The role and the numbers are the whole finding. */
export interface FolderOwnership {
  role: string;
  exists: boolean;
  known: boolean;
  uid: number;
  gid: number;
  user: string;
  group: string;
  mode: string;
}

/** Who this instance writes files as. Reads only - it stats nothing and creates
 *  nothing, so a page may hold it and refresh it freely. */
export async function fetchFileOwner(): Promise<FileOwnerIdentity> {
  return json<FileOwnerIdentity>(await fetch('/api/fileowner'));
}

/**
 * Measure what a file written into each configured folder actually comes out as.
 *
 * A POST because it WRITES: one probe file and one probe sub-folder per folder,
 * both removed again. It must not be reachable by a link, a prefetch or a page
 * refresh. `dirs` narrows the run to some of the folders and must name folders
 * this instance already writes into - anything else is refused with a 400 that
 * names it, rather than skipped, because a report that measured three of the
 * four folders it was asked about looks complete and is not.
 */
export async function checkFolderOwners(dirs?: string[]): Promise<FolderOwnerReport> {
  return json<FolderOwnerReport>(await post('/api/fileowner/check', dirs ? { dirs } : {}));
}

// ----- The instance's own self-test, and the reverse-proxy echo -----

/**
 * internal/selftest.Status. Five values, and the last two are the point:
 * `skipped` means there is nothing configured here to check, `unknown` means it
 * IS configured and this build cannot find out. Collapsing them is the easiest
 * mistake in this feature - the same distinction internal/diskspace's second
 * return value exists to keep. Neither is ever the word "off".
 */
export type SelfTestStatus = 'pass' | 'warn' | 'fail' | 'skipped' | 'unknown';

/** One check, mirroring internal/selftest.Result field for field. */
export interface SelfTestResult {
  /** "jd" | "ytdlp" | "folders" | "accounts" | "relay" | "clock" | "torrentPort",
   *  or - inside `rows` - the folder path or account key that row describes. */
  id: string;
  status: SelfTestStatus;
  /** The stable name of the sentence to render, e.g. "ytdlp.old". Never English
   *  prose from the server: the same call routes_features.go's Feature.ID and
   *  reconnect.ConfigProblem.Code already make, because the server has no idea
   *  which of the forty-two locales is in front of the reader. */
  code: string;
  /** That sentence's substitutions: version, days, dir, measured, free, mark,
   *  port, label, hosts, zone, time. Byte counts arrive as decimal strings of
   *  BYTES and are formatted by fmtBytes here - a server that wrote "4,2 GB"
   *  would have picked the reader's language and decimal separator for them. */
  params?: Record<string, string>;
  /** The OTHER side's own words: a Go error, a provider's refusal, a router's
   *  fault string. English and untranslated on purpose, exactly as
   *  portmap.Result.Detail is - they did not come from this app. */
  detail?: string;
  /** One level only: one row per debrid account, one per folder. */
  rows?: SelfTestResult[];
  /** RFC3339. */
  at: string;
}

export interface SelfTestRun {
  /** Empty on an instance that has never been swept, which is what the page
   *  draws its "not run yet" line from. */
  id: string;
  startedAt: string;
  /** Absent while the sweep is still going - the page polls until it appears. */
  finishedAt?: string;
  /** Every check id this sweep will report, in order, so the page draws all
   *  seven rows as waiting before the first result lands. */
  planned: string[];
  results: SelfTestResult[];
}

/**
 * What this instance saw of the very request that asked - the server half of
 * the reverse-proxy card. The browser half is window.location plus a probe
 * WebSocket, and the check IS the comparison of the two: a probe run on the
 * server would dial its own listener on loopback, never touch the proxy, and
 * report four rows that are always green.
 */
export interface SelfTestRequestView {
  /** r.Host, as this instance received it. internal/api's sameOrigin compares
   *  the browser's Origin against exactly this and 403s a mismatch. */
  host: string;
  /** X-Forwarded-Host, "" when absent. */
  forwardedHost: string;
  /** X-Forwarded-Proto, lowercased, "" when absent. */
  forwardedProto: string;
  /** Whether the connection into THIS process was TLS (r.TLS != nil), which is
   *  a different fact from whether the browser is on https. */
  tls: boolean;
  /** r.URL.Path, as evidence that nothing rewrote the path on the way in. */
  path: string;
  /** X-Forwarded-Prefix with any trailing slash removed, "" when it names no
   *  prefix at all. The only signature of a stripped path prefix that survives
   *  the stripping: an UNstripped one never reaches this route in the first
   *  place, and a stripped one is otherwise invisible by construction. */
  forwardedPrefix: string;
  /** How many hops X-Forwarded-For names. The addresses are deliberately not
   *  sent: the chain is a map of somebody's internal network, and the count
   *  answers the only question the card asks of it. */
  forwardedForHops: number;
  /** This instance's clock when it answered, RFC3339 with its offset. Note the
   *  local time before and after the call and take the midpoint, so half of a
   *  slow round trip is not read as clock drift - see lib/selftest.ts. */
  now: string;
  /** time.Local's name ("UTC", "Europe/Berlin") and its offset right now. */
  zone: string;
  zoneOffsetSeconds: number;
  /** False only when $TZ names a zone the database could not supply, in which
   *  case every time in that process has silently fallen back to UTC. */
  zoneReadable: boolean;
  /** buildinfo.Deployment, so the page can hide the proxy rows on desktop. */
  deployment: string;
}

/**
 * Starts one sweep and returns at once - the route answers 202 and the work
 * outlives the request, because seven checks including up to seven provider
 * logins outlive any reverse proxy's read timeout (this repo has hit that once
 * already, on POST /api/links). Poll fetchSelfTest until finishedAt appears.
 *
 * A second call while a sweep is in flight JOINS it and answers with that
 * sweep's id rather than starting a second one: seven providers asked twice
 * over is a rate-limit refusal that then reads like a dead key.
 */
export async function startSelfTest(): Promise<SelfTestRun> {
  return json<SelfTestRun>(await fetch('/api/selftest', { method: 'POST' }));
}

/** The current or last sweep. An instance that has never been swept answers a
 *  run with an empty id rather than a 404, so the ordinary first load of the
 *  page needs no special case. */
export async function fetchSelfTest(): Promise<SelfTestRun> {
  return json<SelfTestRun>(await fetch('/api/selftest'));
}

/** Timed by the caller: the round trip is what the clock comparison discounts.
 *  no-store because a cached answer would hand back a timestamp minutes old and
 *  have it reported as drift. */
export async function fetchRequestView(): Promise<SelfTestRequestView> {
  return json<SelfTestRequestView>(await fetch('/api/selftest/request', { cache: 'no-store' }));
}

/**
 * What the two files this instance keeps are costing, and where a compaction's
 * scratch copy would go (internal/app's StorageInfo).
 *
 * THE PATHS ARE HERE AND DELIBERATELY NOT IN THE DIAGNOSTICS BUNDLE. That
 * bundle is a file people attach to public bug reports, and a desktop data
 * directory is C:\Users\<their real name>\AppData\...; this document only ever
 * reaches somebody already looking at their own settings pages, where the path
 * is the single most useful thing on screen the moment a check comes back
 * damaged.
 */
export interface StorageInfo {
  storePath: string;
  /** The .db plus any -journal/-wal/-shm beside it. */
  storeBytes: number;
  /** freelist_count * page_size. A FLOOR: compacting repacks half-filled pages too, so it usually gives back more. */
  storeReclaimableBytes: number;
  settingsPath: string;
  settingsBytes: number;
  /** False on an install that has never saved a settings page - the server reads settings.json and never writes it. */
  settingsPresent: boolean;
  /**
   * Where SQLite writes the full second copy a compaction needs, which is
   * almost never where anybody looks: the container image sets no TMPDIR, so
   * the scratch copy of a 6 GB store lands in the container's own writable
   * layer rather than on the mounted data volume. Without this the failure
   * reads "database or disk is full" and names the wrong disk.
   */
  tempDir: string;
  /** 0 when this build cannot ask the platform, which is not the same as 0 bytes free. Render it with tempDir, never alone. */
  tempFreeBytes: number;
}

/** One completed maintenance pass, exactly as the server recorded it. */
export interface MaintenanceRun {
  kind: 'check' | 'compact' | 'analyze';
  at: string;
  durationMs: number;
  /** A check that found damage is not ok; neither is one that could not run at all, and `error` tells the two apart. */
  ok: boolean;
  /** integrity_check's findings, one per entry, never null and never containing the bare word "ok". */
  problems: string[];
  /** The file's size either side of a compaction, and 0 for the two kinds that do not change it. */
  bytesBefore: number;
  bytesAfter: number;
  error: string;
  /** '' on a pass that ran; 'downloads-running' on a SCHEDULED one that stood down. */
  skipped: string;
}

/**
 * The whole answer to GET /api/system/maintenance.
 *
 * `last` and `nextRunAt` are NULLABLE on purpose. "Nothing has ever run here"
 * and "something ran and found nothing wrong" are different answers, and a
 * zero-valued object would render the second when the truth is the first -
 * which on this page is a clean bill of health nobody earned. Same reasoning
 * core.Task.AutoExtract already carries on the Go side.
 */
export interface MaintenanceState {
  storage: StorageInfo;
  /** '' when idle. The page polls while this is non-empty and stops when it clears. */
  running: '' | 'check' | 'compact' | 'analyze';
  intervalDays: number;
  compactOnSchedule: boolean;
  nextRunAt: string | null;
  last: MaintenanceRun | null;
}

export async function fetchMaintenance(): Promise<MaintenanceState> {
  return json<MaintenanceState>(await fetch('/api/system/maintenance'));
}

/**
 * Starts one pass and returns at once - the route answers 202 and the work
 * outlives the request, because a compaction on a multi-gigabyte store outlives
 * any browser timeout and any reverse proxy's. Poll fetchMaintenance while
 * `running` is non-empty.
 *
 * Throws an ApiError with `status === 409` when a pass is already going. That
 * refusal is the feature and not an accident: two rewrites queued on the
 * store's one connection is one rewrite followed by a second, pointless one,
 * with every write in the process frozen for the sum of both. The 409's body is
 * the state document rather than an error envelope, so `message` is JSON - read
 * the status, not the sentence, and show settings.dbmaint.busy.
 */
export async function startMaintenance(action: 'check' | 'compact' | 'analyze'): Promise<MaintenanceState> {
  return json<MaintenanceState>(
    await fetch('/api/system/maintenance', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action }),
    }),
  );
}

/** What internal/api/routes_lifecycle.go's DeploymentInfo answers - which build this is and what quit/restart actually do here. */
export interface DeploymentInfo {
  deployment: string;
  canQuit: boolean;
  canRestart: boolean;
  note: string;
}

export async function fetchDeploymentInfo(): Promise<DeploymentInfo> {
  return json<DeploymentInfo>(await fetch('/api/system/deployment'));
}

export async function requestQuit(): Promise<{ status: string }> {
  return json(await fetch('/api/system/quit', { method: 'POST' }));
}

export async function requestRestart(): Promise<{ status: string }> {
  return json(await fetch('/api/system/restart', { method: 'POST' }));
}

/**
 * Where a backup archive streams from - opened directly (an <a href> or
 * window.location, same as taskFileURL above), never fetched through this
 * client: the browser's own download handling is what a multi-hundred-MB
 * database export needs.
 */
export const BACKUP_DOWNLOAD_URL = '/api/system/backup';

export interface RestoreResult {
  manifest: { version: string; deployment: string; createdAt: string };
  restarting: boolean;
  status: string;
}

export async function uploadRestore(file: File): Promise<RestoreResult> {
  const body = new FormData();
  body.append('file', file);
  return json(await fetch('/api/system/restore', { method: 'POST', body }));
}

/**
 * A settings-only export, as internal/settings.PortableDoc writes it.
 *
 * NOT a backup, and the distinction is the whole point of the pair sitting
 * here together. The archive above moves an INSTALL - the database, the task
 * history, this box's own identity - and applies at the next start-up,
 * wholesale, over whatever was there. This is settings.json alone, minus the
 * two identity keys (instanceId, knownDomains), taken back in key by key and
 * applied live with no restart.
 */
export interface SettingsExportDoc {
  kind: 'knightloader-settings';
  version: string;
  deployment: string;
  createdAt: string;
  /** What the file CLAIMS about itself. Judge a document by what it holds and
   *  never by this: it is a string in a file anybody can edit, which is why
   *  both settings.Secretless (Go) and secretlessKeys (settingsTransfer.ts)
   *  read the settings themselves instead. */
  secrets: 'included' | 'omitted';
  /** settings.json's own top-level keys, raw. Deliberately not typed as
   *  Settings: a file written by an older build carries keys this one no
   *  longer has and misses keys it has gained, and the import has to be able
   *  to list both rather than let either vanish. */
  settings: Record<string, unknown>;
}

/**
 * Where a settings export streams from - opened directly (window.location or
 * an <a href>), never fetched through this client, exactly like
 * BACKUP_DOWNLOAD_URL above: the browser owns the save dialog.
 *
 * includeSecrets is false at every call site (jdp, 2026-09-08). The server
 * treats anything but the literal "include" as "omit", so a caller that gets
 * this wrong leaks nothing.
 */
export const settingsExportURL = (includeSecrets: boolean) =>
  `/api/settings/export?secrets=${includeSecrets ? 'include' : 'omit'}`;

/**
 * What POST /api/settings/import did, key by key. "ok" is not an answer here:
 * three of these five fields describe things that save cleanly and then fail
 * silently hours later.
 */
export interface SettingsImportResult {
  /** The keys that reached the store. */
  applied: string[];
  /** Asked for and refused: the identity keys, and keys the file does not
   *  actually carry. */
  skipped: string[];
  /** In the file, absent from this build's Settings struct. Reported because
   *  encoding/json would otherwise drop them without a word, and because
   *  migrate() runs only inside settings.Load against settings.json's own
   *  bytes - never against a patch body - so a renamed key cannot be mapped. */
  unknown: string[];
  /** Codes, not sentences: "reconnect.password", "connections.password",
   *  "archivePasswords". The interface picks the words - the server has no
   *  idea which of forty-two languages the reader is looking at. */
  incomplete: string[];
  /** How many imported rules this build cannot compile. They save and never
   *  fire (sanitizeRules changes nothing on purpose), so a non-zero count has
   *  to reach the screen. */
  ruleProblems: number;
  /** The whole document as it now stands, for folding back into the settings
   *  shell's two copies - see SettingsDraft.reseed. */
  settings: Settings;
}

/**
 * importSettings takes over exactly the named keys and nothing else.
 *
 * The document travels with the selection because the selection was made
 * against THAT document, in a preview the browser built from it; re-uploading
 * the file and remembering the ticks separately would be two round trips that
 * can disagree about what was on screen.
 *
 * On a refusal it throws an ApiError carrying the server's own sentence, and
 * for the one refusal that is an instruction rather than a diagnosis - a file
 * written by a newer build - also the code "transfer.tooNew" with
 * { version, running }.
 */
export async function importSettings(
  document: SettingsExportDoc,
  keys: string[],
): Promise<SettingsImportResult> {
  const r = await fetch('/api/settings/import', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ document, keys }),
  });
  return json<SettingsImportResult>(r);
}

export async function fetchInstances(): Promise<Instance[]> {
  return (await json<Instance[]>(await fetch('/api/instances'))) ?? [];
}

/**
 * One instance announcing itself on this network right now (internal/discovery).
 *
 * Nothing here is paired or trusted - being on the same network is not
 * consent. This is the address, found without anyone typing it. Adding it is a
 * deliberate action, and it stores that address and nothing else: a peer with
 * a password set will still refuse it, because an address is not a
 * credential. The connection phrase is what makes two instances trust each
 * other, and it does so for every instance at once rather than per peer.
 */
export interface DiscoveredInstance {
  id: string;
  name: string;
  url: string;
  deployment: string;
  /** Already a stored or relay peer. Shown anyway, greyed - "the one I wanted
   *  is missing" and "it is here and already added" are different answers. */
  known: boolean;
}

export async function fetchDiscovered(): Promise<DiscoveredInstance[]> {
  return (await json<DiscoveredInstance[]>(await fetch('/api/discovery'))) ?? [];
}

/**
 * addInstance registers a peer BY ADDRESS. It exchanges no credential, so
 * `refused` is how a peer that was reached and said no is told apart from one
 * that could not be reached at all. Two problems with two different fixes
 * that used to share the word "offline" - and the fix for the first is the
 * connection phrase, which both instances hold rather than trading a code.
 */
export async function addInstance(
  name: string,
  url: string,
): Promise<{ online: boolean; refused: boolean }> {
  const r = await fetch('/api/instances', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, url }),
  });
  if (!r.ok) throw new Error(await r.text());
  return json(r);
}

export const removeInstance = (name: string) =>
  fetch(`/api/instances/${encodeURIComponent(name)}`, { method: 'DELETE' });

/**
 * What GET and PUT /api/relay/config both answer with - api.relayConfig. The
 * relay key itself is never in it, not even redacted: it lives in the
 * encrypted account store, and the only thing anybody is entitled to read
 * back is whether one is there at all, the same shape GET /api/tokens and
 * GET /api/accounts already report a credential in.
 */
/**
 * Which relay an instance uses. Three answers, not two, and the third is the
 * one the old shape could not express: 'off' means no relay at all, which is
 * exactly right for somebody whose instances all sit on one network and find
 * each other over local discovery.
 */
export type RelayMode = 'project' | 'own' | 'off';

export interface RelayConfig {
  /** Which relay is in force. Always one of the three - the server resolves
   *  the empty value an install from before this field stores. */
  mode: RelayMode;
  relayUrl: string;
  keySet: boolean;
  /** Whether the socket to the relay is actually up right now - NOT merely
   *  whether an address and a key are stored. A typo'd address, a key the
   *  relay rejects and a relay that is down all leave the config filled in
   *  and this false. */
  connected: boolean;
  /** Whether this instance is itself running the relay, under /relay/connect
   *  on its own address. */
  serve: boolean;
  /** How many instances are connected to the relay this instance is serving,
   *  this one included when it dials its own. Zero while `serve` is false. */
  serveClients: number;
}

export async function fetchRelayConfig(): Promise<RelayConfig> {
  return json<RelayConfig>(await fetch('/api/relay/config'));
}

/**
 * saveRelayConfig stores the address and, when `key` is given, the key -
 * answering with what is now stored, so the form renders from the save
 * rather than from what it hoped the save did.
 *
 * `key` has three meanings and the route can only tell them apart by
 * whether the field is on the wire at all, so this signature keeps that
 * distinction instead of flattening it: undefined leaves the stored key
 * alone (the ordinary save of an edited address from a form that was never
 * shown the key), '' clears it, and anything else replaces it.
 *
 * A relay that is unreachable is not a failure here. The address is stored
 * either way and the client keeps dialling it - an outage of an optional,
 * self-hosted relay must never read as "your settings were rejected".
 */
export async function saveRelayConfig(
  relayUrl: string,
  key?: string,
  serve?: boolean,
  mode?: RelayMode,
): Promise<RelayConfig> {
  // `serve` is omitted the same way `key` is, and for the same reason: a save
  // from the address form must not carry the switch back to whatever it was
  // when that form was drawn.
  const body: Record<string, unknown> = { relayUrl };
  if (key !== undefined) body.key = key;
  if (serve !== undefined) body.serve = serve;
  // Omitted the same way, and for the same reason: the address form and the
  // two mode switches are separate controls, and a save from one must not
  // carry the other back to whatever it was when that form was drawn.
  if (mode !== undefined) body.mode = mode;
  return json<RelayConfig>(
    await fetch('/api/relay/config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  );
}

/**
 * What GET /api/connect answers with - api.ConnectInfo.
 *
 * The connection phrase is how a person's own instances find each other:
 * one instance mints a phrase, every other one is given it, and they meet
 * on the relay. No account, no login, nothing to configure - the relay's
 * address is compiled into the binary, so the phrase carries only the
 * secret.
 *
 * What it does NOT give you is a public https:// address a stranger's
 * browser can open - that is a different job, and the answer to it is your
 * own domain in front of a reverse proxy.
 */
export interface ConnectInfo {
  /** Whether this instance holds a connection secret at all. */
  active: boolean;
  /** Whether the relay socket is actually up - a different question from
   *  `active`, since a stored phrase with an unreachable relay is
   *  configured but not working. */
  connected: boolean;
  /** Mirrors GET /api/auth, so the page can warn before minting a phrase
   *  that reaches every instance in the group. */
  passwordSet: boolean;
  /** Which relay this instance is pointed at. */
  relayUrl: string;
  /** Whether that is somebody's own relay rather than the default one. */
  selfHosted: boolean;
  /** The same three-way answer RelayConfig carries. `selfHosted` cannot
   *  express 'off', which is why both are here. */
  relayMode: RelayMode;
}

export async function fetchConnect(): Promise<ConnectInfo> {
  return json<ConnectInfo>(await fetch('/api/connect'));
}

/**
 * activateConnect mints this instance's phrase. Answers with it once - the
 * only time it comes back without the password, because the person who just
 * pressed the button is by definition already looking at the screen.
 */
export async function activateConnect(): Promise<{ phrase: string; qr?: QRMatrix; info: ConnectInfo }> {
  const r = await fetch('/api/connect/activate', { method: 'POST' });
  if (!r.ok) throw new Error(await r.text());
  return json(r);
}

/**
 * PhraseRejected is a phrase the server would not take, in the form this side
 * needs to say so in the reader's own language.
 *
 * The server sends the reason and the specifics rather than a sentence,
 * because a sentence it wrote could only ever be in one language. `word` and
 * `position` name the offending word for 'unknown_word' - the difference
 * between a message somebody can act on and "invalid phrase" - and `count`
 * says how many words actually arrived for 'word_count'.
 */
export class PhraseRejected extends Error {
  reason: 'word_count' | 'unknown_word' | 'checksum';
  word: string;
  position: number;
  count: number;

  constructor(body: { error?: string; reason?: string; word?: string; position?: number; count?: number }) {
    super(body.error ?? 'phrase rejected');
    this.name = 'PhraseRejected';
    // Anything the server did not name falls back to the checksum case: it
    // is the reason with no specifics to render, so an unknown code degrades
    // into the one sentence that is true of every rejected phrase rather
    // than into a blank message.
    this.reason =
      body.reason === 'word_count' || body.reason === 'unknown_word' ? body.reason : 'checksum';
    this.word = body.word ?? '';
    this.position = body.position ?? 0;
    this.count = body.count ?? 0;
  }
}

/** joinConnect is the other half: every instance after the first. */
export async function joinConnect(phrase: string): Promise<ConnectInfo> {
  const r = await fetch('/api/connect/join', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ phrase }),
  });
  if (!r.ok) {
    const raw = await r.text();
    // A rejected phrase answers with JSON; anything else that can fail here
    // (a session that expired, a proxy in the way) answers with text, so a
    // parse failure is the signal to fall back rather than an error itself.
    try {
      throw new PhraseRejected(JSON.parse(raw));
    } catch (e) {
      if (e instanceof PhraseRejected) throw e;
      throw new Error(raw.trim() || `${r.status}`);
    }
  }
  return json(r);
}

/**
 * revealConnect shows the phrase again. The password is required whenever
 * one is set - a live session is not enough, because what is behind this is
 * not this instance's own password but the key to every instance in the
 * group.
 */
export async function revealConnect(password: string): Promise<{ phrase: string; qr?: QRMatrix }> {
  const r = await fetch('/api/connect/reveal', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  });
  if (!r.ok) throw new Error(await r.text());
  return json(r);
}

/** leaveConnect forgets the secret and stops dialling the relay. */
export async function leaveConnect(): Promise<void> {
  const r = await fetch('/api/connect', { method: 'DELETE' });
  if (!r.ok) throw new Error(await r.text());
}

/**
 * connectWS opens the live task, queue and activity stream and
 * auto-reconnects. Returns a closer.
 *
 * kinds narrows what this connection receives - the hub's own Subscribe
 * (internal/hub/hub.go) - to exactly those broadcast types. Every real
 * caller in this app now passes one: lib/useTasks.ts and app/Layout.tsx's
 * useCompletionToasts want 'task'/'removed', components/IdleActionBanner.tsx
 * wants 'idleAction', components/StatusStrip.tsx wants 'activity',
 * components/Archives.tsx's useExtractJobs wants 'extract',
 * components/CaptchaModal.tsx wants 'captcha'/'captchaResolved', and
 * components/SkippedLinks.tsx wants 'skipped' - each opens its own
 * connection (there is no shared multiplexer yet) and previously received,
 * parsed and discarded every OTHER kind too. A `Hub.SendTo` message
 * ('snapshot', 'activitySnapshot') is a direct send to one connection, not a
 * Broadcast, so it bypasses this filter entirely and still arrives whether
 * or not its type is in kinds - see each call site's own note. kinds is
 * still optional: omitting it keeps getting everything, unchanged, since
 * the server-side default for a connection that never subscribes is
 * "everything". Sent again on every reconnect, since a fresh socket starts
 * unfiltered until it says otherwise.
 */
export function connectWS(onMessage: (type: string, data: any) => void, kinds?: string[]): () => void {
  let ws: WebSocket | null = null;
  let closed = false;
  const open = () => {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    ws = new WebSocket(`${proto}://${location.host}/api/ws`);
    if (kinds && kinds.length > 0) {
      const subscribe = kinds;
      ws.onopen = () => ws?.send(JSON.stringify({ type: 'subscribe', kinds: subscribe }));
    }
    ws.onmessage = (e) => {
      try {
        const m = JSON.parse(e.data);
        onMessage(m.type, m.data);
      } catch {
        /* ignore */
      }
    };
    ws.onclose = () => {
      if (!closed) setTimeout(open, 1500);
    };
  };
  open();
  return () => {
    closed = true;
    ws?.close();
  };
}
