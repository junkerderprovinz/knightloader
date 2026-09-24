import { NavLink } from 'react-router-dom';
import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react';
import { LOGO_SCOPE_ID, logoInline } from '../lib/logoInline';
import { hueVars, rainbowAt } from '../lib/appearance';
import { useRainbow } from '../lib/useRainbow';
import { setHidden, useHidden } from '../lib/sidebarPrefs';
import { asNavLabelMode, setNavLabels, useNavLabels, type NavLabelMode } from '../lib/navLabels';
import { useTooltip } from './ui';
import { useT } from '../lib/i18n';
import { fetchAuth, fetchSettings, logout } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { EventBell } from './EventBell';
import {
  IconDashboard,
  IconCollector,
  IconDownloads,
  IconInstances,
  IconAccounts,
  IconSettings,
  IconSignOut,
} from '../lib/icons';

/**
 * useDrawAndStrike is the sidebar logo's easter egg: press and hold to draw the
 * blade, release to swing it. A short press still navigates home. The animation
 * is CSS (.kl-egg in index.css), so it follows the motion setting; this hook
 * only picks the state.
 */
function useDrawAndStrike(): {
  state: 'idle' | 'draw' | 'strike';
  onPointerDown: () => void;
  onPointerUp: () => void;
  onPointerCancel: () => void;
  onClick: (e: React.MouseEvent) => void;
} {
  const [state, setState] = useState<'idle' | 'draw' | 'strike'>('idle');
  const hold = useRef<number | undefined>(undefined);
  const done = useRef<number | undefined>(undefined);
  const suppress = useRef(false);

  useEffect(
    () => () => {
      window.clearTimeout(hold.current);
      window.clearTimeout(done.current);
    },
    [],
  );

  // The impact keyframe of .kl-egg's strike: 16% of the 540ms swing.
  const IMPACT_MS = 86;

  // Shudders the rail's rows one after another from the impact.
  function shove() {
    const rail = document.querySelector('[data-nav-rail]');
    if (!rail) return;
    [...rail.children].forEach((el, i) => {
      window.setTimeout(() => {
        el.classList.add('kl-egg-struck');
        window.setTimeout(() => el.classList.remove('kl-egg-struck'), 400);
      }, IMPACT_MS + i * 45);
    });
  }

  return {
    state,
    onPointerDown: () => {
      suppress.current = false;
      window.clearTimeout(hold.current);
      // Longer than an ordinary click.
      hold.current = window.setTimeout(() => {
        suppress.current = true;
        setState('draw');
      }, 320);
    },
    onPointerUp: () => {
      window.clearTimeout(hold.current);
      setState((cur) => {
        if (cur !== 'draw') return cur;
        window.clearTimeout(done.current);
        done.current = window.setTimeout(() => setState('idle'), 700);
        shove();
        return 'strike';
      });
    },
    onPointerCancel: () => {
      window.clearTimeout(hold.current);
      setState((cur) => (cur === 'draw' ? 'idle' : cur));
    },
    onClick: (e) => {
      // Only the click that ends a hold is swallowed.
      if (!suppress.current) return;
      suppress.current = false;
      e.preventDefault();
    },
  };
}

// The rail's row classes, exported for EventBell.tsx's row. The active row is
// filled with the accent. No `gap` here: the centred modes need none, and two
// gap utilities on one element resolve by stylesheet order.
export const navBase =
  'glim-nav-row relative flex items-center rounded-[var(--radius-control)] px-3 py-2.5 text-[15px] font-medium transition duration-150 select-none';
const navActive = 'glim-active bg-accent text-accentContrast';
// The 2px nudge makes hover readable on a quiet rail.
export const navInactive =
  'text-[var(--sidebar-text)] hover:bg-carbon-hover hover:text-carbon-text motion-safe:hover:translate-x-0.5';

// In rainbow mode the glyph takes the row's hue.
export const navHued = 'glim-hue glim-hue-icon';

/**
 * NavLabel renders a row's label for the display mode. In `hover` mode it
 * collapses to zero width inside a centred row and grows back under the
 * pointer, so the glyph slides without anything being measured. The gap comes
 * from its own padding because a collapsed element cannot squeeze a row gap or
 * padding away.
 */
export function NavLabel({ label, mode }: { label: string; mode: NavLabelMode }) {
  if (mode === 'glyph') return null;
  if (mode !== 'hover') return <span className="flex-1">{label}</span>;
  return (
    <span
      className="max-w-0 overflow-hidden whitespace-nowrap opacity-0 transition-all duration-200
        group-hover:ps-3 group-hover:max-w-40 group-hover:opacity-100
        group-focus-visible:ps-3 group-focus-visible:max-w-40 group-focus-visible:opacity-100"
    >
      {label}
    </span>
  );
}

function Item({
  to,
  label,
  icon,
  end,
  badge,
  hue,
  mode,
}: {
  to: string;
  label: string;
  icon: React.ReactNode;
  end?: boolean;
  badge?: number;
  hue: number;
  mode: NavLabelMode;
}) {
  const centred = mode === 'glyph' || mode === 'hover';
  // Only glyph mode gets a tooltip; hover mode reveals the label itself.
  const tip = useTooltip<HTMLAnchorElement>(mode === 'glyph' ? label : undefined);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <NavLink
        to={to}
        end={end}
        style={hueVars(rainbowAt(hue)) as CSSProperties}
        // Glyph mode has no visible text to name the link.
        aria-label={mode === 'glyph' ? label : undefined}
        {...(mode === 'glyph' ? tipHoverProps : {})}
        className={({ isActive }) =>
          `${navHued} ${navBase} group ${centred ? 'justify-center' : 'gap-3'} ${isActive ? navActive : navInactive}`
        }
      >
        {mode !== 'text' && icon}
        <NavLabel label={label} mode={mode} />
        {/* In the centred modes the badge pins to the glyph's corner, so the
            glyph stays centred. On the active row it takes the ink colour. */}
        {badge ? (
          <span
            // A new key per value replays .kl-count's entrance, so the number
            // visibly rolls when it changes.
            key={badge}
            className={`kl-count glim-num rounded-[var(--radius-pill)] bg-carbon-surface3/60 px-1.5 py-0.5 text-[11px] font-semibold leading-none text-carbon-textSub [.glim-active_&]:bg-black/15 [.glim-active_&]:text-current
              ${centred ? 'absolute end-1 top-1' : ''}`}
          >
            {badge}
          </span>
        ) : null}
      </NavLink>
      {tip.node}
    </>
  );
}

export function Sidebar() {
  const { t } = useT();
  const tasks = useTasks('');
  // Subscribed so palette edits show in the rail at once.
  useRainbow();

  const [locked, setLocked] = useState(false);

  useEffect(() => {
    // Sign-out is offered only when auth is enabled.
    fetchAuth()
      .then((a) => setLocked(a.enabled))
      .catch(() => {});
  }, []);

  // Live stores that the settings tabs update directly; the fetch below only
  // seeds them after a reload.
  const hideAccounts = useHidden('accounts');
  const hideInstances = useHidden('instances');
  const mode = useNavLabels();
  const egg = useDrawAndStrike();
  useEffect(() => {
    fetchSettings()
      .then((s) => {
        setHidden('accounts', s.hideAccountsFromSidebar);
        setHidden('instances', s.hideInstancesFromSidebar);
        setNavLabels(asNavLabelMode(s.navLabels));
      })
      .catch(() => {});
  }, []);

  const { collected, active } = useMemo(() => {
    let collected = 0,
      active = 0;
    for (const t of Object.values(tasks)) {
      // Held links and rows a hoster preset set aside are not in the
      // collector's list, so not in its badge either.
      if (t.status === 'collected') {
        if (!t.skipped && !t.variantOff) collected++;
      }
      else if (t.status === 'running' || t.status === 'queued' || t.status === 'extracting') active++;
    }
    return { collected, active };
  }, [tasks]);

  // Only glyph mode narrows the rail; hover mode keeps its width so nothing
  // moves under the pointer.
  const narrow = mode === 'glyph';

  // Hues are counted in render order, so a hidden row leaves no gap in the
  // rainbow.
  let hue = 0;
  const nextHue = () => hue++;

  // Declared outside the conditional sign-out row, since hooks cannot be.
  const signOutTip = useTooltip<HTMLButtonElement>(mode === 'glyph' ? t('auth.signOut') : undefined);
  const { role: _signOutRole, tabIndex: _signOutTabIndex, ...signOutTipProps } = signOutTip.triggerProps;
  return (
    // A card with the card radius and no shadow (GlimStone 1.8.0), on its own
    // sidebar surface token. overflow-hidden keeps the logo inside the rounded
    // corner, which also makes the rail a scroller: anything that opens beside
    // it must portal to <body> (see EventBell.tsx). The narrow width, 85px, is
    // GlimStone's.
    <aside
      className={`flex h-full shrink-0 flex-col overflow-hidden rounded-[var(--radius-card)]
        bg-carbon-sidebar ${narrow ? 'w-[85px]' : 'w-56'}`}
    >
      {/* The logo above the name; the narrow rail drops the name and shrinks
          the logo to fit. */}
      <NavLink
        to="/"
        end
        className={`flex flex-col items-center gap-2 hover:opacity-90 transition-opacity ${narrow ? 'px-2 py-4' : 'px-4 py-6'}`}
        onPointerDown={egg.onPointerDown}
        onPointerUp={egg.onPointerUp}
        onPointerLeave={egg.onPointerCancel}
        onPointerCancel={egg.onPointerCancel}
        onClick={egg.onClick}
      >
        {/* Inline SVG rather than <img> so the egg's CSS can move the sword
            alone; see lib/logoInline for the catch. */}
        <span
          className={`kl-egg shrink-0 ${narrow ? 'h-10' : 'h-28'}`}
          id={LOGO_SCOPE_ID}
          data-egg={egg.state}
          dangerouslySetInnerHTML={{ __html: logoInline }}
        />
        {!narrow && <span className="text-carbon-text font-bold text-xl tracking-tight">KnightLoader</span>}
      </NavLink>

      <nav data-nav-rail className={`flex flex-col gap-1 flex-1 ${narrow ? 'p-2' : 'p-3'}`}>
        {/* Downloads before the collector, as in JDownloader. */}
        <Item to="/" end hue={nextHue()} mode={mode} label={t('nav.overview')} icon={<IconDashboard />} />
        <Item to="/downloads" hue={nextHue()} mode={mode} label={t('nav.downloads')} icon={<IconDownloads />} badge={active} />
        <Item to="/collector" hue={nextHue()} mode={mode} label={t('nav.collector')} icon={<IconCollector />} badge={collected} />
        {!hideInstances && <Item to="/instances" hue={nextHue()} mode={mode} label={t('nav.instances')} icon={<IconInstances />} />}
        {!hideAccounts && <Item to="/accounts" hue={nextHue()} mode={mode} label={t('nav.accounts')} icon={<IconAccounts />} />}
      </nav>

      <div className={`flex flex-col gap-1 ${narrow ? 'p-2' : 'p-3'}`}>
        {/* The bell opens a panel rather than a route, so it sits down here. */}
        <EventBell hue={nextHue()} />
        {locked && (
          <>
            <button
              aria-label={mode === 'glyph' ? t('auth.signOut') : undefined}
              {...(mode === 'glyph' ? signOutTipProps : {})}
              onClick={async () => {
                try {
                  await logout();
                  location.reload();
                } catch {
                  // The sidebar has nowhere to show the error.
                }
              }}
              // Not an Item, since it navigates nowhere, but styled as a rail
              // row. text-start because a <button> centres its text.
              className={`${navHued} ${navBase} ${navInactive} group w-full text-start ${mode === 'glyph' || mode === 'hover' ? 'justify-center' : 'gap-3'}`}
              style={hueVars(rainbowAt(nextHue())) as CSSProperties}
            >
              {mode !== 'text' && <IconSignOut />}
              <NavLabel label={t('auth.signOut')} mode={mode} />
            </button>
            {signOutTip.node}
          </>
        )}
        <Item to="/settings" hue={nextHue()} mode={mode} label={t('nav.settings')} icon={<IconSettings />} />
      </div>
    </aside>
  );
}
