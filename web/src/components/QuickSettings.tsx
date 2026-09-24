// Quick settings: the square in the shell bar and the panel it opens. Each row
// is the field the setting's own page draws, bound to the same key, so a value
// reads and saves the same way in both places. The settings routes are not
// forwarded to peers, so the shell offers this for the local instance only.
import { Fragment, useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { fetchSettings, patchSettings, type Settings } from '../lib/api';
import { useT } from '../lib/i18n';
import { IconSliders } from '../lib/icons';
import { useToast } from '../lib/toast';
import { SpeedLimitField } from '../pages/settings/downloads/SpeedLimit';
import { Button, SectionTitle } from './ui';

type Patch = (fields: Partial<Settings>) => void;

/** A row of the panel: the settings page's own field for one key. */
interface QuickRow {
  id: string;
  render: (cfg: Settings, patch: Patch) => ReactNode;
}

const ROWS: QuickRow[] = [
  {
    id: 'speedLimit',
    render: (cfg, patch) => <SpeedLimitField value={cfg.speedLimit} onValue={(speedLimit) => patch({ speedLimit })} />,
  },
];

// The settings page's autosave delay (pages/Settings.tsx), so a number typed
// here goes out once rather than once per keystroke.
const SAVE_MS = 600;

const MARGIN = 8;
// Below the square the gap also holds the lower half of the title badge, which
// straddles the panel's top edge.
const GAP_BELOW = 18;

/**
 * placePanel puts the panel under the square, on the edge the text starts
 * from, and above it when the window has no room below. It is clamped into the
 * window with an 8px margin, like ui.tsx's placeBubble. The panel declares its
 * width, so measuring it before placing is safe.
 */
function placePanel(r: DOMRect, w: number, h: number, rtl: boolean): { left: number; top: number } {
  const vw = document.documentElement.clientWidth || window.innerWidth;
  const vh = document.documentElement.clientHeight || window.innerHeight;
  const left = Math.max(MARGIN, Math.min(vw - MARGIN - w, rtl ? r.right - w : r.left));
  const below = r.bottom + GAP_BELOW;
  const above = r.top - MARGIN - h;
  return { left, top: below + h <= vh - MARGIN || above < MARGIN ? below : above };
}

/** Whether an event's target is the panel, the square or anything inside them. */
function inPanel(n: EventTarget | null, panel: HTMLElement | null, anchor: HTMLElement | null): boolean {
  return n instanceof Node && (!!panel?.contains(n) || !!anchor?.contains(n));
}

function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/**
 * useQuickDraft reads the settings each time the panel opens, so a change made
 * on the settings page since then shows, and saves the way that page does:
 * debounced, and only the fields that changed. Closing sends what is waiting.
 * A failed read calls onFail, since a panel without its rows has nothing to offer.
 */
function useQuickDraft(open: boolean, onFail: () => void) {
  const { t } = useT();
  const { toast } = useToast();
  const [cfg, setCfg] = useState<Settings | null>(null);
  const pending = useRef<Partial<Settings>>({});
  const timer = useRef<number | undefined>(undefined);

  const flush = useCallback(async () => {
    window.clearTimeout(timer.current);
    const fields = pending.current;
    if (Object.keys(fields).length === 0) return;
    pending.current = {};
    try {
      const saved = await patchSettings(fields);
      // An answer that arrives after another keystroke would undo it.
      if (Object.keys(pending.current).length === 0) setCfg(saved);
    } catch (e) {
      toast(t('list.failed', { error: message(e) }), 'fail');
    }
  }, [t, toast]);

  const patch = useCallback<Patch>(
    (fields) => {
      setCfg((c) => (c ? { ...c, ...fields } : c));
      pending.current = { ...pending.current, ...fields };
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => void flush(), SAVE_MS);
    },
    [flush],
  );

  useEffect(() => {
    if (!open) {
      void flush();
      return;
    }
    let live = true;
    void fetchSettings().then(
      (s) => {
        // Anything typed while the read was on the wire stays on top of it.
        if (live) setCfg({ ...s, ...pending.current });
      },
      (e) => {
        if (!live) return;
        toast(t('list.failed', { error: message(e) }), 'fail');
        onFail();
      },
    );
    return () => {
      live = false;
    };
  }, [open, flush, onFail, t, toast]);

  useEffect(
    () => () => {
      void flush();
    },
    [flush],
  );

  return { cfg, patch };
}

/**
 * QuickSettings is the square and its panel. The panel is portalled to <body>,
 * where the page's scroll column cannot clip it, and placed against the window.
 * A press outside, Escape, a scroll outside it or a resize closes it, since a
 * fixed panel stops pointing at its square once anything moves.
 */
export function QuickSettings() {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  const close = useCallback(() => setOpen(false), []);
  const { cfg, patch } = useQuickDraft(open, close);
  // The anchor is the wrapper, since the button's own ref belongs to its tooltip.
  const wrap = useRef<HTMLSpanElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const [at, setAt] = useState<{ left: number; top: number } | null>(null);
  const title = t('quick.title');
  const ready = cfg !== null;
  const placed = at !== null;

  // Measured before paint and hidden until then. Placed again when the rows
  // arrive, because a flip above depends on the height they add.
  useLayoutEffect(() => {
    if (!open) {
      setAt(null);
      return;
    }
    const anchor = wrap.current;
    const box = panel.current;
    if (!anchor || !box) return;
    const rtl = getComputedStyle(anchor).direction === 'rtl';
    setAt(placePanel(anchor.getBoundingClientRect(), box.offsetWidth, box.offsetHeight, rtl));
  }, [open, ready]);

  // Focus moves in once the panel sits where it stays, so Tab reaches the
  // rows next rather than the rest of the bar.
  useEffect(() => {
    if (open && placed) panel.current?.focus({ preventScroll: true });
  }, [open, placed]);

  useEffect(() => {
    if (!open) return;
    const inside = (n: EventTarget | null) => inPanel(n, panel.current, wrap.current);
    const onDown = (e: PointerEvent) => {
      if (!inside(e.target)) close();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      close();
      wrap.current?.querySelector('button')?.focus();
    };
    const onScroll = (e: Event) => {
      if (!inside(e.target)) close();
    };
    document.addEventListener('pointerdown', onDown, true);
    document.addEventListener('keydown', onKey);
    window.addEventListener('scroll', onScroll, true);
    window.addEventListener('resize', close);
    return () => {
      document.removeEventListener('pointerdown', onDown, true);
      document.removeEventListener('keydown', onKey);
      window.removeEventListener('scroll', onScroll, true);
      window.removeEventListener('resize', close);
    };
  }, [open, close]);

  return (
    <span ref={wrap} className="inline-flex">
      <Button
        kind={open ? 'primary' : 'secondary'}
        icon={<IconSliders />}
        // No tooltip while the panel is open, where it would cover the title.
        title={open ? undefined : title}
        aria-label={title}
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      />
      {open &&
        createPortal(
          <div
            ref={panel}
            role="dialog"
            aria-label={title}
            tabIndex={-1}
            // Tabbing out of the panel closes it; a press outside is handled above.
            onBlur={(e) => {
              if (e.relatedTarget && !inPanel(e.relatedTarget, panel.current, wrap.current)) close();
            }}
            style={{ left: at?.left ?? 0, top: at?.top ?? 0, visibility: placed ? undefined : 'hidden' }}
            className="glim-card glim-fade fixed z-40 flex w-80 max-w-[calc(100vw-1rem)] flex-col gap-5 p-5 outline-none"
          >
            <SectionTitle>{title}</SectionTitle>
            {cfg && ROWS.map((row) => <Fragment key={row.id}>{row.render(cfg, patch)}</Fragment>)}
          </div>,
          document.body,
        )}
    </span>
  );
}
