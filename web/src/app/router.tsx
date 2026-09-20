import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { Layout } from './Layout';
import { AuthGate } from './AuthGate';
import { Dashboard } from '../pages/Dashboard';
import { Collector } from '../pages/Collector';
import { Downloads } from '../pages/Downloads';
import { Instances } from '../pages/Instances';
import { Accounts } from '../pages/Accounts';
import { SettingsPage } from '../pages/Settings';
import { QuickAdd } from '../pages/QuickAdd';
import { ToastProvider } from '../lib/toast';
import { I18nProvider } from '../lib/i18n';
import { TabIndicator } from '../components/TabIndicator';

export function AppRouter() {
  return (
    <I18nProvider>
    <ToastProvider>
      <AuthGate>
        {/* Outside the router: the tab title and favicon need no route. */}
        <TabIndicator />
        <BrowserRouter>
          <Routes>
            {/* Outside <Layout>: the bookmarklet and the extension open it as
                a small window with no room for a sidebar. */}
            <Route path="/quickadd" element={<QuickAdd />} />
            <Route element={<Layout />}>
              <Route index element={<Dashboard />} />
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
        </BrowserRouter>
      </AuthGate>
    </ToastProvider>
    </I18nProvider>
  );
}
