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
import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button, EmptyState, SectionTitle } from './ui';
import { navBase, navInactive, NavLabel } from './Sidebar';
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
    <button type="button" title={t('events.jump')} onClick={() => onJump(target)} className={`${shared} hover:bg-carbon-hover`}>
      {body}
    </button>
  );
}

export function EventBell() {
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
  const wrapRef = useRef<HTMLDivElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

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
    const onDown = (e: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) setEventsPanelOpen(false);
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

  return (
    <div ref={wrapRef} className="relative">
      <button
        type="button"
        aria-haspopup="dialog"
        aria-expanded={open}
        title={spoken}
        aria-label={centred || unread > 0 ? spoken : undefined}
        onClick={() => setEventsPanelOpen(!open)}
        // Not an Item: it navigates nowhere. It is still a row in the same rail
        // and follows the same four label modes, and text-start is here for the
        // reason the sign-out button below it carries it - a <button> centres
        // its own text where an <a> does not.
        className={`${navBase} ${navInactive} group w-full text-start ${centred ? 'justify-center' : 'gap-3'}`}
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

      {open && (
        <div
          role="dialog"
          aria-label={name}
          // bottom-0 start-full: it opens beside the rail and grows upward, so
          // a long list runs into the window rather than off the bottom of it.
          // Logical properties throughout, so the panel flips to the other side
          // of the rail in the Arabic and Hebrew locales without knowing it has.
          className="glim-card absolute bottom-0 start-full z-40 ms-2 flex w-[22rem] max-w-[calc(100vw-6rem)]
            flex-col gap-3 p-4"
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
              section badge above sits at `absolute -top-[11px]`, and an
              overflow on its positioning box shears the top half of it off.
              tabIndex={-1} because focus needs somewhere to land - the ring
              drops its oldest entry at three hundred, and if that entry is the
              row somebody had focused, its node unmounts and focus falls to
              <body>, which restarts the next Tab at the top of the document
              with the panel still open.
              Not virtualised, deliberately: three hundred plain rows render
              once when the panel opens, and a window here would break Tab order
              to save a cost nothing is paying. */}
          <div ref={scrollRef} tabIndex={-1} className="-mx-1 max-h-[60vh] overflow-y-auto px-1">
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
            <Button kind="danger" className="self-end px-2.5 text-xs" onClick={clearEvents}>
              {t('events.clear')}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
