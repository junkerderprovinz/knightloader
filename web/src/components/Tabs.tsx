// The one horizontal chooser in the app.
//
// There were two of these: the download list's quick-filter chips and the
// settings rail, built a week apart out of the same three strings and drifting
// ever since — different padding, different keyboard behaviour, one of them
// scrolling and the other not. "GlimStone soll überall gleich sein" is not a
// rule about how things look, it is a rule about how many of them there are.
//
// So: one component, two jobs.
//
//   select="one"   pick exactly one — the settings pages, a view switch
//   select="many"  toggle several   — the quick filters, where two filters on
//                                     means "show me both kinds"
//
// It is built FROM segBase/segOn/segOff in ui.tsx rather than from copies of
// them, so a tab, a chip and a segment cannot come apart again: the chosen one
// is FILLED with the accent, everywhere, and in rainbow mode it is filled with
// its own palette colour instead.
//
// Someone arriving from JDownloader meets a tab strip where they expect one and
// the arrow keys do what Swing's tabs do: move along the strip and take the
// selection with them.
import { useEffect, useRef, useState, type KeyboardEvent, type MouseEvent, type PointerEvent, type ReactNode } from 'react';
import { useRainbow } from '../lib/useRainbow';
import { hueStyle, segBase, segOff, segOn } from './ui';

export interface TabDef {
  /** Stable id — what onSelect hands back, and the route segment where there is one. */
  id: string;
  label: string;
  /** 16×16 glyph, drawn before the label. In rainbow mode it carries the tab's own hue. */
  icon?: ReactNode;
  /** A count or short mark after the label. Quiet by default; ink on the filled tab. */
  badge?: ReactNode;
  /** Reachable but visibly empty — a settings page that has no controls yet. */
  dim?: boolean;
  /**
   * Makes the tab a real link. Middle-click and Ctrl+click then open it in a
   * new tab as anyone would expect from something that looks like navigation;
   * a plain click is still handed to onSelect, so the router keeps the page.
   */
  href?: string;
  /** Native tooltip, for a label that may be truncated. */
  title?: string;
}

interface Common {
  items: TabDef[];
  /** Accessible name for the strip: "Settings sections", "Quick filters". */
  label: string;
  /**
   * `md` is a page-level tab, `sm` a filter chip. One treatment, two weights —
   * a chip above a list must not read as loud as the page's own navigation.
   */
  size?: 'sm' | 'md';
  /**
   * Whether an arrow key selects as it moves, or only moves focus.
   *
   * Defaults to true for select="one", which is what a JTabbedPane does and
   * what the muscle memory expects. Pass false where selecting is expensive or
   * pushes a history entry the user did not ask for.
   */
  activateOnFocus?: boolean;
  /** Lives inside the strip, after the tabs — the "show everything" reset. */
  after?: ReactNode;
  className?: string;
  /**
   * Opt-in long-press-to-reorder (jdp: "die Tabs in den Einstellungen soll
   * man nach Belieben anordnen können" - then, after seeing the first cut,
   * "beim mouseover erscheint die Hand, das soll nicht so sein, erst bei
   * langem gedrückten Klick sollen die Tabs anfangen zu wackeln, wie in
   * CC"). Off by default so every OTHER caller - the download list's quick
   * filters, the corner/shape picker - is completely unaffected; only a
   * caller that passes both `reorderable` and `onReorder` gets this. A tab
   * is a plain clickable control at rest (no grab cursor, no draggable
   * affordance) - only a ~300ms hold with under 8px of movement arms
   * reorder mode, at which point every tab starts to wiggle (the same
   * gesture CannonadeCommand's own nav rail uses) and the held tab can be
   * dragged into a new position in the same, continuous gesture. Reordering
   * never changes `active`: the caller decides what that means (it does
   * not, for Settings - moving a tab does not navigate to it).
   */
  reorderable?: boolean;
  /** Called with the full, reordered list of ids after a drop. */
  onReorder?: (ids: string[]) => void;
  /**
   * Every tab sized to the longest label in the set, not to its own content
   * (GlimStone: "a strip where each tab hugs its own text reads as a ransom
   * note"). Opt-in - a strip of quick-filter chips (varying label lengths by
   * design, e.g. the download list's "Fertig"/"Fehlgeschlagen") stays
   * content-sized; a page-navigation strip like Settings' own tabs does not.
   * Implemented as a shared `ch`-based min-width (character-count, not a
   * measured pixel width) rather than equal flex-basis, so it still wraps
   * correctly at narrow viewports instead of forcing one unbroken row.
   */
  equalWidth?: boolean;
  /**
   * `'well'` is a second, opt-in styling of this same component (GlimStone:
   * "A second styling: the well... First built for BombVault's shape
   * picker") for a genuinely EXCLUSIVE, small, closely-related set - three
   * shape options, an on/off pair - as opposed to the default's row of
   * independent badges. All segments share one padded track
   * (`bg-carbon-surface2`, `p-[0.2rem]`/`gap-[0.2rem]`) instead of each
   * carrying its own resting fill, split evenly (`flex-1`) rather than
   * sized to content or to `equalWidth`'s longest-label measurement.
   * Confirmed live off the real BombVault test container's own Corners
   * and Design pickers, not the repo/docs. Defaults to `'default'`.
   */
  variant?: 'default' | 'well';
}

export type TabsProps =
  | (Common & { select?: 'one'; active: string | null; onSelect: (id: string) => void })
  | (Common & { select: 'many'; active: ReadonlySet<string>; onSelect: (id: string) => void });

const SIZE = {
  sm: 'gap-1.5 px-2.5 py-1 text-xs',
  md: 'gap-2 px-3 py-2 text-[13px]',
} as const;

export function Tabs(props: TabsProps) {
  const {
    items,
    label,
    size = 'md',
    after,
    className = '',
    reorderable = false,
    onReorder,
    equalWidth = false,
    variant = 'default',
  } = props;
  const isWell = variant === 'well';

  // Subscribed, not read: the palette is resolved during render, so a strip
  // that only learned about a change on the next paint would keep the previous
  // colours. This is what makes the Look page's own corner picker recolour
  // while the palette below it is being edited — and it is the reason the
  // subscription lives HERE and not in every caller, who would each have to
  // discover the same thing.
  useRainbow();

  // Read out of the union once, so nothing below has to narrow it again.
  const many = props.select === 'many';
  const chosen = props.select === 'many' ? props.active : null;
  const only = props.select === 'many' ? null : props.active;
  const { onSelect } = props;
  const auto = props.activateOnFocus ?? !many;

  const strip = useRef<HTMLDivElement>(null);
  const isOn = (id: string) => (chosen ? chosen.has(id) : only === id);

  function tabNodes(): HTMLElement[] {
    return Array.from(strip.current?.querySelectorAll<HTMLElement>('[data-tab-id]') ?? []);
  }

  // --- Long-press-to-wiggle reorder (ported from CannonadeCommand's own nav
  // rail - Pointer Events + a hold timer + manual position swapping, no HTML5
  // draggable and no library). liveOrder is the working copy shown WHILE a
  // drag is in progress (state, so the strip re-renders as tabs swap places);
  // null the rest of the time, when `items`' own order is authoritative.
  // Everything else below is refs: none of it should re-render the strip on
  // its own, only liveOrder/reordering changing does.
  const [reordering, setReordering] = useState(false);
  const [draggingId, setDraggingId] = useState<string | null>(null);
  const [liveOrder, setLiveOrder] = useState<string[] | null>(null);
  const holdTimer = useRef<number | null>(null);
  const pressStart = useRef<{ x: number; y: number } | null>(null);
  const pressId = useRef<string | null>(null);
  const pressPointerId = useRef<number | null>(null);
  const moved = useRef(false);
  const suppressClick = useRef(false);

  const orderedItems = liveOrder ? liveOrder.map((id) => items.find((i) => i.id === id)).filter((i): i is TabDef => !!i) : items;

  // `ch` (the current font's own "0"-glyph width), not a measured pixel
  // value - no ResizeObserver, no layout effect, just a CSS unit that is
  // already text-metric-aware. +4 covers the icon, the icon-to-label gap and
  // the tab's own horizontal padding, which none of the label characters
  // themselves account for.
  const maxLabelLen = equalWidth ? Math.max(0, ...items.map((i) => i.label.length)) : 0;

  // Mirrors of the state above, read by the document-level listeners further
  // down: those bind once (empty-ish dependency array) rather than re-binding
  // on every state change, so they read the CURRENT value through a ref
  // instead of closing over a stale one from whichever render attached them.
  const draggingIdRef = useRef<string | null>(null);
  draggingIdRef.current = draggingId;
  const reorderingRef = useRef(false);
  reorderingRef.current = reordering;
  const liveOrderRef = useRef<string[] | null>(null);
  liveOrderRef.current = liveOrder;

  function cancelHold() {
    if (holdTimer.current !== null) {
      window.clearTimeout(holdTimer.current);
      holdTimer.current = null;
    }
    pressStart.current = null;
    pressId.current = null;
  }

  function exitReorder() {
    setReordering(false);
    setDraggingId(null);
    pressPointerId.current = null;
    moved.current = false;
  }

  function onTabPointerDown(e: PointerEvent<HTMLElement>, id: string) {
    if (!reorderable || e.button !== 0) return;
    cancelHold();
    pressStart.current = { x: e.clientX, y: e.clientY };
    pressId.current = id;
    pressPointerId.current = e.pointerId;
    moved.current = false;
    // A held-open drop target or a live insertion-line is more machinery
    // than reordering four to twelve settings tabs needs - the CC pattern's
    // own answer is the same: the strip simply wiggles, and the held tab
    // slides other tabs aside as it passes over them.
    // No setPointerCapture: capturing the press target and then moving past
    // its own bounds triggered a spurious pointercancel in testing (Chromium,
    // via CDP-dispatched input at least, possibly a broader interaction with
    // a captured <button>/<a> - browsers vary here). Document-level move/up
    // listeners already track the gesture regardless of which element the
    // pointer is physically over, so capture was never load-bearing; it's
    // simply not worth whatever platform-specific cancel behaviour it invites.
    holdTimer.current = window.setTimeout(() => {
      holdTimer.current = null;
      setReordering(true);
      setDraggingId(id);
      setLiveOrder(items.map((i) => i.id));
    }, 300);
  }

  // Document-level, bound once (not per tab): the same reasons
  // GlobalIntake.tsx's own whole-window listeners are document-level - a
  // pointer that has left the pressed element entirely, mid-drag, must
  // still be tracked.
  useEffect(() => {
    if (!reorderable) return;

    function onMove(e: globalThis.PointerEvent) {
      if (pressStart.current && !draggingIdRef.current) {
        // Still deciding whether this is a hold or a click/scroll: a real
        // move before the timer fires cancels the hold outright, the same
        // 8px tolerance CC's own version uses.
        if (Math.abs(e.clientX - pressStart.current.x) > 8 || Math.abs(e.clientY - pressStart.current.y) > 8) {
          cancelHold();
        }
        return;
      }
      const dragging = draggingIdRef.current;
      if (!dragging) return;
      moved.current = true;
      const nodes = tabNodes();
      for (const node of nodes) {
        const id = node.getAttribute('data-tab-id');
        if (!id || id === dragging) continue;
        const r = node.getBoundingClientRect();
        // The +/-20 vertical fudge (CC's own figure) forgives a slightly
        // sloppy hold on a strip that wraps to more than one line.
        if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top - 20 || e.clientY > r.bottom + 20) continue;
        setLiveOrder((prev) => {
          if (!prev) return prev;
          const from = prev.indexOf(dragging);
          const to = prev.indexOf(id);
          if (from < 0 || to < 0 || from === to) return prev;
          const next = [...prev];
          next.splice(from, 1);
          next.splice(to, 0, dragging);
          return next;
        });
        break;
      }
    }

    function onUp() {
      const wasReordering = reorderingRef.current;
      const finalOrder = liveOrderRef.current;
      const didMove = moved.current;
      cancelHold();
      if (wasReordering && finalOrder && onReorder) onReorder(finalOrder);
      if (wasReordering && !didMove) {
        // Armed (held past 300ms) but the pointer never actually moved
        // anywhere - the browser still fires a click right after this
        // pointerup, and without suppressing it a plain long-press-then-
        // release would also select the tab.
        suppressClick.current = true;
      }
      if (wasReordering) exitReorder();
    }

    function onEscape(e: globalThis.KeyboardEvent) {
      if (e.key === 'Escape' && reorderingRef.current) {
        cancelHold();
        exitReorder();
      }
    }

    document.addEventListener('pointermove', onMove);
    document.addEventListener('pointerup', onUp);
    document.addEventListener('pointercancel', onUp);
    document.addEventListener('keydown', onEscape);
    return () => {
      document.removeEventListener('pointermove', onMove);
      document.removeEventListener('pointerup', onUp);
      document.removeEventListener('pointercancel', onUp);
      document.removeEventListener('keydown', onEscape);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- the refs above
    // are read for their CURRENT value inside the listeners, not captured by
    // this effect's own closure; only `reorderable`/`onReorder` (identity)
    // ever need this effect to re-bind.
  }, [reorderable, onReorder]);

  // Roving tabindex: the strip is ONE stop in the tab order and the arrows move
  // inside it. Tabbing through thirteen settings pages to reach the page is how
  // a keyboard user learns to stop using the keyboard.
  const roved = Math.max(
    0,
    orderedItems.findIndex((i) => isOn(i.id)),
  );

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    // Space on an <a> scrolls the page instead of activating it, so the one
    // key an anchor does not handle for us is handled here.
    if (e.key === ' ' && e.target instanceof HTMLAnchorElement) {
      const id = e.target.getAttribute('data-tab-id');
      if (id) {
        e.preventDefault();
        onSelect(id);
      }
      return;
    }

    if (!['ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(e.key)) return;
    const nodes = tabNodes();
    if (nodes.length === 0) return;

    // Right means "further along the strip", which in Arabic and Hebrew is to
    // the left. Read the direction off the strip rather than assuming it, so
    // the same component behaves in an RTL locale the way that locale reads.
    const rtl = strip.current ? getComputedStyle(strip.current).direction === 'rtl' : false;
    const step = e.key === 'ArrowRight' ? (rtl ? -1 : 1) : e.key === 'ArrowLeft' ? (rtl ? 1 : -1) : 0;

    const here = nodes.indexOf(document.activeElement as HTMLElement);
    const next =
      e.key === 'Home'
        ? 0
        : e.key === 'End'
          ? nodes.length - 1
          : here < 0
            ? 0
            : (here + step + nodes.length) % nodes.length;

    e.preventDefault();
    const node = nodes[next];
    node?.focus();
    const id = node?.getAttribute('data-tab-id');
    if (auto && id) onSelect(id);
  }

  function onClick(e: MouseEvent<HTMLElement>, item: TabDef) {
    // A long-press that armed reorder mode but never actually moved still
    // ends in a click once the pointer lifts - without this, holding a tab
    // for 300ms and letting go in place would both wiggle it AND select it.
    if (suppressClick.current) {
      suppressClick.current = false;
      e.preventDefault();
      e.stopPropagation();
      return;
    }
    // A modified click on a link belongs to the browser: this is the gesture
    // that opens a settings page in a second window, and swallowing it is the
    // reason people stop trusting things that look like links.
    if (item.href && (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0)) return;
    e.preventDefault();
    onSelect(item.id);
  }

  return (
    <div
      ref={strip}
      role={many ? 'group' : 'tablist'}
      aria-label={label}
      aria-orientation="horizontal"
      onKeyDown={onKeyDown}
      // No well behind the tabs, and no padding: the tabs are the furniture.
      // Wrapped in a surface they read as a toolbar sitting on the page, which
      // is one more box than the page needs — the filled tab already says which
      // one is chosen, and a container around it says nothing at all. The gap
      // grows to carry the separation the box used to.
      //
      // Wraps, never scrolls. A strip that scrolls hides its own contents
      // behind a gesture nobody makes on a desktop, and a strip that grows
      // wider than its column puts the whole page into a horizontal scroll —
      // which is the one thing a tab bar across the top must never do.
      //
      // The well variant is the one exception to "no wrapping bar" - its
      // whole point is one shared track behind a small, exclusive set.
      className={
        isWell
          ? `flex flex-nowrap items-center gap-[0.2rem] rounded-[var(--radius-control)] bg-carbon-surface2 p-[0.2rem] ${className}`
          : `flex flex-wrap items-center gap-1 ${className}`
      }
    >
      {orderedItems.map((item, i) => {
        const on = isOn(item.id);
        const wiggling = reordering && item.id !== draggingId;
        const dragged = item.id === draggingId;
        const cls = isWell
          ? // No min-w-0 here, unlike the default variant below: this well
            // sits inside a `w-fit` track (jdp: "die horizontalen Selektoren
            // nicht die ganze Card-Breite - exakt so breit wie in BV"), and
            // min-w-0 lets a flex-basis-0 item's content shrink below its
            // own label's width when the BROWSER computes that track's
            // fit-content size - "Leicht"/"Dunkel" rendered as "Lei…"/"Du…"
            // the moment the track stopped stretching full-width. The
            // default (browser-native) min-width:auto keeps each item's
            // min-content size in that computation, so the track ends up
            // exactly as wide as its labels need, never narrower.
            `${segBase} glim-hue glim-hue-icon flex-1 justify-center text-center gap-2 py-1.5 text-sm
              ${on ? 'glim-active bg-accent text-accentContrast' : 'bg-transparent text-carbon-textSub hover:bg-carbon-hover hover:text-carbon-text'}
              flex items-center ${!on && item.dim ? 'opacity-60' : ''}`
          : `${segBase} glim-hue glim-hue-icon ${on ? `glim-active ${segOn}` : segOff} ${
              SIZE[size]
            } flex min-w-0 max-w-full items-center ${!on && item.dim ? 'opacity-60' : ''}
              ${wiggling ? 'glim-tab-wiggle' : ''} ${dragged ? 'glim-tab-dragging' : ''}`;

        const inner = (
          <>
            {item.icon}
            <span className="truncate">{item.label}</span>
            {item.badge !== undefined && item.badge !== null && (
              // Quiet beside an unselected tab; on the filled one it sits on the
              // accent itself, so it borrows the ink rather than keeping a
              // surface tint that would be invisible there.
              <span
                className="glim-num rounded-[var(--radius-pill)] px-1 text-[11px] font-semibold leading-none
                  text-carbon-textMuted [.glim-active_&]:bg-black/15 [.glim-active_&]:text-current"
              >
                {item.badge}
              </span>
            )}
          </>
        );

        const shared = {
          'data-tab-id': item.id,
          title: item.title,
          tabIndex: i === roved ? 0 : -1,
          style: equalWidth ? { ...hueStyle(i), minWidth: `${maxLabelLen + 4}ch`, justifyContent: 'center' as const } : hueStyle(i),
          // No grab cursor, no draggable affordance at rest - a tab is a
          // plain clickable control until a hold arms reorder mode (jdp:
          // "beim mouseover erscheint die Hand, das soll nicht so sein").
          className: cls,
          // A plain <a href> is natively draggable in every browser (the
          // "drag this link to a bookmarks bar" gesture) - moving the
          // pointer while pressed on one starts that native drag instead of
          // (or as well as) the long-press gesture above, and starting it
          // fires a real `pointercancel`, which this component's own
          // pointercancel listener (further down) reads as "gesture over"
          // and exits reorder mode the instant the pointer moves at all.
          // Caught empirically: the hold armed correctly every time, but
          // reorder mode dropped out on the very first pointermove past it.
          draggable: reorderable ? false : undefined,
          onPointerDown: reorderable ? (e: PointerEvent<HTMLElement>) => onTabPointerDown(e, item.id) : undefined,
          onClick: (e: MouseEvent<HTMLElement>) => onClick(e, item),
        };

        return item.href ? (
          <a
            key={item.id}
            href={item.href}
            role={many ? undefined : 'tab'}
            aria-selected={many ? undefined : on}
            aria-current={on ? 'page' : undefined}
            {...shared}
          >
            {inner}
          </a>
        ) : (
          <button
            key={item.id}
            type="button"
            role={many ? undefined : 'tab'}
            aria-selected={many ? undefined : on}
            aria-pressed={many ? on : undefined}
            {...shared}
          >
            {inner}
          </button>
        );
      })}
      {after}
    </div>
  );
}
