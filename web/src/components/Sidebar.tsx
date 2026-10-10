import { NavLink, useLocation } from 'react-router-dom';
import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
  type RefObject,
} from 'react';
import { LOGO_SCOPE_ID, logoInline } from '../lib/logoInline';
import { hueVars } from '../lib/appearance';
import { setEventsPanelOpen, useUnreadEvents } from '../lib/eventLog';
import { setHidden, useHidden } from '../lib/sidebarPrefs';
import { seedLabelModes, useLabelMode, type LabelMode } from '../lib/labelModes';
import { Button, IconTile, Modal, useTooltip } from './ui';
import { usePhoneLayout } from '../lib/phoneLayout';
import { useT } from '../lib/i18n';
import { fetchAuth, fetchSettings, logout } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { countTasks, openDownloads } from '../lib/needs';
import { useNeeds } from '../lib/useNeeds';
import { EventBell } from './EventBell';
import {
  IconDashboard,
  IconCollector,
  IconDownloads,
  IconInstances,
  IconAccounts,
  IconBell,
  IconChevronEnd,
  IconClose,
  IconMore,
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

  // Shudders the rail's rows one after another from the impact, down through
  // the pages and on through the rows at its foot.
  function shove() {
    document.querySelectorAll('[data-nav-rail] > *').forEach((el, i) => {
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
  'glim-nav-row relative flex items-center rounded-[var(--radius-pill)] px-3 py-2.5 text-body font-medium transition duration-150 select-none';
const navActive = 'glim-active bg-accent text-accentContrast';
// The 2px nudge toward the content makes hover readable on a quiet rail.
export const navInactive =
  'text-[var(--sidebar-text)] hover:bg-carbon-hover hover:text-carbon-text motion-safe:hover:translate-x-0.5 motion-safe:hover:rtl:-translate-x-0.5';

// In rainbow mode the glyph takes the row's hue.
export const navHued = 'glim-hue glim-hue-icon';

// A count wears the colours of the glyph beside it, the accent or the row's
// rainbow hue, with that colour's ink. On the active row, which is filled with
// the colour itself, the two swap.
export const navCount =
  'glim-num rounded-[var(--radius-pill)] bg-accent px-1.5 py-0.5 text-meta font-semibold leading-none text-accentContrast [.glim-active_&]:bg-accentContrast [.glim-active_&]:text-accent';

/**
 * NavLabel renders a row's label for the display mode. In `hover` mode it
 * collapses to zero width inside a centred row and grows back under the
 * pointer, so the glyph slides without anything being measured. The gap comes
 * from its own padding because a collapsed element cannot squeeze a row gap or
 * padding away. The reveal runs on the motion level's label dials
 * (.glim-label-reactive).
 */
export function NavLabel({ label, mode }: { label: string; mode: LabelMode }) {
  if (mode === 'glyph') return null;
  if (mode !== 'hover') return <span className="flex-1">{label}</span>;
  return (
    <span
      className="glim-label-reactive max-w-0 overflow-hidden whitespace-nowrap opacity-0
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
  mode: LabelMode;
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
            glyph stays centred. */}
        {badge ? (
          <span
            // A new key per value replays .kl-count's entrance, so the number
            // visibly rolls when it changes.
            key={badge}
            className={`kl-count ${navCount} ${centred ? 'absolute end-1 top-1' : ''}`}
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
  // The overview's own list, so the count here is the number of rows there.
  const needs = useNeeds(tasks).needs.length;

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
  const mode = useLabelMode('sidebar');
  const barMode = useLabelMode('bottombar');
  const egg = useDrawAndStrike();
  useEffect(() => {
    fetchSettings()
      .then((s) => {
        setHidden('accounts', s.hideAccountsFromSidebar);
        setHidden('instances', s.hideInstancesFromSidebar);
        seedLabelModes(s);
      })
      .catch(() => {});
  }, []);

  // The overview's own counting, so a badge here and a figure there agree.
  const { collected, active } = useMemo(() => {
    const counts = countTasks(Object.values(tasks), Date.now());
    return { collected: counts.collected, active: openDownloads(counts) };
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
        needs={needs}
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
        <Item to="/" end hue={nextHue()} mode={mode} label={t('nav.overview')} icon={<IconDashboard />} badge={needs} />
        <Item to="/downloads" hue={nextHue()} mode={mode} label={t('nav.downloads')} icon={<IconDownloads />} badge={active} />
        <Item to="/collector" hue={nextHue()} mode={mode} label={t('nav.collector')} icon={<IconCollector />} badge={collected} />
        {!hideInstances && <Item to="/instances" hue={nextHue()} mode={mode} label={t('nav.instances')} icon={<IconInstances />} />}
        {!hideAccounts && <Item to="/accounts" hue={nextHue()} mode={mode} label={t('nav.accounts')} icon={<IconAccounts />} />}
      </nav>

      <div data-nav-rail className={`flex flex-col gap-1 ${narrow ? 'p-2' : 'p-3'}`}>
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
  'relative flex min-w-0 flex-1 flex-col items-center justify-center gap-0.5 rounded-[var(--radius-pill)] text-meta font-medium leading-[14px] transition-colors duration-150 select-none';
export const barIdle = 'text-[var(--sidebar-text)] hover:bg-carbon-hover hover:text-carbon-text';

/** The smallest the bar's words go; a word too long even then is cut. */
const BAR_WORD_FLOOR = 10;

/**
 * useBarFit shrinks the bar's words together until the longest fits its
 * segment, as the README buttons do with a long translation. Together, so the
 * segments keep one type size. `words` names what the bar shows, for a fit
 * when the language or the label mode changes it.
 */
function useBarFit(bar: RefObject<HTMLElement | null>, words: string): void {
  useLayoutEffect(() => {
    const nav = bar.current;
    if (!nav) return;
    const fit = () => {
      nav.style.removeProperty('--bar-word');
      const shown = [...nav.querySelectorAll<HTMLElement>('[data-bar-word]')].filter((w) => w.clientWidth > 0);
      const over = Math.max(1, ...shown.map((w) => w.scrollWidth / w.clientWidth));
      if (over === 1) return;
      const size = parseFloat(getComputedStyle(shown[0]).fontSize);
      // A quarter pixel under the exact fit, which the rounding of glyph
      // widths would otherwise push back over the edge.
      nav.style.setProperty('--bar-word', `${Math.max(BAR_WORD_FLOOR, size / over - 0.25)}px`);
    };
    fit();
    const watch = new ResizeObserver(fit);
    watch.observe(nav);
    // A word measures wider once the web font replaces the fallback.
    void document.fonts.ready.then(fit);
    return () => watch.disconnect();
  }, [bar, words]);
}

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
  mode: LabelMode;
  current?: boolean;
  badge?: number;
}) {
  const glyph = mode !== 'text';
  const word = mode === 'text' || mode === 'both' || (mode === 'hover' && current);
  // On the glyph's corner, or on the segment's when there is no glyph. The
  // bar's count is grey and takes the accent on the current segment, as ink on
  // the accent's own contrast, since the segment is filled with the accent.
  const count = badge > 0 && (
    <span
      key={badge}
      className={`kl-count glim-num absolute rounded-[var(--radius-pill)] bg-carbon-textSub px-1 py-px text-meta font-semibold
        leading-none text-carbon-background [.glim-active_&]:bg-accentContrast [.glim-active_&]:text-accent
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
      {word && (
        <span data-bar-word className="max-w-full truncate px-px text-[length:var(--bar-word,var(--text-meta))]">
          {label}
        </span>
      )}
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
  mode: LabelMode;
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

/**
 * BarMore is the bar's last slot: everything the rail shows beyond the bar's
 * four places. It is filled while one of the pages behind it is open, and its
 * count is that of the event list, the one entry behind it that counts.
 */
function BarMore({
  hue,
  mode,
  current,
  unread,
  open,
  onOpen,
}: {
  hue: number;
  mode: LabelMode;
  current: boolean;
  unread: number;
  open: boolean;
  onOpen: () => void;
}) {
  const { t } = useT();
  const label = t('menu.more');
  const named = mode === 'glyph' || mode === 'hover';
  const tip = useTooltip<HTMLButtonElement>(named ? label : undefined);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <button
        type="button"
        aria-label={named ? label : undefined}
        aria-haspopup="dialog"
        aria-expanded={open}
        {...(named ? tipHoverProps : {})}
        onClick={onOpen}
        className={`${navHued} ${barSegment} ${current ? navActive : barIdle}`}
        style={hueVars(hue) as CSSProperties}
      >
        <BarBody icon={<IconMore />} label={label} mode={mode} current={current} badge={unread} />
      </button>
      {tip.node}
    </>
  );
}

/** A row of the More sheet: the entry's glyph tile, its name and its count. */
const sheetRow =
  'glim-row flex w-full items-center gap-3 rounded-[var(--radius-control)] px-2 py-2 text-start text-body text-carbon-text hover:bg-carbon-hover';

/**
 * PhoneBar is the rail at phone width: a card along the bottom, where a thumb
 * reaches it (GlimStone, "The bottom bar"). It keeps four places, and whatever
 * else the rail shows opens from More as a sheet, so a page pinned to the rail
 * never squeezes another word into the bar. Where the event list is all that
 * is left, its bell takes the slot itself. Every entry keeps the colour it has
 * in the rail. The bar's height is fixed, so a change of label mode never
 * moves the page above it.
 */
function PhoneBar({
  mode,
  locked,
  showInstances,
  showAccounts,
  needs,
  active,
  collected,
}: {
  mode: LabelMode;
  locked: boolean;
  showInstances: boolean;
  showAccounts: boolean;
  needs: number;
  active: number;
  collected: number;
}) {
  const { t, lang } = useT();
  const { pathname } = useLocation();
  const unread = useUnreadEvents();
  const [sheet, setSheet] = useState(false);
  const bar = useRef<HTMLElement>(null);
  // In hover mode the word moves with the page.
  useBarFit(bar, `${lang} ${mode} ${pathname}`);
  let hue = 0;
  const nextHue = () => hue++;

  const overview = nextHue();
  const downloads = nextHue();
  const collector = nextHue();
  const pinned = [
    ...(showInstances ? [{ to: '/instances', label: t('nav.instances'), icon: <IconInstances />, hue: nextHue() }] : []),
    ...(showAccounts ? [{ to: '/accounts', label: t('nav.accounts'), icon: <IconAccounts />, hue: nextHue() }] : []),
  ];
  const bell = nextHue();
  if (locked) nextHue();
  const settings = nextHue();
  const more = pinned.length > 0 || locked;

  return (
    // order-last draws it under the page while it stays ahead of the page in
    // the document, where the rail is.
    <nav ref={bar} className="relative order-last flex h-12 shrink-0 gap-0.5 rounded-[var(--radius-card)] bg-carbon-sidebar p-1">
      <BarItem to="/" end hue={overview} mode={mode} label={t('nav.overview')} icon={<IconDashboard />} badge={needs} />
      <BarItem to="/downloads" hue={downloads} mode={mode} label={t('nav.downloads')} icon={<IconDownloads />} badge={active} />
      <BarItem to="/collector" hue={collector} mode={mode} label={t('nav.collector')} icon={<IconCollector />} badge={collected} />
      <BarItem to="/settings" hue={settings} mode={mode} label={t('nav.settings')} icon={<IconSettings />} />
      {/* Behind More the bell draws no segment and stands its list on the bar. */}
      <EventBell hue={bell} bar={more ? 'behind' : 'segment'} />
      {more && (
        <BarMore
          hue={nextHue()}
          mode={mode}
          current={pinned.some((p) => pathname.startsWith(p.to))}
          unread={unread}
          open={sheet}
          onOpen={() => setSheet(true)}
        />
      )}
      {sheet && (
        <Modal
          title={t('menu.more')}
          onClose={() => setSheet(false)}
          footer={
            <>
              <Button kind="secondary" labelled icon={<IconClose />} title={t('common.close')} onClick={() => setSheet(false)} />
              {locked && (
                <Button kind="secondary" icon={<IconSignOut />} onClick={() => void signOut()}>
                  {t('auth.signOut')}
                </Button>
              )}
            </>
          }
        >
          <div className="flex flex-col gap-1">
            {pinned.map((p) => (
              <NavLink
                key={p.to}
                to={p.to}
                onClick={() => setSheet(false)}
                className={({ isActive }) => `${sheetRow} ${isActive ? 'glim-active' : ''}`}
              >
                <IconTile icon={p.icon} hue={p.hue} />
                <span className="min-w-0 flex-1 truncate">{p.label}</span>
                <IconChevronEnd aria-hidden width={14} height={14} className="shrink-0 text-carbon-textMuted rtl:-scale-x-100" />
              </NavLink>
            ))}
            <button
              type="button"
              className={sheetRow}
              onClick={() => {
                setSheet(false);
                setEventsPanelOpen(true);
              }}
            >
              <IconTile icon={<IconBell />} hue={bell} />
              <span className="min-w-0 flex-1 truncate">{t('events.title')}</span>
              {unread > 0 && (
                <span className="glim-num rounded-[var(--radius-pill)] bg-carbon-surface3 px-1.5 py-0.5 text-meta font-semibold leading-none text-carbon-text">
                  {unread > 99 ? '99+' : unread}
                </span>
              )}
              <IconChevronEnd aria-hidden width={14} height={14} className="shrink-0 text-carbon-textMuted rtl:-scale-x-100" />
            </button>
          </div>
        </Modal>
      )}
    </nav>
  );
}
