import { NavLink } from 'react-router-dom';
import { useState } from 'react';
import { getTheme, toggleTheme } from '../lib/theme';
import {
  IconDashboard,
  IconCollector,
  IconDownloads,
  IconInstances,
  IconAccounts,
  IconSettings,
  IconMoon,
  IconSun,
} from '../lib/icons';

const navBase =
  'flex items-center gap-3 px-3.5 py-2.5 rounded-lg text-[15px] font-medium transition duration-150 select-none motion-safe:active:scale-[.98]';
const navActive = 'bg-accent text-accentContrast';
const navInactive =
  'text-[var(--sidebar-text)] hover:bg-carbon-hover hover:text-carbon-text motion-safe:hover:translate-x-0.5';

function Item({ to, label, icon, end }: { to: string; label: string; icon: React.ReactNode; end?: boolean }) {
  return (
    <NavLink to={to} end={end} className={({ isActive }) => `${navBase} ${isActive ? navActive : navInactive}`}>
      {icon}
      <span>{label}</span>
    </NavLink>
  );
}

export function Sidebar() {
  const [theme, setThemeState] = useState(getTheme);
  return (
    <aside className="flex flex-col w-56 shrink-0 h-full bg-carbon-sidebar">
      <NavLink to="/" end className="flex items-center gap-2.5 px-4 py-5 hover:opacity-90 transition-opacity">
        <span className="text-3xl leading-none" aria-hidden>
          ⚔️
        </span>
        <span className="flex flex-col leading-none">
          <span className="text-carbon-text font-bold text-xl tracking-tight">KnightLoader</span>
          <span className="text-carbon-textMuted text-[11px]">working title</span>
        </span>
      </NavLink>

      <nav className="flex flex-col gap-1 p-3 flex-1">
        <Item to="/" end label="Overview" icon={<IconDashboard />} />
        <Item to="/collector" label="Collector" icon={<IconCollector />} />
        <Item to="/downloads" label="Downloads" icon={<IconDownloads />} />
        <Item to="/instances" label="Instances" icon={<IconInstances />} />
        <Item to="/accounts" label="Accounts" icon={<IconAccounts />} />
      </nav>

      <div className="flex flex-col gap-1 p-3">
        <button
          onClick={() => setThemeState(toggleTheme())}
          className={`${navBase} ${navInactive} w-full`}
          title="Toggle theme"
        >
          {theme === 'dark' ? <IconMoon /> : <IconSun />}
          <span>{theme === 'dark' ? 'Dark' : 'Light'}</span>
        </button>
        <Item to="/settings" label="Settings" icon={<IconSettings />} />
      </div>
    </aside>
  );
}
