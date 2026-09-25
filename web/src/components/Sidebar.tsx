import { NavLink } from 'react-router-dom';
import { useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { LOGO_SCOPE_ID, logoInline } from '../lib/logoInline';
import { hueVars } from '../lib/appearance';
import { setHidden, useHidden } from '../lib/sidebarPrefs';
import { asBarLabelMode, asNavLabelMode, setBarLabels, setNavLabels, useBarLabels, useNavLabels, type NavLabelMode } from '../lib/navLabels';
import { useTooltip } from './ui';
import { usePhoneLayout } from '../lib/phoneLayout';
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
  'glim-nav-row relative flex items-center rounded-[var(--radius-pill)] px-3 py-2.5 text-[15px] font-medium transition duration-150 select-none';
const navActive = 'glim-active bg-accent text-accentContrast';
// The 2px nudge toward the content makes hover readable on a quiet rail.
export const navInactive =
  'text-[var(--sidebar-text)] hover:bg-carbon-hover hover:text-carbon-text motion-safe:hover:translate-x-0.5 motion-safe:hover:rtl:-translate-x-0.5';

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
        style={hueVars(hue) as CSSProperties}
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
  const barMode = useBarLabels();
  const egg = useDrawAndStrike();
  useEffect(() => {
    fetchSettings()
      .then((s) => {
        setHidden('accounts', s.hideAccountsFromSidebar);
        setHidden('instances', s.hideInstancesFromSidebar);
        setNavLabels(asNavLabelMode(s.navLabels));
        setBarLabels(asBarLabelMode(s.bottomBarLabels));
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

  // Beside a phone's page the rail would take more than half its width.
  const phone = usePhoneLayout();
  if (phone) {
    return (
      <PhoneBar
        mode={barMode}
        locked={locked}
        showInstances={!hideInstances}
        showAccounts={!hideAccounts}
        active={active}
        collected={collected}
      />
    );
  }

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
        className={`flex flex-col items-center gap-2 ${narrow ? 'px-2 py-4' : 'px-4 py-6'}`}
        onPointerDown={egg.onPointerDown}
        onPointerUp={egg.onPointerUp}
        onPointerLeave={egg.onPointerCancel}
        onPointerCancel={egg.onPointerCancel}
        onClick={egg.onClick}
      >
        {/* Inline SVG rather than <img> so the egg's CSS can move the sword
            alone; see lib/logoInline for the catch. */}
        <span
          className={`kl-egg shrink-0 ${narrow ? 'h-11' : 'h-26'}`}
          id={LOGO_SCOPE_ID}
          data-egg={egg.state}
          dangerouslySetInnerHTML={{ __html: logoInline }}
        />
        {!narrow && <span className="text-xl font-bold tracking-tight text-carbon-text">KnightLoader</span>}
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
              onClick={() => void signOut()}
              // Not an Item, since it navigates nowhere, but styled as a rail
              // row. text-start because a <button> centres its text.
              className={`${navHued} ${navBase} ${navInactive} group w-full text-start ${mode === 'glyph' || mode === 'hover' ? 'justify-center' : 'gap-3'}`}
              style={hueVars(nextHue()) as CSSProperties}
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

async function signOut(): Promise<void> {
  try {
    await logout();
    location.reload();
  } catch {
    // The sidebar has nowhere to show the error.
  }
}

// A segment of the phone's bar, shared with EventBell's. flex-1 and min-w-0
// make the segments equal whatever their words, so the filled one keeps its
// shape from page to page (GlimStone, "The bottom bar").
export const barSegment =
  'relative flex min-w-0 flex-1 flex-col items-center justify-center gap-0.5 rounded-[var(--radius-pill)] px-0.5 text-[11px] font-medium leading-[14px] transition-colors duration-150 select-none';
export const barIdle = 'text-[var(--sidebar-text)] hover:bg-carbon-hover hover:text-carbon-text';

/**
 * BarBody is a segment's glyph over its word, or either alone, as the label
 * mode says. A phone has no pointer to hover with, so in `hover` mode only the
 * current segment keeps its word, as the rail's active row does. A segment
 * without a word names itself through the caller's aria-label.
 */
export function BarBody({
  icon,
  label,
  mode,
  current = false,
  badge = 0,
}: {
  icon: ReactNode;
  label: string;
  mode: NavLabelMode;
  current?: boolean;
  badge?: number;
}) {
  const glyph = mode !== 'text';
  const word = mode === 'text' || mode === 'both' || (mode === 'hover' && current);
  // On the glyph's corner, or on the segment's when there is no glyph.
  const count = badge > 0 && (
    <span
      key={badge}
      className={`kl-count glim-num absolute rounded-[var(--radius-pill)] bg-carbon-surface3 px-1 py-px text-[11px] font-semibold
        leading-none text-carbon-textSub [.glim-active_&]:bg-black/15 [.glim-active_&]:text-current
        ${glyph ? '-top-1.5 start-3.5' : 'end-0.5 top-0.5'}`}
    >
      {badge > 99 ? '99+' : badge}
    </span>
  );
  return (
    <>
      {glyph && (
        <span className="relative flex [&>svg]:h-5 [&>svg]:w-5">
          {icon}
          {count}
        </span>
      )}
      {word && <span className="max-w-full truncate">{label}</span>}
      {!glyph && count}
    </>
  );
}

/** BarItem is one destination of the phone's bar. */
function BarItem({
  to,
  end,
  label,
  icon,
  badge,
  hue,
  mode,
}: {
  to: string;
  end?: boolean;
  label: string;
  icon: ReactNode;
  badge?: number;
  hue: number;
  mode: NavLabelMode;
}) {
  // Named when a segment may show no word: always in glyph mode, and all but
  // the current one in hover mode.
  const named = mode === 'glyph' || mode === 'hover';
  const tip = useTooltip<HTMLAnchorElement>(named ? label : undefined);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <NavLink
        to={to}
        end={end}
        style={hueVars(hue) as CSSProperties}
        aria-label={named ? label : undefined}
        {...(named ? tipHoverProps : {})}
        className={({ isActive }) => `${navHued} ${barSegment} ${isActive ? navActive : barIdle}`}
      >
        {({ isActive }) => <BarBody icon={icon} label={label} mode={mode} current={isActive} badge={badge} />}
      </NavLink>
      {tip.node}
    </>
  );
}

function BarSignOut({ hue, mode }: { hue: number; mode: NavLabelMode }) {
  const { t } = useT();
  const label = t('auth.signOut');
  const named = mode === 'glyph' || mode === 'hover';
  const tip = useTooltip<HTMLButtonElement>(named ? label : undefined);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <button
        type="button"
        aria-label={named ? label : undefined}
        {...(named ? tipHoverProps : {})}
        onClick={() => void signOut()}
        className={`${navHued} ${barSegment} ${barIdle}`}
        style={hueVars(hue) as CSSProperties}
      >
        <BarBody icon={<IconSignOut />} label={label} mode={mode} />
      </button>
      {tip.node}
    </>
  );
}

/**
 * PhoneBar is the rail at phone width: a card along the bottom, where a thumb
 * reaches it (GlimStone, "The bottom bar"). The rail's entries in the rail's
 * order and colours, one segment each. Its height is fixed, so a change of
 * label mode never moves the page above it.
 */
function PhoneBar({
  mode,
  locked,
  showInstances,
  showAccounts,
  active,
  collected,
}: {
  mode: NavLabelMode;
  locked: boolean;
  showInstances: boolean;
  showAccounts: boolean;
  active: number;
  collected: number;
}) {
  const { t } = useT();
  let hue = 0;
  const nextHue = () => hue++;
  return (
    // order-last draws it under the page while it stays ahead of the page in
    // the document, where the rail is.
    <nav className="order-last flex h-12 shrink-0 gap-0.5 rounded-[var(--radius-card)] bg-carbon-sidebar p-1">
      <BarItem to="/" end hue={nextHue()} mode={mode} label={t('nav.overview')} icon={<IconDashboard />} />
      <BarItem to="/downloads" hue={nextHue()} mode={mode} label={t('nav.downloads')} icon={<IconDownloads />} badge={active} />
      <BarItem to="/collector" hue={nextHue()} mode={mode} label={t('nav.collector')} icon={<IconCollector />} badge={collected} />
      {showInstances && <BarItem to="/instances" hue={nextHue()} mode={mode} label={t('nav.instances')} icon={<IconInstances />} />}
      {showAccounts && <BarItem to="/accounts" hue={nextHue()} mode={mode} label={t('nav.accounts')} icon={<IconAccounts />} />}
      <EventBell hue={nextHue()} bar />
      {locked && <BarSignOut hue={nextHue()} mode={mode} />}
      <BarItem to="/settings" hue={nextHue()} mode={mode} label={t('nav.settings')} icon={<IconSettings />} />
    </nav>
  );
}
