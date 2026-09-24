// Quick settings: the square in the shell bar and the panel it opens. Each row
// is the control the setting's own page draws, bound to the same value, so it
// reads and saves the same way in both places. The settings routes are not
// forwarded to peers, so the shell offers this for the local instance only.
import { Fragment, useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { Link } from 'react-router-dom';
import {
  fetchIdleActions,
  fetchScheduleSuspension,
  fetchSettings,
  patchSettings,
  type ScheduleSuspension,
  type Settings,
} from '../lib/api';
import { useT } from '../lib/i18n';
import { IconChevronEnd, IconRetry, IconSliders } from '../lib/icons';
import { useQuietMode, useToast } from '../lib/toast';
import { IdleActionPicker } from '../pages/settings/automation/IdleAction';
import { ScheduleSuspendField } from '../pages/settings/automation/ScheduleSuspend';
import { ChunksField, MaxConcurrentField, MaxPerHostField } from '../pages/settings/downloads/Concurrency';
import { SpeedLimitField } from '../pages/settings/downloads/SpeedLimit';
import { fetchFeatures, type Feature } from '../pages/settings/features';
import {
  fetchReconnectState,
  runReconnect,
  useReasonText,
  type ReconnectState,
} from '../pages/settings/Reconnect';
import { Button, SectionTitle, ToggleRow } from './ui';

type Patch = (fields: Partial<Settings>) => void;

/**
 * What the rows read besides the settings, each from its own route. A read
 * that failed leaves its field null or empty: the schedule and idle rows stay
 * out, and the reconnect button says it could not read the state.
 */
interface Extras {
  suspension: ScheduleSuspension | null;
  schedules: number;
  schedulerModule: Feature | undefined;
  idleActions: string[];
  reconnect: ReconnectState | null;
  reconnectModule: Feature | undefined;
}

interface RowContext {
  cfg: Settings;
  patch: Patch;
  extras: Extras;
  setSuspension: (s: ScheduleSuspension) => void;
  close: () => void;
}

/** A row of the panel: the settings page's own control for one value. */
interface QuickRow {
  id: string;
  render: (c: RowContext) => ReactNode;
}

// What people reach for while downloads run comes first: how fast and how many,
// then the timetable and what happens once the queue is empty, then the two
// switches, and the one action last.
const ROWS: QuickRow[] = [
  {
    id: 'counts',
    render: ({ cfg, patch }) => (
      // Two columns once the panel has room for two captions side by side.
      <div className="grid grid-cols-1 items-end gap-x-3 gap-y-4 @[21rem]:grid-cols-2">
        <SpeedLimitField value={cfg.speedLimit} onValue={(speedLimit) => patch({ speedLimit })} />
        <MaxConcurrentField value={cfg.maxConcurrent} onValue={(maxConcurrent) => patch({ maxConcurrent })} />
        <MaxPerHostField value={cfg.maxPerHost} onValue={(maxPerHost) => patch({ maxPerHost })} />
        <ChunksField value={cfg.chunks} onValue={(chunks) => patch({ chunks })} />
      </div>
    ),
  },
  {
    id: 'suspendSchedule',
    render: ({ extras, setSuspension, close }) =>
      extras.suspension && (
        <SuspendRow
          state={extras.suspension}
          onState={setSuspension}
          schedules={extras.schedules}
          module={extras.schedulerModule}
          close={close}
        />
      ),
  },
  {
    id: 'idleAction',
    render: ({ cfg, patch, extras, close }) =>
      extras.idleActions.length > 0 && (
        <IdleRow cfg={cfg} patch={patch} actions={extras.idleActions} close={close} />
      ),
  },
  { id: 'quiet', render: () => <QuietRow /> },
  {
    id: 'autoConfirm',
    render: ({ cfg, patch }) => (
      <AutoStartRow value={cfg.autoConfirm} onValue={(autoConfirm) => patch({ autoConfirm })} />
    ),
  },
  {
    id: 'reconnect',
    render: ({ extras, close }) => (
      <ReconnectRow state={extras.reconnect} module={extras.reconnectModule} close={close} />
    ),
  },
];

/**
 * SuspendRow is the schedule status card's dropdown. Without a schedule there
 * is nothing to suspend, so it is disabled with the reason and the page that
 * changes that, unless a suspension still runs and wants ending.
 */
function SuspendRow({
  state,
  onState,
  schedules,
  module,
  close,
}: {
  state: ScheduleSuspension;
  onState: (s: ScheduleSuspension) => void;
  schedules: number;
  module: Feature | undefined;
  close: () => void;
}) {
  const { t } = useT();
  let blocked: { reason: string; to: string; page: string } | null = null;
  if (!state.suspended && module?.parked) {
    blocked = { reason: t('quick.schedulerOff'), to: '/settings/modules', page: t('settings.nav.modules') };
  } else if (!state.suspended && schedules === 0) {
    blocked = { reason: t('settings.schedule.suspendNone'), to: '/settings/automation', page: t('settings.nav.automation') };
  }
  return (
    <div className="flex flex-col gap-2">
      <ScheduleSuspendField state={state} onState={onState} blocked={blocked?.reason} />
      {blocked && <PageLink to={blocked.to} label={t('quick.openPage', { page: blocked.page })} onFollow={close} />}
    </div>
  );
}

function IdleRow({ cfg, patch, actions, close }: { cfg: Settings; patch: Patch; actions: string[]; close: () => void }) {
  const { t } = useT();
  const action = cfg.idleAction.action;
  // The command's fields are too many for this panel, so an action that has
  // no program to run points at the page that sets one.
  const noProgram = action === 'command' && !cfg.idleAction.command?.program;
  return (
    <div className="flex flex-col gap-2">
      <IdleActionPicker
        actions={actions}
        value={action}
        onValue={(id) => patch({ idleAction: { ...cfg.idleAction, action: id } })}
      />
      {noProgram && <PageLink to="/settings/automation" label={t('quick.idleCommandSetup')} onFollow={close} />}
    </div>
  );
}

/** The notification centre's quiet mode, which lives in this browser's interface state. */
function QuietRow() {
  const { t } = useT();
  const [quiet, setQuiet] = useQuietMode();
  return <ToggleRow label={t('notifications.quiet')} hint={t('notifications.quietHint')} checked={quiet} onChange={setQuiet} />;
}

/**
 * The collector's auto-confirm switch. Its countdown stays on the collector
 * page, which the page's own hint places "below"; here it names the page.
 */
function AutoStartRow({ value, onValue }: { value: boolean; onValue: (v: boolean) => void }) {
  const { t } = useT();
  return (
    <ToggleRow
      label={t('settings.autoStart')}
      hint={t('quick.autoStartHint', { page: t('settings.nav.collector') })}
      checked={value}
      onChange={onValue}
    />
  );
}

/**
 * ReconnectRow runs the reconnect the Network page set up. It is disabled
 * while the module is switched off, while nothing is set up and while a run is
 * going, and the (i) inside the button says which; the first two also get the
 * way to the page that changes it.
 */
function ReconnectRow({
  state,
  module,
  close,
}: {
  state: ReconnectState | null;
  module: Feature | undefined;
  close: () => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const reasonText = useReasonText(state);
  const [running, setRunning] = useState(false);

  let blocked: { reason: string; to?: string; page?: string } | null = null;
  if (module?.parked) {
    blocked = { reason: t('quick.reconnectOff'), to: '/settings/modules', page: t('settings.nav.modules') };
  } else if (!state) {
    blocked = { reason: t('settings.reconnect.stateUnreadable') };
  } else if (!state.configured) {
    blocked = {
      reason: state.reasonCode === 'off' ? t('quick.reconnectUnset') : t('settings.reconnect.notReady', { reason: reasonText }),
      to: '/settings/network',
      page: t('settings.nav.network'),
    };
  }
  const busy = running || Boolean(state?.busy);

  async function run() {
    setRunning(true);
    try {
      const res = await runReconnect();
      if (res) toast(t('settings.reconnect.runMoved', { from: res.oldIp, to: res.newIp }), 'ok');
      else toast(t('settings.reconnect.runBusy'), 'info');
    } catch (e) {
      toast(t('settings.reconnect.runFailed', { reason: e instanceof Error ? e.message : String(e) }), 'fail');
    } finally {
      setRunning(false);
    }
  }

  return (
    <div className="flex flex-col items-end gap-2">
      <Button
        kind="secondary"
        icon={<IconRetry width={16} height={16} />}
        hint={blocked?.reason ?? t('quick.reconnectHint')}
        disabled={busy || blocked !== null}
        onClick={() => void run()}
      >
        {busy ? t('settings.reconnect.running') : t('settings.reconnect.runNow')}
      </Button>
      {blocked?.to && blocked.page && (
        <PageLink to={blocked.to} label={t('quick.openPage', { page: blocked.page })} onFollow={close} />
      )}
    </div>
  );
}

/** PageLink leads to the settings page a row cannot change from here, and closes the panel on the way. */
function PageLink({ to, label, onFollow }: { to: string; label: string; onFollow: () => void }) {
  return (
    <Link
      to={to}
      onClick={onFollow}
      className="flex w-fit items-center gap-1 text-[11px] text-carbon-textSub underline-offset-2 hover:text-carbon-text hover:underline focus-visible:underline"
    >
      {label}
      <IconChevronEnd className="h-3 w-3 rtl:-scale-x-100" aria-hidden />
    </Link>
  );
}

// The settings page's autosave delay (pages/Settings.tsx), so a number typed
// here goes out once rather than once per keystroke.
const SAVE_MS = 600;

const MARGIN = 8;
// The title badge straddles the panel's top edge, so above the panel there has
// to be room for its upper half as well as the margin.
const GAP_BELOW = 18;

/**
 * placePanel puts the panel under the square, on the edge the text starts
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
 * Whether an event's target is the panel, the square or anything inside them.
 * A dropdown's menu counts as inside: the rows' dropdowns open it in a portal
 * of its own, and no other menu can be open while the panel is.
 */
function inPanel(n: EventTarget | null, panel: HTMLElement | null, anchor: HTMLElement | null): boolean {
  if (!(n instanceof Node)) return false;
  if (panel?.contains(n) || anchor?.contains(n)) return true;
  return n instanceof Element && n.closest('[role="menu"]') !== null;
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
 * useQuickExtras reads what the rows need besides the settings, again on each
 * opening, and answers null until every read has settled, so the panel is
 * placed once with all its rows rather than growing under the pointer.
 */
function useQuickExtras(open: boolean) {
  const [extras, setExtras] = useState<Extras | null>(null);
  useEffect(() => {
    if (!open) {
      setExtras(null);
      return;
    }
    let live = true;
    void Promise.allSettled([fetchScheduleSuspension(), fetchIdleActions(), fetchReconnectState(), fetchFeatures()]).then(
      ([schedule, idleActions, reconnect, features]) => {
        if (!live) return;
        const module = (id: string) =>
          features.status === 'fulfilled' ? features.value.modules.find((m) => m.id === id) : undefined;
        setExtras({
          suspension:
            schedule.status === 'fulfilled'
              ? { suspended: schedule.value.suspended, suspendedUntil: schedule.value.suspendedUntil }
              : null,
          schedules: schedule.status === 'fulfilled' ? schedule.value.schedules : 0,
          schedulerModule: module('scheduler'),
          idleActions: idleActions.status === 'fulfilled' ? idleActions.value : [],
          reconnect: reconnect.status === 'fulfilled' ? reconnect.value : null,
          reconnectModule: module('reconnect'),
        });
      },
    );
    return () => {
      live = false;
    };
  }, [open]);
  const setSuspension = useCallback((suspension: ScheduleSuspension) => {
    setExtras((x) => (x ? { ...x, suspension } : x));
  }, []);
  return { extras, setSuspension };
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
  const { extras, setSuspension } = useQuickExtras(open);
  // The anchor is the wrapper, since the button's own ref belongs to its tooltip.
  const wrap = useRef<HTMLSpanElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const body = useRef<HTMLDivElement>(null);
  const [at, setAt] = useState<{ left: number; top: number; maxHeight: number } | null>(null);
  const title = t('quick.title');
  const ready = cfg !== null && extras !== null;
  const placed = at !== null;

  // Measured before paint and hidden until then, placed again when the rows
  // arrive and whenever the panel's size changes, because a flip above depends
  // on its height. The height measured is the one the rows want, not the one
  // an earlier placement allowed them.
  useLayoutEffect(() => {
    if (!open) {
      setAt(null);
      return;
    }
    const anchor = wrap.current;
    const box = panel.current;
    const rows = body.current;
    if (!anchor || !box) return;
    const place = () => {
      const rtl = getComputedStyle(anchor).direction === 'rtl';
      const h = box.offsetHeight + (rows ? rows.scrollHeight - rows.clientHeight : 0);
      const next = placePanel(anchor.getBoundingClientRect(), box.offsetWidth, h, rtl);
      setAt((cur) =>
        cur && cur.left === next.left && cur.top === next.top && cur.maxHeight === next.maxHeight ? cur : next,
      );
    };
    place();
    if (!rows) return;
    const watch = new ResizeObserver(place);
    for (const child of Array.from(rows.children)) watch.observe(child);
    return () => watch.disconnect();
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
            style={{
              left: at?.left ?? 0,
              top: at?.top ?? 0,
              maxHeight: at?.maxHeight,
              visibility: placed ? undefined : 'hidden',
            }}
            className="glim-card glim-fade @container fixed z-40 flex w-[24rem] max-w-[calc(100vw-1rem)] flex-col gap-5 p-5 outline-none"
          >
            <SectionTitle>{title}</SectionTitle>
            {cfg && extras && (
              // The rows scroll inside the card rather than the card itself,
              // which would clip the title badge on its edge. The inset keeps
              // focus outlines inside the scrolling box.
              <div ref={body} className="-m-1 flex min-h-0 flex-col gap-5 overflow-y-auto p-1">
                {ROWS.map((row) => (
                  <Fragment key={row.id}>{row.render({ cfg, patch, extras, setSuspension, close })}</Fragment>
                ))}
              </div>
            )}
          </div>,
          document.body,
        )}
    </span>
  );
}
