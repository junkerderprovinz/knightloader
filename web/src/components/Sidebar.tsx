import { NavLink } from 'react-router-dom';
import { useEffect, useMemo, useState } from 'react';
import { getTheme, toggleTheme } from '../lib/theme';
import { useT, LANGUAGES } from '../lib/i18n';
import { fetchHealth } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import {
  IconDashboard,
  IconCollector,
  IconDownloads,
  IconInstances,
  IconAccounts,
  IconSettings,
  IconMoon,
  IconSun,
  IconGlobe,
} from '../lib/icons';

// The active item is marked by a gold rail and gold icon on a raised surface —
// not by a solid gold slab, which would shout louder than the page content.
const navBase =
  'relative flex items-center gap-3 rounded-[var(--radius-control)] pl-4 pr-3 py-2.5 text-[14px] font-medium transition duration-150 select-none';
const navActive =
  'bg-carbon-surface text-carbon-text before:absolute before:left-0 before:top-1/2 before:-translate-y-1/2 ' +
  'before:h-5 before:w-[3px] before:rounded-full before:bg-accent [&_svg]:text-accent';
const navInactive = 'text-[var(--sidebar-text)] hover:bg-carbon-hover hover:text-carbon-text';

function Item({
  to,
  label,
  icon,
  end,
  badge,
}: {
  to: string;
  label: string;
  icon: React.ReactNode;
  end?: boolean;
  badge?: number;
}) {
  return (
    <NavLink to={to} end={end} className={({ isActive }) => `${navBase} ${isActive ? navActive : navInactive}`}>
      {icon}
      <span className="flex-1">{label}</span>
      {badge ? (
        <span className="keep-num rounded-full bg-carbon-surface3/60 px-1.5 py-0.5 text-[11px] font-semibold leading-none text-carbon-textSub">
          {badge}
        </span>
      ) : null}
    </NavLink>
  );
}

export function Sidebar() {
  const { t, lang, setLang } = useT();
  const [theme, setThemeState] = useState(getTheme);
  const [version, setVersion] = useState('');
  const tasks = useTasks('');

  useEffect(() => {
    fetchHealth()
      .then((h) => setVersion(h.version))
      .catch(() => {});
  }, []);

  const { collected, active } = useMemo(() => {
    let collected = 0,
      active = 0;
    for (const t of Object.values(tasks)) {
      if (t.status === 'collected') collected++;
      else if (t.status === 'running' || t.status === 'queued' || t.status === 'extracting') active++;
    }
    return { collected, active };
  }, [tasks]);

  const nextLang = LANGUAGES[(LANGUAGES.findIndex((l) => l.code === lang) + 1) % LANGUAGES.length];

  return (
    <aside className="flex flex-col w-56 shrink-0 h-full bg-carbon-sidebar">
      <NavLink to="/" end className="flex items-center gap-2.5 px-4 py-5 hover:opacity-90 transition-opacity">
        <span className="text-3xl leading-none" aria-hidden>
          ⚔️
        </span>
        <span className="flex flex-col leading-none">
          <span className="text-carbon-text font-bold text-xl tracking-tight">KnightLoader</span>
          <span className="text-carbon-textMuted text-[11px]">
            {version && version !== 'dev' ? version : t('nav.workingTitle')}
          </span>
        </span>
      </NavLink>

      <nav className="flex flex-col gap-1 p-3 flex-1">
        <Item to="/" end label={t('nav.overview')} icon={<IconDashboard />} />
        <Item to="/collector" label={t('nav.collector')} icon={<IconCollector />} badge={collected} />
        <Item to="/downloads" label={t('nav.downloads')} icon={<IconDownloads />} badge={active} />
        <Item to="/instances" label={t('nav.instances')} icon={<IconInstances />} />
        <Item to="/accounts" label={t('nav.accounts')} icon={<IconAccounts />} />
      </nav>

      <div className="flex flex-col gap-1 p-3">
        <button
          onClick={() => setLang(nextLang.code)}
          className={`${navBase} ${navInactive} w-full`}
          title={t('lang.label')}
        >
          <IconGlobe />
          <span>{LANGUAGES.find((l) => l.code === lang)?.label}</span>
        </button>
        <button
          onClick={() => setThemeState(toggleTheme())}
          className={`${navBase} ${navInactive} w-full`}
          title={t('theme.toggle')}
        >
          {theme === 'dark' ? <IconMoon /> : <IconSun />}
          <span>{theme === 'dark' ? t('theme.dark') : t('theme.light')}</span>
        </button>
        <Item to="/settings" label={t('nav.settings')} icon={<IconSettings />} />
      </div>
    </aside>
  );
}
