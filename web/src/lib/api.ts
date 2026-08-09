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
  /** Connections this one download opens; 0 means "whatever the resolver says". */
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
  speedLimit: number; // bytes/s, 0 = unlimited
  extract: boolean;
  deleteArchive: boolean;
  autoStart: boolean;
  downloadDir: string;
  subfolderByPackage: boolean;
  archivePasswords: string[];
  maxRetries: number;
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

async function json<T>(r: Response): Promise<T> {
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

// setPriority lifts or drops tasks in the wait queue (-2..2, higher runs first).
export const setPriority = (ids: string[], priority: number, base = '/api') =>
  post(`${base}/tasks/priority`, { ids, priority });

// moveTasks reorders the queue by hand.
export const moveTasks = (ids: string[], where: 'top' | 'bottom', base = '/api') =>
  post(`${base}/tasks/move`, { ids, where });

// setTaskOptions applies per-task overrides; omitted fields stay as they are.
export const setTaskOptions = (
  ids: string[],
  opts: { dir?: string; password?: string },
  base = '/api',
) => post(`${base}/tasks/options`, { ids, ...opts });

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
