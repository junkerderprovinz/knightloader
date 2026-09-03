// The right-click menu, as a shell. It knows how to be a menu — where to sit,
// when to close, how to be walked with the keyboard — and nothing about
// downloads. Every wave that adds actions hands it another group.
//
// It renders into <body> for the same reason InfoBubble does (see
// docs/design-language.md): anchored where it was opened, it is at the mercy of
// every card, table and scroll container above it, and one `overflow: hidden`
// turns the menu into a sliver. At body level nothing clips it, and the position
// is measured against the viewport each time it opens.
//
// A menu is one or more PANELS: the one at the pointer, and one more for every
// submenu that is open under it. Panels are siblings at body level rather than
// nested markup, so a submenu is clipped by nothing either — and the panel that
// opened it stays exactly where it was.
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
  /** Painted as a fault. For entries that destroy something, and only those. */
  danger?: boolean;
  disabled?: boolean;
  /**
   * Marks the one row of a submenu that is a set of CHOICES rather than a list
   * of verbs: the value the selection is already at.
   *
   * It is its own mark rather than a tick the caller passes as `icon`, because
   * the two answer different questions and a row needs both at once - the
   * gutter glyph says what the row is, this says which row is in force. Setting
   * it at all, true or false, turns the group into a radio set for a screen
   * reader, so a choice with nothing selected still announces as a choice
   * rather than as seven unrelated commands.
   */
  checked?: boolean;
  /** What choosing it does. Absent on an item whose whole job is its submenu. */
  onSelect?: () => void;
  /**
   * A nested menu, in the same groups shape.
   *
   * JDownloader's menus are full of these and the muscle memory expects them:
   * four ways to move something in the queue behind one word reads as one idea,
   * while four more entries in a menu that already has a dozen reads as noise.
   * An item that has one never acts on its own — it opens, it does not fire.
   */
  submenu?: MenuGroup[];
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

/**
 * How long an open submenu, and the highlight on the row that opened it,
 * survive the pointer moving off that row. In milliseconds.
 *
 * jdp: "Der hoverbalken bleibt immer an aufräumen, priorität und verschieben
 * hängen." Those three are the submenu parents, and nothing ever let go of
 * them: the panel and the lit row were only ever replaced by hovering another
 * parent, so once one had been touched the menu carried a second highlight
 * around for the rest of its life.
 *
 * Letting go the instant the pointer leaves is the other broken menu. The
 * nested panel is a sibling at body level, not a child of the row, so reaching
 * it means leaving the row first, and a submenu that closes mid-crossing can
 * never be entered at all. So the release is delayed by about the length of
 * that crossing, and entering the panel cancels it. Anything much shorter
 * clips a slow diagonal; anything much longer and the highlight reads as stuck
 * again, which is the complaint.
 */
const SUBMENU_GRACE = 200;

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

/**
 * Where a panel wants to sit, and where it should grow instead when it does not
 * fit there.
 *
 * `flipAt` is the coordinate the panel flips around: the pointer for the menu
 * itself, the opening row's other edge for a submenu. Sliding along the window
 * edge instead would put a submenu on top of the item that opened it.
 */
interface Spot {
  x: number;
  y: number;
  flipAt?: number;
}

/**
 * Every panel of the currently open menu, so a click inside a submenu is not
 * mistaken for a click outside the menu. One ref per menu tree, not a module
 * global: two menus can be mounted at once (a dropdown and a right-click), and
 * they must be able to close each other.
 */
const PanelsCtx = createContext<{ current: Set<HTMLElement> } | null>(null);

/** submenuSpot places a nested panel beside the row that opens it. */
function submenuSpot(el: HTMLElement): Spot {
  const r = el.getBoundingClientRect();
  const rtl = document.documentElement.dir === 'rtl';
  // The 2px overlap is deliberate: a submenu flush against its row leaves a
  // hairline gap that the pointer can fall through on the way over.
  return rtl
    ? { x: r.left + 2, y: r.top - 4, flipAt: r.right - 2 }
    : { x: r.right - 2, y: r.top - 4, flipAt: r.left + 2 };
}

/**
 * The "there is more behind this one" mark, as a solid triangle.
 *
 * It was a stroked chevron, which is the one thing GlimStone's icon rule
 * forbids (see lib/icons.tsx's own header): a hairline outline beside the
 * filled glyphs in the same row reads as a lighter class of mark, and this is
 * the most-rendered glyph in the whole menu. A triangle rather than a filled
 * chevron so it stays distinct from the fold entries' own chevrons, which point
 * up and down two rows away.
 */
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
  onClose,
  onDismiss,
  onPointerIn,
  onPointerOut,
}: {
  spot: Spot;
  groups: MenuGroup[];
  label: string;
  /** Close the whole menu. What choosing an item does. */
  onClose: () => void;
  /** Close only this panel and hand focus back. Absent on the outermost one. */
  onDismiss?: () => void;
  /**
   * Told when the pointer arrives in or leaves this panel, so the panel that
   * opened it can hold off its own SUBMENU_GRACE timer. Both are chained up the
   * whole stack rather than handled one level deep: moving from a submenu into
   * a submenu of its own leaves every panel above it, and without the chain the
   * outermost one would time out and take the branch the pointer is standing in
   * with it.
   */
  onPointerIn?: () => void;
  onPointerOut?: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const itemRefs = useRef<(HTMLButtonElement | null)[]>([]);
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null);
  const [sub, setSub] = useState<OpenSub | null>(null);
  const panels = useContext(PanelsCtx);

  // An empty group is a group whose wave has nothing to offer for this
  // selection; dropping it here keeps every caller from having to check.
  const shown = groups.filter((g) => g.items.length > 0);
  const flat = shown.flatMap((g) => g.items);
  const firstEnabled = Math.max(
    0,
    flat.findIndex((i) => !i.disabled),
  );
  const [active, setActive] = useState(firstEnabled);

  // The grace timer behind SUBMENU_GRACE. It lives on the panel that OPENED the
  // submenu, because that is the panel holding both things the pointer leaving
  // has to release: the nested panel and the highlight on the row it belongs
  // to. `openHere` paints that highlight, so dropping `sub` drops both at once.
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
  // Choosing an entry closes the whole tree at once, so a panel can go away
  // with its timer still armed and fire into a component that is no longer
  // there.
  useEffect(() => cancelClose, [cancelClose]);

  const pointerIn = useCallback(() => {
    cancelClose();
    onPointerIn?.();
  }, [cancelClose, onPointerIn]);
  const pointerOut = useCallback(() => {
    scheduleClose();
    onPointerOut?.();
  }, [scheduleClose, onPointerOut]);

  // Every panel joins the tree's registry, which is what tells the outermost
  // panel that a mousedown landed inside the menu and not outside it.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || !panels) return;
    const set = panels.current;
    set.add(el);
    return () => {
      set.delete(el);
    };
  }, [panels]);

  // Measured, then placed — never the other way round. The panel is laid out at
  // its spot, its real size read back, and only then moved so it fits; until
  // that has happened it stays invisible, because a menu that appears half off
  // the screen and jumps is worse than one that appears a frame later.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const { width, height } = el.getBoundingClientRect();
    const rtl = document.documentElement.dir === 'rtl';
    const flip = spot.flipAt ?? spot.x;
    // Flip to the other side rather than sliding along the edge: sliding puts
    // the panel under the cursor, and the first item is then one stray click
    // away from firing.
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

  // Focus lands in the panel as it opens, so the arrow keys work without a
  // click first — for a submenu that means the first of its own entries.
  useEffect(() => {
    itemRefs.current[firstEnabled]?.focus();
    // Deliberately once per open: firstEnabled changes as groups are rebuilt
    // for a changing selection, and re-running this would drag focus back to
    // the top while somebody is arrowing down.
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
      // Out of a submenu and back onto the row that opened it — the one thing
      // that makes a nested menu usable without a mouse.
      e.preventDefault();
      onDismiss();
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
            // Transparent rather than `visibility: hidden` for the one frame
            // before the size is known: a hidden element cannot take focus, and
            // the first item is focused as soon as the panel mounts.
            opacity: pos ? undefined : 0,
          }}
          className="glim-card glim-fade z-[60] min-w-[13rem] max-w-[22rem] divide-y divide-carbon-border/60 py-0.5"
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
                      // Hovering a submenu parent swaps the open panel. Hovering
                      // a plain entry no longer leaves the open one alone - it
                      // starts the grace timer, which is what finally lets go of
                      // a parent the pointer has walked away from. Travelling
                      // diagonally across a row or two to reach the panel takes
                      // a fraction of SUBMENU_GRACE, so the crossing survives.
                      if (nested) openSub(item, e.currentTarget);
                      else if (sub) scheduleClose();
                    }}
                    onClick={(e) => {
                      if (nested) {
                        openSub(item, e.currentTarget);
                        return;
                      }
                      // Closed before the action runs: an entry that opens a
                      // dialog must not have a menu sitting on top of it.
                      onClose();
                      item.onSelect?.();
                    }}
                    className={`flex w-full items-center gap-2.5 px-3 py-1.5 text-start text-[13px]
                      transition-colors outline-none disabled:opacity-35 disabled:pointer-events-none ${
                        item.danger
                          ? 'text-statusFail hover:bg-statusFailBg focus-visible:bg-statusFailBg'
                          : `${item.checked ? 'text-carbon-text' : 'text-carbon-textSub'}
                             hover:bg-carbon-hover hover:text-carbon-text
                             focus-visible:bg-carbon-hover focus-visible:text-carbon-text ${
                               openHere ? 'bg-carbon-hover text-carbon-text' : ''
                             }`
                      }`}
                  >
                    {/* The gutter sizes the glyph rather than the call site: a
                        CSS height/width beats the width/height attributes an
                        icon component sets, so every entry lands at exactly
                        14px whatever its author passed. Before this the same
                        panel could hold a 14px folder next to a 16px key, and
                        the alternative was ~30 call sites each repeating the
                        same two numbers. It stays rendered when there is no
                        icon, so a group where only some entries carry one still
                        lines its labels up. The column is glyphs only: what is
                        SELECTED is marked at the other end of the row (see
                        MenuItem.checked), because a row can need to say what it
                        is and that it is the one in force at the same time. */}
                    <span className="grid h-4 w-4 shrink-0 place-items-center [&_svg]:h-3.5 [&_svg]:w-3.5">
                      {item.icon}
                    </span>
                    <span className="min-w-0 flex-1 truncate">{item.label}</span>
                    {item.detail && (
                      <span className="glim-num shrink-0 text-[11px] text-carbon-textMuted">{item.detail}</span>
                    )}
                    {/* The "this is the one in force" mark, at the trailing end
                        where the caret would be on a parent row - the two never
                        occur together, a choice has no submenu. Accent ink and a
                        smaller box than the gutter, so it reads as a mark on the
                        row rather than a second glyph competing with the first. */}
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
  onClose,
}: {
  anchor: MenuAnchor;
  groups: MenuGroup[];
  /** The menu's accessible name — a screen reader announces this, not the items. */
  label: string;
  onClose: () => void;
}) {
  const panels = useRef(new Set<HTMLElement>());

  // Focus goes into the menu on open and comes back on close — unless an item
  // has already moved it somewhere better, which is what opening a dialog does.
  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    const open = panels.current;
    return () => {
      const at = document.activeElement;
      if (at && [...open].some((el) => el.contains(at))) opener?.focus?.();
    };
  }, []);

  useEffect(() => {
    const inside = (target: Node | null) =>
      !!target && [...panels.current].some((el) => el.contains(target));
    const onDown = (e: MouseEvent) => {
      if (!inside(e.target as Node)) onClose();
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

  if (!groups.some((g) => g.items.length > 0)) return null;

  return (
    <PanelsCtx.Provider value={panels}>
      <Panel spot={{ x: anchor.x, y: anchor.y }} groups={groups} label={label} onClose={onClose} />
    </PanelsCtx.Provider>
  );
}
