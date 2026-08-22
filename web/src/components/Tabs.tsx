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
import { useRef, type DragEvent, type KeyboardEvent, type MouseEvent, type ReactNode } from 'react';
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
   * Opt-in drag-to-reorder (jdp: "die Tabs in den Einstellungen soll man
   * nach Belieben anordnen können"). Off by default so every OTHER caller —
   * the download list's quick filters, the corner/shape picker — is
   * completely unaffected; only a caller that passes both `reorderable` and
   * `onReorder` gets draggable tabs. Reordering never changes `active`: the
   * caller decides what that means (it does not, for Settings — moving a
   * tab does not navigate to it).
   */
  reorderable?: boolean;
  /** Called with the full, reordered list of ids after a drop. */
  onReorder?: (ids: string[]) => void;
}

export type TabsProps =
  | (Common & { select?: 'one'; active: string | null; onSelect: (id: string) => void })
  | (Common & { select: 'many'; active: ReadonlySet<string>; onSelect: (id: string) => void });

const SIZE = {
  sm: 'gap-1.5 px-2.5 py-1 text-xs',
  md: 'gap-2 px-3 py-2 text-[13px]',
} as const;

export function Tabs(props: TabsProps) {
  const { items, label, size = 'md', after, className = '', reorderable = false, onReorder } = props;

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

  // A ref, not state: the dragged id is read-only scratch space for the drag
  // gesture itself and never needs to trigger a render — dragover fires
  // continuously while the pointer moves, and re-rendering the whole strip on
  // every one of those would be wasted work for a value nothing displays.
  const draggedId = useRef<string | null>(null);

  function onDragStart(e: DragEvent<HTMLElement>, id: string) {
    draggedId.current = id;
    e.dataTransfer.effectAllowed = 'move';
  }
  function onDragOver(e: DragEvent<HTMLElement>) {
    // Required for onDrop to fire at all — a dragover with no
    // preventDefault tells the browser this is not a valid drop target.
    e.preventDefault();
  }
  function onDrop(e: DragEvent<HTMLElement>, overId: string) {
    e.preventDefault();
    const fromId = draggedId.current;
    draggedId.current = null;
    if (!fromId || fromId === overId || !onReorder) return;
    const ids = items.map((i) => i.id);
    const from = ids.indexOf(fromId);
    if (from < 0) return;
    ids.splice(from, 1);
    const to = ids.indexOf(overId);
    ids.splice(to < 0 ? ids.length : to, 0, fromId);
    onReorder(ids);
  }

  // Roving tabindex: the strip is ONE stop in the tab order and the arrows move
  // inside it. Tabbing through thirteen settings pages to reach the page is how
  // a keyboard user learns to stop using the keyboard.
  const roved = Math.max(
    0,
    items.findIndex((i) => isOn(i.id)),
  );

  function tabNodes(): HTMLElement[] {
    return Array.from(strip.current?.querySelectorAll<HTMLElement>('[data-tab-id]') ?? []);
  }

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
      className={`flex flex-wrap items-center gap-1 ${className}`}
    >
      {items.map((item, i) => {
        const on = isOn(item.id);
        const cls = `${segBase} glim-hue glim-hue-icon ${on ? `glim-active ${segOn}` : segOff} ${
          SIZE[size]
        } flex min-w-0 max-w-full items-center ${!on && item.dim ? 'opacity-60' : ''}`;

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
          style: hueStyle(i),
          // Dragging its own visible feedback: the browser already renders a
          // drag ghost, and a held-open drop target beyond that (a highlighted
          // insertion point) is more machinery than reordering four to twelve
          // settings tabs needs — the list simply jumps to its new order on drop.
          className: `${cls} ${reorderable ? 'cursor-grab active:cursor-grabbing' : ''}`,
          draggable: reorderable,
          onDragStart: reorderable ? (e: DragEvent<HTMLElement>) => onDragStart(e, item.id) : undefined,
          onDragOver: reorderable ? onDragOver : undefined,
          onDrop: reorderable ? (e: DragEvent<HTMLElement>) => onDrop(e, item.id) : undefined,
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
