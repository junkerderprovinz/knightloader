import { useEffect, useRef } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { Sidebar } from '../components/Sidebar';
import { connectWS, type Task } from '../lib/api';
import { useToast } from '../lib/toast';

// Global completion toasts: watch the local task stream and notify when a
// download finishes or fails (transition only, so the initial snapshot is quiet).
function useCompletionToasts() {
  const { toast } = useToast();
  const prev = useRef<Record<string, string>>({});
  useEffect(() => {
    return connectWS((type, data) => {
      if (type === 'snapshot') {
        prev.current = Object.fromEntries((data ?? []).map((t: Task) => [t.id, t.status]));
      } else if (type === 'task') {
        const before = prev.current[data.id];
        prev.current[data.id] = data.status;
        if (before && before !== data.status) {
          if (data.status === 'done') toast(`${data.name || 'Download'} finished`, 'ok');
          else if (data.status === 'error') toast(`${data.name || 'Download'} failed`, 'fail');
        }
      } else if (type === 'removed') {
        delete prev.current[data.id];
      }
    });
  }, [toast]);
}

export function Layout() {
  const location = useLocation();
  useCompletionToasts();
  return (
    <div className="flex h-screen overflow-hidden bg-carbon-background">
      <Sidebar />
      <main className="flex-1 overflow-y-auto min-w-0">
        <div key={location.pathname} className="kl-page-enter max-w-5xl p-6">
          <Outlet />
        </div>
      </main>
    </div>
  );
}
