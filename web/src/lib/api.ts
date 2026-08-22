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
// | 'network' | 'diskFull' | 'unsupported' | 'captcha' | 'cancelled', or '' when
// nothing recognised the failure — a value it declines to guess at rather than
// one it forgot to set.
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
  /** A package the user chose by hand; automatic re-packaging leaves it alone. */
  manualPackage?: boolean;
  reason?: Reason;
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
   *  its own - 0 disables the countdown. No UI control yet; server default
   *  applies. */
  autoConfirmDelay: number;
  /** What a CONFIRMED batch does next: start immediately (true, the default -
   *  preserves this app's behaviour from before autoConfirm/autoStart were
   *  split apart) or wait on Hold for a person to start it by hand. */
  autoStart: boolean;
  /** confirm.Policy value ("include"|"exclude"|"exclude-and-remove"|"ask") for
   *  a link that duplicates one already in the list at confirm time. No UI
   *  control yet; server default (exclude) applies. */
  onDupes: string;
  /** Same shape as onDupes, for a link already known offline at confirm time.
   *  No UI control yet; server default (exclude) applies. */
  onOffline: string;
  /** A newly-confirmed batch is placed at the front of the queue rather than
   *  the back. No UI control yet; server default (false) applies. */
  addAtTop: boolean;
  downloadDir: string;
  subfolderByPackage: boolean;
  archivePasswords: string[];

  /**
   * Where extractions are collected. Empty means beside the archive, which is
   * what every install did before the setting existed. May be a pathvars
   * template, so the folder chooser has to keep the tail - see FolderPicker.
   */
  extractTo: string;
  /** Each package in its own folder below extractTo. Does nothing without one. */
  extractSubfolder: boolean;
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
  /** Days a finished download stays in the LIST. 0 keeps it forever. */
  keepFinishedDays: number;
  /** How many entries the history keeps. 0 keeps every one. */
  historyMax: number;

  crawl: boolean;
  watchDir: string;
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
 */
export type ServiceGroup = 'debrid' | 'hoster' | 'captchaSolver';

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
  name: string;
  url: string;
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
  settings: Record<string, unknown>;
  /** How many archive passwords are configured - the values themselves are never in this bundle. */
  archivePasswordCount: number;
  logLines: string[];
  logCapacity: number;
}

/**
 * What every operation on a whole selection answers with: the ids actually
 * touched, so the interface can say "12 removed" without re-fetching the list to
 * work out which twelve.
 */
export interface BulkResult {
  ids: string[];
  count: number;
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
  /** The resolver options page's two menus - ytdlp.Qualities()/SubtitleModes()
   *  (internal/resolver/ytdlp/options.go), served for the same reason as
   *  every list above it: a value this build cannot honour must never be
   *  selectable. */
  ytdlpQualities: string[];
  ytdlpSubtitleModes: string[];
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

  constructor(message: string, code?: string, params?: Record<string, string | number>) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.params = params;
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
    throw new ApiError(body || String(r.status));
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

// startTasks moves collected tasks into the download queue (empty = start all).
export const startTasks = (ids: string[], base = '/api') =>
  fetch(`${base}/tasks/start`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids }),
  });

// setPackage moves tasks into a package (empty name = ungrouped).
export const setPackage = (ids: string[], pkg: string, base = '/api') =>
  fetch(`${base}/tasks/package`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids, package: pkg }),
  });

// restartTasks re-runs finished/errored tasks (empty = all errored).
export const restartTasks = (ids: string[], base = '/api') =>
  fetch(`${base}/tasks/restart`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ids }),
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

/** settings.Settings.IdleAction (internal/settings/settings.go) on the wire. */
export interface IdleActionConfig {
  /** 'none' | 'pause', and whatever a later build adds - see fetchIdleActions. */
  action: string;
  delaySeconds: number;
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

// ---- resolver routing facts (internal/resolver, GET /api/resolvers/*) -----

/** One resolver's identity and priority - resolver.Info (internal/resolver/resolver.go). */
export interface ResolverInfo {
  id: string;
  prio: number;
}

/**
 * fetchResolverPriority is the deterministic order configured services are
 * tried in, highest priority first - resolver.Registry.AllInfo when host is
 * omitted, resolver.Registry.PriorityFor (narrowed to what actually matches
 * that host) when it is given.
 */
export async function fetchResolverPriority(host?: string): Promise<ResolverInfo[]> {
  const q = host ? `?host=${encodeURIComponent(host)}` : '';
  return (await json<ResolverInfo[]>(await fetch(`/api/resolvers/priority${q}`))) ?? [];
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
 * Mirrors ytdlp.Options (internal/resolver/ytdlp/options.go) field for
 * field. Every value is a plain string rather than a TS union, matching
 * every other server-sourced menu in this file (archiveDisposal,
 * collisionPolicy, resumeOnStart): the choices come from
 * ApiOptions.ytdlpQualities/ytdlpSubtitleModes, so a value this build adds
 * later still round-trips instead of failing to compile.
 *
 * Every field's zero value ('' / false) reproduces exactly what this
 * backend did before any of them existed - see the Go type's own doc
 * comment. An install that never opens the resolver options page downloads
 * exactly as it always has.
 */
export interface YtdlpOptions {
  /** 'best' | '2160p' | '1440p' | '1080p' | '720p' | '480p' | '360p' |
   *  'audioOnly' | 'custom' - see ApiOptions.ytdlpQualities. */
  quality: string;
  /** yt-dlp's own -f selector, used verbatim when quality is 'custom' and
   *  ignored otherwise. */
  customFormat: string;
  /** 'off' | 'file' | 'embed' - see ApiOptions.ytdlpSubtitleModes. */
  subtitles: string;
  /** yt-dlp's own --sub-langs value (e.g. "en,de"); empty defaults to "en"
   *  server-side whenever subtitles is not 'off'. */
  subtitleLangs: string;
  /** Also fetch auto-generated captions when no manual track exists. */
  subtitleAuto: boolean;
  /** A playlist URL fetches every entry instead of only the one link
   *  pointed at - off is what every install had before this existed. */
  playlist: boolean;
  /** yt-dlp's own -o template syntax; empty uses the built-in
   *  "%(title)s.%(ext)s". Server-sanitized against path traversal on save -
   *  see ytdlp.sanitizeTemplate's own doc comment. */
  outputTemplate: string;
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
}

/**
 * The three-way sync status one stored login can be in against JD -
 * hosterauth.LoginStatus. 'queued' and 'rejected' are deliberately distinct:
 * a login JD has not validated yet reads as "still checking", not as "wrong
 * password" - collapsing the two is how a user gives up on a login seconds
 * from working.
 */
export type HosterLoginStatus = 'queued' | 'active' | 'rejected';

/** One stored native hoster login and its status - hosterauth.LoginState. Never the password. */
export interface HosterLogin {
  host: string;
  username: string;
  status: HosterLoginStatus;
  detail?: string;
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

export const logout = () => fetch('/api/auth/logout', { method: 'POST' });

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

// fetchDiagnostics is called both to render the diagnostics page's live
// preview and, again, right before a download - the bundle is meant to
// reflect the moment it was pulled, not whatever the page happened to load
// with (log lines and the goroutine count move constantly, and both are the
// point of the bundle).
export async function fetchDiagnostics(): Promise<Diagnostics> {
  return json<Diagnostics>(await fetch('/api/diagnostics'));
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

export async function fetchInstances(): Promise<Instance[]> {
  return (await json<Instance[]>(await fetch('/api/instances'))) ?? [];
}

export async function addInstance(name: string, url: string): Promise<{ online: boolean }> {
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
