import { useEffect, useState, useSyncExternalStore } from 'react';
import { type Task, apiBase, fetchTasks, connectWS } from './api';

/**
 * How long changes from the socket gather before the list applies them. The
 * server sends one message per task, so starting a few hundred links would
 * otherwise draw the list a few hundred times while its rows leave one by one.
 * A timer rather than an animation frame, which a hidden tab never runs, and
 * the tab's title counts progress.
 */
const GATHER_MS = 50;

type Change = { removed: false; task: Task } | { removed: true; id: string };

const NONE: Record<string, Task> = {};

// This instance's tasks, shared over one socket by every page and count that
// shows them, so a page that opens shows at once what is already known instead
// of waiting for a snapshot of its own, which with thousands of links takes
// seconds.
let tasks = NONE;
const listeners = new Set<() => void>();
let close: (() => void) | undefined;
let pending: Change[] = [];
let timer: ReturnType<typeof setTimeout> | undefined;

function publish(next: Record<string, Task>) {
  tasks = next;
  for (const l of listeners) l();
}

function flush() {
  timer = undefined;
  const batch = pending;
  pending = [];
  const n = { ...tasks };
  for (const c of batch) {
    if (c.removed) delete n[c.id];
    else n[c.task.id] = c.task;
  }
  publish(n);
}

function queue(c: Change) {
  pending.push(c);
  timer ??= setTimeout(flush, GATHER_MS);
}

function open() {
  // No initial GET: the socket's snapshot is the first data, and a slower GET
  // could overwrite newer updates. The snapshot is a direct send and arrives
  // without being in kinds.
  close = connectWS(
    (type, data) => {
      if (type === 'snapshot') {
        // It holds every change gathered so far.
        pending = [];
        clearTimeout(timer);
        timer = undefined;
        publish(Object.fromEntries((data ?? []).map((t: Task) => [t.id, t])));
      } else if (type === 'task') queue({ removed: false, task: data });
      else if (type === 'removed') queue({ removed: true, id: data.id });
    },
    ['task', 'removed'],
  );
}

function subscribeLocal(listener: () => void) {
  listeners.add(listener);
  if (!close) open();
  return () => {
    listeners.delete(listener);
    if (listeners.size > 0) return;
    close?.();
    close = undefined;
    clearTimeout(timer);
    timer = undefined;
    pending = [];
    tasks = NONE;
  };
}

const readLocal = () => tasks;
const subscribeNone = () => () => {};
const readNone = () => NONE;

// useTasks streams the task list for an instance ('' = this instance, live over
// the WebSocket; a named peer is polled every 2s).
export function useTasks(instance: string): Record<string, Task> {
  const local = useSyncExternalStore(instance ? subscribeNone : subscribeLocal, instance ? readNone : readLocal);
  const [peer, setPeer] = useState<Record<string, Task>>(NONE);
  useEffect(() => {
    setPeer(NONE);
    if (!instance) return;
    const base = apiBase(instance);
    const load = () => fetchTasks(base).then((l) => setPeer(Object.fromEntries((l ?? []).map((t) => [t.id, t]))));
    load();
    const iv = setInterval(load, 2000);
    return () => clearInterval(iv);
  }, [instance]);
  return instance ? peer : local;
}
