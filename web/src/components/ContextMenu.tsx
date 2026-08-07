// The right-click menu, as a shell. It knows how to be a menu — where to sit,
// when to close, how to be walked with the keyboard — and nothing about
// downloads. Every wave that adds actions hands it another group.
//
// It renders into <body> for the same reason InfoBubble does (see
// docs/design-language.md): anchored where it was opened, it is at the mercy of
// every card, table and scroll container above it, and one `overflow: hidden`
// turns the menu into a sliver. At body level nothing clips it, and the position
// is measured against the viewport each time it opens.
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';

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
  /** Painted as a fault. For entries that destroy something, and only those. */
  danger?: boolean;
  disabled?: boolean;
  onSelect: () => void;
}

/**
 * A run of items separated from its neighbours by a hairline.
 *
 * Groups carry no heading: a menu of six entries under three titles is a form,
 * not a menu, and the grouping is already visible from the spacing.
 */
export interface MenuGroup {
  id: string;
  items: MenuItem[];
}

/** The gap kept between the menu and the edge of the window. */
const MARGIN = 8;

/** useContextMenu holds the open/closed state of one menu. */
export function useContextMenu() {
  const [anchor, setAnchor] = useState<MenuAnchor | null>(null);
  const openAt = useCallback((at: MenuAnchor) => setAnchor(at), []);
  const close = useCallback(() => setAnchor(null), []);
  return { anchor, openAt, close };
}

/**
 * anchorFromEvent turns a contextmenu event into a point.
 *
 * The Menu key and Shift+F10 raise the same event with no useful coordinates —
 * some browsers send 0/0, others the element's centre — so a keyboard-opened
 * menu falls back to the corner of whatever was focused. Without this the menu
 * lands in the top-left of the window and the feature is mouse-only.
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
 * edge the text starts from — the button's right edge in a right-to-left
 * language, where the menu grows the other way.
 */
export function anchorBelow(el: Element | null): MenuAnchor {
  const r = el?.getBoundingClientRect();
  if (!r) return { x: MARGIN, y: MARGIN };
  const rtl = document.documentElement.dir === 'rtl';
  return { x: rtl ? r.right : r.left, y: r.bottom + 4 };
}

export function ContextMenu({
  anchor,
  groups,
  label,
  onClose,
}: {
  anchor: MenuAnchor;
  groups: MenuGroup[];
  /** The menu's accessible name — a screen reader announces this, not the items. */
  label: string;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const itemRefs = useRef<(HTMLButtonElement | null)[]>([]);
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null);

  // An empty group is a group whose wave has nothing to offer for this
  // selection; dropping it here keeps every caller from having to check.
  const shown = groups.filter((g) => g.items.length > 0);
  const flat = shown.flatMap((g) => g.items);
  const firstEnabled = flat.findIndex((i) => !i.disabled);
  const [active, setActive] = useState(firstEnabled);

  // Measured, then placed — never the other way round. The menu is laid out at
  // the anchor, its real size read back, and only then moved so it fits; until
  // that has happened it stays invisible, because a menu that appears half off
  // the screen and jumps is worse than one that appears a frame later.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const { width, height } = el.getBoundingClientRect();
    const rtl = document.documentElement.dir === 'rtl';
    let left = rtl ? anchor.x - width : anchor.x;
    // Flip to the other side of the pointer rather than sliding along the edge:
    // sliding puts the menu under the cursor, and the first item is then one
    // stray click away from firing.
    if (left + width > window.innerWidth - MARGIN) left = anchor.x - width;
    if (left < MARGIN) left = anchor.x;
    left = Math.max(MARGIN, Math.min(left, window.innerWidth - width - MARGIN));
    let top = anchor.y;
    if (top + height > window.innerHeight - MARGIN) top = anchor.y - height;
    top = Math.max(MARGIN, Math.min(top, window.innerHeight - height - MARGIN));
    setPos({ top, left });
  }, [anchor.x, anchor.y, flat.length]);

  // Focus goes into the menu on open so the arrow keys work without a click
  // first, and comes back on close — unless an item has already moved it
  // somewhere better, which is what opening a dialog does.
  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    const el = ref.current;
    itemRefs.current[firstEnabled]?.focus();
    return () => {
      if (el && el.contains(document.activeElement)) opener?.focus?.();
    };
    // Deliberately once per open: firstEnabled changes as groups are rebuilt for
    // a changing selection, and re-running this would drag focus back to the top
    // of the menu while somebody is arrowing down it.
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) onClose();
    };
    // A measured position goes stale the moment the page moves, and a menu that
    // drifts away from the row it belongs to is pointing at the wrong download.
    const onMove = () => onClose();
    document.addEventListener('mousedown', onDown, true);
    document.addEventListener('contextmenu', onDown, true);
    window.addEventListener('scroll', onMove, true);
    window.addEventListener('resize', onMove);
    return () => {
      document.removeEventListener('mousedown', onDown, true);
      document.removeEventListener('contextmenu', onDown, true);
      window.removeEventListener('scroll', onMove, true);
      window.removeEventListener('resize', onMove);
    };
  }, [onClose]);

  if (flat.length === 0) return null;

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

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Escape') {
      e.stopPropagation();
      onClose();
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      step(active, 1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      step(active, -1);
    } else if (e.key === 'Home') {
      e.preventDefault();
      step(-1, 1);
    } else if (e.key === 'End') {
      e.preventDefault();
      step(0, -1);
    } else if (e.key === 'Tab') {
      // Tab out of a menu means "I am done with it". Letting focus land on the
      // page behind while the menu is still open leaves two things focused.
      onClose();
    }
  }

  let index = -1;
  return createPortal(
    <div
      ref={ref}
      role="menu"
      aria-label={label}
      tabIndex={-1}
      onKeyDown={onKeyDown}
      onContextMenu={(e) => e.preventDefault()}
      style={{
        position: 'fixed',
        top: pos?.top ?? anchor.y,
        left: pos?.left ?? anchor.x,
        // Transparent rather than `visibility: hidden` for the one frame before
        // the size is known: a hidden element cannot take focus, and the first
        // item is focused as soon as the menu mounts.
        opacity: pos ? undefined : 0,
      }}
      className="glim-card glim-fade z-[60] min-w-[13rem] max-w-[22rem] divide-y divide-carbon-border/60 py-0.5"
    >
      {shown.map((g) => (
        <div key={g.id} className="py-1">
          {g.items.map((item) => {
            index++;
            const i = index;
            return (
              <button
                key={item.id}
                ref={(el) => {
                  itemRefs.current[i] = el;
                }}
                role="menuitem"
                type="button"
                tabIndex={-1}
                disabled={item.disabled}
                onMouseEnter={() => setActive(i)}
                onClick={() => {
                  // Closed before the action runs: an entry that opens a dialog
                  // must not have a menu sitting on top of it.
                  onClose();
                  item.onSelect();
                }}
                className={`flex w-full items-center gap-2.5 px-3 py-1.5 text-start text-[13px]
                  transition-colors outline-none disabled:opacity-35 disabled:pointer-events-none ${
                    item.danger
                      ? 'text-statusFail hover:bg-statusFailBg focus-visible:bg-statusFailBg'
                      : 'text-carbon-textSub hover:bg-carbon-hover hover:text-carbon-text focus-visible:bg-carbon-hover focus-visible:text-carbon-text'
                  }`}
              >
                <span className="grid h-4 w-4 shrink-0 place-items-center">{item.icon}</span>
                <span className="min-w-0 flex-1 truncate">{item.label}</span>
                {item.detail && (
                  <span className="glim-num shrink-0 text-[11px] text-carbon-textMuted">{item.detail}</span>
                )}
              </button>
            );
          })}
        </div>
      ))}
    </div>,
    document.body,
  );
}
