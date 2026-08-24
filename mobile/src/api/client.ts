import type { AuthState, Instance, QueueState, ServerConnection, Task } from './types';

export class ApiError extends Error {
  constructor(
    message: string,
    public status: number
  ) {
    super(message);
  }
}

// Every call takes a connection (which host + token to talk to) and a base
// path prefix. base defaults to '/api', the connection's own instance; a
// peer's routes proxy through the connected server at
// '/api/instances/{name}' instead (internal/api/routes_federation.go) -
// same host, same token, only the prefix changes. That mirrors the web UI's
// own lib/api.ts, so this app and the web client never drift on the shape of
// a "which instance is this for" call.
async function request<T>(conn: ServerConnection, base: string, path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${conn.baseUrl}${base}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${conn.token}`,
      ...(init?.headers ?? {}),
    },
  });
  if (!res.ok) {
    const body = await res.text().catch(() => '');
    throw new ApiError(body || res.statusText, res.status);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
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

export async function setQueueHalted(conn: ServerConnection, halted: boolean, base = '/api'): Promise<QueueState> {
  return request<QueueState>(conn, base, '/queue', {
    method: 'POST',
    body: JSON.stringify({ halted }),
  });
}

// --- Federation: the peer instances the connected server itself knows -----
//
// These always run against the connected server's OWN base ('/api'), never
// a peer's - a peer's peers are not this app's concern, same as the web
// UI's Instances.tsx only ever calls these against its own origin.

export async function fetchInstances(conn: ServerConnection): Promise<Instance[]> {
  return request<Instance[]>(conn, '/api', '/instances');
}

export async function addInstance(conn: ServerConnection, name: string, url: string): Promise<{ name: string; url: string; online: boolean }> {
  return request(conn, '/api', '/instances', {
    method: 'POST',
    body: JSON.stringify({ name, url }),
  });
}

export async function removeInstance(conn: ServerConnection, name: string): Promise<void> {
  await request(conn, '/api', `/instances/${encodeURIComponent(name)}`, { method: 'DELETE' });
}

// redeemPairingCode is the one-scan way to add a peer: the code (pasted, or
// read off the peer's own Access-tab QR) already carries that peer's name,
// address and a one-time token, so this single call registers it AND tells
// it about the connected server back - see routes_pairing.go's own doc
// comment on why it completes the other side before adding it locally.
export async function redeemPairingCode(conn: ServerConnection, code: string): Promise<{ name: string; url: string; online: boolean }> {
  return request(conn, '/api', '/instances/pairing-code/redeem', {
    method: 'POST',
    body: JSON.stringify({ code }),
  });
}

// --- Live task stream -------------------------------------------------
//
// Mirrors internal/api's /api/ws contract: on connect the server sends one
// {"type":"snapshot","data":Task[]} with the full current list, then
// incremental {"type":"task","data":Task} messages as things change. This
// client folds both into one onSnapshot(tasks) callback rather than
// exposing the wire protocol, since every screen just wants "the current
// list", not the delta mechanics.
//
// Only the connected server's own queue has this: the federation proxy
// (routes_federation.go) forwards plain REST calls, not a WebSocket
// upgrade, so a peer's tasks are never streamed here - see fetchTasks +
// pollTasks below for how a peer's screen stays live instead.
export type UnsubscribeFn = () => void;

export function subscribeTasks(
  conn: ServerConnection,
  onSnapshot: (tasks: Task[]) => void,
  onError?: (err: unknown) => void
): UnsubscribeFn {
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

  const tick = async () => {
    if (stopped) return;
    try {
      const tasks = await fetchTasks(conn, base);
      if (!stopped) onSnapshot(tasks.slice().sort((a, b) => a.position - b.position));
    } catch (err) {
      if (!stopped) onError?.(err);
    } finally {
      if (!stopped) timer = setTimeout(tick, intervalMs);
    }
  };

  tick();

  return () => {
    stopped = true;
    if (timer) clearTimeout(timer);
  };
}
