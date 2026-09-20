// The sidebar bell and its event list: what this window has shown since the
// page loaded. No server route backs it (see lib/eventLog.ts), so the list and
// its hints never claim a complete record. It never requests notification
// permission; only lib/notify.ts does, on a press in settings.
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate } from 'react-router-dom';
import { Button, EmptyState, SectionTitle, useTooltip } from './ui';
import { navBase, navHued, navInactive, NavLabel } from './Sidebar';
import { hueVars, rainbowAt } from '../lib/appearance';
import type { CSSProperties } from 'react';
import { Tabs, type TabDef } from './Tabs';
import { useT, type TranslationKey } from '../lib/i18n';
import { useNavLabels } from '../lib/navLabels';
import { IconBell } from '../lib/icons';
import { fmtClock } from '../lib/format';
import { TONE_DOT, useToast } from '../lib/toast';
import { requestReveal } from '../lib/reveal';
import {
  CAPACITY,
  EVENT_FAMILIES,
  FAMILY_OF,
  clearEvents,
  markEventsSeen,
  setEventsPanelOpen,
  useEventLog,
  useEventsPanelOpen,
  useUnreadEvents,
  type EventFamily,
  type EventSubject,
  type LoggedEvent,
} from '../lib/eventLog';

const FAMILY_LABEL: Record<EventFamily, TranslationKey> = {
  downloads: 'events.kind.downloads',
  archives: 'events.kind.archives',
  captcha: 'events.kind.captcha',
  accounts: 'events.kind.accounts',
  actions: 'events.kind.actions',
};

const MARGIN = 8;

/**
 * placePanel positions the panel against the window, like ui.tsx's placeBubble.
 * It opens on the far side of the rail and flips only when the other side has
 * room. Anchored by `bottom`, it grows upward and needs no re-measuring when a
 * row arrives. Reading its width is safe because the width is declared.
 */
function placePanel(r: DOMRect, w: number, rtl: boolean): { left: number; bottom: number; maxHeight: number } {
  const vw = document.documentElement.clientWidth || window.innerWidth;
  const vh = document.documentElement.clientHeight || window.innerHeight;
  const after = r.right + MARGIN;
  const before = r.left - MARGIN - w;
  const first = rtl ? before : after;
  const other = rtl ? after : before;
  const fits = (x: number) => x >= MARGIN && x + w <= vw - MARGIN;
  const left = fits(first) || !fits(other) ? first : other;
  const bottom = Math.max(MARGIN, vh - r.bottom);
  return {
    left: Math.max(MARGIN, Math.min(vw - MARGIN - w, left)),
    bottom,
    maxHeight: vh - bottom - MARGIN,
  };
}

/**
 * EventRow is one line of the log. The tone colours only the dot, so a failure
 * stands out among hundreds of rows. A row is a button only when its event has
 * a target to jump to.
 */
function EventRow({ event, onJump }: { event: LoggedEvent; onJump: (target: EventSubject) => void }) {
  const { t } = useT();
  // Called unconditionally for the rules of hooks; used only by jumpable rows.
  const tip = useTooltip<HTMLButtonElement>(t('events.jump'));
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  const body = (
    <>
      <span className="glim-num shrink-0 text-[11px] leading-5 text-carbon-textMuted">
        {fmtClock(event.at)}
      </span>
      <span className={`mt-[7px] h-1.5 w-1.5 shrink-0 rounded-[var(--radius-pill)] ${TONE_DOT[event.tone]}`} />
      {/* dir="auto": the message usually carries a file name in any script. */}
      <span className="min-w-0 flex-1 break-words leading-5 text-carbon-text" dir="auto">
        {event.message}
      </span>
    </>
  );
  const shared = 'flex w-full items-start gap-2 rounded-[var(--radius-control)] px-2 py-1 text-start text-xs';
  const target = event.target;
  if (!target) return <div className={shared}>{body}</div>;
  return (
    <>
      <button
        type="button"
        // No aria-label: the visible text is the name, and the tip arrives as
        // aria-describedby.
        {...tipHoverProps}
        onClick={() => onJump(target)}
        className={`${shared} hover:bg-carbon-hover`}
      >
        {body}
      </button>
      {tip.node}
    </>
  );
}

/**
 * EventBell is the sidebar row that opens the event panel. It takes a rail
 * `hue` like every other row; the caller counts it so a hidden row leaves no
 * gap in the sequence.
 */
export function EventBell({ hue }: { hue: number }) {
  const { t } = useT();
  const { toast } = useToast();
  const navigate = useNavigate();
  const mode = useNavLabels();
  const events = useEventLog();
  const unread = useUnreadEvents();
  const open = useEventsPanelOpen();
  // Not persisted, so a stale filter cannot make the list look empty.
  const [families, setFamilies] = useState<ReadonlySet<EventFamily>>(new Set());
  // The anchor is the wrapper, since the button's ref belongs to the tooltip.
  const wrapRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ left: number; bottom: number; maxHeight: number } | null>(null);

  // Only an open panel marks the log read (see markEventsSeen), including events
  // that arrive while it is open. Keyed on the newest id, which is all
  // markEventsSeen reads.
  const newestId = events[0]?.id ?? 0;
  useEffect(() => {
    if (!open || newestId === 0) return;
    markEventsSeen();
  }, [open, newestId]);

  // Outside click and Escape close; the cleanup restores focus however the
  // panel closes.
  useEffect(() => {
    if (!open) return;
    const opener = document.activeElement as HTMLElement | null;
    const raf = requestAnimationFrame(() => scrollRef.current?.focus());
    // Both boxes, since the panel is portalled to <body>.
    const onDown = (e: MouseEvent) => {
      const at = e.target as Node;
      if (wrapRef.current?.contains(at) || panelRef.current?.contains(at)) return;
      setEventsPanelOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setEventsPanelOpen(false);
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      cancelAnimationFrame(raf);
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
      opener?.focus?.();
    };
  }, [open]);

  // Placed before paint, with a visibility guard for the unmeasured frame.
  // Resize re-places it; the full-height rail never scrolls under it.
  useLayoutEffect(() => {
    if (!open) return;
    const place = () => {
      const row = wrapRef.current;
      const panel = panelRef.current;
      if (!row || !panel) return;
      setPos(placePanel(row.getBoundingClientRect(), panel.offsetWidth, getComputedStyle(row).direction === 'rtl'));
    };
    place();
    window.addEventListener('resize', place);
    return () => {
      window.removeEventListener('resize', place);
      setPos(null);
    };
  }, [open]);

  const counts = useMemo(() => {
    const m = new Map<EventFamily, number>();
    for (const e of events) {
      const f = FAMILY_OF[e.kind];
      m.set(f, (m.get(f) ?? 0) + 1);
    }
    return m;
  }, [events]);

  // Only families with events, like offeredQuickFilters, since most toasts land
  // in Actions. An active family stays at zero so its chip does not vanish.
  const chips = useMemo<TabDef[]>(
    () =>
      EVENT_FAMILIES.filter((f) => (counts.get(f) ?? 0) > 0 || families.has(f)).map((f) => ({
        id: f,
        label: t(FAMILY_LABEL[f]),
        badge: counts.get(f) ?? 0,
      })),
    [counts, families, t],
  );

  // An empty selection means everything, so there is no "all" chip.
  const shown = useMemo(
    () => (families.size === 0 ? events : events.filter((e) => families.has(FAMILY_OF[e.kind]))),
    [events, families],
  );

  function toggleFamily(id: string): void {
    const family = EVENT_FAMILIES.find((f) => f === id);
    if (!family) return;
    setFamilies((prev) => {
      const next = new Set(prev);
      if (next.has(family)) next.delete(family);
      else next.add(family);
      return next;
    });
  }

  function jump(target: EventSubject): void {
    setEventsPanelOpen(false);
    // No ?instance=: every logged event came from the local socket.
    navigate('/downloads');
    // 'fail' so quiet mode cannot swallow the answer to a direct press.
    requestReveal(target.id, () => toast(t('events.jumpGone'), 'fail'));
  }

  // Centred in the same modes as Sidebar's Item.
  const centred = mode === 'glyph' || mode === 'hover';
  const name = t('events.title');
  // In the button's name rather than an aria-live region, which would announce
  // every event a third time.
  const spoken = unread > 0 ? `${name} (${t('events.unread', { n: unread })})` : t('events.open');
  // No tooltip in hover mode, where the row reveals its own label.
  const tip = useTooltip<HTMLButtonElement>(mode === 'hover' ? undefined : spoken);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;

  return (
    <div ref={wrapRef}>
      <button
        type="button"
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={centred || unread > 0 ? spoken : undefined}
        {...(mode === 'hover' ? {} : tipHoverProps)}
        onClick={() => setEventsPanelOpen(!open)}
        // Not an Item, since it navigates nowhere. text-start because a
        // <button> centres its text where an <a> does not.
        className={`${navHued} ${navBase} ${navInactive} group w-full text-start ${centred ? 'justify-center' : 'gap-3'}`}
        style={hueVars(rainbowAt(hue)) as CSSProperties}
      >
        {mode !== 'text' && <IconBell />}
        <NavLabel label={name} mode={mode} />
        {/* Sidebar Item's badge, pinned to the corner in the centred modes so
            the glyph stays centred. */}
        {unread > 0 && (
          <span
            className={`glim-num rounded-[var(--radius-pill)] bg-carbon-surface3/60 px-1.5 py-0.5 text-[11px]
              font-semibold leading-none text-carbon-textSub
              ${centred ? 'absolute end-1 top-1' : ''}`}
          >
            {unread > 99 ? '99+' : unread}
          </span>
        )}
      </button>
      {tip.node}

      {open &&
        createPortal(
          <div
            ref={panelRef}
            role="dialog"
            aria-label={name}
            // Portalled to <body>: inside the rail, whose overflow-hidden makes
            // it a scroller, focusing the list would scroll the rail sideways.
            style={{
              left: pos?.left ?? 0,
              bottom: pos?.bottom ?? 0,
              maxHeight: pos?.maxHeight,
              visibility: pos ? undefined : 'hidden',
            }}
            className="glim-card fixed z-40 flex w-[22rem] max-w-[calc(100vw-1rem)] flex-col gap-3 p-4"
          >
            {/* The bubble says the list starts over on every page load. */}
            <SectionTitle hint={t('events.titleHint', { max: CAPACITY })}>{name}</SectionTitle>

            {(chips.length > 1 || families.size > 0) && (
              <Tabs select="many" size="sm" label={t('events.filterLabel')} items={chips} active={families} onSelect={toggleFamily} />
            )}

            {/* The scroll sits here, not on the card, whose overflow would clip
                the title badge. min-h-0 lets it shrink within the capped card.
                tabIndex={-1} gives focus a place to land when the ring drops
                a focused row. Not virtualised: at most 300 plain rows. */}
            <div ref={scrollRef} tabIndex={-1} className="-mx-1 max-h-[60vh] min-h-0 overflow-y-auto px-1">
              {events.length === 0 ? (
                <EmptyState nested title={t('events.empty')} hint={t('events.emptyHint')} />
              ) : shown.length === 0 ? (
                <EmptyState nested title={t('events.noMatch')} />
              ) : (
                <div className="flex flex-col">
                  {shown.map((e) => (
                    <EventRow key={e.id} event={e} onJump={jump} />
                  ))}
                </div>
              )}
            </div>

            {/* No undo, so it sits at the foot, away from the rows. */}
            {events.length > 0 && (
              <Button kind="secondary" className="self-end px-2.5 text-xs" onClick={clearEvents}>
                {t('events.clear')}
              </Button>
            )}
          </div>,
          document.body,
        )}
    </div>
  );
}
