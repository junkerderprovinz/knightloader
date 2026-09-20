// The app's one tab strip and chip row, so tabs, chips and segments cannot
// drift apart. select="one" picks exactly one, as on the settings pages;
// select="many" toggles several, as the quick filters do. It is built from
// ui.tsx's segBase/segOn/segOff, and the arrow keys move along the strip and
// select, as in Swing.
import { useEffect, useRef, useState, type KeyboardEvent, type MouseEvent, type PointerEvent, type ReactNode } from 'react';
import { useRainbow } from '../lib/useRainbow';
import type { NavLabelMode } from '../lib/navLabels';
import { hueStyle, segBase, segOff, segOn } from './ui';

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
  /** Native tooltip, for a label that may be truncated. */
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
   * makes the tabs wiggle, and the held tab can then be dragged, as in
   * CannonadeCommand's nav rail. Needs `onReorder` too, and never changes
   * `active`.
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

/**
 * emWidth turns visual units into a CSS length at 0.62em per unit, GlimStone's
 * figure. Not `ch`, which measures the narrow "0" glyph and clips long labels.
 */
function emWidth(units: number): string {
  return `${(units * 0.62).toFixed(2)}em`;
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

  // Long-press reordering with pointer events, a hold timer and manual swaps,
  // ported from CannonadeCommand's nav rail. liveOrder is the order shown during
  // a drag and null otherwise; the rest are refs so they do not re-render.
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

  // Derived from the labels so the width is known before first paint; +4 units
  // cover the icon, its gap and the padding.
  const maxLabelLen = equalWidth ? Math.max(0, ...items.map((i) => labelUnits(i.label))) : 0;

  // A well segment's width: a 200px floor so short labels are not cramped, the
  // label's own width, and a 22rem ceiling past which the label wraps rather
  // than pushing the card off the page. `sm` uses the label width alone, since
  // its labels are single words.
  const wellLabel = emWidth(Math.max(0, ...items.map((i) => labelUnits(i.label))) + 4);
  const wellWidth = !isWell ? undefined : size === 'sm' ? wellLabel : `clamp(200px, ${wellLabel}, 22rem)`;

  // Ref mirrors for the document listeners below, which bind once and must
  // read current values.
  const draggingIdRef = useRef<string | null>(null);
  draggingIdRef.current = draggingId;
  const reorderingRef = useRef(false);
  reorderingRef.current = reordering;
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
    // No setPointerCapture: in Chromium, moving past a captured button's bounds
    // fired a spurious pointercancel, and the document listeners track the
    // gesture anyway.
    holdTimer.current = window.setTimeout(() => {
      holdTimer.current = null;
      setReordering(true);
      setDraggingId(id);
      setLiveOrder(items.map((i) => i.id));
    }, 300);
  }

  // On the document, so a pointer that leaves the pressed tab is still tracked.
  useEffect(() => {
    if (!reorderable) return;

    function onMove(e: globalThis.PointerEvent) {
      if (pressStart.current && !draggingIdRef.current) {
        // Moving more than 8px before the hold arms makes it a click or scroll.
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
        // Exact along the strip, 20px of slack across it.
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
      // Only a moved drag that changed the order writes; liveOrder starts as
      // the existing order, and each write costs a settings PUT.
      const changed =
        !!finalOrder &&
        (finalOrder.length !== idsRef.current.length || finalOrder.some((id, at) => id !== idsRef.current[at]));
      if (wasReordering && didMove && changed && finalOrder && onReorder) onReorder(finalOrder);
      if (wasReordering && !didMove) {
        // A hold released in place must not also select the tab.
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
    if (suppressClick.current) {
      suppressClick.current = false;
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
        const wiggling = reordering && item.id !== draggingId;
        const dragged = item.id === draggingId;
        // A vertical tile is a row in both and text modes, and a centred glyph
        // (over a collapsed label in hover mode) in glyph and hover modes.
        const stacked = vertical && (display === 'glyph' || labelOnHover);
        // A tab without a glyph keeps its label in glyph mode rather than
        // rendering as an empty box.
        const glyphless = nameOnly && !item.icon;
        const cls = vertical
          ? // Sized like Sidebar.tsx's navBase rows beside it.
            `${segBase} glim-nav-row glim-hue glim-hue-icon group ${on ? `glim-active ${segOn}` : segOff}
              flex w-full min-w-0 overflow-hidden text-[15px]
              ${stacked ? 'flex-col items-center justify-center gap-0.5 px-2 py-1.5' : 'flex-row items-center gap-3 px-3 py-2.5'}
              ${fill ? 'min-h-10 flex-1 shrink-0 basis-0' : ''}
              ${!on && item.dim ? 'opacity-60' : ''}
              ${wiggling ? 'glim-tab-wiggle' : ''} ${dragged ? 'glim-tab-dragging' : ''}`
          : isWell
          ? // wellWidth sets the width; min-w-0 and shrink-0 stop the flex
            // minimum content size from overriding it.
            `${segBase} glim-nav-row glim-hue glim-hue-icon min-w-0 shrink-0 justify-center text-center leading-snug ${WELL_SIZE[size]}
              ${on ? 'glim-active bg-accent text-accentContrast' : 'bg-transparent text-carbon-textSub hover:bg-carbon-hover hover:text-carbon-text'}
              flex items-center ${!on && item.dim ? 'opacity-60' : ''}`
          : `${segBase} glim-nav-row glim-hue glim-hue-icon ${on ? `glim-active ${segOn}` : segOff} ${
              SIZE[size]
            } flex min-w-0 max-w-full items-center ${!on && item.dim ? 'opacity-60' : ''}
              ${wiggling ? 'glim-tab-wiggle' : ''} ${dragged ? 'glim-tab-dragging' : ''}`;

        // In hover mode the label grows from zero height inside a centred
        // tile, pushing the glyph up without the tile changing size. Focus
        // reveals it too. leading-4 matches the 1rem ceiling so descenders are
        // not clipped, and the tile stays within 42px.
        const hiddenLabel =
          'leading-4 max-h-0 opacity-0 transition-all duration-200 group-hover:max-h-4 group-hover:opacity-100 ' +
          'group-focus-visible:max-h-4 group-focus-visible:opacity-100';
        const inner = (
          <>
            {showIcon && item.icon}
            {(showLabel || glyphless) && (
              // A well segment can grow taller, so it wraps; elsewhere the row
              // height is fixed, so the label truncates.
              <span
                className={`${isWell ? 'text-pretty break-words' : 'truncate'} ${labelOnHover ? hiddenLabel : ''}`}
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

        const shared = {
          'data-tab-id': item.id,
          title: item.title ?? (nameOnly ? item.label : undefined),
          'aria-label': nameOnly && !glyphless ? item.label : undefined,
          tabIndex: i === roved ? 0 : -1,
          style: isWell
            ? // The filled segment follows the shape setting, or its square
              // corner would poke out of the track's rounded one. Inline, since
              // two competing radius classes resolve by stylesheet order.
              {
                ...hueStyle(i),
                width: wellWidth,
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
