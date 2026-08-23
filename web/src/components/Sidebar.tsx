import { NavLink, useLocation } from 'react-router-dom';
import { useEffect, useMemo, useState, type CSSProperties } from 'react';
import logoUrl from '../assets/logo.svg';
import { hueVars, rainbowAt } from '../lib/appearance';
import { useRainbow } from '../lib/useRainbow';
import { useT } from '../lib/i18n';
import { fetchAuth, fetchSettings, logout } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import {
  IconDashboard,
  IconCollector,
  IconDownloads,
  IconInstances,
  IconAccounts,
  IconSettings,
  IconSignOut,
} from '../lib/icons';

// The active item is FILLED with the accent. It used to be marked by a 3px rail
// on a raised surface; jdp wants no vertical lines anywhere, and the fill is
// also the only treatment that survives the square corner setting, where a rail
// reads as a stray border rather than as a mark.
//
// text-[15px], matching BombVault's own nav row exactly (jdp: "Die texte in
// der sidebar sind zu klein. exakt gleiche Schriftgröße wie in BV") - this
// was 14px, one size below BombVault's navBase.
const navBase =
  'relative flex items-center gap-3 rounded-[var(--radius-control)] px-3 py-2.5 text-[15px] font-medium transition duration-150 select-none';
const navActive = 'glim-active bg-accent text-accentContrast';
const navInactive = 'text-[var(--sidebar-text)] hover:bg-carbon-hover hover:text-carbon-text';

// In rainbow mode the icon carries the item's own hue, so the rail and the
// glyph agree and the nav reads as a set rather than as one gold item and five
// grey ones. Without the mode the rule below never matches.
const navHued = 'glim-hue glim-hue-icon';

function Item({
  to,
  label,
  icon,
  end,
  badge,
  hue,
}: {
  to: string;
  label: string;
  icon: React.ReactNode;
  end?: boolean;
  badge?: number;
  hue: number;
}) {
  return (
    <NavLink
      to={to}
      end={end}
      style={hueVars(rainbowAt(hue)) as CSSProperties}
      className={({ isActive }) => `${navHued} ${navBase} ${isActive ? navActive : navInactive}`}
    >
      {icon}
      <span className="flex-1">{label}</span>
      {/* On the filled active item the badge sits on the accent, so it borrows
          the ink colour instead of the surface tint it uses when idle. */}
      {badge ? (
        <span className="glim-num rounded-[var(--radius-pill)] bg-carbon-surface3/60 px-1.5 py-0.5 text-[11px] font-semibold leading-none text-carbon-textSub [.glim-active_&]:bg-black/15 [.glim-active_&]:text-current">
          {badge}
        </span>
      ) : null}
    </NavLink>
  );
}

export function Sidebar() {
  const { t } = useT();
  const tasks = useTasks('');
  // Subscribed, not read: the nav re-renders when the palette or the mode
  // changes, so editing a swatch in Settings is visible in the rail at once.
  useRainbow();

  const [locked, setLocked] = useState(false);

  useEffect(() => {
    // Signing out only makes sense on an instance that can be signed in to.
    fetchAuth()
      .then((a) => setLocked(a.enabled))
      .catch(() => {});
  }, []);

  // Refetched on every navigation, not just on mount: this is a persistent
  // layout element that never remounts, so a value flipped on the Konten
  // settings tab a moment ago has to be picked up the next time someone
  // actually looks at the rail (the next navigation), not only on a full
  // page reload.
  const routerLocation = useLocation();
  const [hideAccounts, setHideAccounts] = useState(false);
  useEffect(() => {
    fetchSettings()
      .then((s) => setHideAccounts(s.hideAccountsFromSidebar))
      .catch(() => {});
  }, [routerLocation.pathname]);

  const { collected, active } = useMemo(() => {
    let collected = 0,
      active = 0;
    for (const t of Object.values(tasks)) {
      // Held links are not in the collector's list, so they must not be in its
      // badge either: a filter that is working would otherwise put a permanent
      // number in the sidebar pointing at links that are not there.
      if (t.status === 'collected') {
        if (!t.skipped) collected++;
      }
      else if (t.status === 'running' || t.status === 'queued' || t.status === 'extracting') active++;
    }
    return { collected, active };
  }, [tasks]);

  return (
    <aside className="flex flex-col w-56 shrink-0 h-full bg-carbon-sidebar">
      {/* Centered and stacked - jdp's own call for KnightLoader specifically,
          overriding the horizontal BV-matched row this briefly became ("Das
          Logo in der Sidebar wieder größer und Text unter das Logo"): a
          bigger mark reads better centered above its name than squeezed
          into a compact side-by-side row sized for BV's own smaller 64px
          icon. */}
      <NavLink to="/" end className="flex flex-col items-center gap-2 px-4 py-6 hover:opacity-90 transition-opacity">
        <img src={logoUrl} alt="" aria-hidden className="h-28 w-auto shrink-0" />
        <span className="text-carbon-text font-bold text-xl tracking-tight">KnightLoader</span>
      </NavLink>

      <nav className="flex flex-col gap-1 p-3 flex-1">
        {/* Downloads above the collector: the download list is what this app is
            open for, and the collector is the room links pass through on their
            way into it. JDownloader puts its download tab first for the same
            reason, and somebody arriving from it reaches for the first entry. */}
        <Item to="/" end hue={0} label={t('nav.overview')} icon={<IconDashboard />} />
        <Item to="/downloads" hue={1} label={t('nav.downloads')} icon={<IconDownloads />} badge={active} />
        <Item to="/collector" hue={2} label={t('nav.collector')} icon={<IconCollector />} badge={collected} />
        <Item to="/instances" hue={3} label={t('nav.instances')} icon={<IconInstances />} />
        {!hideAccounts && <Item to="/accounts" hue={4} label={t('nav.accounts')} icon={<IconAccounts />} />}
      </nav>

      {/* Sprache and Hell/Dunkel used to live here too, mirrored from the
          Aussehen settings tab - one control, one home now, not two copies
          of the same switch (jdp: "Sprach und hell dunkel ist in der
          sidebar immer noch vorhanden"). Both still live on the Aussehen
          tab (pages/settings/Look.tsx). */}
      <div className="flex flex-col gap-1 p-3">
        <Item to="/settings" hue={5} label={t('nav.settings')} icon={<IconSettings />} />
        {locked && (
          <button
            onClick={async () => {
              await logout();
              location.reload();
            }}
            className={`${navBase} ${navInactive} w-full`}
          >
            <IconSignOut />
            <span>{t('auth.signOut')}</span>
          </button>
        )}
      </div>
    </aside>
  );
}
