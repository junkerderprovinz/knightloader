// The context menu shell: placement, closing and keyboard navigation, with
// callers supplying groups of items. Every panel, the menu and each open
// submenu, renders into <body> as a sibling so no overflow above can clip it,
// and is placed against the viewport.
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { createPortal } from 'react-dom';
import { IconCheck } from '../lib/icons';

/** Where the menu was asked for, in viewport coordinates. */
export interface MenuAnchor {
  x: number;
  y: number;
}

export interface MenuItem {
  id: string;
  label: string;
  icon?: ReactNode;
  /** Quiet text at the end of the row: a count, a keyboard shortcut. */
  detail?: string;
  // No `danger` flag: a destructive entry looks like its neighbours, and the
  // dialog it opens does the warning.
  disabled?: boolean;
  /**
   * Marks the current value in a submenu of choices. Setting it at all turns
   * the group into a radio set for screen readers; it is separate from `icon`
   * because a row can need both.
   */
  checked?: boolean;
  /** Absent on an item whose only job is its submenu. */
  onSelect?: () => void;
  /** A nested menu. An item with one opens it rather than firing. */
  submenu?: MenuGroup[];
}

/** A run of items separated from its neighbours by a hairline, without a heading. */
export interface MenuGroup {
  id: string;
  items: MenuItem[];
}

const MARGIN = 8;

// How long, in ms, an open submenu and its lit parent row survive the pointer
// leaving that row. The pointer has to cross the row to reach the sibling
// panel, and entering the panel cancels the timer; much longer would make the
// highlight look stuck.
const SUBMENU_GRACE = 200;

/** useContextMenu holds the open/closed state of one menu. */
export function useContextMenu() {
  const [anchor, setAnchor] = useState<MenuAnchor | null>(null);
  const openAt = useCallback((at: MenuAnchor) => setAnchor(at), []);
  const close = useCallback(() => setAnchor(null), []);
  return { anchor, openAt, close };
}

/**
 * anchorFromEvent turns a contextmenu event into a point. The Menu key and
 * Shift+F10 send no useful coordinates, so a keyboard-opened menu falls back
 * to the focused element's corner.
 */
export function anchorFromEvent(e: {
  clientX: number;
  clientY: number;
  currentTarget: EventTarget | null;
  target: EventTarget | null;
}): MenuAnchor {
  if (e.clientX > 0 || e.clientY > 0) return { x: e.clientX, y: e.clientY };
  const el = (e.target instanceof Element ? e.target : null) ?? (e.currentTarget as Element | null);
  const r = el?.getBoundingClientRect();
  return r ? { x: r.left + 12, y: r.bottom - 4 } : { x: MARGIN, y: MARGIN };
}

/**
 * anchorBelow puts a dropdown under the control that opened it, aligned on the
 * edge the text starts from, which is the right edge in RTL languages.
 */
export function anchorBelow(el: Element | null): MenuAnchor {
  const r = el?.getBoundingClientRect();
  if (!r) return { x: MARGIN, y: MARGIN };
  const rtl = document.documentElement.dir === 'rtl';
  return { x: rtl ? r.right : r.left, y: r.bottom + 4 };
}

/**
 * Spot is where a panel wants to sit. `flipAt` is the coordinate it flips
 * around when it does not fit: the pointer for the menu, the opening row's
 * other edge for a submenu, so a submenu never covers its parent row.
 */
interface Spot {
  x: number;
  y: number;
  flipAt?: number;
}

// Every panel of one open menu, so a click in a submenu is not taken as a click
// outside. Per tree rather than global, since two menus can be open at once.
const PanelsCtx = createContext<{ current: Set<HTMLElement> } | null>(null);

/** submenuSpot places a nested panel beside the row that opens it. */
function submenuSpot(el: HTMLElement): Spot {
  const r = el.getBoundingClientRect();
  const rtl = document.documentElement.dir === 'rtl';
  // A 2px overlap, so the pointer cannot fall through a gap on the way over.
  return rtl
    ? { x: r.left + 2, y: r.top - 4, flipAt: r.right - 2 }
    : { x: r.right - 2, y: r.top - 4, flipAt: r.left + 2 };
}

// The submenu mark: a solid triangle, since GlimStone glyphs are filled, and
// distinct from the fold entries' chevrons.
function Caret({ rtl }: { rtl: boolean }) {
  return (
    <svg
      viewBox="0 0 16 16"
      width={12}
      height={12}
      fill="currentColor"
      aria-hidden
      focusable="false"
      style={rtl ? { transform: 'scaleX(-1)' } : undefined}
    >
      <path d="M6 3.6 10.6 8 6 12.4Z" />
    </svg>
  );
}

interface OpenSub {
  id: string;
  spot: Spot;
  groups: MenuGroup[];
  label: string;
}

function Panel({
  spot,
  groups,
  label,
  minWidth,
  onClose,
  onDismiss,
  onPointerIn,
  onPointerOut,
}: {
  spot: Spot;
  groups: MenuGroup[];
  label: string;
  minWidth?: number;
  /** Closes the whole menu. */
  onClose: () => void;
  /** Closes only this submenu and hands focus back. */
  onDismiss?: () => void;
  /**
   * Pointer arrival and departure, chained up the whole stack so every parent
   * holds its SUBMENU_GRACE timer while the pointer is in a deeper submenu.
   */
  onPointerIn?: () => void;
  onPointerOut?: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const itemRefs = useRef<(HTMLButtonElement | null)[]>([]);
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null);
  const [sub, setSub] = useState<OpenSub | null>(null);
  const panels = useContext(PanelsCtx);

  // Empty groups are dropped here so callers need not check.
  const shown = groups.filter((g) => g.items.length > 0);
  const flat = shown.flatMap((g) => g.items);
  // A list of choices opens on the one in force, as a native select does, so
  // the arrow keys start from where the value is.
  const checked = flat.findIndex((i) => i.checked && !i.disabled);
  const firstEnabled = checked >= 0 ? checked : Math.max(0, flat.findIndex((i) => !i.disabled));
  const [active, setActive] = useState(firstEnabled);

  // Held by the parent panel, since dropping `sub` releases both the submenu
  // and its lit row.
  const closeTimer = useRef<number | null>(null);
  const cancelClose = useCallback(() => {
    if (closeTimer.current === null) return;
    window.clearTimeout(closeTimer.current);
    closeTimer.current = null;
  }, []);
  const scheduleClose = useCallback(() => {
    cancelClose();
    closeTimer.current = window.setTimeout(() => {
      closeTimer.current = null;
      setSub(null);
    }, SUBMENU_GRACE);
  }, [cancelClose]);
  // A panel can unmount with its timer still armed.
  useEffect(() => cancelClose, [cancelClose]);

  const pointerIn = useCallback(() => {
    cancelClose();
    onPointerIn?.();
  }, [cancelClose, onPointerIn]);
  const pointerOut = useCallback(() => {
    scheduleClose();
    onPointerOut?.();
  }, [scheduleClose, onPointerOut]);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || !panels) return;
    const set = panels.current;
    set.add(el);
    return () => {
      set.delete(el);
    };
  }, [panels]);

  // Laid out at its spot, measured, then moved to fit; transparent until then.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const { width, height } = el.getBoundingClientRect();
    const rtl = document.documentElement.dir === 'rtl';
    const flip = spot.flipAt ?? spot.x;
    // Flipping rather than sliding keeps the first item out from under the cursor.
    let left = rtl ? spot.x - width : spot.x;
    if (rtl ? left < MARGIN : left + width > window.innerWidth - MARGIN) {
      left = rtl ? flip : flip - width;
    }
    left = Math.max(MARGIN, Math.min(left, window.innerWidth - width - MARGIN));
    let top = spot.y;
    if (top + height > window.innerHeight - MARGIN) top = spot.y - height;
    top = Math.max(MARGIN, Math.min(top, window.innerHeight - height - MARGIN));
    setPos({ top, left });
  }, [spot.x, spot.y, spot.flipAt, flat.length]);

  // Once per open, so the arrow keys work at once; re-running on a changed
  // firstEnabled would pull focus back to the top mid-navigation.
  useEffect(() => {
    itemRefs.current[firstEnabled]?.focus();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  function step(from: number, delta: number): void {
    if (flat.length === 0) return;
    let i = from;
    for (let n = 0; n < flat.length; n++) {
      i = (i + delta + flat.length) % flat.length;
      if (!flat[i].disabled) break;
    }
    setActive(i);
    itemRefs.current[i]?.focus();
  }

  function openSub(item: MenuItem, el: HTMLElement | null): void {
    if (!item.submenu || !el) return;
    cancelClose();
    setSub({ id: item.id, spot: submenuSpot(el), groups: item.submenu, label: item.label });
  }

  function closeSub(refocus: boolean): void {
    const id = sub?.id;
    cancelClose();
    setSub(null);
    if (!refocus || !id) return;
    const i = flat.findIndex((x) => x.id === id);
    if (i >= 0) itemRefs.current[i]?.focus();
  }

  function onKeyDown(e: React.KeyboardEvent) {
    const rtl = document.documentElement.dir === 'rtl';
    const forward = rtl ? 'ArrowLeft' : 'ArrowRight';
    const back = rtl ? 'ArrowRight' : 'ArrowLeft';
    const item = flat[active];

    if (e.key === 'Escape') {
      e.stopPropagation();
      if (onDismiss) onDismiss();
      else onClose();
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      step(active, 1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      step(active, -1);
    } else if (e.key === forward && item?.submenu) {
      e.preventDefault();
      openSub(item, itemRefs.current[active]);
    } else if (e.key === back && onDismiss) {
      e.preventDefault();
      onDismiss();
    } else if (e.key === 'Home') {
      e.preventDefault();
      step(-1, 1);
    } else if (e.key === 'End') {
      e.preventDefault();
      step(0, -1);
    } else if (e.key === 'Tab') {
      // Tab leaves the menu, so the menu closes.
      onClose();
    }
  }

  const rtl = typeof document !== 'undefined' && document.documentElement.dir === 'rtl';
  let index = -1;

  return (
    <>
      {createPortal(
        <div
          ref={ref}
          role="menu"
          aria-label={label}
          tabIndex={-1}
          onKeyDown={onKeyDown}
          onMouseEnter={pointerIn}
          onMouseLeave={pointerOut}
          onContextMenu={(e) => e.preventDefault()}
          style={{
            position: 'fixed',
            top: pos?.top ?? spot.y,
            left: pos?.left ?? spot.x,
            // Not visibility: hidden, which would block the first item's focus.
            opacity: pos ? undefined : 0,
            // Inline, so it has to carry the class's own floor as well.
            minWidth: minWidth ? `max(13rem, ${minWidth}px)` : undefined,
          }}
          // A long list of choices scrolls inside the panel rather than running
          // off the screen.
          className="glim-card glim-fade z-[60] max-h-[min(24rem,calc(100vh-1rem))] min-w-[13rem] max-w-[22rem]
            divide-y divide-carbon-border/60 overflow-y-auto py-0.5"
        >
          {shown.map((g) => (
            <div key={g.id} className="py-1">
              {g.items.map((item) => {
                index++;
                const i = index;
                const nested = !!item.submenu;
                const openHere = sub?.id === item.id;
                return (
                  <button
                    key={item.id}
                    ref={(el) => {
                      itemRefs.current[i] = el;
                    }}
                    role={item.checked === undefined ? 'menuitem' : 'menuitemradio'}
                    aria-checked={item.checked}
                    type="button"
                    tabIndex={-1}
                    disabled={item.disabled}
                    aria-haspopup={nested ? 'menu' : undefined}
                    aria-expanded={nested ? openHere : undefined}
                    onMouseEnter={(e) => {
                      setActive(i);
                      // A plain entry starts the grace timer on an open submenu.
                      if (nested) openSub(item, e.currentTarget);
                      else if (sub) scheduleClose();
                    }}
                    onClick={(e) => {
                      if (nested) {
                        openSub(item, e.currentTarget);
                        return;
                      }
                      // Closed first, so a dialog the entry opens is not covered.
                      onClose();
                      item.onSelect?.();
                    }}
                    className={`flex w-full items-center gap-2.5 px-3 py-1.5 text-start text-[12px]
                      transition-colors outline-none disabled:opacity-35 disabled:pointer-events-none
                      ${item.checked ? 'text-carbon-text' : 'text-carbon-textSub'}
                      hover:bg-carbon-hover hover:text-carbon-text
                      focus-visible:bg-carbon-hover focus-visible:text-carbon-text
                      ${openHere ? 'bg-carbon-hover text-carbon-text' : ''}`}
                  >
                    {/* The gutter sets every glyph to 14px whatever size the
                        caller passed, and stays without an icon so labels line
                        up. */}
                    <span className="grid h-4 w-4 shrink-0 place-items-center [&_svg]:h-3.5 [&_svg]:w-3.5">
                      {item.icon}
                    </span>
                    <span className="min-w-0 flex-1 truncate">{item.label}</span>
                    {item.detail && (
                      <span className="glim-num shrink-0 text-[11px] text-carbon-textMuted">{item.detail}</span>
                    )}
                    {/* In the caret's place, since a choice has no submenu. */}
                    {item.checked && (
                      <span className="shrink-0 text-accentInk [&_svg]:h-3 [&_svg]:w-3">
                        <IconCheck />
                      </span>
                    )}
                    {nested && (
                      <span className="shrink-0 text-carbon-textMuted">
                        <Caret rtl={rtl} />
                      </span>
                    )}
                  </button>
                );
              })}
            </div>
          ))}
        </div>,
        document.body,
      )}

      {sub && (
        <Panel
          key={sub.id}
          spot={sub.spot}
          groups={sub.groups}
          label={sub.label}
          onClose={onClose}
          onDismiss={() => closeSub(true)}
          onPointerIn={pointerIn}
          onPointerOut={pointerOut}
        />
      )}
    </>
  );
}

export function ContextMenu({
  anchor,
  groups,
  label,
  minWidth,
  onClose,
}: {
  anchor: MenuAnchor;
  groups: MenuGroup[];
  /** The menu's accessible name. */
  label: string;
  /** A dropdown's menu is at least as wide as the field it opens from. */
  minWidth?: number;
  onClose: () => void;
}) {
  const panels = useRef(new Set<HTMLElement>());

  // Restores focus to the opener on close. It has to be a layout effect at both
  // ends: it captures the opener before the Panel's passive effect focuses the
  // first entry, and its cleanup runs before the Panel's, while the panels are
  // still registered and focus is still inside them. A dialog opened by an
  // entry still takes focus afterwards.
  useLayoutEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    const open = panels.current;
    return () => {
      const at = document.activeElement;
      // Only while focus is inside the menu, so a click elsewhere keeps its focus.
      if (!at || ![...open].some((el) => el.contains(at))) return;
      if (opener?.isConnected) opener.focus?.();
    };
  }, []);

  useEffect(() => {
    const inside = (target: Node | null) =>
      !!target && [...panels.current].some((el) => el.contains(target));
    const onDown = (e: MouseEvent) => {
      if (!inside(e.target as Node)) onClose();
    };
    // A scroll or resize leaves the menu pointing at the wrong row. A panel
    // scrolling its own list moves nothing it is anchored to, and focusing an
    // entry below the fold scrolls it.
    const onScroll = (e: Event) => {
      if (!inside(e.target as Node)) onClose();
    };
    const onResize = () => onClose();
    document.addEventListener('mousedown', onDown, true);
    document.addEventListener('contextmenu', onDown, true);
    window.addEventListener('scroll', onScroll, true);
    window.addEventListener('resize', onResize);
    return () => {
      document.removeEventListener('mousedown', onDown, true);
      document.removeEventListener('contextmenu', onDown, true);
      window.removeEventListener('scroll', onScroll, true);
      window.removeEventListener('resize', onResize);
    };
  }, [onClose]);

  if (!groups.some((g) => g.items.length > 0)) return null;

  return (
    <PanelsCtx.Provider value={panels}>
      <Panel spot={{ x: anchor.x, y: anchor.y }} groups={groups} label={label} minWidth={minWidth} onClose={onClose} />
    </PanelsCtx.Provider>
  );
}
