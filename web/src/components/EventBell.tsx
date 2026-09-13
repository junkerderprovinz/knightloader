// The bell in the sidebar, and the list behind it.
//
// WHAT IT PROMISES, because an interface that promises more than it can keep is
// worse than none: this is the log of what THIS WINDOW has told you since the
// page was loaded. There is no server route behind it (lib/eventLog.ts says why
// at length), so somebody who was away for two hours and reloaded sees an empty
// list, and the empty state has to read as "the list starts here", not as
// "nothing happened". That is what events.emptyHint says, and it is why the
// panel's title bubble carries the same sentence for the non-empty case. No
// "load older", no date range, no "since your last visit": every one of those
// would be a control standing over a question the server cannot answer.
//
// It also says nothing about the events it never saw. The socket reconnects on
// a timer (lib/api.ts) and a download that finished during a drop produces no
// bubble and therefore no entry. That silence is deliberate in the existing
// code; nothing here may word itself as a complete record.
//
// AND IT ASKS FOR NOTHING. A bell invites Notification.requestPermission(), and
// this component never calls it. The one place in this app that asks is
// lib/notify.ts, from a real press on the notifications settings card, for the
// reasons written down there - a prompt on first paint, on an origin that is
// usually plain HTTP on a LAN address, would be a permission dialog for a
// feature nobody asked for and a refusal that sticks for good.
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

/** The gap kept between the panel and the rail, and between it and the window. */
const MARGIN = 8;

/**
 * WHERE THE PANEL GOES, measured against the WINDOW rather than against the row
 * it hangs off.
 *
 * Clamp, then flip, with the same 8px the house's own bubble uses
 * (ui.tsx's placeBubble) - it opens on the far side of the rail, which is the
 * right in a left-to-right locale and the left in ar/he/fa, and it crosses to
 * the other side only when the preferred one would run off the edge AND the
 * other one genuinely has room. A flip that is unconditional trades one clipped
 * edge for the opposite one.
 *
 * `bottom`, not `top`: the panel grows UPWARD out of the bell, so a list that
 * gets longer while it is open stays pinned to the row it belongs to instead of
 * walking off the bottom of the window - and nothing has to re-measure when a
 * row arrives. maxHeight is whatever is left between that bottom edge and the
 * top margin, which is what keeps the head of a full list on screen without
 * measuring the panel's height at all.
 *
 * Measuring the panel's own width is legitimate here only because the width is
 * declared (`w-[22rem]`, capped in vw): a shrink-to-fit fixed box would resize
 * itself in response to the very `left` this computes, which is the trap
 * placeBubble's `width: max-content` notes for the tooltip.
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
 * One line of the log.
 *
 * The tone lives in the dot and not in the text. A bubble is one line somebody
 * is meant to notice, so it colours its own message; three hundred lines
 * coloured the same way is a wall nobody reads, and the one failure in it stops
 * standing out - which is the only reason to colour anything here at all.
 *
 * A row becomes a button only when there is somewhere to go. An event with no
 * target - a captcha, a benched account, a plain "saved" - has no row to jump
 * to, and dressing it as if it did is a press that lands nowhere.
 */
function EventRow({ event, onJump }: { event: LoggedEvent; onJump: (target: EventSubject) => void }) {
  const { t } = useT();
  // The house bubble, not the native `title` this row used to carry. One
  // control, one tooltip mechanism: the panel this row sits in already shows
  // the app's own bubble on its title badge, so a row underneath it drawing
  // the operating system's box instead reads as a rendering fault rather than
  // as a second style. Always called, Rules of Hooks; a row with nowhere to
  // jump never spreads the props and stays as inert as before.
  const tip = useTooltip<HTMLButtonElement>(t('events.jump'));
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  const body = (
    <>
      <span className="glim-num shrink-0 text-[11px] leading-5 text-carbon-textMuted">
        {fmtClock(event.at)}
      </span>
      <span className={`mt-[7px] h-1.5 w-1.5 shrink-0 rounded-[var(--radius-pill)] ${TONE_DOT[event.tone]}`} />
      {/* dir="auto": the message usually carries a file name, and a file name
          can be written in a right-to-left script as easily as a Latin one. */}
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
        // No aria-label: the row's own visible text (the clock and the
        // message) is its accessible name, and a label here would replace
        // that with the three words of the tip. The bubble reaches assistive
        // tech through the hook's own aria-describedby instead, which is what
        // that attribute is for - a description beside a name, not instead of
        // one.
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
 * hue, wie jede andere Zeile der Schiene.
 *
 * Die Glocke navigiert nirgendwohin, sie oeffnet eine Klappe - und genau deshalb
 * war sie die einzige Zeile ohne Farbposition, zusammen mit Abmelden. Auf dem
 * Bildschirm heisst das: fuenf farbige Symbole und zwei graue, ohne erkennbaren
 * Grund (jdp: "die glyphen von ereignisse und abmelden haben keine farbe"). Die
 * Sprache nimmt von der Regenbogenposition genau eine Art Zeile aus, naemlich
 * eine EINZELNE ihrer Art ohne Geschwister; eine Schienenzeile neben sechs
 * anderen Schienenzeilen ist das nicht.
 *
 * Die Zahl kommt vom Zaehler des Aufrufers und nicht von hier, damit eine
 * ausgeblendete Zeile keine Luecke in der Folge hinterlaesst.
 */
export function EventBell({ hue }: { hue: number }) {
  const { t } = useT();
  const { toast } = useToast();
  const navigate = useNavigate();
  const mode = useNavLabels();
  const events = useEventLog();
  const unread = useUnreadEvents();
  const open = useEventsPanelOpen();
  // Page state, not stored: which kinds you were looking at last week is not a
  // preference, and a filter that comes back on its own is a filter that makes
  // the list look empty for a reason nobody can see.
  const [families, setFamilies] = useState<ReadonlySet<EventFamily>>(new Set());
  // The row's own box, and the panel's anchor. Measured off the WRAPPER rather
  // than off the button, because the button's ref belongs to the tooltip hook
  // below - a second `ref` on the same element would quietly take that one's
  // place and leave the bubble unable to measure its own trigger. The wrapper
  // hugs the button exactly (one block child in a flex column), so the two
  // rects are the same rect.
  const wrapRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ left: number; bottom: number; maxHeight: number } | null>(null);

  // Opening the panel is what marks the log read, and it is the ONLY thing that
  // does - see markEventsSeen's own note on the three tempting places this must
  // not be called from. It keeps firing while the panel stays open, because an
  // event that arrives onto a list somebody is already looking at has been read
  // by the time it has finished appearing.
  //
  // Keyed on the newest id rather than on the array: markEventsSeen only moves
  // the mark to events[0], so nothing else about the list is worth re-running
  // for. It does not touch the array either, which is what stops this looping.
  const newestId = events[0]?.id ?? 0;
  useEffect(() => {
    if (!open || newestId === 0) return;
    markEventsSeen();
  }, [open, newestId]);

  // Outside click, Escape, and focus back where it came from - the same three
  // the command palette and the download list's own search popover already do.
  // The restore is the cleanup rather than a handler on each way out, so a
  // close by any route (including this row unmounting when the rail is hidden)
  // still leaves focus somewhere real.
  useEffect(() => {
    if (!open) return;
    const opener = document.activeElement as HTMLElement | null;
    const raf = requestAnimationFrame(() => scrollRef.current?.focus());
    // Both boxes, because the panel is no longer a descendant of the row: it
    // hangs off <body> (see the panel's own note), so a press inside it lands
    // outside wrapRef and would close the thing that was just being used.
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

  // Placed before the browser paints, which is what makes the unmeasured first
  // frame a non-event rather than a flash at the corner of the window. The
  // `visibility` guard on the panel is the belt the house's own bubble wears
  // for the same frame.
  //
  // Re-placed on a resize, and NOT on a scroll: the rail it hangs off cannot
  // scroll under it - it is a full-height column in a page that does not scroll
  // - so there is nothing to go stale. It does not need re-placing when a row
  // arrives either; see placePanel on why the bottom edge is the anchored one.
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
      // Dropped on close so the next opening measures again instead of
      // spending its first frame at the position of the last one.
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

  // Only the families that actually have something in them, exactly the rule
  // offeredQuickFilters already applies to the download list's own chips. It
  // matters more here than there: almost every call site in this app toasts
  // without naming a kind, so KIND_BY_TONE lands it in Actions, and Archives
  // and Accounts are usually empty. Four permanently empty chips do not read as
  // "nothing of that kind happened", they read as a filter that does not work.
  //
  // A family that is switched ON stays offered at zero, or turning a chip on
  // could make that chip disappear out from under the finger that pressed it.
  const chips = useMemo<TabDef[]>(
    () =>
      EVENT_FAMILIES.filter((f) => (counts.get(f) ?? 0) > 0 || families.has(f)).map((f) => ({
        id: f,
        label: t(FAMILY_LABEL[f]),
        badge: counts.get(f) ?? 0,
      })),
    [counts, families, t],
  );

  // An empty selection means everything, the same way the download list already
  // treats an empty filter set. There is deliberately no "all" chip beside the
  // five: a value that means "no opinion" is not one of the choices, and giving
  // it a name would put a sixth thing to press in a row of five.
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
    // A bare path, with no search string carried over. Every event in this log
    // was recorded off Layout's own local socket, so the row is always on THIS
    // machine - and keeping the current ?instance= would point the list at a
    // peer where that row does not exist and never will (lib/instance.tsx reads
    // the scope straight off the query string).
    navigate('/downloads');
    // The list answers by claiming the request; the deadline behind it is what
    // turns a download that has since been cleaned up into one sentence rather
    // than a press that appears to do nothing at all.
    //
    // 'fail', not 'info': quiet mode swallows a non-critical bubble, and a
    // direct press is the one thing that must always be answered. It is also
    // honest - somebody asked to be shown a row and cannot be.
    requestReveal(target.id, () => toast(t('events.jumpGone'), 'fail'));
  }

  // Centred whenever the label is not permanently drawn, the same two modes
  // Sidebar's own Item centres in - a bell that sat left-aligned among centred
  // glyphs would read as a row the display mode had missed.
  const centred = mode === 'glyph' || mode === 'hover';
  const name = t('events.title');
  // The count gets words, on the button. Deliberately NOT an aria-live region:
  // the bubble is already role="status" and StatusStrip is already
  // aria-live="polite", so a third announcement would read every event twice
  // and then count out loud after it.
  const spoken = unread > 0 ? `${name} (${t('events.unread', { n: unread })})` : t('events.open');
  // The house bubble instead of the browser's own box, and NOT in hover mode:
  // there the row reveals its own words under the pointer, and a bubble saying
  // the same thing on top of them is the same information twice. The rail's
  // links next door follow the identical rule - see Sidebar.tsx's Item.
  const tip = useTooltip<HTMLButtonElement>(mode === 'hover' ? undefined : spoken);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;

  return (
    // No `relative` on this box any more: nothing is positioned against it now
    // that the panel is placed against the window, and the unread badge below
    // hangs off the button's own `relative` (navBase), not off this.
    <div ref={wrapRef}>
      <button
        type="button"
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={centred || unread > 0 ? spoken : undefined}
        {...(mode === 'hover' ? {} : tipHoverProps)}
        onClick={() => setEventsPanelOpen(!open)}
        // Not an Item: it navigates nowhere. It is still a row in the same rail
        // and follows the same four label modes, and text-start is here for the
        // reason the sign-out button below it carries it - a <button> centres
        // its own text where an <a> does not.
        className={`${navHued} ${navBase} ${navInactive} group w-full text-start ${centred ? 'justify-center' : 'gap-3'}`}
        style={hueVars(rainbowAt(hue)) as CSSProperties}
      >
        {mode !== 'text' && <IconBell />}
        <NavLabel label={name} mode={mode} />
        {/* Structurally the badge from Sidebar's own Item, corner-pinned in the
            two centred modes for the same reason: it is the one thing in this
            rail somebody watches without reading, and left in the flow it would
            push the resting glyph off centre by its own width.
            99+ rather than a number that widens the row: past a hundred the
            exact figure has stopped being information. */}
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
            // IT HANGS OFF <body>, AND THAT IS THE WHOLE FIX.
            //
            // Positioned in the tree it was `absolute bottom-0 start-full`
            // beside the row, which held for exactly as long as nothing above
            // it clipped. The rail now carries `overflow-hidden` (Sidebar.tsx,
            // so the brand mark cannot cross its rounded corner), and an
            // overflow box is a clip AND a scroller: a 352px panel beside a
            // 224px rail pushed that rail's scrollWidth to 572, and opening the
            // panel focuses its list, so the browser scrolled the RAIL to reach
            // the focus (scrollLeft 284, measured). What was left on screen was
            // a 224px strip out of the MIDDLE of the panel - the title badge
            // reading "NISSE", a sentence stopping mid-word, "Liste" where the
            // button says "Liste leeren" - with the rail's own logo and rows
            // shoved out of frame to the left. One cause, two cut edges;
            // widening anything would have moved the cuts, not removed them.
            //
            // At body level nothing clips it, which is the same reason
            // ContextMenu, ColumnMenu and the house bubble all render there.
            // The price is that the panel is no longer inside the row for an
            // outside-press test, which is why that handler checks both boxes.
            //
            // The placement is measured (placePanel) rather than expressed in
            // logical properties, so the side it opens on still follows the
            // writing direction - the far side of the rail in both, read off
            // the row's own computed `direction`.
            style={{
              left: pos?.left ?? 0,
              bottom: pos?.bottom ?? 0,
              maxHeight: pos?.maxHeight,
              visibility: pos ? undefined : 'hidden',
            }}
            className="glim-card fixed z-40 flex w-[22rem] max-w-[calc(100vw-1rem)] flex-col gap-3 p-4"
          >
            {/* The one SectionTitle this card gets, and its bubble is where the
                honesty lives: what the list holds, how much of it, and that it
                starts over every time the page is loaded. That sentence belongs
                in the bubble rather than as a line of prose over the rows, where
                it would be read once and cost that space for ever. */}
            <SectionTitle hint={t('events.titleHint', { max: CAPACITY })}>{name}</SectionTitle>

            {(chips.length > 1 || families.size > 0) && (
              <Tabs select="many" size="sm" label={t('events.filterLabel')} items={chips} active={families} onSelect={toggleFamily} />
            )}

            {/* The scroll lives on this INNER box and never on the card: the
                section badge above straddles the card's own top edge (`top-0`
                plus a self-relative `-translate-y-1/2`), and an overflow on its
                positioning box shears the half that hangs over it off.
                min-h-0 so it is the part that gives when the card runs out of
                room: the card is capped at the space between the bell and the
                top of the window (placePanel), and a flex item will not shrink
                below its own content height without it - which on a short
                window would push the head of the list off the top edge instead
                of scrolling it here.
                tabIndex={-1} because focus needs somewhere to land - the ring
                drops its oldest entry at three hundred, and if that entry is the
                row somebody had focused, its node unmounts and focus falls to
                <body>, which restarts the next Tab at the top of the document
                with the panel still open.
                Not virtualised, deliberately: three hundred plain rows render
                once when the panel opens, and a window here would break Tab order
                to save a cost nothing is paying. */}
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

            {/* The one destructive control in here, with no undo behind it, so it
                stands at the foot of the panel where nothing else is pressed
                rather than anywhere a click aimed at a row could reach it. Absent
                entirely while there is nothing to empty. */}
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
