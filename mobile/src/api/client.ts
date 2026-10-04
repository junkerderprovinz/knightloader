import {
  isRelayConnection,
  needsAddingAgain,
  type AuthState,
  type CaptchaAbortScope,
  type CaptchaChallenge,
  type CaptchaTestResult,
  type DirectConnection,
  type ExtractJob,
  type Instance,
  type QueueState,
  type ServerConnection,
  type Task,
  type TorrentFileView,
  type WatchList,
} from './types';
import { WATCHED, widgetPagePath, widgetPath } from './captcha';
import { relayClientFor } from './relayClient';
import { fromHex } from './sha256';
import type { InstanceAppearance } from '../theme/appearance';
import type { CardSwitches } from './taskState';
import { relayIdentity, resetRelayIdentity } from '../storage/relayIdentity';
import { removeGroupConnections } from '../storage/connections';
import { deviceName } from './deviceName';
import type { TranslationKey } from '../i18n/en';

export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
    /** Names the reason where the server sent one, so it can be translated. */
    public code?: string
  ) {
    super(message);
  }
}

// refusal reads a refused call. A refusal the interface can translate comes as
// {error, code}; the message is then the sentence, not the envelope.
function refusal(body: string, status: number): ApiError {
  try {
    const p = JSON.parse(body) as { error?: unknown; code?: unknown };
    if (p && typeof p.error === 'string') {
      return new ApiError(p.error, status, typeof p.code === 'string' ? p.code : undefined);
    }
  } catch {
    // Plain text.
  }
  return new ApiError(body, status);
}

// The refusals this app words itself. The rest show the server's sentence.
const REFUSALS: Partial<Record<string, TranslationKey>> = {
  federationOff: 'error.federationOff',
  addAgain: 'error.addAgain',
};

/** errorText is what a failed call shows: translated where the code is known. */
export function errorText(t: (key: TranslationKey) => string, e: unknown): string {
  const key = e instanceof ApiError && e.code ? REFUSALS[e.code] : undefined;
  if (key) return t(key);
  return e instanceof Error ? e.message : String(e);
}

// Every call takes a connection and a base path prefix. base is '/api' for the
// connection's own instance; a peer's routes proxy through the connected server
// at '/api/instances/{name}' (internal/api/routes_federation.go), same host and
// token, only the prefix changes. The web UI's lib/api.ts is shaped the same
// way, so the two clients cannot drift on it.
//
// This is also the only place that knows a connection has a transport at all.
// Everything above it works in terms of (connection, base, path), so a relay
// connection reaches the same routes with the same code, federation prefix
// included.
export async function request<T>(conn: ServerConnection, base: string, path: string, init?: RequestInit): Promise<T> {
  // Said plainly, at the one place that can tell, instead of a call that
  // times out or a transport error that names nothing.
  if (needsAddingAgain(conn)) {
    throw new ApiError('this connection has to be added again with the phrase', 0, 'addAgain');
  }
  const { status, body, statusText } = isRelayConnection(conn)
    ? await relayRequest(conn, base + path, init)
    : await httpRequest(conn, base + path, init);

  if (status < 200 || status >= 300) {
    throw refusal(body || statusText, status);
  }
  if (status === 204 || body === '') return undefined as T;
  return JSON.parse(body) as T;
}

interface RawResponse {
  status: number;
  body: string;
  statusText: string;
}

async function httpRequest(conn: ServerConnection, path: string, init?: RequestInit): Promise<RawResponse> {
  const base = isRelayConnection(conn) ? '' : conn.baseUrl;
  const res = await fetch(`${base}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${conn.token}`,
      ...(init?.headers ?? {}),
    },
  });
  return { status: res.status, body: await res.text().catch(() => ''), statusText: res.statusText };
}

let removedHandler: (() => void) | null = null;

/** onRemovedFromGroup registers what the app does once an instance says this
 *  phone was taken out of its group. */
export function onRemovedFromGroup(handler: () => void): void {
  removedHandler = handler;
}

// A relay call is the same request, addressed to an instance id instead of a
// host. The token travels in the frame's own authorization field rather than a
// header, because the frame is all there is - see relay.ProxyRequest.
async function relayRequest(conn: ServerConnection, path: string, init?: RequestInit): Promise<RawResponse> {
  if (!isRelayConnection(conn) || !conn.relayFrameKey) throw new Error('relayRequest called with an unusable connection');
  const client = relayClientFor({
    url: conn.relayUrl,
    key: conn.relayKey,
    frameKey: fromHex(conn.relayFrameKey),
    selfId: await relayIdentity(),
    selfName: deviceName(),
  });
  const body = typeof init?.body === 'string' ? init.body : undefined;
  const r = await client.proxy(
    conn.instanceId,
    init?.method ?? 'GET',
    path,
    body,
    conn.token ? `Bearer ${conn.token}` : undefined,
  );
  // A transport failure throws out of proxy() and never reaches here, so
  // anything with a status is genuinely the instance's own answer.
  if (r.status === 410 && r.body === 'removed') {
    // Every connection into the group goes, and a new relay id makes the
    // next scan of the phrase a new phone rather than the removed one.
    await removeGroupConnections(conn.relayUrl, conn.relayKey);
    await resetRelayIdentity();
    removedHandler?.();
  }
  return { status: r.status, body: r.body, statusText: `relay ${r.status}` };
}

// checkConnection is what the connect screen calls before saving anything: it
// proves the URL is reachable and the token is accepted, without requiring a
// password (a token stands on its own, see routes_tokens.go).
//
// This app has no sign-in, which is why neither a second factor nor a passkey
// reaches it. It never posts to /api/auth/login; every call carries a named API
// token in an Authorization header, and internal/api's `authenticated` accepts
// that token on its own, while the second factor sits in front of the password
// exchange this client never performs.
//
// Passkeys are refused with the reason rather than offered as a button that
// fails (GlimStone 1.15.0). WebAuthn does not exist in this runtime: React
// Native's navigator is `{product: 'ReactNative'}`, with no
// navigator.credentials and no PublicKeyCredential, so
// /api/auth/passkey/login/begin has nothing to hand its challenge to. There
// would also be no password for a passkey to replace, and app.json blocks
// USE_BIOMETRIC and USE_FINGERPRINT. The reason sits here rather than beside a
// control because there is no control, and this is the file somebody opens when
// they ask where the app logs in.
export async function checkConnection(conn: ServerConnection): Promise<AuthState> {
  return request<AuthState>(conn, '/api', '/auth');
}

export async function fetchTasks(conn: ServerConnection, base = '/api', signal?: AbortSignal): Promise<Task[]> {
  return request<Task[]>(conn, base, '/tasks', { signal });
}

/** The task list as the background watch compares it, or only the tag again
 *  while the list is as it was under `tag`. */
export async function fetchWatch(conn: ServerConnection, tag: string, signal?: AbortSignal): Promise<WatchList> {
  return request<WatchList>(conn, '/api', `/tasks/watch${tag ? `?tag=${encodeURIComponent(tag)}` : ''}`, { signal });
}

// addLinks stages one batch of links exactly like the paste box on the web
// UI (POST /api/links). One link per line, matching the server's own
// newline-separated convention.
export async function addLinks(conn: ServerConnection, links: string[], base = '/api'): Promise<Task[]> {
  return request<Task[]>(conn, base, '/links', {
    method: 'POST',
    body: JSON.stringify({ links: links.join('\n') }),
  });
}

// addSharedText stages what another app shared, as the web UI's quick-add page
// does: the server picks the links out of the text, and a title the sharing
// app sent names the package. The relay forwards /api/links but not the
// container and torrent uploads, which is why the share sheet offers no files.
export async function addSharedText(conn: ServerConnection, text: string, title?: string): Promise<Task[]> {
  return request<Task[]>(conn, '/api', '/links', {
    method: 'POST',
    body: JSON.stringify({ links: text, package: title || undefined }),
  });
}

export async function setTasksEnabled(conn: ServerConnection, ids: string[], enabled: boolean, base = '/api'): Promise<void> {
  await request(conn, base, '/tasks/enabled', {
    method: 'POST',
    body: JSON.stringify({ ids, enabled }),
  });
}

// Stops or starts the seeding of finished torrents. A stop holds across a
// restart of the instance, and a start seeds to the targets counted from now.
export async function setSeeding(conn: ServerConnection, ids: string[], seed: boolean, base = '/api'): Promise<void> {
  await request(conn, base, seed ? '/tasks/seeding/start' : '/tasks/seeding/stop', {
    method: 'POST',
    body: JSON.stringify({ ids }),
  });
}

// A torrent's files, asked for only while its row is open: the task carries
// just their count, since a torrent can list thousands.
export async function fetchTorrentFiles(conn: ServerConnection, id: string, base = '/api'): Promise<TorrentFileView[]> {
  return (await request<TorrentFileView[] | null>(conn, base, `/tasks/${encodeURIComponent(id)}/torrent-files`)) ?? [];
}

// playURL is a link to task id's file that a media player opens without the
// token, for twelve hours. While the download runs, the instance fetches the
// part being played first. Only a direct connection has an address a player
// can reach.
export async function playURL(conn: DirectConnection, id: string): Promise<string> {
  const { path } = await request<{ path: string }>(conn, '/api', `/tasks/${encodeURIComponent(id)}/play`, {
    method: 'POST',
  });
  return conn.baseUrl + path;
}

// selectTorrentFiles makes `paths` the files a torrent fetches. A running
// torrent carries on with them and keeps what it has.
export async function selectTorrentFiles(
  conn: ServerConnection,
  id: string,
  paths: string[],
  base = '/api',
): Promise<TorrentFileView[]> {
  return (
    (await request<TorrentFileView[] | null>(conn, base, `/tasks/${encodeURIComponent(id)}/torrent-files`, {
      method: 'POST',
      body: JSON.stringify({ selectedPaths: paths }),
    })) ?? []
  );
}

export async function deleteTasks(conn: ServerConnection, ids: string[], deleteFiles: boolean, base = '/api'): Promise<void> {
  await request(conn, base, '/tasks/delete', {
    method: 'POST',
    body: JSON.stringify({ ids, files: deleteFiles }),
  });
}

// "Start/stop this instance" is the queue switch the web UI's quick controls
// flip. There is no remote power-on for the server process itself (see
// routes_remote.go); only the queue it already runs can be halted or released.
export async function fetchQueue(conn: ServerConnection, base = '/api'): Promise<QueueState> {
  return request<QueueState>(conn, base, '/queue');
}

/**
 * Move collected links into the download queue.
 *
 * The collector is a staging area: a link that arrives from a container, a
 * right-click or the watch folder lands there with status "collected" and does
 * nothing until somebody says go. This is that go, per package or for the whole
 * collector, and the route the web UI's Start button calls.
 */
export async function startTasks(conn: ServerConnection, ids: string[], base = '/api'): Promise<StartResult> {
  const r = await request<StartResult | undefined>(conn, base, '/tasks/start', {
    method: 'POST',
    body: JSON.stringify({ ids }),
  });
  // An instance too old for this answer replies 204 with no body, which request
  // turns into undefined. Read as "it started something and had nothing to
  // report".
  return r ?? { started: ids.length, skipped: 0, released: false, blocked: false };
}

/**
 * What a start actually did.
 *
 * "Nothing happened" has three causes a bare 204 cannot tell apart: a halted
 * queue, the named tasks held back, or ids matching nothing. The
 * shape is the server's (App.StartTasks in internal/app/app_queue.go), named
 * the same on both sides.
 */
export interface StartResult {
  started: number;
  skipped: number;
  /** The queue was taken off a halt the user had set by hand. */
  released: boolean;
  /** A schedule window is holding the queue; the tasks are queued and waiting. */
  blocked: boolean;
}

/**
 * The new order after a drag: one whole band of the wait queue in the exact
 * order given. The same call the web interface's drag-and-drop makes against
 * the same route, so neither surface has a private idea of what an order is,
 * which matters because a phone and a browser can be looking at the same queue
 * at the same moment.
 */
export async function reorderTasks(conn: ServerConnection, ids: string[], base = '/api'): Promise<void> {
  await request(conn, base, '/tasks/reorder', { method: 'POST', body: JSON.stringify({ ids }) });
}

export async function setQueueHalted(conn: ServerConnection, halted: boolean, base = '/api'): Promise<QueueState> {
  return request<QueueState>(conn, base, '/queue', {
    method: 'POST',
    body: JSON.stringify({ halted }),
  });
}

/**
 * The hard stop.
 *
 * POST /api/queue with `halted: true` stops the dispatcher: nothing new starts,
 * and whatever is already downloading runs to the end, because killing a
 * transfer mid-file throws away work nobody asked to lose (see `SetHalted`).
 * That is the wrong verb for a button labelled stop, so this calls
 * POST /api/queue/stop, `StopAll`, which stops every transfer in flight and
 * halts the queue behind them, as the web interface's stop button does.
 *
 * Relay-forwardable like every other queue route (`queue/` prefix, see
 * routes_relay.go).
 */
export async function stopAll(conn: ServerConnection, base = '/api'): Promise<QueueState> {
  const res = await request<{ queue: QueueState }>(conn, base, '/queue/stop', {
    method: 'POST',
    body: JSON.stringify({}),
  });
  return res.queue;
}

// The live task stream mirrors internal/api's /api/ws contract: on connect the
// server sends one {"type":"snapshot","data":Task[]} with the full list, then
// {"type":"task","data":Task} messages as things change. Both fold into one
// onSnapshot(tasks) callback, since every screen wants the current list rather
// than the delta mechanics.
//
// Only a directly connected server's own queue has this. The federation proxy
// (routes_federation.go) and the relay (internal/relay) both forward plain REST
// calls with no socket to upgrade, so they fall back to pollTasks. liveTasks
// picks between them, so a screen can ask for the tasks kept current without
// knowing which transport it got.
export type UnsubscribeFn = () => void;

/**
 * An unsubscribe that can also be asked to pull once, now.
 *
 * A plain function with a property, so it is still a valid React effect cleanup
 * and still assignable to `() => void`. `refresh` is optional because a direct
 * connection is told about a change before the request that caused it answers.
 */
export type LiveTasks = UnsubscribeFn & { refresh?: () => Promise<void> };

export function liveTasks(
  conn: ServerConnection,
  base: string,
  onSnapshot: (tasks: Task[]) => void,
  onError?: (err: unknown) => void,
  onJobs?: (jobs: ExtractJob[]) => void
): LiveTasks {
  // A connection that has to be added again polls, so it fails with the
  // reason each cycle rather than retrying a socket nothing will open.
  const streamable = !isRelayConnection(conn) && base === '/api' && !needsAddingAgain(conn);
  return streamable
    ? subscribeTasks(conn, onSnapshot, onError, onJobs)
    : pollTasks(conn, base, onSnapshot, onError, onJobs);
}

/** Every unpacking the instance knows about, oldest first. */
export async function fetchExtractJobs(conn: ServerConnection, base = '/api'): Promise<ExtractJob[]> {
  return request<ExtractJob[]>(conn, base, '/extract');
}

export function subscribeTasks(
  conn: ServerConnection,
  onSnapshot: (tasks: Task[]) => void,
  onError?: (err: unknown) => void,
  onJobs?: (jobs: ExtractJob[]) => void
): UnsubscribeFn {
  if (isRelayConnection(conn)) throw new Error('subscribeTasks: a relay connection has no stream - use liveTasks');
  const wsUrl = conn.baseUrl.replace(/^http/, 'ws') + '/api/ws';
  let tasks = new Map<string, Task>();
  let jobs = new Map<string, ExtractJob>();
  let closedByCaller = false;
  let socket: WebSocket | null = null;
  let retryDelayMs = 1000;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;

  const emit = () => {
    onSnapshot(Array.from(tasks.values()).sort((a, b) => a.position - b.position));
  };
  // Map keeps insertion order, and the server lists jobs oldest first, which
  // is the order unpackingByTask needs.
  const emitJobs = () => onJobs?.(Array.from(jobs.values()));
  // A job that ended while the socket was down never sends its last message,
  // so every (re)connect reads the whole list once.
  const catchUp = () => {
    if (!onJobs) return;
    fetchExtractJobs(conn)
      .then((list) => {
        if (closedByCaller) return;
        jobs = new Map(list.map((j) => [j.id, j]));
        emitJobs();
      })
      .catch(() => {});
  };

  const connect = () => {
    // The server's guard (internal/api/api.go, bearerToken()) reads only the
    // Authorization header, with no query-param fallback. A browser's WebSocket
    // cannot set one; React Native's takes a third constructor argument that
    // is not in the browser spec, so this works from the app alone.
    type RNWebSocketCtor = new (url: string, protocols: string[], options: { headers: Record<string, string> }) => WebSocket;
    socket = new (WebSocket as unknown as RNWebSocketCtor)(wsUrl, [], {
      headers: { Authorization: `Bearer ${conn.token}` },
    });

    socket.onopen = () => {
      retryDelayMs = 1000;
    };

    socket.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data as string) as { type: string; data: unknown };
        if (msg.type === 'snapshot' && Array.isArray(msg.data)) {
          tasks = new Map((msg.data as Task[]).map((t) => [t.id, t]));
          emit();
          catchUp();
        } else if (msg.type === 'task' && msg.data && typeof msg.data === 'object') {
          const t = msg.data as Task;
          tasks.set(t.id, t);
          emit();
        } else if (msg.type === 'taskRemoved' && typeof msg.data === 'string') {
          tasks.delete(msg.data);
          emit();
        } else if (msg.type === 'extract' && onJobs && msg.data && typeof msg.data === 'object') {
          const j = msg.data as ExtractJob;
          jobs.set(j.id, j);
          emitJobs();
        }
        // Other broadcast kinds (activity, activitySnapshot and so on) are
        // ignored: this client tracks the queue, not the activity feed.
      } catch (err) {
        onError?.(err);
      }
    };

    socket.onerror = (err) => {
      onError?.(err);
    };

    socket.onclose = () => {
      if (closedByCaller) return;
      retryTimer = setTimeout(connect, retryDelayMs);
      retryDelayMs = Math.min(retryDelayMs * 2, 30_000);
    };
  };

  connect();

  return () => {
    closedByCaller = true;
    if (retryTimer) clearTimeout(retryTimer);
    socket?.close();
  };
}

// pollTasks is subscribeTasks' equivalent for a proxied peer, which has no
// WebSocket to attach to. Same callback shape, so a screen can point at either.
export function pollTasks(
  conn: ServerConnection,
  base: string,
  onSnapshot: (tasks: Task[]) => void,
  onError?: (err: unknown) => void,
  onJobs?: (jobs: ExtractJob[]) => void,
  intervalMs = 3000
): UnsubscribeFn {
  // The unpackings are read only while an archive waits for its turn or is
  // being unpacked, and once more after that, so a job that ended leaves its
  // last state on the rows. An idle list costs one request per cycle.
  let following = false;
  const read = async () => {
    const tasks = (await fetchTasks(conn, base)).slice().sort((a, b) => a.position - b.position);
    const unpacking = tasks.some((t) => t.status === 'extracting');
    // An instance too old to forward the route refuses it, and the rows keep
    // their download bars.
    const jobs =
      onJobs && (unpacking || following) ? await fetchExtractJobs(conn, base).catch(() => undefined) : undefined;
    following = unpacking;
    return { tasks, jobs };
  };
  return poll(
    read,
    ({ tasks, jobs }) => {
      onSnapshot(tasks);
      if (jobs) onJobs?.(jobs);
    },
    onError,
    intervalMs
  );
}

/** A running poll: calling it stops it, and `refresh` pulls once, now. */
export type Polling = UnsubscribeFn & { refresh: () => Promise<void> };

function poll<T>(
  get: () => Promise<T>,
  onValue: (value: T) => void,
  onError: ((err: unknown) => void) | undefined,
  intervalMs: number
): Polling {
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | null = null;
  // Which request is the newest. An immediate refresh on top of the running
  // cycle puts two fetches in flight, and over a relay the older one can land
  // last, putting the state from before the action back on screen. An answer
  // that is not the newest is dropped.
  let issued = 0;

  const tick = async () => {
    if (stopped) return;
    const mine = ++issued;
    try {
      const value = await get();
      if (!stopped && mine === issued) onValue(value);
    } catch (err) {
      if (!stopped && mine === issued) onError?.(err);
    } finally {
      if (!stopped && mine === issued) timer = setTimeout(tick, intervalMs);
    }
  };

  tick();

  const stop = () => {
    stopped = true;
    if (timer) clearTimeout(timer);
  };
  // Pull now, and restart the cycle from now rather than letting the pending
  // timer fire straight after. Handed out so a screen that has just changed
  // something on the server does not have to sit out the interval to see it.
  stop.refresh = async () => {
    if (stopped) return;
    if (timer) clearTimeout(timer);
    await tick();
  };
  return stop;
}

// The captcha calls the web UI's CaptchaModal makes, on the same routes
// (internal/api/routes_captcha.go). Over the relay they reach an instance that
// forwards them (relayCaptchaRoute); an older one refuses them with a 403, see
// captchaErrorText.

/** Every captcha waiting on the connected instance, read from its cache. The
 *  read counts as watching the kinds this phone can answer, so with "only when
 *  nobody is watching" on, the paid solvers wait for it on those. */
export async function fetchCaptchas(conn: ServerConnection): Promise<CaptchaChallenge[]> {
  const watch = WATCHED.join(',');
  return (await request<CaptchaChallenge[] | null>(conn, '/api', `/captcha?watch=${watch}`)) ?? [];
}

/** The same list read without watching, for the background watch: a phone in
 *  somebody's pocket must not hold the paid solvers back. */
export async function fetchCaptchasUnwatched(conn: ServerConnection, signal?: AbortSignal): Promise<CaptchaChallenge[]> {
  return (await request<CaptchaChallenge[] | null>(conn, '/api', '/captcha?watch=0', { signal })) ?? [];
}

/** Has the instance ask JD now instead of at its next check. */
export async function refreshCaptchas(conn: ServerConnection): Promise<CaptchaChallenge[]> {
  return (await request<CaptchaChallenge[] | null>(conn, '/api', '/captcha/refresh', { method: 'POST', body: '{}' })) ?? [];
}

/** stillValid is JD's verdict on whether the answer arrived in time; trust it
 *  over the countdown on screen. */
export async function answerCaptcha(
  conn: ServerConnection,
  id: string,
  text: string,
): Promise<{ stillValid: boolean; test?: CaptchaTestResult }> {
  return request<{ stillValid: boolean; test?: CaptchaTestResult }>(conn, '/api', `/captcha/${encodeURIComponent(id)}/answer`, {
    method: 'POST',
    body: JSON.stringify({ text }),
  });
}

/** Gives up on one challenge. JD keeps the blacklist for the two wider scopes. */
export async function skipCaptcha(conn: ServerConnection, id: string, scope: CaptchaAbortScope): Promise<void> {
  await request(conn, '/api', `/captcha/${encodeURIComponent(id)}/skip`, {
    method: 'POST',
    body: JSON.stringify({ scope }),
  });
}

/** Tells the instance this phone could not load widget challenge `id`, so its
 *  reads of the list stop holding the paid solvers back for it. An instance
 *  from before the phone's path refuses it and records nothing. */
export async function reportCaptchaUnanswerable(conn: ServerConnection, id: string): Promise<void> {
  await request(conn, '/api', `/captcha/${encodeURIComponent(id)}/unanswerable/phone`, { method: 'POST', body: '{}' });
}

/** Takes that back once Refresh has loaded the widget after all. A solver
 *  already at work on it carries on. */
export async function withdrawCaptchaUnanswerable(conn: ServerConnection, id: string): Promise<void> {
  await request(conn, '/api', `/captcha/${encodeURIComponent(id)}/unanswerable/phone`, { method: 'DELETE' });
}

export function pollCaptchas(
  conn: ServerConnection,
  onList: (list: CaptchaChallenge[]) => void,
  onError?: (err: unknown) => void,
  intervalMs = 5000
): Polling {
  return poll(() => fetchCaptchas(conn), onList, onError, intervalMs);
}

/** The widget page for challenge `id` and the hoster's page address, which a
 *  WebView loads it under so the vendor sees the hoster's origin. */
export async function fetchCaptchaWidgetPage(
  conn: ServerConnection,
  id: string,
  lang: string
): Promise<{ html: string; baseUrl: string }> {
  return request<{ html: string; baseUrl: string }>(conn, '/api', widgetPagePath(id, lang));
}

/**
 * What a WebView loads for a widget challenge from an instance that predates
 * fetchCaptchaWidgetPage: the web UI's widget page, from the instance's own
 * address, and the token it asks for.
 */
export function captchaWidgetSource(
  conn: DirectConnection,
  ch: CaptchaChallenge,
  lang: string
): { uri: string; headers: Record<string, string> } {
  return {
    uri: `${conn.baseUrl}/api${widgetPath(ch, lang)}`,
    headers: conn.token ? { Authorization: `Bearer ${conn.token}` } : {},
  };
}

/** errorText for a captcha call. The relay refuses a route the instance does
 *  not forward with a bare 403, which on these routes means it predates them. */
export function captchaErrorText(t: (key: TranslationKey) => string, conn: ServerConnection, e: unknown): string {
  if (isRelayConnection(conn) && e instanceof ApiError && e.status === 403) return t('captcha.relayRefused');
  return errorText(t, e);
}

/**
 * fetchAppearance reads the accent, the shape and the rainbow state an instance
 * is configured with, so the app shows the same product as that instance's own
 * web UI.
 *
 * It reads GET /api/appearance rather than picking the fields out of
 * /api/settings, which would hand a sibling every download path and connection
 * the instance has configured. That is what keeps /api/settings off the relay
 * allowlist.
 *
 * Never throws. An instance too old for these fields, or one out of reach,
 * leaves the app on GlimStone's defaults, which is what it shows before any
 * connection exists.
 */
export async function fetchAppearance(conn: ServerConnection): Promise<InstanceAppearance | undefined> {
  try {
    const s = await request<Record<string, unknown>>(conn, '/api', '/appearance');
    return {
      shape: typeof s.shape === 'string' ? s.shape : undefined,
      accent: typeof s.accent === 'string' ? s.accent : undefined,
      rainbow: !!s.rainbow,
      rainbowReactive: !!s.rainbowReactive,
      rainbowRotate: !!s.rainbowRotate,
      rainbowSeed: typeof s.rainbowSeed === 'number' ? s.rainbowSeed : 0,
      rainbowPalette: Array.isArray(s.rainbowPalette) ? (s.rainbowPalette as string[]) : undefined,
    };
  } catch {
    return undefined;
  }
}

/**
 * fetchListCards reads whether the instance splits finished packages and
 * torrents out of its download list, the switches the web's Downloads page
 * follows too. An instance too old to answer, or out of reach, has both on,
 * which is their default.
 */
export async function fetchListCards(conn: ServerConnection): Promise<CardSwitches> {
  try {
    const s = await request<Record<string, unknown>>(conn, '/api', '/appearance');
    return { finished: s.finishedCard !== false, torrents: s.torrentCard !== false };
  } catch {
    return { finished: true, torrents: true };
  }
}

/**
 * Write the rainbow palette back to the instance.
 *
 * The palette is the one part of the look that is not a local choice: colours
 * are handed out by position, so a palette kept on the phone would make the
 * same card teal in a browser and pink here. Editing it means editing the
 * instance's, so this posts rather than storing anything.
 *
 * `null` is the reset: it clears the stored list so the instance falls back to
 * GlimStone's own eight, the same shape the web UI's reset badge sends.
 *
 * Answers with the instance's new look, so the caller applies what was stored
 * rather than what it hoped would be.
 */
export async function setRainbowPalette(
  conn: ServerConnection,
  palette: string[] | null,
): Promise<InstanceAppearance> {
  const s = await request<Record<string, unknown>>(conn, '/api', '/appearance', {
    method: 'POST',
    body: JSON.stringify({ rainbowPalette: palette }),
  });
  return {
    shape: typeof s.shape === 'string' ? s.shape : undefined,
    accent: typeof s.accent === 'string' ? s.accent : undefined,
    rainbow: !!s.rainbow,
    rainbowReactive: !!s.rainbowReactive,
    rainbowRotate: !!s.rainbowRotate,
    rainbowSeed: typeof s.rainbowSeed === 'number' ? s.rainbowSeed : 0,
    rainbowPalette: Array.isArray(s.rainbowPalette) ? (s.rainbowPalette as string[]) : undefined,
  };
}
