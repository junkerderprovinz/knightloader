import type {
  CaptchaAbortScope,
  CaptchaChallenge,
  CaptchaTestResult,
  ExtractJob,
  Instance,
  QueueState,
  ServerConnection,
  Task,
  TorrentFileView,
  WatchList,
} from './types';
import { WATCHED, widgetPagePath } from './captcha';
import { relayClientFor, type RelayClient } from './relayClient';
import { fromHex } from './sha256';
import type { InstanceAppearance } from '../theme/appearance';
import type { CardSwitches } from './taskState';
import { memberOptions, resetRelayIdentity } from '../storage/relayIdentity';
import { removeGroupConnections } from '../storage/connections';
import { deviceName } from './deviceName';
import type { TranslationKey } from '../i18n/en';
import { ApiError, errorText, refusedCall } from './refusal';

export { ApiError, errorText } from './refusal';

// Every call takes a connection and a base path prefix. base is '/api' for the
// connection's own instance; a peer's routes proxy through it at
// '/api/instances/{name}' (internal/api/routes_federation.go), only the prefix
// changes. The web UI's lib/api.ts is shaped the same way, so the two clients
// cannot drift on it.
//
// This app has no sign-in, which is why neither a second factor nor a passkey
// reaches it: the phone's member key signs every call, and the instance it
// reaches signs the answer, which relayClient.ts checks.
export async function request<T>(conn: ServerConnection, base: string, path: string, init?: RequestInit): Promise<T> {
  // Said plainly, at the one place that can tell, instead of a call that
  // times out or a transport error that names nothing.
  if (!conn.relayFrameKey) {
    throw new ApiError('this connection has to be added again with the phrase', 0, 'addAgain');
  }
  const { status, body } = await relayRequest(conn, conn.relayFrameKey, base + path, init);
  if (status < 200 || status >= 300) throw refusedCall(status, body || `relay ${status}`);
  if (status === 204 || body === '') return undefined as T;
  return JSON.parse(body) as T;
}

let removedHandler: (() => void) | null = null;

/** onRemovedFromGroup registers what the app does once an instance says this
 *  phone was taken out of its group. */
export function onRemovedFromGroup(handler: () => void): void {
  removedHandler = handler;
}

/** relayClientOf is the shared relay client of conn's group. */
export async function relayClientOf(conn: ServerConnection, frameKey: string): Promise<RelayClient> {
  return relayClientFor({
    url: conn.relayUrl,
    key: conn.relayKey,
    frameKey: fromHex(frameKey),
    ...(await memberOptions()),
    selfName: deviceName(),
  });
}

// A relay call is the same request, addressed to an instance id instead of a
// host.
async function relayRequest(
  conn: ServerConnection,
  frameKey: string,
  path: string,
  init?: RequestInit,
): Promise<{ status: number; body: string }> {
  const client = await relayClientOf(conn, frameKey);
  const body = typeof init?.body === 'string' ? init.body : undefined;
  const r = await client.proxy(conn.instanceId, init?.method ?? 'GET', path, body);
  // A transport failure or an answer the instance did not sign throws out of
  // proxy() and never reaches here, so anything with a status is the
  // instance's own answer.
  if (r.status === 410 && r.body === 'removed') {
    // Every connection into the group goes, and a new relay id makes the
    // next scan of the phrase a new phone rather than the removed one.
    await removeGroupConnections(conn.relayUrl, conn.relayKey);
    await resetRelayIdentity();
    removedHandler?.();
  }
  return { status: r.status, body: r.body };
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
// container and torrent uploads, which is why the share sheet offers text
// files and no others.
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
 * fetchSpeedHistory reads the instance's record of the last two minutes, one
 * sample a second, the one the web interface seeds its speed curves from. An
 * older server sends no upload, which reads as none sent.
 */
export async function fetchSpeedHistory(conn: ServerConnection): Promise<{ down: number[]; up: number[] }> {
  const h = await request<{ recent?: unknown; recentUp?: unknown }>(conn, '/api', '/stats/speed');
  const down = Array.isArray(h.recent) ? (h.recent as number[]) : [];
  const up = Array.isArray(h.recentUp) && h.recentUp.length === down.length ? (h.recentUp as number[]) : down.map(() => 0);
  return { down, up };
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
 * Reachable over the relay like every other queue route (groupRoutes in
 * internal/api/group_routes.go).
 */
export async function stopAll(conn: ServerConnection, base = '/api'): Promise<QueueState> {
  const res = await request<{ queue: QueueState }>(conn, base, '/queue/stop', {
    method: 'POST',
    body: JSON.stringify({}),
  });
  return res.queue;
}

export type UnsubscribeFn = () => void;

/** Every unpacking the instance knows about, oldest first. */
export async function fetchExtractJobs(conn: ServerConnection, base = '/api'): Promise<ExtractJob[]> {
  return request<ExtractJob[]>(conn, base, '/extract');
}

// pollTasks follows the task list of an instance or a peer behind it. The relay
// forwards plain calls and has no socket to attach to, so the list is read
// again every few seconds.
export function pollTasks(
  conn: ServerConnection,
  base: string,
  onSnapshot: (tasks: Task[]) => void,
  onError?: (err: unknown) => void,
  onJobs?: (jobs: ExtractJob[]) => void,
  intervalMs = 3000
): Polling {
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
// lets a group member call them (groupRoutes); an older one refuses them with
// a 403, see captchaErrorText.

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

/** What the instance downloads at, in bytes a second, read off the queue's
 *  figures since the watch list leaves progress out. */
export async function fetchSpeed(conn: ServerConnection, signal?: AbortSignal): Promise<number> {
  return (await request<{ speed?: number }>(conn, '/api', '/queue/counters', { signal })).speed ?? 0;
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

/** errorText for a captcha call. The relay refuses a route the instance does
 *  not forward with a bare 403, which on these routes means it predates them. */
export function captchaErrorText(t: (key: TranslationKey) => string, e: unknown): string {
  if (e instanceof ApiError && e.status === 403) return t('captcha.relayRefused');
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
