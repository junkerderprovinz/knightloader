import { useEffect, useRef } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { Sidebar } from '../components/Sidebar';
import { connectWS, type Task } from '../lib/api';
import { useToast } from '../lib/toast';
import { useT } from '../lib/i18n';

// Global completion toasts: watch the local task stream and notify when a
// download finishes or fails (transition only, so the initial snapshot is quiet).
function useCompletionToasts() {
  const { toast } = useToast();
  const { t } = useT();
  const prev = useRef<Record<string, string>>({});
  useEffect(() => {
    return connectWS((type, data) => {
      if (type === 'snapshot') {
        prev.current = Object.fromEntries((data ?? []).map((t: Task) => [t.id, t.status]));
      } else if (type === 'task') {
        const before = prev.current[data.id];
        prev.current[data.id] = data.status;
        if (before && before !== data.status) {
          const name = data.name || t('nav.downloads');
          if (data.status === 'done') toast(t('downloads.finished', { name }), 'ok');
          else if (data.status === 'error') toast(t('downloads.failed', { name }), 'fail');
        }
      } else if (type === 'removed') {
        delete prev.current[data.id];
      }
    });
  }, [toast, t]);
}

export function Layout() {
  const location = useLocation();
  useCompletionToasts();
  return (
    <div className="flex h-screen overflow-hidden bg-carbon-background">
      <Sidebar />
      <main className="flex-1 overflow-y-auto min-w-0">
        <div key={location.pathname} className="keep-page-enter mx-auto w-full max-w-5xl p-6 md:p-8">
          <Outlet />
        </div>
      </main>
    </div>
  );
}
