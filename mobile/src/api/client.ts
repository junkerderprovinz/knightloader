import type { AuthState, ServerConnection, Task } from './types';

export class ApiError extends Error {
  constructor(
    message: string,
    public status: number
  ) {
    super(message);
  }
}

async function request<T>(conn: ServerConnection, path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${conn.baseUrl}${path}`, {
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
  return request<AuthState>(conn, '/api/auth');
}

// addLinks stages one batch of links exactly like the paste box on the web
// UI (POST /api/links). One link per line, matching the server's own
// newline-separated convention.
export async function addLinks(conn: ServerConnection, links: string[]): Promise<Task[]> {
  return request<Task[]>(conn, '/api/links', {
    method: 'POST',
    body: JSON.stringify({ links: links.join('\n') }),
  });
}

export async function setTasksEnabled(conn: ServerConnection, ids: string[], enabled: boolean): Promise<void> {
  await request(conn, '/api/tasks/enabled', {
    method: 'POST',
    body: JSON.stringify({ ids, enabled }),
  });
}

export async function deleteTasks(conn: ServerConnection, ids: string[], deleteFiles: boolean): Promise<void> {
  await request(conn, '/api/tasks/delete', {
    method: 'POST',
    body: JSON.stringify({ ids, files: deleteFiles }),
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
