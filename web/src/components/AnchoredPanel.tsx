// A window that opens under the button that asked for it rather than in the
// middle of the screen: the quick settings in the shell bar and the batch
// options of the link collector. It stands on the scrim every window in the
// app stands on, so it is as modal as a centred one, and both share one width.
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode, type RefObject } from 'react';
import { useLocation } from 'react-router-dom';
import { hueVars } from '../lib/appearance';
import { useT } from '../lib/i18n';
import { IconClose } from '../lib/icons';
import { openWindow } from '../lib/windowStack';
import { Button, SectionTitle } from './ui';

const MARGIN = 8;
// The title badge straddles the panel's top edge, so above the panel there has
// to be room for its upper half as well as the margin.
const GAP_BELOW = 18;

/**
 * placePanel puts the panel under its button, on the edge the text starts
 * from, and above it when the window has no room below. Where neither side
 * holds the whole panel it takes the roomier one and its rows scroll, which
 * `maxHeight` sets up. It is clamped into the window with an 8px margin, like
 * ui.tsx's placeBubble. The panel declares its width, so measuring it before
 * placing is safe.
 */
function placePanel(r: DOMRect, w: number, h: number, rtl: boolean): { left: number; top: number; maxHeight: number } {
  const vw = document.documentElement.clientWidth || window.innerWidth;
  const vh = document.documentElement.clientHeight || window.innerHeight;
  const left = Math.max(MARGIN, Math.min(vw - MARGIN - w, rtl ? r.right - w : r.left));
  const below = r.bottom + GAP_BELOW;
  const roomBelow = vh - MARGIN - below;
  const roomAbove = r.top - MARGIN - GAP_BELOW;
  if (h <= roomBelow || roomBelow >= roomAbove) return { left, top: below, maxHeight: roomBelow };
  const height = Math.min(h, roomAbove);
  return { left, top: r.top - MARGIN - height, maxHeight: height };
}

/**
 * Whether an event's target is the panel, its button or anything opened from
 * them: a dropdown's menu in its own portal, or a window over the panel such
 * as the folder picker.
 */
function inPanel(n: EventTarget | null, panel: HTMLElement | null, anchor: HTMLElement | null): boolean {
  if (!(n instanceof Node)) return false;
  if (panel?.contains(n) || anchor?.contains(n)) return true;
  return n instanceof Element && n.closest('[role="menu"], [role="dialog"]') !== null;
}

/**
 * AnchoredPanel is the open window under `anchor`. Escape, a press on the
 * scrim, a scroll outside it, a resize or a change of page closes it, since a
 * fixed panel stops pointing at its button once anything moves. It is rendered
 * in place rather than portalled to <body>, so the toasts its rows raise stay
 * above the scrim, as they do over a Modal. `ready` tells it the rows have
 * arrived, so it places itself once with all of them.
 */
export function AnchoredPanel({
  anchor,
  title,
  hue,
  ready = true,
  onClose,
  children,
}: {
  anchor: RefObject<HTMLElement | null>;
  title: string;
  hue?: number;
  ready?: boolean;
  onClose: () => void;
  children?: ReactNode;
}) {
  const { t } = useT();
  const scrim = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const body = useRef<HTMLDivElement>(null);
  const [at, setAt] = useState<{ left: number; top: number; maxHeight: number } | null>(null);
  const placed = at !== null;

  const closeAndReturn = useCallback(() => {
    onClose();
    anchor.current?.querySelector('button')?.focus();
  }, [onClose, anchor]);

  // Measured before paint and hidden until then, placed again whenever the
  // rows change size, because a flip above depends on the height. The height
  // measured is the one the rows want, not the one an earlier placement
  // allowed them.
  useLayoutEffect(() => {
    const button = anchor.current;
    const box = panel.current;
    const rows = body.current;
    if (!button || !box) return;
    const place = () => {
      const rtl = getComputedStyle(button).direction === 'rtl';
      const h = box.offsetHeight + (rows ? rows.scrollHeight - rows.clientHeight : 0);
      const next = placePanel(button.getBoundingClientRect(), box.offsetWidth, h, rtl);
      setAt((cur) =>
        cur && cur.left === next.left && cur.top === next.top && cur.maxHeight === next.maxHeight ? cur : next,
      );
    };
    place();
    if (!rows) return;
    const watch = new ResizeObserver(place);
    for (const child of Array.from(rows.children)) watch.observe(child);
    return () => watch.disconnect();
  }, [anchor, ready]);

  // Focus moves in once the panel sits where it stays, so Tab reaches the
  // rows next rather than the rest of the page.
  useEffect(() => {
    if (placed) panel.current?.focus({ preventScroll: true });
  }, [placed]);

  // Escape goes through the window stack like every window's, so a window
  // opened over the panel closes first.
  useEffect(() => {
    const me = scrim.current;
    if (!me) return;
    return openWindow(me, closeAndReturn);
  }, [closeAndReturn]);

  const { pathname } = useLocation();
  const first = useRef(pathname);
  useEffect(() => {
    if (pathname !== first.current) onClose();
  }, [pathname, onClose]);

  useEffect(() => {
    const onScroll = (e: Event) => {
      if (!inPanel(e.target, panel.current, anchor.current)) onClose();
    };
    window.addEventListener('scroll', onScroll, true);
    window.addEventListener('resize', onClose);
    return () => {
      window.removeEventListener('scroll', onScroll, true);
      window.removeEventListener('resize', onClose);
    };
  }, [onClose, anchor]);

  return (
    // The scrim is Modal's (ui.tsx) and its fade carries the panel in. The
    // rows' menus and bubbles open above it.
    <div
      ref={scrim}
      className="glim-modal-backdrop fixed inset-0 z-50"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        // Tabbing out of the panel closes it, as a press on the scrim does.
        onBlur={(e) => {
          if (e.relatedTarget && !inPanel(e.relatedTarget, panel.current, anchor.current)) onClose();
        }}
        style={{
          left: at?.left ?? 0,
          top: at?.top ?? 0,
          maxHeight: at?.maxHeight,
          visibility: placed ? undefined : 'hidden',
          ...(hue === undefined ? {} : (hueVars(hue) as CSSProperties)),
        }}
        // Wide enough for the longest caption and the widest control on one
        // line, the seven priority rungs included.
        className={`glim-card fixed flex w-[48rem] max-w-[calc(100vw-1rem)] flex-col gap-5 p-5 outline-none ${
          hue === undefined ? '' : 'glim-hue'
        }`}
      >
        <SectionTitle>{title}</SectionTitle>
        {ready && (
          // The rows scroll inside the card rather than the card itself,
          // which would clip the title badge on its edge. The inset keeps
          // focus outlines inside the scrolling box.
          <div ref={body} className="-m-1 flex min-h-0 flex-col gap-5 overflow-y-auto p-1">
            {children}
          </div>
        )}
        {/* The way out every window has, below the rows so it stays in view
            while they scroll. */}
        <div className="flex justify-end">
          <Button kind="secondary" labelled icon={<IconClose />} title={t('common.close')} onClick={closeAndReturn} />
        </div>
      </div>
    </div>
  );
}
