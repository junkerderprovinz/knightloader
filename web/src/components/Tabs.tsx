// The app's one tab strip and chip row, so tabs, chips and segments cannot
// drift apart. select="one" picks exactly one, as on the settings pages;
// select="many" toggles several, as the quick filters do. It is built from
// ui.tsx's segBase/segOn/segOff, and the arrow keys move along the strip and
// select, as in Swing.
import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type HTMLAttributes,
  type KeyboardEvent,
  type MouseEvent,
  type PointerEvent,
  type ReactNode,
  type RefObject,
} from 'react';
import { flushSync } from 'react-dom';
import { useRainbow } from '../lib/useRainbow';
import type { NavLabelMode } from '../lib/navLabels';
import { hueStyle, segBase, segOff, segOn, useTooltip } from './ui';

export interface TabDef {
  /** Stable id, handed back by onSelect and used as the route segment. */
  id: string;
  label: string;
  /**
   * The glyph before the label, in the tab's hue in rainbow mode. Its size
   * comes from `glim-nav-row` (20px, like a sidebar row), which overrides the
   * glyph's own width and height attributes.
   */
  icon?: ReactNode;
  /** A count or short mark after the label. */
  badge?: ReactNode;
  /** Reachable but visibly empty, such as a settings page with no controls yet. */
  dim?: boolean;
  /**
   * Makes the tab a real link, so modified clicks open a new tab; a plain
   * click still goes to onSelect.
   */
  href?: string;
  /** The tooltip, for a label that may be truncated. */
  title?: string;
}

interface Common {
  items: TabDef[];
  /** Accessible name for the strip. */
  label: string;
  /** `md` is a page-level tab, `sm` a filter chip. */
  size?: 'sm' | 'md';
  /**
   * Whether an arrow key selects as it moves, as a JTabbedPane does. Defaults
   * to true for select="one"; pass false where selecting is expensive or
   * pushes a history entry.
   */
  activateOnFocus?: boolean;
  /** Rendered inside the strip after the tabs. */
  after?: ReactNode;
  className?: string;
  /**
   * Long-press reordering: a hold of about 300ms with under 8px of movement
   * makes the tabs wiggle and lifts the held tab, which then follows the
   * pointer, as in CannonadeCommand's nav rail. Needs `onReorder` too, and
   * never changes `active`.
   */
  reorderable?: boolean;
  /** Called with the full, reordered list of ids after a drop. */
  onReorder?: (ids: string[]) => void;
  /**
   * Gives every tab a minimum width from the longest label, in `em` of visual
   * units, so a navigation strip does not read ragged but still wraps.
   */
  equalWidth?: boolean;
  /**
   * `well` draws a small exclusive set as segments on one shared track, like
   * BombVault's shape picker, instead of separate badges.
   */
  variant?: 'default' | 'well';
  /**
   * `vertical` is the settings rail. It is a mode of this component so the
   * rail shares the reorder gesture, roving tabindex and rainbow wiring.
   */
  orientation?: 'horizontal' | 'vertical';
  /**
   * Vertical only: the tabs share the strip's full height, each with a floor
   * height so a long list scrolls instead of shrinking to slivers.
   */
  fill?: boolean;
  /** How much of each tab is drawn; see lib/navLabels.ts. */
  display?: NavLabelMode;
}

export type TabsProps =
  | (Common & { select?: 'one'; active: string | null; onSelect: (id: string) => void })
  | (Common & { select: 'many'; active: ReadonlySet<string>; onSelect: (id: string) => void });

// Two steps of the 20/14/12/11 type scale: body for a page tab, dense for a chip.
const SIZE = {
  sm: 'gap-1.5 px-2.5 py-1 text-xs',
  md: 'gap-2 px-3 py-2 text-sm',
} as const;

// A well segment reads bigger than a chip at the same stage, so it has its own
// padding; `sm` suits TaskProperties' smaller selectors.
const WELL_SIZE: Record<'sm' | 'md', string> = {
  sm: 'gap-1.5 px-2.5 py-1 text-xs',
  md: 'gap-2 px-3 py-1.5 text-sm',
};

/**
 * labelUnits measures a label in visual units, counting a fullwidth (CJK)
 * character as two and an astral character as one. It is ported from
 * GlimStone's reference labelWidth so the ranges match the sibling apps.
 */
function labelUnits(label: string): number {
  let total = 0;
  for (const ch of label) {
    const code = ch.codePointAt(0) ?? 0;
    const fullwidth =
      (code >= 0x1100 && code <= 0x115f) ||
      (code >= 0x2e80 && code <= 0xa4cf) ||
      (code >= 0xac00 && code <= 0xd7a3) ||
      (code >= 0xf900 && code <= 0xfaff) ||
      (code >= 0xfe30 && code <= 0xfe6f) ||
      (code >= 0xff00 && code <= 0xff60) ||
      (code >= 0xffe0 && code <= 0xffe6);
    total += fullwidth ? 2 : 1;
  }
  return total;
}

/** GlimStone's width of one visual unit, in `em` of the label's own type. */
const EM_PER_UNIT = 0.62;

/**
 * emWidth turns visual units into a CSS length at 0.62em per unit, GlimStone's
 * figure. Not `ch`, which measures the narrow "0" glyph and clips long labels.
 */
function emWidth(units: number): string {
  return `${(units * EM_PER_UNIT).toFixed(2)}em`;
}

/**
 * A big well segment is at least this wide, so every page-level selector in
 * the app matches, and at most this many rem, past which its label wraps.
 */
const WELL_FLOOR_PX = 200;
const WELL_CEIL_REM = 22;

/**
 * wellGrid lays a well out over rows when its segments do not fit on one: as
 * few rows as `fit` allows, holding as nearly the same number of segments as
 * they can, and each row shared out evenly, so no row ends in an empty stretch
 * of groove. The grid has a column count every row length divides, and each
 * segment spans its row's share of it. Null when one row holds them all.
 */
function wellGrid(count: number, fit: number): { columns: number; spans: number[] } | null {
  if (count <= fit) return null;
  const rows = Math.ceil(count / fit);
  const short = Math.floor(count / rows);
  // The first `long` rows take one segment more than the rest.
  const long = count % rows;
  const columns = long > 0 ? short * (short + 1) : short;
  const spans: number[] = [];
  for (let row = 0; row < rows; row++) {
    const k = row < long ? short + 1 : short;
    for (let i = 0; i < k; i++) spans.push(columns / k);
  }
  return { columns, spans };
}

interface Point {
  x: number;
  y: number;
}

/** A tab's box in the strip's layout, which no transform moves. */
interface Box extends Point {
  w: number;
  h: number;
}

/** The tab a long press lifted, measured when the hold armed. */
interface Held {
  id: string;
  /** Where the pointer went down, in client pixels. */
  down: Point;
  from: Box;
  /**
   * The range its corner may take: the area all tabs cover, inset by what the
   * lift's scale adds, so it cannot overflow a strip that scrolls and clips.
   */
  min: Point;
  max: Point;
  /** Whether it follows the pointer on both axes, which only a wrapped strip needs. */
  free: boolean;
  rtl: boolean;
}

function boxOf(node: HTMLElement): Box {
  return { x: node.offsetLeft, y: node.offsetTop, w: node.offsetWidth, h: node.offsetHeight };
}

function clamp(v: number, lo: number, hi: number): number {
  return Math.min(Math.max(v, lo), hi);
}

function sameOrder(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((id, at) => id === b[at]);
}

/** Tabs renders a tab strip or chip row; see the props above for its modes. */
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
    orientation = 'horizontal',
    fill = false,
    display = 'both',
  } = props;
  const isWell = variant === 'well';
  const vertical = orientation === 'vertical';

  // `hover` renders the label collapsed rather than omitting it, so it grows
  // back in place without re-measuring anything.
  const showIcon = display !== 'text';
  const labelOnHover = display === 'hover';
  const showLabel = display === 'both' || display === 'text' || labelOnHover;
  // In glyph mode the label becomes the accessible name and tooltip.
  const nameOnly = display === 'glyph';

  // Subscribed here so every strip recolours in the same paint as a palette edit.
  useRainbow();

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

  function tabNode(id: string): HTMLElement | undefined {
    return tabNodes().find((node) => node.getAttribute('data-tab-id') === id);
  }

  // Long-press reordering with pointer events and a hold timer, ported from
  // CannonadeCommand's nav rail. liveOrder is the order laid out while a tab is
  // held or settling, and null otherwise. Positions go straight to each tab's
  // `translate` rather than through state, so the held tab keeps up with the
  // pointer; how a lifted or sliding tab looks is in index.css.
  const [draggingId, setDraggingId] = useState<string | null>(null);
  const [settlingId, setSettlingId] = useState<string | null>(null);
  const [liveOrder, setLiveOrder] = useState<string[] | null>(null);
  const holdTimer = useRef<number | null>(null);
  const pressStart = useRef<Point | null>(null);
  const pressPointerId = useRef<number | null>(null);
  const pointer = useRef<Point>({ x: 0, y: 0 });
  const held = useRef<Held | null>(null);
  // Where each tab was painted before liveOrder moved it, for the slide.
  const slideFrom = useRef<Map<string, Point> | null>(null);
  const suppressClick = useRef(false);

  const orderedItems = liveOrder ? liveOrder.map((id) => items.find((i) => i.id === id)).filter((i): i is TabDef => !!i) : items;

  // Derived from the labels so the width is known before first paint; +4 units
  // cover the icon, its gap and the padding.
  const maxLabelLen = equalWidth ? Math.max(0, ...items.map((i) => labelUnits(i.label))) : 0;

  // A well segment's width: a 200px floor so short labels are not cramped, the
  // label's own width, and a 22rem ceiling past which the label wraps rather
  // than pushing the card off the page. `sm` uses the label width alone, since
  // its labels are single words.
  const wellUnits = Math.max(0, ...items.map((i) => labelUnits(i.label))) + 4;
  const wellLabel = emWidth(wellUnits);
  const wellWidth = !isWell
    ? undefined
    : size === 'sm'
      ? wellLabel
      : `clamp(${WELL_FLOOR_PX}px, ${wellLabel}, ${WELL_CEIL_REM}rem)`;

  // How many well segments fit on one line of the room the strip has. The
  // room is the parent's: while the strip hugs its segments its own width says
  // nothing about the space around it.
  const [fit, setFit] = useState<number | null>(null);
  useLayoutEffect(() => {
    const el = strip.current;
    const parent = el?.parentElement;
    if (!isWell || vertical || !el || !parent) return;
    function measure() {
      const seg = el?.querySelector<HTMLElement>('[data-tab-id]');
      if (!el || !parent || !seg) return;
      const outer = getComputedStyle(parent);
      const room = parent.clientWidth - parseFloat(outer.paddingLeft) - parseFloat(outer.paddingRight);
      const own = getComputedStyle(el);
      const gap = parseFloat(own.columnGap) || 0;
      const inset = parseFloat(own.paddingLeft) + parseFloat(own.paddingRight);
      // The same sum wellWidth hands the stylesheet, in pixels.
      const label = wellUnits * EM_PER_UNIT * parseFloat(getComputedStyle(seg).fontSize);
      const rem = parseFloat(getComputedStyle(document.documentElement).fontSize);
      const width = size === 'sm' ? label : Math.max(WELL_FLOOR_PX, Math.min(label, WELL_CEIL_REM * rem));
      setFit(Math.max(1, Math.floor((room - inset + gap) / (width + gap))));
    }
    measure();
    const watch = new ResizeObserver(measure);
    watch.observe(parent);
    return () => watch.disconnect();
  }, [isWell, vertical, wellUnits, size]);
  const grid = isWell && fit !== null ? wellGrid(items.length, fit) : null;

  // Ref mirrors for the document listeners below, which bind once and must
  // read current values.
  const liveOrderRef = useRef<string[] | null>(null);
  liveOrderRef.current = liveOrder;
  const idsRef = useRef<string[]>([]);
  idsRef.current = items.map((i) => i.id);

  function cancelHold() {
    if (holdTimer.current !== null) {
      window.clearTimeout(holdTimer.current);
      holdTimer.current = null;
    }
    pressStart.current = null;
    pressPointerId.current = null;
  }

  function onTabPointerDown(e: PointerEvent<HTMLElement>, id: string) {
    // A second finger during a drag does not start another.
    if (!reorderable || e.button !== 0 || held.current) return;
    cancelHold();
    const down = { x: e.clientX, y: e.clientY };
    pressStart.current = down;
    pointer.current = down;
    pressPointerId.current = e.pointerId;
    suppressClick.current = false;
    // No setPointerCapture: in Chromium, moving past a captured button's bounds
    // fired a spurious pointercancel, and the document listeners track the
    // gesture anyway.
    holdTimer.current = window.setTimeout(() => {
      holdTimer.current = null;
      lift(id, down);
    }, 300);
  }

  function lift(id: string, down: Point) {
    const node = tabNode(id);
    const el = strip.current;
    if (!node || !el) return;
    const boxes = tabNodes().map(boxOf);
    const from = boxOf(node);
    const grow = ((parseFloat(getComputedStyle(el).getPropertyValue('--drag-lift-scale')) || 1) - 1) / 2;
    const left = Math.min(...boxes.map((b) => b.x));
    const top = Math.min(...boxes.map((b) => b.y));
    const right = Math.max(...boxes.map((b) => b.x + b.w));
    const bottom = Math.max(...boxes.map((b) => b.y + b.h));
    held.current = {
      id,
      down,
      from,
      min: { x: left + from.w * grow, y: top + from.h * grow },
      max: { x: right - from.w * (1 + grow), y: bottom - from.h * (1 + grow) },
      free: !vertical && boxes.some((b) => b.y >= from.y + from.h || b.y + b.h <= from.y),
      rtl: getComputedStyle(el).direction === 'rtl',
    };
    setSettlingId(null);
    setDraggingId(id);
    setLiveOrder(idsRef.current);
  }

  /**
   * Where the held tab's corner goes for the current pointer, in the strip's
   * layout. Only the drawn tab is kept inside the strip: aimed from there, the
   * lift's inset would leave the first and last slots out of reach.
   */
  function heldAt(h: Held, drawn: boolean): Point {
    const x = h.from.x + pointer.current.x - h.down.x;
    const y = h.from.y + pointer.current.y - h.down.y;
    const keep = (v: number, lo: number, hi: number) => (drawn ? clamp(v, lo, hi) : v);
    return {
      x: vertical ? h.from.x : keep(x, h.min.x, h.max.x),
      y: vertical || h.free ? keep(y, h.min.y, h.max.y) : h.from.y,
    };
  }

  function placeHeld(h: Held) {
    const node = tabNode(h.id);
    if (!node) return;
    const at = heldAt(h, true);
    node.style.translate = `${at.x - node.offsetLeft}px ${at.y - node.offsetTop}px`;
  }

  /**
   * aim returns the order the held tab's position asks for. A tab counts as
   * before it once the held tab's centre passes the tab's centre; the passed
   * tab then moves away by the held tab's own size, so two tabs of different
   * sizes cannot trade places back and forth.
   */
  function aim(h: Held): string[] {
    const at = heldAt(h, false);
    const cx = at.x + h.from.w / 2;
    const cy = at.y + h.from.h / 2;
    const rest: string[] = [];
    let ahead = 0;
    for (const node of tabNodes()) {
      const id = node.getAttribute('data-tab-id');
      if (!id || id === h.id) continue;
      const b = boxOf(node);
      const mid = b.x + b.w / 2;
      // A wrapped strip reads by line first, then along the line.
      const before = vertical
        ? b.y + b.h / 2 < cy
        : cy > b.y + b.h || (cy >= b.y && (h.rtl ? mid > cx : mid < cx));
      if (before) ahead++;
      rest.push(id);
    }
    rest.splice(ahead, 0, h.id);
    return rest;
  }

  /** Where each tab is painted, a slide in flight included. */
  function painted(): Map<string, Point> {
    const out = new Map<string, Point>();
    for (const node of tabNodes()) {
      const id = node.getAttribute('data-tab-id');
      const t = getComputedStyle(node).translate;
      const [tx = 0, ty = 0] = t === 'none' ? [] : t.split(' ').map(parseFloat);
      if (id) out.set(id, { x: node.offsetLeft + tx, y: node.offsetTop + ty });
    }
    return out;
  }

  function release(h: Held) {
    held.current = null;
    // A click that follows the release would select the held tab.
    suppressClick.current = true;
    setDraggingId(null);
    setSettlingId(h.id);
  }

  // After every render, since a new liveOrder moves the tabs in the DOM: the
  // held tab is placed under the pointer again, and every tab the order moved
  // starts from where it was painted and slides to its new slot.
  useLayoutEffect(() => {
    const h = held.current;
    // First, so the forced layout below commits this offset with the others.
    if (h) placeHeld(h);
    const from = slideFrom.current;
    if (!from) return;
    slideFrom.current = null;
    const moved: HTMLElement[] = [];
    for (const node of tabNodes()) {
      const id = node.getAttribute('data-tab-id');
      const was = id ? from.get(id) : undefined;
      if (!was || id === h?.id) continue;
      const dx = was.x - node.offsetLeft;
      const dy = was.y - node.offsetTop;
      if (dx === 0 && dy === 0) continue;
      node.style.transition = 'none';
      node.style.translate = `${dx}px ${dy}px`;
      moved.push(node);
    }
    if (moved.length === 0) return;
    // Reading layout commits the jump back, so the transition starts from there.
    void strip.current?.offsetWidth;
    for (const node of moved) {
      node.style.transition = '';
      node.style.translate = '0px 0px';
    }
  });

  // The dropped tab slides into its slot, and once the settle has played the
  // offsets and liveOrder go, leaving the order to `items`.
  useLayoutEffect(() => {
    if (!settlingId) return;
    const node = tabNode(settlingId);
    let ms = 0;
    if (node) {
      node.style.translate = '0px 0px';
      ms = Math.max(...getComputedStyle(node).transitionDuration.split(',').map(parseFloat)) * 1000;
    }
    const timer = window.setTimeout(() => {
      for (const n of tabNodes()) n.style.translate = '';
      setSettlingId(null);
      setLiveOrder(null);
    }, ms);
    return () => window.clearTimeout(timer);
    // tabNode and tabNodes read the strip's DOM, not render state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settlingId]);

  // On the document, so a pointer that leaves the pressed tab is still tracked.
  useEffect(() => {
    if (!reorderable) return;
    const el = strip.current;

    function onMove(e: globalThis.PointerEvent) {
      if (e.pointerId !== pressPointerId.current) return;
      pointer.current = { x: e.clientX, y: e.clientY };
      const h = held.current;
      if (!h) {
        // Moving more than 8px before the hold arms makes it a click or scroll.
        const start = pressStart.current;
        if (start && (Math.abs(e.clientX - start.x) > 8 || Math.abs(e.clientY - start.y) > 8)) cancelHold();
        return;
      }
      placeHeld(h);
      const next = aim(h);
      const shown = liveOrderRef.current;
      if (shown && !sameOrder(next, shown)) {
        slideFrom.current = painted();
        setLiveOrder(next);
      }
    }

    function onUp(e: globalThis.PointerEvent) {
      if (e.pointerId !== pressPointerId.current) return;
      cancelHold();
      const h = held.current;
      if (!h) return;
      // A drop where it started writes nothing; each write costs a settings PUT.
      const order = liveOrderRef.current;
      if (order && onReorder && !sameOrder(order, idsRef.current)) onReorder(order);
      release(h);
    }

    // Escape, or the system taking the touch: everything slides back and
    // nothing is written, as the phone's DragList does on a terminated pan.
    function putBack(h: Held) {
      cancelHold();
      const order = liveOrderRef.current;
      if (order && !sameOrder(order, idsRef.current)) {
        // The others go home while the tab is still lifted, so it settles from
        // under the pointer rather than jumping to its old slot first.
        slideFrom.current = painted();
        flushSync(() => setLiveOrder(idsRef.current));
      }
      release(h);
    }

    function onCancel(e: globalThis.PointerEvent) {
      if (e.pointerId !== pressPointerId.current) return;
      const h = held.current;
      if (h) putBack(h);
      else cancelHold();
    }

    function onEscape(e: globalThis.KeyboardEvent) {
      const h = held.current;
      if (e.key === 'Escape' && h) putBack(h);
    }

    // Once the hold has armed the gesture is the drag's: the long press does not
    // open the link menu, and a moving finger drags instead of scrolling the
    // strip. Bound natively, since React's touchmove listener is passive and
    // cannot cancel.
    function onMenu(e: Event) {
      if (held.current) e.preventDefault();
    }
    function onTouchMove(e: TouchEvent) {
      if (held.current) e.preventDefault();
    }

    document.addEventListener('pointermove', onMove);
    document.addEventListener('pointerup', onUp);
    document.addEventListener('pointercancel', onCancel);
    document.addEventListener('keydown', onEscape);
    el?.addEventListener('contextmenu', onMenu);
    el?.addEventListener('touchmove', onTouchMove, { passive: false });
    return () => {
      document.removeEventListener('pointermove', onMove);
      document.removeEventListener('pointerup', onUp);
      document.removeEventListener('pointercancel', onCancel);
      document.removeEventListener('keydown', onEscape);
      el?.removeEventListener('contextmenu', onMenu);
      el?.removeEventListener('touchmove', onTouchMove);
    };
    // The listeners read refs, so only these values need a re-bind.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reorderable, onReorder, vertical]);

  // Roving tabindex: the strip is one tab stop and the arrows move inside it.
  const roved = Math.max(
    0,
    orderedItems.findIndex((i) => isOn(i.id)),
  );

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    // Space on an <a> would scroll the page instead of activating it.
    if (e.key === ' ' && e.target instanceof HTMLAnchorElement) {
      const id = e.target.getAttribute('data-tab-id');
      if (id) {
        e.preventDefault();
        onSelect(id);
      }
      return;
    }

    const [fwd, back] = vertical ? ['ArrowDown', 'ArrowUp'] : ['ArrowRight', 'ArrowLeft'];
    if (![fwd, back, 'Home', 'End'].includes(e.key)) return;
    const nodes = tabNodes();
    if (nodes.length === 0) return;

    // A horizontal strip reverses in RTL; a vertical one never does.
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
    // No click follows a release that ends off the tab or after the reorder
    // moved the tab's node, so the flag can outlive its gesture. That is why
    // only a pointer's click is swallowed (a key or a screen reader activates
    // with a detail of 0), and why no timer clears it: one would race a touch
    // tap's later click.
    const endsDrag = suppressClick.current && e.detail > 0;
    suppressClick.current = false;
    if (endsDrag) {
      e.preventDefault();
      e.stopPropagation();
      return;
    }
    // A modified click on a link is left to the browser.
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
      // Horizontal strips, the well included, wrap rather than scroll, so they
      // never push the page sideways. The vertical rail scrolls; min-h-0 lets
      // it shrink below its content inside a flex parent.
      //
      // A reorderable strip is the tabs' offsetParent, the layout a drag
      // measures in. A reorderable rail also gets 4px of room inside its
      // clipping edge for the lift's scale and the reduced-motion outline,
      // which the negative margin takes back so the tiles stay put.
      //
      // The well's groove is surface3, the one step that differs from both
      // places a well sits, a card and a glim-well; surface2 would vanish into
      // a glim-well, which is surface2 itself. It hugs its segments on one
      // line, which also keeps a flex column from stretching it, and takes the
      // whole line once it needs more than one (wellGrid).
      className={
        vertical
          ? `flex min-h-0 flex-col gap-1 overflow-y-auto ${fill ? 'h-full' : ''} ${
              reorderable ? 'relative -mx-1 px-1' : ''
            } ${className}`
          : isWell
            ? `${grid ? 'grid' : 'flex w-fit max-w-full flex-wrap items-center'} gap-[0.2rem]
              rounded-[var(--radius-control)] bg-carbon-surface3 p-[0.2rem] ${className}`
            : `flex flex-wrap items-center gap-1 ${reorderable ? 'relative' : ''} ${className}`
      }
      style={grid ? { width: '100%', gridTemplateColumns: `repeat(${grid.columns}, minmax(0, 1fr))` } : undefined}
    >
      {orderedItems.map((item, i) => {
        const on = isOn(item.id);
        const wiggling = draggingId !== null && item.id !== draggingId;
        const drag =
          item.id === draggingId
            ? 'glim-drag-lift'
            : item.id === settlingId
              ? 'glim-drag-settle'
              : draggingId !== null || settlingId !== null
                ? 'glim-drag-shift'
                : '';
        // A long press would otherwise select the label, and on iOS open the
        // link callout, which only CSS can turn off.
        const grip = reorderable ? 'select-none [-webkit-touch-callout:none]' : '';
        // A vertical tile is a row in both and text modes, and a centred glyph
        // (over a collapsed label in hover mode) in glyph and hover modes.
        const stacked = vertical && (display === 'glyph' || labelOnHover);
        // A tab without a glyph keeps its label in glyph mode rather than
        // rendering as an empty box.
        const glyphless = nameOnly && !item.icon;
        // In hover mode a stacked tile has the shape of the phone's bottom bar
        // tab (GlimStone 2.3.0): 48px for a 20px glyph, a 2px gap and a caption
        // line, with about 5px above and below. At 40px with the row's 15px
        // label, the label gets a 6px sliver.
        const captioned = stacked && labelOnHover;
        const cls = vertical
          ? // Sized like Sidebar.tsx's navBase rows beside it.
            `${segBase} glim-nav-row glim-hue glim-hue-icon group ${on ? `glim-active ${segOn}` : segOff}
              flex w-full min-w-0 overflow-hidden text-[15px]
              ${stacked ? `flex-col items-center justify-center gap-0.5 px-2 ${captioned ? 'py-1' : 'py-1.5'}` : 'flex-row items-center gap-3 px-3 py-2.5'}
              ${fill ? `${captioned ? 'min-h-12' : 'min-h-10'} grow shrink-0 ${stacked ? 'basis-0' : 'basis-auto'}` : ''}
              ${!on && item.dim ? 'opacity-60' : ''}
              ${wiggling ? 'glim-tab-wiggle' : ''} ${drag} ${grip}`
          : isWell
          ? // wellWidth sets the width; min-w-0 and shrink-0 stop the flex
            // minimum content size from overriding it.
            // An idle segment has no fill of its own and shows the surface3
            // groove, so its hover is the step above that (rule 21).
            `${segBase} glim-nav-row glim-hue glim-hue-icon min-w-0 shrink-0 justify-center text-center leading-snug ${WELL_SIZE[size]}
              ${on ? 'glim-active bg-accent text-accentContrast' : 'bg-transparent text-carbon-textSub hover:bg-carbon-hoverRaised hover:text-carbon-text'}
              flex items-center ${!on && item.dim ? 'opacity-60' : ''}`
          : `${segBase} glim-nav-row glim-hue glim-hue-icon ${on ? `glim-active ${segOn}` : segOff} ${
              SIZE[size]
            } flex min-w-0 max-w-full items-center ${!on && item.dim ? 'opacity-60' : ''}
              ${wiggling ? 'glim-tab-wiggle' : ''} ${drag} ${grip}`;

        // In hover mode the label grows from zero height inside a centred
        // tile, pushing the glyph up without the tile changing size. Focus
        // reveals it too. The ceiling is the label's own line box: a line
        // height of 1 cuts off every descender in a box that hides its overflow.
        const hiddenLabel =
          'leading-[1.4] max-h-0 opacity-0 transition-all duration-200 group-hover:max-h-[1.4em] ' +
          'group-hover:opacity-100 group-focus-visible:max-h-[1.4em] group-focus-visible:opacity-100';
        const inner = (
          <>
            {showIcon && item.icon}
            {(showLabel || glyphless) && (
              // A well segment can grow taller, so it wraps. A rail tile beside
              // its glyph takes a second line where its name needs one, split
              // evenly, and only that tile grows: its basis is its content and
              // every tile shares what is left of the rail. The 20px line keeps
              // a one-line tile at the 40px floor. Elsewhere the row height is
              // fixed, so the label truncates.
              <span
                className={`${
                  isWell
                    ? 'text-pretty break-words'
                    : vertical && !stacked
                      ? 'line-clamp-2 break-words text-balance leading-5'
                      : 'truncate'
                } ${labelOnHover ? hiddenLabel : ''} ${captioned ? 'text-xs' : ''}`}
              >
                {item.label}
              </span>
            )}
            {item.badge !== undefined && item.badge !== null && (
              // On the filled tab the badge takes the ink colour.
              <span
                className="glim-num rounded-[var(--radius-pill)] px-1 text-[11px] font-semibold leading-none
                  text-carbon-textMuted [.glim-active_&]:bg-black/15 [.glim-active_&]:text-current"
              >
                {item.badge}
              </span>
            )}
          </>
        );

        const shared: TabShared = {
          'data-tab-id': item.id,
          'aria-label': nameOnly && !glyphless ? item.label : undefined,
          tabIndex: i === roved ? 0 : -1,
          style: isWell
            ? // The filled segment follows the shape setting, or its square
              // corner would poke out of the track's rounded one. Inline, since
              // two competing radius classes resolve by stylesheet order. On
              // more than one line the grid sizes the segment, not wellWidth.
              {
                ...hueStyle(i),
                ...(grid ? { gridColumn: `span ${grid.spans[i]}` } : { width: wellWidth }),
                borderRadius: 'var(--radius-control)',
                justifyContent: 'center' as const,
              }
            : equalWidth
              ? { ...hueStyle(i), minWidth: emWidth(maxLabelLen + 4), justifyContent: 'center' as const }
              : hueStyle(i),
          className: cls,
          // A native link drag would fire pointercancel and end the reorder.
          draggable: reorderable ? false : undefined,
          onPointerDown: reorderable ? (e: PointerEvent<HTMLElement>) => onTabPointerDown(e, item.id) : undefined,
          onClick: (e: MouseEvent<HTMLElement>) => onClick(e, item),
        };

        return (
          <TabTrigger
            key={item.id}
            href={item.href}
            many={many}
            on={on}
            tip={item.title ?? (nameOnly ? item.label : undefined)}
            shared={shared}
          >
            {inner}
          </TabTrigger>
        );
      })}
      {after}
    </div>
  );
}

type TabShared = HTMLAttributes<HTMLElement> & { 'data-tab-id': string };

/**
 * TabTrigger is one tab, a component of its own because the tooltip is a hook.
 * `tip` is what the tab cannot show itself: its name in glyph mode, or the
 * whole of a label that may be truncated.
 */
function TabTrigger({
  href,
  many,
  on,
  tip,
  shared,
  children,
}: {
  href?: string;
  many: boolean;
  on: boolean;
  tip?: string;
  shared: TabShared;
  children: ReactNode;
}) {
  const bubble = useTooltip<HTMLElement>(tip);
  // A tab has a role of its own and a roving tab stop, so the trigger's go.
  const { role: _tipRole, tabIndex: _tipTabIndex, ref, ...tipHoverProps } = bubble.triggerProps;
  const tipProps = tip ? tipHoverProps : undefined;
  return (
    <>
      {href ? (
        <a
          ref={ref as RefObject<HTMLAnchorElement | null>}
          href={href}
          role={many ? undefined : 'tab'}
          aria-selected={many ? undefined : on}
          aria-current={on ? 'page' : undefined}
          {...shared}
          {...tipProps}
        >
          {children}
        </a>
      ) : (
        <button
          ref={ref as RefObject<HTMLButtonElement | null>}
          type="button"
          role={many ? undefined : 'tab'}
          aria-selected={many ? undefined : on}
          aria-pressed={many ? on : undefined}
          {...shared}
          {...tipProps}
        >
          {children}
        </button>
      )}
      {bubble.node}
    </>
  );
}
