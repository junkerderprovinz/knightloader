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
import type { NavLabelMode } from '../lib/navLabels';
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
   * Externally-driven reorder mode (jdp, 2026-08-23: replaces the InfoBubble
   * hint at the end of the strip with a pencil button - clicking it should
   * wiggle every tab immediately, no long-press needed). While true: every
   * tab wiggles right away, and a plain pointerdown on one starts dragging it
   * instantly instead of arming the usual 300ms hold timer first. A click
   * never selects while this is on - there is no safe way to tell "picking
   * this tab up" from "tapping it to navigate" once every tab is already
   * primed to be dragged. Requires `reorderable`/`onReorder` the same as the
   * long-press path; the two triggers share the rest of the machinery below.
   */
  editMode?: boolean;
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
  /**
   * Which way the strip runs. `'vertical'` is KnightLoader's settings rail
   * and nothing else (jdp, 2026-08-27: "Alle Einstellungstabs werden rechts
   * von der Sidebar vertikal in kacheln angezeigt", following JD
   * Highlighter's own arrangement) - every other caller leaves this alone and
   * is untouched by it.
   *
   * It is a mode of THIS component rather than a second one beside it for the
   * reason this file opens with: there were two horizontal choosers once, and
   * they drifted. A vertical copy would carry its own duplicate of the
   * long-press reorder gesture, the roving tabindex and the rainbow wiring,
   * and would drift from all three the same way.
   */
  orientation?: 'horizontal' | 'vertical';
  /**
   * Vertical only: the tabs share the strip's full height between them
   * instead of each hugging its own content (jdp: "die kacheln sollen sich
   * immer von ganz oben bis ganz nach unten in einer spalte anordnen"). Each
   * keeps a floor height, so a long enough list scrolls rather than
   * collapsing into a row of unreadable slivers.
   */
  fill?: boolean;
  /**
   * How much of each tab is drawn - see lib/navLabels.ts. Defaults to `both`,
   * which is what every caller that does not pass it has always rendered.
   */
  display?: NavLabelMode;
}

export type TabsProps =
  | (Common & { select?: 'one'; active: string | null; onSelect: (id: string) => void })
  | (Common & { select: 'many'; active: ReadonlySet<string>; onSelect: (id: string) => void });

const SIZE = {
  sm: 'gap-1.5 px-2.5 py-1 text-xs',
  md: 'gap-2 px-3 py-2 text-[13px]',
} as const;

// The well variant's own gap/padding/text, kept separate from SIZE above
// (its md does not match SIZE.md - a well segment already reads bigger
// than a chip at the same nominal stage, so the two were never the same
// numbers) and gated on `size` for the first time here: every existing
// well caller (Look.tsx's shape/theme pickers, Archives.tsx) leaves `size`
// unset, defaults to 'md', and gets the exact classes this replaces -
// pixel-identical, nothing to re-verify there. `sm` reuses SIZE.sm's own
// values (BombVault's Selector establishes the same idea - a well
// segment's size stage still varies gap/text even though its box comes
// from a fixed height rather than padding math - this codebase's own
// existing chip-sm numbers are the in-house-consistent choice over
// copying BombVault's literal ones) for a genuinely smaller track, used by
// TaskProperties' priority/auto-extract selectors (jdp, 2026-08-25: "Die
// horizontalen Selektoren sollen kleiner sein... kleinere Größe in BV und
// Glimstone etabliert").
const WELL_SIZE: Record<'sm' | 'md', string> = {
  sm: 'gap-1.5 px-2.5 py-1 text-xs',
  md: 'gap-2 px-3 py-1.5 text-sm',
};

export function Tabs(props: TabsProps) {
  const {
    items,
    label,
    size = 'md',
    after,
    className = '',
    reorderable = false,
    onReorder,
    editMode = false,
    equalWidth = false,
    variant = 'default',
    orientation = 'horizontal',
    fill = false,
    display = 'both',
  } = props;
  const isWell = variant === 'well';
  const vertical = orientation === 'vertical';

  // What each tab actually draws. `hover` renders BOTH, and hides the label
  // with CSS rather than leaving it out - that is the whole mechanism: a
  // label that is present but collapsed can grow back in place, and nothing
  // around it has to be re-measured. See NavLabelMode's own doc comment for
  // why "nothing resizes" is the requirement here rather than a nicety.
  const showIcon = display !== 'text';
  const labelOnHover = display === 'hover';
  const showLabel = display === 'both' || display === 'text' || labelOnHover;
  // Glyph-only draws nothing anybody can read, so the label becomes the
  // accessible name and the tooltip instead of the visible text. The other
  // three modes have the label right there in the markup and adding a second
  // copy of it would only make a screen reader say it twice.
  const nameOnly = display === 'glyph';

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

  // Every well segment's own width: a flat 200px for `md` (unchanged -
  // Look.tsx/Archives.tsx's 2-3 item pickers, where a content-hugging width
  // was tried and explicitly rejected: "much too narrow" for a short label
  // like "Rund"). `sm` uses the same ch-based measurement equalWidth uses
  // above instead - introduced for TaskProperties' seven-item priority
  // selector, where inheriting the SAME flat 200px (built for 2-3 items)
  // is the reason it needed a horizontal scrollbar to hold them at all;
  // `md`'s own callers never had that problem, so their fixed width is
  // untouched.
  const wellWidth = !isWell ? undefined : size === 'sm' ? `${Math.max(0, ...items.map((i) => i.label.length)) + 4}ch` : '200px';

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
    // Edit mode: every tab is already wiggling, so a press picks one up
    // immediately - no hold timer, the strip is already primed.
    if (editMode) {
      setReordering(true);
      setDraggingId(id);
      setLiveOrder((prev) => prev ?? items.map((i) => i.id));
      return;
    }
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
        // Tight across the strip's own axis, forgiving across the other: the
        // +/-20 (CC's own figure) covers a slightly sloppy hold on a
        // horizontal strip that has wrapped to a second line, and a pointer
        // that has wandered out past the edge of a vertical one. Transposed
        // with the orientation, so each axis keeps the job it was given.
        const along = vertical
          ? e.clientY >= r.top && e.clientY <= r.bottom
          : e.clientX >= r.left && e.clientX <= r.right;
        const across = vertical
          ? e.clientX >= r.left - 20 && e.clientX <= r.right + 20
          : e.clientY >= r.top - 20 && e.clientY <= r.bottom + 20;
        if (!along || !across) continue;
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
    // and `vertical` (captured by onMove's hit test, and cheap to re-bind on)
    // ever need this effect to re-bind.
  }, [reorderable, onReorder, vertical]);

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

    // The pair of arrows that means "along the strip" is the pair that points
    // the way the strip runs. A vertical rail answering Left/Right and
    // ignoring Down is the sort of thing that reads as broken rather than as
    // unsupported.
    const [fwd, back] = vertical ? ['ArrowDown', 'ArrowUp'] : ['ArrowRight', 'ArrowLeft'];
    if (![fwd, back, 'Home', 'End'].includes(e.key)) return;
    const nodes = tabNodes();
    if (nodes.length === 0) return;

    // Right means "further along the strip", which in Arabic and Hebrew is to
    // the left. Read the direction off the strip rather than assuming it, so
    // the same component behaves in an RTL locale the way that locale reads.
    // Down always means down: writing direction is horizontal in every script
    // this app has a locale for.
    const rtl = !vertical && strip.current ? getComputedStyle(strip.current).direction === 'rtl' : false;
    const step = e.key === fwd ? (rtl ? -1 : 1) : e.key === back ? (rtl ? 1 : -1) : 0;

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
    // Edit mode: every tab is primed to be picked up, so a click never
    // navigates - there is no way to tell "tapping to select" apart from
    // "about to drag" once the whole strip is already wiggling.
    if (editMode) {
      e.preventDefault();
      e.stopPropagation();
      return;
    }
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
      aria-orientation={vertical ? 'vertical' : 'horizontal'}
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
      // The well variant used to be the one exception to "no wrapping bar",
      // on the reasoning that its whole point is one shared track behind a
      // small, exclusive set - true for the 2-3 item pickers (Look.tsx's
      // shape/theme) it was built for, but TaskProperties' own priority
      // selector carries seven, wide enough at any legible size that a
      // fixed-width nowrap track needed a horizontal scrollbar to hold them
      // (jdp, 2026-08-25: "die buttons so breit machen dass kein
      // horizontaler scrollbar notwendig ist"). flex-wrap costs nothing for
      // a set that already fits its track in one row - it only ever
      // engages once a track would otherwise have overflowed into a
      // scrollbar, growing the track's height instead.
      //
      // Vertical never wraps and never hugs: it is a column, and with `fill`
      // it is a column that owns its container's whole height. `min-h-0` is
      // what lets it scroll instead of overflowing its parent once the tabs
      // hit their floor height - a flex child's automatic minimum size is its
      // content, so without it a twenty-tab rail simply grows past the
      // window's bottom edge and takes the page's scrollbar with it.
      className={
        vertical
          ? `flex min-h-0 flex-col gap-1 overflow-y-auto ${fill ? 'h-full' : ''} ${className}`
          : isWell
            ? `flex flex-wrap items-center gap-[0.2rem] rounded-[var(--radius-control)] bg-carbon-surface2 p-[0.2rem] ${className}`
            : `flex flex-wrap items-center gap-1 ${className}`
      }
    >
      {orderedItems.map((item, i) => {
        const on = isOn(item.id);
        const wiggling = (reordering || editMode) && item.id !== draggingId;
        const dragged = item.id === draggingId;
        // A vertical tab is a tile, and its own layout follows the display
        // mode rather than the other way round:
        //
        //   both/text   a row - glyph, then label, reading left to right like
        //               any other list of named things. It is also the
        //               shortest of the three, which is what lets twenty
        //               tabs share one window's height without scrolling.
        //   glyph       a centred glyph, and the rail is narrow to match:
        //               with no label ever shown there is nothing to be wide
        //               for.
        //   hover       a centred glyph over a label that is collapsed to
        //               nothing. See `inner` below for the actual mechanism.
        //
        // `group` is load-bearing in hover mode and inert in the others: it
        // is what the label's own group-hover rules hang off.
        const stacked = vertical && (display === 'glyph' || labelOnHover);
        // A page the icon map has not met yet has no glyph, and glyph-only
        // would render it as an empty box you can click but not identify. It
        // keeps its label instead - a tab that looks out of place is a gap; a
        // tab that looks like nothing at all is a trap.
        const glyphless = nameOnly && !item.icon;
        const cls = vertical
          ? `${segBase} glim-hue glim-hue-icon group ${on ? `glim-active ${segOn}` : segOff}
              flex w-full min-w-0 overflow-hidden text-[13px]
              ${stacked ? 'flex-col items-center justify-center gap-0.5 px-2 py-1' : 'flex-row items-center gap-2.5 px-3 py-2'}
              ${fill ? 'min-h-9 flex-1 shrink-0 basis-0' : ''}
              ${!on && item.dim ? 'opacity-60' : ''}
              ${wiggling ? 'glim-tab-wiggle' : ''} ${dragged ? 'glim-tab-dragging' : ''}`
          : isWell
          ? // wellWidth (above) sets the actual width now, not a class here
            // - not flex-1/content-hugging either: see wellWidth's own doc
            // comment for why a plain content-hugging width was tried and
            // rejected. min-w-0/shrink-0 still matter with an explicit
            // width set via style: without them a flex item's automatic
            // minimum content size can still force it wider than that
            // style asks for. w-fit on the OUTER track (Look.tsx/
            // Archives.tsx's own className) is what keeps the track's
            // visible bg-carbon-surface2 surface from extending past the
            // last segment.
            `${segBase} glim-hue glim-hue-icon min-w-0 shrink-0 justify-center text-center ${WELL_SIZE[size]}
              ${on ? 'glim-active bg-accent text-accentContrast' : 'bg-transparent text-carbon-textSub hover:bg-carbon-hover hover:text-carbon-text'}
              flex items-center ${!on && item.dim ? 'opacity-60' : ''}`
          : `${segBase} glim-hue glim-hue-icon ${on ? `glim-active ${segOn}` : segOff} ${
              SIZE[size]
            } flex min-w-0 max-w-full items-center ${!on && item.dim ? 'opacity-60' : ''}
              ${wiggling ? 'glim-tab-wiggle' : ''} ${dragged ? 'glim-tab-dragging' : ''}`;

        // The label in `hover` mode is RENDERED and collapsed, never omitted,
        // and that is the whole trick (jdp, 2026-08-27: "Kachel und Button in
        // sidebar bleiben gleich groß. vor mouseover ist der glyph zentriert.
        // bei mouseover: der glyph in der kachel rutscht nach oben und der
        // text erscheint darunter").
        //
        // The tile is centred and, under `fill`, has a height it did not get
        // from its contents. So: at rest the content is a glyph alone and
        // centring puts it in the middle; on hover the label grows from zero
        // and the same centring pushes the glyph up to make room. Nothing is
        // measured, nothing is animated by hand, and the tile's own box never
        // changes size - which is the requirement, and the reason a label
        // that is simply absent at rest would not do: adding it back would
        // reflow.
        //
        // Focus-visible gets the same reveal. A rail whose labels can only be
        // read with a pointer is a rail a keyboard cannot read at all.
        // `leading-4` and a 1rem ceiling rather than whatever line-height the
        // page inherits, and the two figures are the same figure on purpose:
        // the tile clips its overflow, so a label allowed to grow taller than
        // the box it grows into would reveal itself with its descenders cut
        // off. 16px glyph + 2px gap + 16px label + 8px padding is 42, which
        // is what a twenty-tab rail has to spend per tile on a 1000px-high
        // window - measured on the real thing, not assumed.
        const hiddenLabel =
          'leading-4 max-h-0 opacity-0 transition-all duration-200 group-hover:max-h-4 group-hover:opacity-100 ' +
          'group-focus-visible:max-h-4 group-focus-visible:opacity-100';
        const inner = (
          <>
            {showIcon && item.icon}
            {(showLabel || glyphless) && (
              <span className={`truncate ${labelOnHover ? hiddenLabel : ''}`}>{item.label}</span>
            )}
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
          title: item.title ?? (nameOnly ? item.label : undefined),
          'aria-label': nameOnly && !glyphless ? item.label : undefined,
          tabIndex: i === roved ? 0 : -1,
          style: isWell
            ? { ...hueStyle(i), width: wellWidth, justifyContent: 'center' as const }
            : equalWidth
              ? { ...hueStyle(i), minWidth: `${maxLabelLen + 4}ch`, justifyContent: 'center' as const }
              : hueStyle(i),
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
