import { useEffect, useRef } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { Sidebar } from '../components/Sidebar';
import { connectWS, fetchSettings, type Task } from '../lib/api';
import { applyAccent, applyRainbow, applyShape, cacheAppearance, rainbowFromSettings } from '../lib/appearance';
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

// The shape and accent live in the server settings, so they follow the instance
// rather than the browser. They are fetched once here, at the top of the app:
// applying them only where the settings page is mounted would leave every other
// page on the default look, which is exactly what a "square corners" setting
// must not do.
function useAppearance() {
  useEffect(() => {
    let live = true;
    fetchSettings()
      .then((s) => {
        if (!live) return;
        const rainbow = rainbowFromSettings(s);
        applyShape(s.shape);
        applyAccent(s.accent);
        applyRainbow(rainbow);
        // Cached so the next load paints the chosen look immediately instead of
        // flashing the default while this request is in flight.
        cacheAppearance(s.shape, s.accent, rainbow);
      })
      .catch(() => {
        // An unreachable API is not a reason to restyle the app; the cached
        // look from the last successful load stays.
      });
    return () => {
      live = false;
    };
  }, []);
}

export function Layout() {
  const location = useLocation();
  useCompletionToasts();
  useAppearance();
  // Keyed on the SECTION, not the whole path.
  //
  // The key is what re-triggers the enter animation, and the way it does that is
  // by remounting everything below it. That is right between top-level pages and
  // wrong inside one: a section with a route per sub-page — settings has
  // thirteen — keeps state above its own outlet, and remounting on every click
  // in its rail throws that state away with nothing said. An edited settings
  // page reverting because somebody looked at another tab of it is the exact
  // failure the sub-page split exists to avoid.
  const section = location.pathname.split('/')[1] ?? '';
  return (
    <div className="flex h-screen overflow-hidden bg-carbon-background">
      <Sidebar />
      <main className="flex-1 overflow-y-auto min-w-0">
        <div key={section} className="glim-page-enter w-full p-6 md:p-8">
          <Outlet />
        </div>
      </main>
    </div>
  );
}
