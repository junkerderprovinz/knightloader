import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { Layout } from './Layout';
import { AuthGate } from './AuthGate';
import { PageBoundary } from './PageBoundary';
import { Dashboard } from '../pages/Dashboard';
import { Collector } from '../pages/Collector';
import { Downloads } from '../pages/Downloads';
import { Instances } from '../pages/Instances';
import { Accounts } from '../pages/Accounts';
import { SettingsPage } from '../pages/Settings';
import { QuickAdd } from '../pages/QuickAdd';
import { TrayOverview } from '../pages/TrayOverview';
import { ToastProvider } from '../lib/toast';
import { I18nProvider } from '../lib/i18n';
import { TabIndicator } from '../components/TabIndicator';
import { WindowBar } from '../components/WindowBar';
import { RainbowProvider } from '../lib/useRainbow';
import { basePath } from '../lib/basePath';
import { useHidden } from '../lib/sidebarPrefs';

/** Home is the overview, or the download list for whoever switched it off. */
function Home() {
  return useHidden('overview') ? <Navigate to="/downloads" replace /> : <Dashboard />;
}

export function AppRouter() {
  return (
    // Outermost, so the sign-in screen, the toasts and every route can read
    // the rainbow state (lib/useRainbow.ts).
    <RainbowProvider>
    <I18nProvider>
    {/* Outside the gate and the router, so the window can be moved and closed
        from the sign-in screen and from a page that failed. */}
    <WindowBar />
    <ToastProvider>
      <AuthGate>
        {/* Outside the router: the tab title and favicon need no route. */}
        <TabIndicator />
        <BrowserRouter basename={basePath()}>
          <PageBoundary>
          <Routes>
            {/* Outside <Layout>: the bookmarklet and the extension open it as
                a small window with no room for a sidebar. */}
            <Route path="/quickadd" element={<QuickAdd />} />
            {/* The desktop app's small window at the tray icon. */}
            <Route path="/tray" element={<TrayOverview />} />
            <Route element={<Layout />}>
              <Route index element={<Home />} />
              <Route path="/collector" element={<Collector />} />
              <Route path="/downloads" element={<Downloads />} />
              <Route path="/instances" element={<Instances />} />
              <Route path="/accounts" element={<Accounts />} />
              {/* The splat keeps sub-pages such as /settings/downloads out of
                  the catch-all below. */}
              <Route path="/settings/*" element={<SettingsPage />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Route>
          </Routes>
          </PageBoundary>
        </BrowserRouter>
      </AuthGate>
    </ToastProvider>
    </I18nProvider>
    </RainbowProvider>
  );
}
