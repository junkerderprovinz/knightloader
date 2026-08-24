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
  baseUrl: string; // e.g. "https://192.168.10.10:1234", no trailing slash
  token: string; // the API token secret, sent as "Authorization: Bearer <token>"
  name: string; // whatever the user called this connection, e.g. "Home KL"
}
