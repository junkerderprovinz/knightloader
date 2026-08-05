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
export type Availability = '' | 'online' | 'offline';

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

export interface AuthState {
  enabled: boolean;
  authenticated: boolean;
}

export interface Instance {
  name: string;
  url: string;
}

async function json<T>(r: Response): Promise<T> {
  return (await r.json()) as T;
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
