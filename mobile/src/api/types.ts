// Mirrors internal/core/task.go's Task struct on the server. Keep field
// names and types in sync with that file, not the other way around, since
// the server is the source of truth for this shape.
export type TaskStatus =
  | 'queued'
  | 'running'
  | 'paused'
  | 'finished'
  | 'failed'
  | 'extracting'
  | string; // the server's Status enum has more values than are worth hard-coding here

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
  online?: string;
  retries?: number;
  priority: number;
  position: number;
  checksum?: string;
}

export interface AuthState {
  enabled: boolean;
  authenticated: boolean;
}

export interface ServerConnection {
  id: string; // stable local id, so a rename doesn't orphan the "last active" pointer
  baseUrl: string; // e.g. "https://192.168.10.10:1234", no trailing slash
  token: string; // the API token secret, sent as "Authorization: Bearer <token>"
  name: string; // whatever the user called this connection, e.g. "Home KL"
}

// Mirrors internal/federation.Instance - a peer the CONNECTED server itself
// knows about. There is no separate token for a peer: /api/instances/{name}/*
// on the connected server proxies task/link/queue requests to it using that
// server's own credentials, so the app never needs the peer's token, only
// its registered name. This app never has to speak the relay's own wire
// protocol to reach a relay-visible peer - it is only ever connected to ONE
// server directly (ServerConnection above), and that server's own
// /api/instances proxy already carries relay peers exactly like stored
// ones, same as the web UI's Instances/Dashboard pages.
export interface Instance {
  // The address every proxied call is built from - always a relay peer's
  // InstanceID, never the name it announced, so it never changes on its own
  // when something else about the connected server's peer list changes
  // (federation.Manager.reachable's own doc comment has the full
  // reasoning). Never render this for a relay peer; render displayName.
  name: string;
  url: string;
  // What a relay peer calls itself, present only when it differs from
  // `name`. Purely a label - nothing addresses a peer by it. Fall back to
  // `name` when absent.
  displayName?: string;
  // Set only for a peer reached through the relay right now; a stored peer
  // never carries one. `url` is empty for one of these, which is also why
  // InstancesScreen shows a "connected via relay" line instead.
  relayId?: string;
}

// Mirrors internal/app.QueueState (app_queue.go) - the master switch for one
// instance's queue, halted or not, and how many transfers are in flight.
export interface QueueState {
  halted: boolean;
  stopMark?: string;
  running: number;
}
