import { useEffect, useState } from 'react';
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

// useTasks streams the task list for an instance ('' = this instance, live over
// the WebSocket; a named peer is polled every 2s).
export function useTasks(instance: string): Record<string, Task> {
  const [tasks, setTasks] = useState<Record<string, Task>>({});
  useEffect(() => {
    const base = apiBase(instance);
    setTasks({});
    if (instance) {
      const load = () =>
        fetchTasks(base).then((l) => setTasks(Object.fromEntries((l ?? []).map((t) => [t.id, t]))));
      load();
      const iv = setInterval(load, 2000);
      return () => clearInterval(iv);
    }
    let pending: Change[] = [];
    let timer: ReturnType<typeof setTimeout> | undefined;
    const flush = () => {
      timer = undefined;
      const batch = pending;
      pending = [];
      setTasks((p) => {
        const n = { ...p };
        for (const c of batch) {
          if (c.removed) delete n[c.id];
          else n[c.task.id] = c.task;
        }
        return n;
      });
    };
    const queue = (c: Change) => {
      pending.push(c);
      timer ??= setTimeout(flush, GATHER_MS);
    };
    // No initial GET: the socket's snapshot is the first data, and a slower
    // GET could overwrite newer updates. The snapshot is a direct send and
    // arrives without being in kinds.
    const close = connectWS(
      (type, data) => {
        if (type === 'snapshot') {
          // It holds every change gathered so far.
          pending = [];
          clearTimeout(timer);
          timer = undefined;
          setTasks(Object.fromEntries((data ?? []).map((t: Task) => [t.id, t])));
        } else if (type === 'task') queue({ removed: false, task: data });
        else if (type === 'removed') queue({ removed: true, id: data.id });
      },
      ['task', 'removed'],
    );
    return () => {
      clearTimeout(timer);
      close();
    };
  }, [instance]);
  return tasks;
}
