// Quick settings: the square in the shell bar and the panel it opens. Each row
// is the control the setting's own page draws, bound to the same value, so it
// reads and saves the same way in both places. The settings routes are not
// forwarded to peers, so the shell offers this for the local instance only.
import { Fragment, useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import {
  fetchIdleActions,
  fetchScheduleSuspension,
  fetchSettings,
  patchSettings,
  type ScheduleSuspension,
  type Settings,
} from '../lib/api';
import { useT } from '../lib/i18n';
import { IconMenu, IconRetry } from '../lib/icons';
import { useQuietMode, useToast } from '../lib/toast';
import { IdleActionPicker } from '../pages/settings/automation/IdleAction';
import { ScheduleSuspendField } from '../pages/settings/automation/ScheduleSuspend';
import { SettingRow } from '../pages/settings/controls';
import { ChunksField, MaxConcurrentField, MaxPerHostField } from '../pages/settings/downloads/Concurrency';
import { SpeedLimitField } from '../pages/settings/downloads/SpeedLimit';
import { UploadLimitField } from '../pages/settings/torrents/UploadLimit';
import { fetchFeatures, type Feature } from '../pages/settings/features';
import { ModuleSwitchBadge, PageBadge } from '../pages/settings/ModuleToggle';
import {
  fetchReconnectState,
  runReconnect,
  useReasonText,
  type ReconnectState,
} from '../pages/settings/Reconnect';
import { label } from '../pages/settings/tx';
import { METER_WINDOW, meterWindow, useMeterWindow } from './SpeedGraph';
import { AnchoredPanel } from './AnchoredPanel';
import { Button, ToggleRow, UnitNumberInput, type FieldUnit } from './ui';

type Patch = (fields: Partial<Settings>) => void;
type Translate = ReturnType<typeof useT>['t'];

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
  torrentsModule: Feature | undefined;
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

/** The settings type leaves the torrent block out, as the Torrents page notes. */
type TorrentBlock = { torrent?: Record<string, unknown> & { uploadLimit?: number } };

// What people reach for while downloads run comes first: how fast and how many,
// then the timetable and what happens once the queue is empty, then the two
// switches and the bar's own curve, and the one action last.
const ROWS: QuickRow[] = [
  {
    id: 'speedLimit',
    render: ({ cfg, patch }) => (
      <SpeedLimitField value={cfg.speedLimit} onValue={(speedLimit) => patch({ speedLimit })} />
    ),
  },
  {
    id: 'uploadLimit',
    // Only torrents upload, so the row goes with the built-in torrent client.
    // The whole block is sent, as the Torrents page sends it.
    render: ({ cfg, patch, extras }) => {
      if (extras.torrentsModule?.parked) return null;
      const torrent = (cfg as TorrentBlock).torrent ?? {};
      return (
        <UploadLimitField
          value={torrent.uploadLimit ?? 0}
          onValue={(uploadLimit) =>
            patch({ torrent: { ...torrent, uploadLimit } } as unknown as Partial<Settings>)
          }
        />
      );
    },
  },
  {
    id: 'maxConcurrent',
    render: ({ cfg, patch }) => (
      <MaxConcurrentField value={cfg.maxConcurrent} onValue={(maxConcurrent) => patch({ maxConcurrent })} />
    ),
  },
  {
    id: 'maxPerHost',
    render: ({ cfg, patch }) => (
      <MaxPerHostField value={cfg.maxPerHost} onValue={(maxPerHost) => patch({ maxPerHost })} />
    ),
  },
  {
    id: 'chunks',
    render: ({ cfg, patch }) => <ChunksField value={cfg.chunks} onValue={(chunks) => patch({ chunks })} />,
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
  { id: 'speedWindow', render: () => <SpeedWindowRow /> },
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
  let blocked: { reason: string; way: ReactNode } | null = null;
  if (!state.suspended && module?.parked) {
    blocked = {
      reason: t('quick.schedulesOff'),
      way: <ModuleSwitchBadge m={module} title={openPage(t, module.page)} onFollow={close} />,
    };
  } else if (!state.suspended && schedules === 0) {
    blocked = {
      reason: t('settings.schedule.suspendNone'),
      way: <PageBadge page="automation" title={openPage(t, 'automation')} onFollow={close} />,
    };
  }
  return (
    <div className="flex flex-col gap-2">
      <ScheduleSuspendField state={state} onState={onState} blocked={blocked?.reason} />
      {blocked && <div className="flex justify-end">{blocked.way}</div>}
    </div>
  );
}

/** The words of a badge leading to a settings page, by the page's rail name. */
function openPage(t: Translate, page: string): string {
  return t('quick.openPage', { page: label(t, 'settings.nav.', page) });
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
        menu
        actions={actions}
        value={action}
        onValue={(id) => patch({ idleAction: { ...cfg.idleAction, action: id } })}
      />
      {noProgram && (
        <div className="flex justify-end">
          <PageBadge page="automation" title={t('quick.idleCommandSetup')} onFollow={close} />
        </div>
      )}
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

// Untranslated symbols, as fmtEta's are. Seconds step by ten up to the minute,
// minutes by one.
const WINDOW_UNITS: FieldUnit[] = [
  { label: 's', factor: 1, step: 10 },
  { label: 'min', factor: 60, step: 60 },
];

/**
 * SpeedWindowRow sets how far back the curve in the bar reaches, under the
 * name the Overview gives its own curve's window. It lives in the interface
 * state like quiet mode, not in the settings.
 */
function SpeedWindowRow() {
  const { t } = useT();
  const [seconds, setSeconds] = useMeterWindow();
  return (
    <SettingRow control="short" label={t('overview.speedWindow')} hint={t('quick.speedWindowHint')}>
      <UnitNumberInput
        value={seconds}
        units={WINDOW_UNITS}
        min={METER_WINDOW.min}
        max={METER_WINDOW.max}
        snap={meterWindow}
        onValue={setSeconds}
      />
    </SettingRow>
  );
}

/**
 * ReconnectRow runs the reconnect the Network page set up. It is disabled
 * while the module is switched off, while nothing is set up and while a run is
 * going, and the (i) inside the button says which; the first two also get the
 * way to the page that changes it, in front of the button so the button keeps
 * the end of the row either way.
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

  let blocked: { reason: string; way?: ReactNode } | null = null;
  if (module?.parked) {
    blocked = {
      reason: t('quick.reconnectOff'),
      way: <ModuleSwitchBadge m={module} title={openPage(t, module.page)} onFollow={close} />,
    };
  } else if (!state) {
    blocked = { reason: t('settings.reconnect.stateUnreadable') };
  } else if (!state.configured) {
    blocked = {
      reason: state.reasonCode === 'off' ? t('quick.reconnectUnset') : t('settings.reconnect.notReady', { reason: reasonText }),
      way: <PageBadge page="network" title={openPage(t, 'network')} onFollow={close} />,
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
    <div className="flex flex-wrap items-center justify-end gap-2">
      {blocked?.way}
      <Button
        kind="secondary"
        icon={<IconRetry width={16} height={16} />}
        hint={blocked?.reason ?? t('quick.reconnectHint')}
        disabled={busy || blocked !== null}
        onClick={() => void run()}
      >
        {busy ? t('settings.reconnect.running') : t('settings.reconnect.runNow')}
      </Button>
    </div>
  );
}

// The settings page's autosave delay (pages/Settings.tsx), so a number typed
// here goes out once rather than once per keystroke.
const SAVE_MS = 600;

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
          torrentsModule: module('torrents'),
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
 * QuickSettings is the square and its panel, which opens under it. Both are
 * rendered in place, so the toasts the rows raise stay above the scrim.
 */
export function QuickSettings() {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  const close = useCallback(() => setOpen(false), []);
  const { cfg, patch } = useQuickDraft(open, close);
  const { extras, setSuspension } = useQuickExtras(open);
  // The anchor is the wrapper, since the button's own ref belongs to its tooltip.
  const wrap = useRef<HTMLSpanElement>(null);
  const title = t('quick.title');

  return (
    <>
      <span ref={wrap} data-new="quick-rows" className="inline-flex">
        {/* Sized with Play, Pause and Stop, since controls in one row share one box. */}
        <Button
          kind={open ? 'primary' : 'secondary'}
          transport
          icon={<IconMenu />}
          // No tooltip while the panel is open, where it would cover the title.
          title={open ? undefined : title}
          aria-label={title}
          aria-haspopup="dialog"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
        />
      </span>
      {open && (
        <AnchoredPanel anchor={wrap} title={title} ready={cfg !== null && extras !== null} onClose={close}>
          {cfg &&
            extras &&
            ROWS.map((row) => <Fragment key={row.id}>{row.render({ cfg, patch, extras, setSuspension, close })}</Fragment>)}
        </AnchoredPanel>
      )}
    </>
  );
}
