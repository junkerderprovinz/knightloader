import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { Layout } from './Layout';
import { AuthGate } from './AuthGate';
import { Dashboard } from '../pages/Dashboard';
import { Collector } from '../pages/Collector';
import { Downloads } from '../pages/Downloads';
import { Instances } from '../pages/Instances';
import { Accounts } from '../pages/Accounts';
import { SettingsPage } from '../pages/Settings';
import { ToastProvider } from '../lib/toast';
import { I18nProvider } from '../lib/i18n';

export function AppRouter() {
  return (
    <I18nProvider>
    <ToastProvider>
      <AuthGate>
        <BrowserRouter>
          <Routes>
            <Route element={<Layout />}>
              <Route index element={<Dashboard />} />
              <Route path="/collector" element={<Collector />} />
              <Route path="/downloads" element={<Downloads />} />
              <Route path="/instances" element={<Instances />} />
              <Route path="/accounts" element={<Accounts />} />
              <Route path="/settings" element={<SettingsPage />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Route>
          </Routes>
        </BrowserRouter>
      </AuthGate>
    </ToastProvider>
    </I18nProvider>
  );
}
