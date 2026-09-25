// The app's one tab strip and chip row, so tabs, chips and segments cannot
// drift apart. select="one" picks exactly one, as on the settings pages;
// select="many" toggles several, as the quick filters do. It is built from
// ui.tsx's segBase/segOn/segOff, and the arrow keys move along the strip and
// select, as in Swing.
import {
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type HTMLAttributes,
  type KeyboardEvent,
  type MouseEvent,
  type PointerEvent,
  type ReactNode,
  type RefObject,
} from 'react';
import type { NavLabelMode } from '../lib/navLabels';
import { useReorder } from './dragLift';
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

/** The space between segments, in the well's groove and in a bare strip. */
const WELL_GAP = '0.2rem';
const STRIP_GAP = '0.25rem';

/**
 * perRowFor is how many pinned segments share a row: all of them while they
 * fit, otherwise as even a share as the rows allow, so six that fit four to a
 * row go three and three and seven go four and three (GlimStone, "A selector
 * that wraps fills its box").
 */
function perRowFor(count: number, room: number, width: number, gap: number): number {
  const fit = Math.max(1, Math.floor((room + gap) / (width + gap)));
  if (count <= fit) return count;
  const rows = Math.ceil(count / fit);
  return Math.ceil(count / rows);
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

  // Long-press reordering, ported from CannonadeCommand's nav rail; see
  // dragLift.ts. A tab is a click target too, so a mouse arms it by holding.
  const drag = useReorder({
    ids: items.map((i) => i.id),
    container: strip,
    attr: 'data-tab-id',
    axis: vertical ? 'y' : 'x',
    arm: 'hold',
    enabled: reorderable,
    onReorder,
  });
  const byId = new Map(items.map((i) => [i.id, i] as const));
  const orderedItems = drag.order.map((id) => byId.get(id)).filter((i): i is TabDef => !!i);

  // The width every segment of a well or an equal-width strip is pinned to,
  // derived from the labels so it is known before first paint: the widest
  // label plus four units for the glyph, its gap and the padding. A big well
  // adds a 200px floor, so short labels are not cramped and every page-level
  // selector matches, and a 22rem ceiling past which the label wraps. A small
  // well takes the label width alone, since its labels are single words.
  // Undefined where each segment hugs its own label.
  const pinUnits = Math.max(0, ...items.map((i) => labelUnits(i.label))) + 4;
  const bigWell = isWell && size === 'md';
  const pinned = vertical
    ? undefined
    : bigWell
      ? `clamp(${WELL_FLOOR_PX}px, ${emWidth(pinUnits)}, ${WELL_CEIL_REM}rem)`
      : isWell || equalWidth
        ? emWidth(pinUnits)
        : undefined;
  const gap = isWell ? WELL_GAP : STRIP_GAP;

  // How many pinned segments share a row of the room the strip has. The room
  // is the parent's: while the strip hugs its segments its own width says
  // nothing about the space around it. Worked out again whenever the parent
  // resizes, since a narrower window holds fewer to a row.
  const [perRow, setPerRow] = useState(items.length);
  const [room, setRoom] = useState<number | null>(null);
  useLayoutEffect(() => {
    const el = strip.current;
    const parent = el?.parentElement;
    if (pinned === undefined || !el || !parent) return;
    function measure() {
      const seg = el?.querySelector<HTMLElement>('[data-tab-id]');
      if (!el || !parent || !seg) return;
      const outer = getComputedStyle(parent);
      const own = getComputedStyle(el);
      const inset = parseFloat(own.paddingLeft) + parseFloat(own.paddingRight);
      const room = parent.clientWidth - parseFloat(outer.paddingLeft) - parseFloat(outer.paddingRight) - inset;
      // The same sum `pinned` hands the stylesheet, in pixels.
      const label = pinUnits * EM_PER_UNIT * parseFloat(getComputedStyle(seg).fontSize);
      const rem = parseFloat(getComputedStyle(document.documentElement).fontSize);
      const width = bigWell ? Math.max(WELL_FLOOR_PX, Math.min(label, WELL_CEIL_REM * rem)) : label;
      setPerRow(perRowFor(items.length, room, width, parseFloat(own.columnGap) || 0));
      setRoom(Math.floor(room));
    }
    measure();
    const watch = new ResizeObserver(measure);
    watch.observe(parent);
    return () => watch.disconnect();
  }, [pinned, bigWell, pinUnits, items.length]);

  // A pinned segment keeps the pinned width as its floor and grows into its
  // share of the row, so a wrapped strip fills every row to its end. The basis
  // leaves one gap of slack against sub-pixel rounding, and the growth takes
  // it back. On one row the track hugs its segments and leaves them nothing to
  // grow into. The floor gives way to the measured room, so a window narrower
  // than one segment squeezes it instead of pushing it out of the card; it is
  // a length because a percentage would count as zero while the track sizes
  // itself to its content. A segment that hugs its label grows too.
  const segmentFlex: CSSProperties = vertical
    ? {}
    : pinned === undefined
      ? { flex: '1 0 auto' }
      : {
          minWidth: room === null ? pinned : `min(${pinned}, ${room}px)`,
          flex: `1 0 calc((100% - ${perRow} * ${gap}) / ${perRow})`,
        };

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
    // moved the tab's node, so the drag's flag can outlive its gesture; see
    // endsDrag for how that stays harmless.
    if (drag.endsDrag(e)) {
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
      // a glim-well, which is surface2 itself.
      //
      // A horizontal track is as wide as its content and never wider than its
      // room: on one row it hugs its segments, which also keeps a flex column
      // from stretching it, and once it wraps it is as wide as the room and
      // its segments grow to fill every row (segmentFlex).
      className={
        vertical
          ? `flex min-h-0 flex-col gap-1 overflow-y-auto ${fill ? 'h-full' : ''} ${
              reorderable ? 'relative -mx-1 px-1' : ''
            } ${className}`
          : isWell
            ? `flex w-fit max-w-full flex-wrap items-center rounded-[var(--radius-control)] bg-carbon-surface3
              p-[0.2rem] ${className}`
            : `flex w-fit max-w-full flex-wrap items-center ${reorderable ? 'relative' : ''} ${className}`
      }
      style={vertical ? undefined : { gap }}
    >
      {orderedItems.map((item, i) => {
        const on = isOn(item.id);
        const wiggling = drag.held !== null && item.id !== drag.held;
        const look = drag.look(item.id);
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
              ${wiggling ? 'glim-tab-wiggle' : ''} ${look} ${grip}`
          : isWell
          ? // An idle segment has no fill of its own and shows the surface3
            // groove, so its hover is the step above that (rule 21).
            `${segBase} glim-nav-row glim-hue glim-hue-icon justify-center text-center leading-snug ${WELL_SIZE[size]}
              ${on ? 'glim-active bg-accent text-accentContrast' : 'bg-transparent text-carbon-textSub hover:bg-carbon-hoverRaised hover:text-carbon-text'}
              flex items-center ${!on && item.dim ? 'opacity-60' : ''}`
          : `${segBase} glim-nav-row glim-hue glim-hue-icon ${on ? `glim-active ${segOn}` : segOff} ${
              SIZE[size]
            } flex max-w-full items-center justify-center ${!on && item.dim ? 'opacity-60' : ''}
              ${wiggling ? 'glim-tab-wiggle' : ''} ${look} ${grip}`;

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
              // two competing radius classes resolve by stylesheet order.
              { ...hueStyle(i), ...segmentFlex, borderRadius: 'var(--radius-control)' }
            : { ...hueStyle(i), ...segmentFlex },
          className: cls,
          // A native link drag would fire pointercancel and end the reorder.
          draggable: reorderable ? false : undefined,
          onPointerDown: reorderable ? (e: PointerEvent<HTMLElement>) => drag.press(e, item.id) : undefined,
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
