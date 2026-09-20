import { useEffect, useState } from 'react';
import { type Task, apiBase, fetchTasks, connectWS } from './api';

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
    // No initial GET: the socket's snapshot is the first data, and a slower
    // GET could overwrite newer updates. The snapshot is a direct send and
    // arrives without being in kinds.
    return connectWS(
      (type, data) => {
        if (type === 'snapshot') setTasks(Object.fromEntries((data ?? []).map((t: Task) => [t.id, t])));
        else if (type === 'task') setTasks((p) => ({ ...p, [data.id]: data }));
        else if (type === 'removed')
          setTasks((p) => {
            const n = { ...p };
            delete n[data.id];
            return n;
          });
      },
      ['task', 'removed'],
    );
  }, [instance]);
  return tasks;
}
