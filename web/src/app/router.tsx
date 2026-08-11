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
        {/* Sibling to the router, not inside it: the tab title/favicon have no
            route or instance scope to read, so nothing here needs the routing
            tree - see TabIndicator's own doc comment. */}
        <TabIndicator />
        <BrowserRouter>
          <Routes>
            {/* Outside <Layout> on purpose — see QuickAdd.tsx's own doc
                comment: it is opened as a small standalone window by the
                bookmarklet and the extension, and a sidebar squeezed into
                that width helps nobody. */}
            <Route path="/quickadd" element={<QuickAdd />} />
            <Route element={<Layout />}>
              <Route index element={<Dashboard />} />
              <Route path="/collector" element={<Collector />} />
              <Route path="/downloads" element={<Downloads />} />
              <Route path="/instances" element={<Instances />} />
              <Route path="/accounts" element={<Accounts />} />
              {/* The splat is load-bearing: settings is a tree of sub-pages with
                  real addresses (/settings/downloads), and an exact match here
                  would send every one of them to the catch-all below. */}
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
