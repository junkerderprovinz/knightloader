import { isRelayConnection, type AuthState, type Instance, type QueueState, type ServerConnection, type Task } from './types';
import { relayClientFor } from './relayClient';
import { fromHex } from './sha256';
import type { InstanceAppearance } from '../theme/appearance';
import { relayIdentity } from '../storage/relayIdentity';

export class ApiError extends Error {
  constructor(
    message: string,
    public status: number
  ) {
    super(message);
  }
}

// Every call takes a connection (which instance + token to talk to) and a base
// path prefix. base defaults to '/api', the connection's own instance; a
// peer's routes proxy through the connected server at
// '/api/instances/{name}' instead (internal/api/routes_federation.go) -
// same host, same token, only the prefix changes. That mirrors the web UI's
// own lib/api.ts, so this app and the web client never drift on the shape of
// a "which instance is this for" call.
//
// This function is also the ONE place that knows a connection has a transport
// at all. Everything above it - every screen, every exported call below -
// works in terms of (connection, base, path), so a relay connection reaches
// exactly the same routes with exactly the same code, including the federation
// proxy prefix: a relay-reached instance's OWN peers stay browsable, because
// that is just another path the target resolves for itself.
export async function request<T>(conn: ServerConnection, base: string, path: string, init?: RequestInit): Promise<T> {
  const { status, body, statusText } = isRelayConnection(conn)
    ? await relayRequest(conn, base + path, init)
    : await httpRequest(conn, base + path, init);

  if (status < 200 || status >= 300) {
    throw new ApiError(body || statusText, status);
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

// A relay call is the same request, addressed to an instance id instead of a
// host. The token travels in the frame's own authorization field rather than a
// header, because the frame is all there is - see relay.ProxyRequest.
async function relayRequest(conn: ServerConnection, path: string, init?: RequestInit): Promise<RawResponse> {
  if (!isRelayConnection(conn)) throw new Error('relayRequest called with a direct connection');
  // A connection saved before frames were sealed has no frame key, and there
  // is nothing to fall back to: an unsealed frame is one every instance now
  // ignores, so the call would time out with no reason given. Said plainly
  // instead, once, at the only place that can tell.
  if (!conn.relayFrameKey) {
    throw new Error('relay: this connection predates encrypted frames - add it again with your phrase');
  }
  const client = relayClientFor({
    url: conn.relayUrl,
    key: conn.relayKey,
    frameKey: fromHex(conn.relayFrameKey),
    selfId: await relayIdentity(),
    selfName: 'KnightLoader app',
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
  return { status: r.status, body: r.body, statusText: `relay ${r.status}` };
}

// checkConnection is what the connect screen calls before saving anything:
// it proves the URL is reachable and the token is accepted, without
// requiring a password (a token stands on its own, see routes_tokens.go).
export async function checkConnection(conn: ServerConnection): Promise<AuthState> {
  return request<AuthState>(conn, '/api', '/auth');
}

export async function fetchTasks(conn: ServerConnection, base = '/api'): Promise<Task[]> {
  return request<Task[]>(conn, base, '/tasks');
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

export async function setTasksEnabled(conn: ServerConnection, ids: string[], enabled: boolean, base = '/api'): Promise<void> {
  await request(conn, base, '/tasks/enabled', {
    method: 'POST',
    body: JSON.stringify({ ids, enabled }),
  });
}

export async function deleteTasks(conn: ServerConnection, ids: string[], deleteFiles: boolean, base = '/api'): Promise<void> {
  await request(conn, base, '/tasks/delete', {
    method: 'POST',
    body: JSON.stringify({ ids, files: deleteFiles }),
  });
}

// --- Queue master switch ------------------------------------------------
//
// "Start/stop this instance" in this app's UI is this switch, the same one
// the web UI's quick controls flip - KnightLoader has no remote power-on for
// the server process itself (there's no relay, see routes_remote.go's own
// doc comment), only the queue it already runs can be halted or released.

export async function fetchQueue(conn: ServerConnection, base = '/api'): Promise<QueueState> {
  return request<QueueState>(conn, base, '/queue');
}

/**
 * Move collected links into the download queue.
 *
 * The collector is a staging area: a link that arrives from a container, a
 * right-click or the watch folder lands there with status "collected" and does
 * nothing until somebody says go. This is that "go", per package or for the
 * whole collector - the same route the web UI's own Start button calls.
 */
export async function startTasks(conn: ServerConnection, ids: string[], base = '/api'): Promise<StartResult> {
  const r = await request<StartResult | undefined>(conn, base, '/tasks/start', {
    method: 'POST',
    body: JSON.stringify({ ids }),
  });
  // An instance older than this answer replies 204 with no body, which request
  // turns into undefined. Read as "it started something and had nothing to
  // report", which is exactly what a 204 used to mean here.
  return r ?? { started: ids.length, skipped: 0, released: false, blocked: false };
}

/**
 * What a start actually did.
 *
 * It exists because "nothing happened" had three causes and the route answered
 * 204 to all of them: a halted queue, a link filter holding the named tasks, or
 * ids matching nothing. See App.StartTasks in internal/app/app_queue.go - the
 * shape is the server's, named the same on both sides so a field cannot mean
 * one thing there and another here.
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
 * The new order after a drag (jdp, 2026-08-31: "Das drag and drop für links und
 * linkordner fehlt").
 *
 * The same call the web interface's own drag-and-drop makes, against the same
 * route: "put one whole band of the wait queue in the exact order given, as a
 * drag would". Both surfaces therefore write the same shape and neither has a
 * private idea of what an order is - which matters here more than usual,
 * because a phone and a browser can be looking at the same queue at the same
 * moment.
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
 * The HARD stop, and the difference is the whole point (jdp, 2026-08-31: "wenn
 * man auf den stopp button drückt werden sie nicht gestoppt").
 *
 * POST /api/queue with `halted: true` stops the DISPATCHER: nothing new starts,
 * and whatever is already downloading runs to the end. That is deliberate on
 * the server (`SetHalted`'s own doc comment: "killing a transfer mid-file
 * throws away work the user did not ask to lose") and it is the wrong verb for
 * a button labelled stop, because the thing somebody is watching move keeps
 * moving. POST /api/queue/stop is `StopAll`: every transfer in flight is
 * stopped where it is AND the queue is halted behind them, which is what the
 * web interface's own stop button has always called.
 *
 * Relay-forwardable like every other queue route (`queue/` prefix, see
 * routes_relay.go), so this works from the phone over the relay too.
 */
export async function stopAll(conn: ServerConnection, base = '/api'): Promise<QueueState> {
  const res = await request<{ queue: QueueState }>(conn, base, '/queue/stop', {
    method: 'POST',
    body: JSON.stringify({}),
  });
  return res.queue;
}

// --- Federation: the peer instances the connected server itself knows -----
//
// Three calls lived here - list, add, remove - against the connected server's
// own base. They went with InstancesScreen, the only caller (see App.tsx for
// why that screen went). `addInstance` in particular took a name and an
// address by hand, which is the path the connection phrase replaced
// everywhere else in this app; leaving the call sitting here is how it finds
// its way back into a screen.


// --- Live task stream -------------------------------------------------
//
// Mirrors internal/api's /api/ws contract: on connect the server sends one
// {"type":"snapshot","data":Task[]} with the full current list, then
// incremental {"type":"task","data":Task} messages as things change. This
// client folds both into one onSnapshot(tasks) callback rather than
// exposing the wire protocol, since every screen just wants "the current
// list", not the delta mechanics.
//
// Only a DIRECTLY connected server's own queue has this. Two separate cases
// forward plain REST calls rather than a WebSocket upgrade, and each falls
// back to pollTasks below: the federation proxy (routes_federation.go), so a
// peer's tasks are never streamed, and the relay (internal/relay), which
// carries request/response frames and has no tunnel for a socket either.
//
// liveTasks picks the right one, so a screen can just ask for "the tasks,
// kept current" without knowing which transport it ended up with.
export type UnsubscribeFn = () => void;

/**
 * An unsubscribe that can also be asked to pull once, now.
 *
 * A plain function with a property, so every existing caller keeps working: it
 * is still a valid React effect cleanup and still assignable to `() => void`.
 * `refresh` is optional because the streaming path does not need one - a direct
 * connection is told about the change before the request that caused it has
 * even answered.
 */
export type LiveTasks = UnsubscribeFn & { refresh?: () => Promise<void> };

export function liveTasks(
  conn: ServerConnection,
  base: string,
  onSnapshot: (tasks: Task[]) => void,
  onError?: (err: unknown) => void
): LiveTasks {
  const streamable = !isRelayConnection(conn) && base === '/api';
  return streamable
    ? subscribeTasks(conn, onSnapshot, onError)
    : pollTasks(conn, base, onSnapshot, onError);
}

export function subscribeTasks(
  conn: ServerConnection,
  onSnapshot: (tasks: Task[]) => void,
  onError?: (err: unknown) => void
): UnsubscribeFn {
  if (isRelayConnection(conn)) throw new Error('subscribeTasks: a relay connection has no stream - use liveTasks');
  const wsUrl = conn.baseUrl.replace(/^http/, 'ws') + '/api/ws';
  let tasks = new Map<string, Task>();
  let closedByCaller = false;
  let socket: WebSocket | null = null;
  let retryDelayMs = 1000;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;

  const emit = () => {
    onSnapshot(Array.from(tasks.values()).sort((a, b) => a.position - b.position));
  };

  const connect = () => {
    // The server's guard (internal/api/api.go, bearerToken()) only ever reads
    // the Authorization header, no query-param fallback. Browsers' WebSocket
    // has no way to set one, but React Native's does - a non-standard third
    // constructor argument, not in the browser spec - which is why this only
    // works from the app, not from a plain web client hitting the same URL.
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
        } else if (msg.type === 'task' && msg.data && typeof msg.data === 'object') {
          const t = msg.data as Task;
          tasks.set(t.id, t);
          emit();
        } else if (msg.type === 'taskRemoved' && typeof msg.data === 'string') {
          tasks.delete(msg.data);
          emit();
        }
        // Other broadcast kinds (activity, activitySnapshot, ...) are ignored
        // here on purpose - this client only tracks the queue, not the
        // activity feed.
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
// WebSocket to attach to (see the doc comment above). Same callback shape,
// so a screen can point at either without caring which one it got.
export function pollTasks(
  conn: ServerConnection,
  base: string,
  onSnapshot: (tasks: Task[]) => void,
  onError?: (err: unknown) => void,
  intervalMs = 3000
): UnsubscribeFn {
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | null = null;
  // Which request is the newest. Two fetches can be in flight at once the
  // moment anything asks for an immediate refresh on top of the running cycle,
  // and over a relay the older one can land last - which would put the state
  // from BEFORE the action back on screen, a fraction of a second after the
  // action worked. A counter is the whole defence: an answer that is not the
  // newest is dropped rather than applied.
  let issued = 0;

  const tick = async () => {
    if (stopped) return;
    const mine = ++issued;
    try {
      const tasks = await fetchTasks(conn, base);
      if (!stopped && mine === issued) onSnapshot(tasks.slice().sort((a, b) => a.position - b.position));
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

/**
 * fetchAppearance reads the look this instance is configured with - the accent,
 * the shape and the rainbow state - so the app can show the same product as
 * that instance's own web UI rather than a second opinion about it.
 *
 * Taken from GET /api/appearance, which exists for this. It used to read the
 * whole of GET /api/settings and pick seven fields out - a fair trade against
 * inventing a route, right up until a phone joined the group over the relay
 * and that call became "hand a sibling every download path and connection you
 * have configured, so it can find out which shade of orange to paint a
 * button". The narrow route is what keeps /api/settings off the relay
 * allowlist entirely.
 *
 * Never throws. An instance too old to carry these fields, or one that cannot
 * be reached right now, means the app keeps GlimStone's own defaults, which is
 * exactly what it shows before any connection exists.
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
 * Write the rainbow palette back to the instance.
 *
 * The palette is the one part of the look that is deliberately NOT a local
 * choice: colours are handed out by POSITION, so a palette kept on the phone
 * would make the same card teal in a browser and pink here. Editing it
 * therefore means editing the instance's, which is why this posts rather than
 * storing anything (jdp, 2026-09-01: "wo sind die farbfelder für den
 * regenbogenmodus?", and before that "alle farbfelder lassen sich nicht
 * bearbeiten").
 *
 * `null` is the reset: it clears the stored list so the instance falls back to
 * GlimStone's own eight, which is the same shape the web UI's reset badge
 * sends.
 *
 * Answers with the instance's new look, so the caller can apply exactly what
 * was stored rather than what it hoped would be.
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
