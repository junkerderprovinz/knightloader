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
  autoStart: boolean;
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
  shape: 'round' | 'soft' | 'square';
  accent: string;
  rainbow: boolean;
  rainbowReactive: boolean;
  rainbowRotate: boolean;
  rainbowSeed: number;
  rainbowPalette: string[] | null;
}

export interface Account {
  id: string;
  label: string;
  configured: boolean;
  fromEnv: boolean;
  ok: boolean;
  detail: string;
  hosts: number;
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

export async function addLinks(links: string, pkg: string, base = '/api'): Promise<Task[]> {
  const r = await fetch(`${base}/links`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ links, package: pkg }),
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

export async function saveSettings(s: Settings): Promise<Settings> {
  const r = await fetch('/api/settings', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(s),
  });
  return json<Settings>(r);
}

export async function fetchAccounts(): Promise<Account[]> {
  return (await json<Account[]>(await fetch('/api/accounts'))) ?? [];
}

// testAccount asks the service whether the stored credential actually works.
export async function testAccount(service: string): Promise<Account> {
  return json<Account>(await fetch(`/api/accounts/${encodeURIComponent(service)}/test`, { method: 'POST' }));
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

export const saveAccount = (service: string, secret: string) =>
  fetch('/api/accounts', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ service, secret }),
  });

export async function fetchHealth(): Promise<{ status: string; version: string }> {
  return json(await fetch('/api/health'));
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

// connectWS opens the live task stream and auto-reconnects. Returns a closer.
export function connectWS(onMessage: (type: string, data: any) => void): () => void {
  let ws: WebSocket | null = null;
  let closed = false;
  const open = () => {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    ws = new WebSocket(`${proto}://${location.host}/api/ws`);
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
