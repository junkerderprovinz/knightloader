import { Outlet, useLocation } from 'react-router-dom';
import { Sidebar } from '../components/Sidebar';

export function Layout() {
  const location = useLocation();
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
