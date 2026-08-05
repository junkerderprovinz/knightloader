import { useEffect, useState } from 'react';
import { type Task, apiBase, fetchTasks, connectWS } from './api';

// useTasks streams the task list for an instance ('' = this instance, live over
// the WebSocket; a named peer is polled every 2s).
export function useTasks(instance: string): Record<string, Task> {
  const [tasks, setTasks] = useState<Record<string, Task>>({});
  useEffect(() => {
    const base = apiBase(instance);
    setTasks({});
    const load = () =>
      fetchTasks(base).then((l) => setTasks(Object.fromEntries((l ?? []).map((t) => [t.id, t]))));
    load();
    if (instance) {
      const iv = setInterval(load, 2000);
      return () => clearInterval(iv);
    }
    return connectWS((type, data) => {
      if (type === 'snapshot') setTasks(Object.fromEntries((data ?? []).map((t: Task) => [t.id, t])));
      else if (type === 'task') setTasks((p) => ({ ...p, [data.id]: data }));
      else if (type === 'removed')
        setTasks((p) => {
          const n = { ...p };
          delete n[data.id];
          return n;
        });
    });
  }, [instance]);
  return tasks;
}
