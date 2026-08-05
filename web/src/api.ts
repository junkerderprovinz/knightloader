export interface Task {
  id: string;
  url: string;
  name: string;
  package: string;
  resolver: string;
  size: number;
  loaded: number;
  speed: number;
  status: 'queued' | 'running' | 'paused' | 'extracting' | 'done' | 'error';
  error?: string;
  createdAt: string;
}

export interface Settings {
  maxConcurrent: number;
  maxPerHost: number;
  speedLimit: number; // bytes/s, 0 = unlimited
  extract: boolean;
  deleteArchive: boolean;
}

async function json<T>(r: Response): Promise<T> {
  return (await r.json()) as T;
}

export async function fetchTasks(): Promise<Task[]> {
  return (await json<Task[]>(await fetch('/api/tasks'))) ?? [];
}

export async function addLinks(links: string, pkg: string): Promise<Task[]> {
  const r = await fetch('/api/links', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ links, package: pkg }),
  });
  return (await json<Task[]>(r)) ?? [];
}

export const pause = (id: string) => fetch(`/api/tasks/${id}/pause`, { method: 'POST' });
export const resume = (id: string) => fetch(`/api/tasks/${id}/resume`, { method: 'POST' });
export const remove = (id: string) => fetch(`/api/tasks/${id}`, { method: 'DELETE' });

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

export async function fetchAccounts(): Promise<string[]> {
  return (await json<string[]>(await fetch('/api/accounts'))) ?? [];
}

export const saveAccount = (service: string, secret: string) =>
  fetch('/api/accounts', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ service, secret }),
  });

// connectWS opens the live stream and auto-reconnects. Returns a closer.
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
