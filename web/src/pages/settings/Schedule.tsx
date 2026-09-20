import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import {
  Button,
  Card,
  Field,
  FieldGroup,
  IconBadge,
  PageHeader,
  SectionTitle,
  TextInput,
  segBase,
  segOn,
} from '../../components/ui';
import { Tabs } from '../../components/Tabs';
import {
  IconArrowDown,
  IconArrowUp,
  IconClock,
  IconPause,
  IconPlay,
  IconPlus,
  IconSliders,
  IconTrash,
} from '../../lib/icons';
import { fetchOptions } from '../../lib/api';
import { RATE_UNITS, fmtRateValue, joinRate, splitRate, type RateUnit } from '../../lib/format';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useResource } from '../../lib/useResource';
import { useToast } from '../../lib/toast';
import { NeutralSwitch } from './controls';

/**
 * Schedule edits the timetable: windows that pause, resume or cap the queue
 * while they are open. It reads and writes PUT /api/schedule rather than the
 * settings draft, so a timetable save never carries a stale unrelated field
 * (routes_schedule.go), and it saves itself.
 *
 * Order matters: every window covering the moment applies in order and the
 * last write to a field wins, as in a rule set. The per-row "next" column is a
 * local hint in the reader's time; the banner shows the server's DST-correct
 * answer.
 */

// Mirrors schedule.Action, left open so an unknown value from a newer server
// still renders as its raw string.
type ScheduleAction = 'pause' | 'resume' | 'limit' | (string & {});

const KNOWN_ACTIONS: ScheduleAction[] = ['pause', 'resume', 'limit'];

/** Mirrors schedule.Entry. Days are 0 = Sunday .. 6 = Saturday, as in both time.Weekday and Date.getDay(). */
interface ScheduleEntry {
  name?: string;
  days: number[];
  start: string;
  end: string;
  action: ScheduleAction;
  limit?: number;
  disabled?: boolean;
}

/** Mirrors schedule.State. */
interface ScheduleStateValue {
  paused: boolean;
  limit: number;
}

/** Mirrors app.ScheduleState, GET and PUT /api/schedule's shared shape. */
interface ScheduleState {
  entries: ScheduleEntry[];
  state: ScheduleStateValue;
  next: string | null;
}

interface ScheduleRowError {
  row: number;
  error: string;
}

async function fetchSchedule(): Promise<ScheduleState> {
  const r = await fetch('/api/schedule');
  if (!r.ok) throw new Error((await r.text()).trim() || String(r.status));
  return (await r.json()) as ScheduleState;
}

type SaveResult =
  | { ok: true; state: ScheduleState }
  | { ok: false; rowErrors: ScheduleRowError[] }
  | { ok: false; rowErrors?: undefined; error: string };

/**
 * saveSchedule posts the ordered table to its own route and reads back the
 * applied state or, for a 400, the refused rows. It skips lib/api.ts's json()
 * helper, whose error parsing expects one sentence rather than a list.
 */
async function saveSchedule(entries: ScheduleEntry[]): Promise<SaveResult> {
  const r = await fetch('/api/schedule', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ entries }),
  });
  if (r.status === 400) {
    const body = (await r.json().catch(() => null)) as { errors?: ScheduleRowError[] } | null;
    return { ok: false, rowErrors: body?.errors ?? [] };
  }
  if (!r.ok) {
    return { ok: false, error: (await r.text()).trim() || String(r.status) };
  }
  return { ok: true, state: (await r.json()) as ScheduleState };
}

/**
 * PENDING holds the English strings until the catalogue has them; the lookup
 * asks the catalogue first.
 */
const PENDING = {
  'settings.schedule.title': 'Schedule',
  'settings.schedule.subtitle': 'Pause, resume or cap the download speed on a timetable.',
  'settings.schedule.statusTitle': 'Current status',
  'settings.schedule.listTitle': 'Timetable',
  'settings.schedule.orderHint':
    'Rows are applied in order, top to bottom, and a later row wins where two windows overlap - so a broad "pause every night" above a narrow exception leaves the exception in force, and the same two rows the other way round do not.',
  'settings.schedule.add': 'Add window',
  'settings.schedule.empty': 'The queue runs on its own schedule',
  'settings.schedule.emptyHint':
    'No windows are configured, so nothing here ever pauses or limits the queue by the clock. Add one to hold downloads overnight or cap the speed while you are on the connection yourself.',
  'settings.schedule.use': 'Use this window',
  'settings.schedule.moveUp': 'Move up',
  'settings.schedule.moveDown': 'Move down',
  'settings.schedule.remove': 'Remove this window',
  'settings.schedule.edit': 'Edit this window',
  'settings.schedule.name': 'Name',
  'settings.schedule.namePlaceholder': 'e.g. Night pause',
  'settings.schedule.days': 'Days',
  'settings.schedule.daysHint':
    'Which weekdays this window opens on. For a window that runs past midnight, tick the day it STARTS on - "Fri 22:00-06:00" ends Saturday morning without Saturday itself being ticked.',
  'settings.schedule.preset.every': 'Every day',
  'settings.schedule.preset.weekdays': 'Weekdays',
  'settings.schedule.preset.weekends': 'Weekends',
  'settings.schedule.preset.custom': 'Custom',
  'settings.schedule.start': 'Start',
  'settings.schedule.end': 'End',
  'settings.schedule.endHint':
    'Before the start time, this window runs past midnight and ends the following morning. Equal to the start time is refused - that could mean a whole day or no time at all, and guessing which one you meant is worse than asking.',
  'settings.schedule.action': 'Action',
  'settings.schedule.action.pause': 'Pause',
  'settings.schedule.action.resume': 'Resume',
  'settings.schedule.action.limit': 'Limit speed',
  'settings.schedule.limit': 'Speed limit',
  'settings.schedule.disabledOff': 'This window is parked and never fires. The queue behaves as if the row were not here at all.',
  'settings.schedule.activeNow': 'Active now, until {time}',
  'settings.schedule.next': 'Next: {when}',
  'settings.schedule.never': 'Never fires as configured',
  'settings.schedule.stateNow.paused': 'The queue is paused by the timetable right now.',
  'settings.schedule.stateNow.limited': 'The queue is capped at {rate} by the timetable right now.',
  'settings.schedule.stateNow.running': 'No window is in force right now.',
  'settings.schedule.nextChange': 'Next change: {when}',
  'settings.schedule.noNextChange': 'Nothing in the table will ever change the queue as configured.',
  'settings.schedule.saveFailed': 'The timetable could not be saved: {error}',
  'settings.schedule.rowError': 'Row {row}: {error}',
} as const;

type PendingKey = keyof typeof PENDING;
type Cx = (key: PendingKey, vars?: Record<string, string | number>) => string;

function useCx(): Cx {
  const { t } = useT();
  return useCallback(
    (key: PendingKey, vars?: Record<string, string | number>) => {
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      let s: string = translated ?? PENDING[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [t],
  );
}

const PRESET_EVERYDAY = [0, 1, 2, 3, 4, 5, 6];
const PRESET_WEEKDAYS = [1, 2, 3, 4, 5];
const PRESET_WEEKENDS = [0, 6];

function sameDays(a: number[], b: number[]): boolean {
  if (a.length !== b.length) return false;
  const sa = [...a].sort((x, y) => x - y);
  const sb = [...b].sort((x, y) => x - y);
  return sa.every((v, i) => v === sb[i]);
}

function presetOf(days: number[]): 'every' | 'weekdays' | 'weekends' | 'custom' {
  if (sameDays(days, PRESET_EVERYDAY)) return 'every';
  if (sameDays(days, PRESET_WEEKDAYS)) return 'weekdays';
  if (sameDays(days, PRESET_WEEKENDS)) return 'weekends';
  return 'custom';
}

/**
 * shortWeekdayLabels names the weekdays in the reader's language, 0 = Sunday,
 * pinned to UTC so the reader's timezone cannot shift the day.
 */
function shortWeekdayLabels(locale: string): string[] {
  const fmt = new Intl.DateTimeFormat(locale || undefined, { weekday: 'short', timeZone: 'UTC' });
  const sunday = Date.UTC(2023, 0, 1); // a Sunday
  return Array.from({ length: 7 }, (_, d) => fmt.format(new Date(sunday + d * 86_400_000)));
}

function uiLocale(): string {
  return document.documentElement.lang || '';
}

function parseClock(s: string): { h: number; m: number } | null {
  const m = /^(\d{1,2}):(\d{2})/.exec(s.trim());
  if (!m) return null;
  const h = Number(m[1]);
  const min = Number(m[2]);
  if (!Number.isFinite(h) || !Number.isFinite(min) || h > 23 || min > 59) return null;
  return { h, m: min };
}

/** isActiveNow mirrors rule.covers in schedule.go, in the reader's local time, as a hint. */
function isActiveNow(entry: ScheduleEntry, now: Date): boolean {
  if (entry.disabled) return false;
  const days = entry.days;
  const start = parseClock(entry.start);
  const end = parseClock(entry.end);
  if (!start || !end || days.length === 0) return false;
  const startM = start.h * 60 + start.m;
  const endM = end.h * 60 + end.m;
  if (startM === endM) return false;
  const nowM = now.getHours() * 60 + now.getMinutes();
  const d = now.getDay();
  if (endM > startM) return days.includes(d) && nowM >= startM && nowM < endM;
  // A window past midnight belongs to the day it opened on, as in the Go
  // evaluator.
  if (days.includes(d) && nowM >= startM) return true;
  return days.includes((d + 6) % 7) && nowM < endM;
}

/** activeUntil returns when an active window closes, or null. */
function activeUntil(entry: ScheduleEntry, now: Date): Date | null {
  if (!isActiveNow(entry, now)) return null;
  const start = parseClock(entry.start);
  const end = parseClock(entry.end);
  if (!start || !end) return null;
  const startM = start.h * 60 + start.m;
  const endM = end.h * 60 + end.m;
  const nowM = now.getHours() * 60 + now.getMinutes();
  const until = new Date(now);
  // Past midnight and still before it: the end is tomorrow.
  if (endM <= startM && nowM >= startM) until.setDate(until.getDate() + 1);
  until.setHours(end.h, end.m, 0, 0);
  return until;
}

/**
 * nextOccurrence returns when this row's window next opens. Eight days ahead
 * are always enough to find it.
 */
function nextOccurrence(entry: ScheduleEntry, now: Date): Date | null {
  if (entry.disabled) return null;
  const days = entry.days;
  const start = parseClock(entry.start);
  if (!start || days.length === 0) return null;
  for (let offset = 0; offset <= 7; offset++) {
    const d = new Date(now);
    d.setDate(d.getDate() + offset);
    d.setHours(start.h, start.m, 0, 0);
    if (days.includes(d.getDay()) && d.getTime() > now.getTime()) return d;
  }
  return null;
}

function fmtWhen(d: Date, locale: string): string {
  return new Intl.DateTimeFormat(locale || undefined, {
    weekday: 'short',
    hour: '2-digit',
    minute: '2-digit',
  }).format(d);
}

function fmtClock(d: Date, locale: string): string {
  return new Intl.DateTimeFormat(locale || undefined, { hour: '2-digit', minute: '2-digit' }).format(d);
}

function fmtRate(bytesPerSecond: number): string {
  const { value, unit } = splitRate(bytesPerSecond);
  return `${fmtRateValue(value)} ${unit}`;
}

let keyCounter = 0;
const freshKey = () => `k${Date.now().toString(36)}${keyCounter++}`;

interface Row {
  key: string;
  entry: ScheduleEntry;
}

/**
 * toRows gives each entry a client-side React key, since the server has none,
 * and turns a null Days (possible in a hand-edited settings.json) into an
 * array.
 */
function toRows(entries: ScheduleEntry[]): Row[] {
  return entries.map((entry) => ({ key: freshKey(), entry: { ...entry, days: entry.days ?? [] } }));
}

const NEW_ROW = (): ScheduleEntry => ({
  days: [...PRESET_WEEKDAYS],
  start: '22:00',
  end: '06:00',
  action: 'pause',
  disabled: false,
});

export function Schedule() {
  const { t } = useT();
  const cx = useCx();
  const locale = uiLocale();
  const { toast } = useToast();

  const { data: loaded, failed, loading, setData: setLoaded, reload } = useResource<ScheduleState>(fetchSchedule);

  const [rows, setRows] = useState<Row[] | null>(null);
  useEffect(() => {
    if (loaded) setRows(toRows(loaded.entries));
  }, [loaded]);

  // Polled on its own and never written into `rows`, so a poll cannot discard
  // unsaved edits.
  const [live, setLive] = useState<Pick<ScheduleState, 'state' | 'next'> | null>(null);
  useEffect(() => {
    if (loaded) setLive({ state: loaded.state, next: loaded.next });
  }, [loaded]);
  useEffect(() => {
    const iv = setInterval(() => {
      fetchSchedule()
        .then((s) => setLive({ state: s.state, next: s.next }))
        .catch(() => {
          /* The banner keeps its last answer through a failed poll. */
        });
    }, 30_000);
    return () => clearInterval(iv);
  }, []);

  // Refreshes the per-row hints; their finest unit is a minute.
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const iv = setInterval(() => setNow(new Date()), 30_000);
    return () => clearInterval(iv);
  }, []);

  const [actions, setActions] = useState<ScheduleAction[]>(KNOWN_ACTIONS);
  useEffect(() => {
    let alive = true;
    fetchOptions().then(
      (o) => {
        if (alive && o.scheduleActions?.length) setActions(o.scheduleActions);
      },
      () => {
        /* The fixed fallback above still lets the page work. */
      },
    );
    return () => {
      alive = false;
    };
  }, []);

  const [openKey, setOpenKey] = useState('');
  const [saving, setSaving] = useState(false);
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({});

  const dirty =
    rows !== null &&
    loaded !== null &&
    JSON.stringify(rows.map((r) => r.entry)) !== JSON.stringify(loaded.entries);

  const write = useCallback((next: Row[]) => {
    setRows(next);
    setRowErrors({});
  }, []);

  const update = (key: string, fields: Partial<ScheduleEntry>) => {
    if (!rows) return;
    write(rows.map((r) => (r.key === key ? { key, entry: { ...r.entry, ...fields } } : r)));
  };

  const move = (index: number, by: number) => {
    if (!rows) return;
    const to = index + by;
    if (to < 0 || to >= rows.length) return;
    const next = [...rows];
    [next[index], next[to]] = [next[to], next[index]];
    write(next);
  };

  const add = () => {
    if (!rows) return;
    const row: Row = { key: freshKey(), entry: NEW_ROW() };
    write([...rows, row]);
    setOpenKey(row.key);
  };

  const remove = (key: string) => {
    if (!rows) return;
    write(rows.filter((r) => r.key !== key));
  };

  async function onSave() {
    if (!rows || saving) return;
    setSaving(true);
    setRowErrors({});
    try {
      const result = await saveSchedule(rows.map((r) => r.entry));
      if (result.ok) {
        setLoaded(result.state);
        setLive({ state: result.state.state, next: result.state.next });
        toast(t('settings.saved'), 'ok');
        return;
      }
      if (result.rowErrors) {
        const byKey: Record<string, string> = {};
        for (const e of result.rowErrors) {
          const row = rows[e.row - 1];
          if (row) byKey[row.key] = e.error;
        }
        setRowErrors(byKey);
        const first = result.rowErrors[0];
        const bad = first && rows[first.row - 1];
        if (bad) setOpenKey(bad.key);
      } else {
        toast(cx('settings.schedule.saveFailed', { error: result.error }), 'fail');
      }
    } finally {
      setSaving(false);
    }
  }

  // Saves itself like every settings tab, through its own route, after 900ms
  // rather than 600ms, since a row is typed field by field and would show its
  // validation error half done.
  const saveTimer = useRef<number | null>(null);
  useEffect(() => {
    if (!dirty) return;
    if (saveTimer.current !== null) window.clearTimeout(saveTimer.current);
    saveTimer.current = window.setTimeout(() => {
      saveTimer.current = null;
      void onSave();
    }, 900);
    return () => {
      if (saveTimer.current !== null) {
        window.clearTimeout(saveTimer.current);
        saveTimer.current = null;
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows]);

  if (loading) return <LoadingState label={t('common.loading')} />;
  if (failed || rows === null) {
    return <ErrorState message={t('common.loadFailed')} retry={reload} retryLabel={t('common.retry')} />;
  }

  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={cx('settings.schedule.title')} />

      <StateBanner live={live} cx={cx} locale={locale} />

      <Card hue={1} className="flex flex-col gap-4">
        <SectionTitle
          hint={cx('settings.schedule.orderHint')}
          right={
            <Button icon={<IconPlus width={16} height={16} />} onClick={add}>
              {cx('settings.schedule.add')}
            </Button>
          }
        >
          {cx('settings.schedule.listTitle')}
        </SectionTitle>

        {rows.length === 0 ? (
          <p className="py-6 text-center text-sm text-carbon-textSub">
            {cx('settings.schedule.empty')}
            <span className="mt-1 block text-[11px] text-carbon-textMuted">{cx('settings.schedule.emptyHint')}</span>
          </p>
        ) : (
          <ul className="flex flex-col">
            {rows.map((row, i) => (
              <EntryRow
                key={row.key}
                row={row}
                index={i}
                last={i === rows.length - 1}
                open={openKey === row.key}
                onToggle={() => setOpenKey(openKey === row.key ? '' : row.key)}
                onChange={(fields) => update(row.key, fields)}
                onMove={(by) => move(i, by)}
                onRemove={() => remove(row.key)}
                error={rowErrors[row.key]}
                actions={actions}
                now={now}
                locale={locale}
                cx={cx}
              />
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}

function LoadingState({ label }: { label: string }) {
  return <div className="glim-card p-10 text-center text-sm text-carbon-textMuted">{label}</div>;
}

function ErrorState({ message, retry, retryLabel }: { message: string; retry: () => void; retryLabel: string }) {
  return (
    <div className="glim-card flex flex-col items-center gap-3 p-10 text-center">
      <div className="text-sm text-statusFail">{message}</div>
      <Button kind="secondary" onClick={retry}>
        {retryLabel}
      </Button>
    </div>
  );
}

/** StateBanner shows the server's current state and next change, never recomputed here. */
function StateBanner({
  live,
  cx,
  locale,
}: {
  live: Pick<ScheduleState, 'state' | 'next'> | null;
  cx: Cx;
  locale: string;
}) {
  if (!live) return null;
  const { state, next } = live;
  const nowText = state.paused
    ? cx('settings.schedule.stateNow.paused')
    : state.limit > 0
      ? cx('settings.schedule.stateNow.limited', { rate: fmtRate(state.limit) })
      : cx('settings.schedule.stateNow.running');
  const changeText = next
    ? cx('settings.schedule.nextChange', { when: fmtWhen(new Date(next), locale) })
    : cx('settings.schedule.noNextChange');
  const active = state.paused || state.limit > 0;
  return (
      <Card hue={0} className="flex items-center gap-3">
        <SectionTitle>{cx('settings.schedule.statusTitle')}</SectionTitle>
        <span className={`h-2 w-2 shrink-0 rounded-[var(--radius-pill)] ${active ? 'bg-accent' : 'bg-carbon-textMuted'}`} aria-hidden />
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="text-sm text-carbon-text">{nowText}</span>
          <span className="text-xs text-carbon-textMuted">{changeText}</span>
        </div>
      </Card>
  );
}

/** actionIcon returns the row's mark at 16px, the glyph size used beside text. */
function actionIcon(action: ScheduleAction) {
  if (action === 'pause') return <IconPause width={16} height={16} />;
  if (action === 'resume') return <IconPlay width={16} height={16} />;
  if (action === 'limit') return <IconSliders width={16} height={16} />;
  return <IconClock width={16} height={16} />;
}

function daysSummary(days: number[], labels: string[]): string {
  // Filtered, since a hand-edited settings.json can hold a weekday outside 0..6.
  return [...days]
    .filter((d) => d >= 0 && d < labels.length)
    .sort((a, b) => a - b)
    .map((d) => labels[d])
    .join(', ');
}

function EntryRow({
  row,
  index,
  last,
  open,
  onToggle,
  onChange,
  onMove,
  onRemove,
  error,
  actions,
  now,
  locale,
  cx,
}: {
  row: Row;
  index: number;
  last: boolean;
  open: boolean;
  onToggle: () => void;
  onChange: (fields: Partial<ScheduleEntry>) => void;
  onMove: (by: number) => void;
  onRemove: () => void;
  error?: string;
  actions: ScheduleAction[];
  now: Date;
  locale: string;
  cx: Cx;
}) {
  const { t } = useT();
  const { entry } = row;
  const labels = useMemo(() => shortWeekdayLabels(locale), [locale]);

  const actionLabel = (a: ScheduleAction): string =>
    KNOWN_ACTIONS.includes(a) ? cx(`settings.schedule.action.${a}` as PendingKey) : a;

  const until = activeUntil(entry, now);
  const next = nextOccurrence(entry, now);
  const nextText = entry.disabled
    ? ''
    : until
      ? cx('settings.schedule.activeNow', { time: fmtClock(until, locale) })
      : next
        ? cx('settings.schedule.next', { when: fmtWhen(next, locale) })
        : cx('settings.schedule.never');

  const description = entry.name?.trim() || `${actionLabel(entry.action)} · ${entry.start}-${entry.end}`;
  const preset = presetOf(entry.days);

  return (
    <li className={last ? '' : 'border-b border-carbon-border/60'}>
      <div className="group grid grid-cols-[auto_1fr_auto] items-center gap-3 py-2.5">
        <NeutralSwitch
          on={!entry.disabled}
          onChange={(v) => onChange({ disabled: !v })}
          name={cx('settings.schedule.use')}
          hue={index}
        />
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={open}
          aria-label={cx('settings.schedule.edit')}
          className={`flex min-w-0 items-center gap-3 text-left ${entry.disabled ? 'opacity-55' : ''}`}
        >
          <span className="glim-num w-5 shrink-0 text-xs text-carbon-textMuted">{index + 1}</span>
          <span className="shrink-0 text-carbon-textMuted">{actionIcon(entry.action)}</span>
          <span className="min-w-0 flex-1 truncate text-sm text-carbon-text">{description}</span>
          <span className="hidden min-w-0 max-w-[10rem] truncate text-xs text-carbon-textMuted lg:block">
            {preset !== 'custom' ? cx(`settings.schedule.preset.${preset}`) : daysSummary(entry.days, labels)}
          </span>
          <span dir="ltr" className="hidden shrink-0 text-xs text-carbon-textMuted sm:block">
            {nextText}
          </span>
        </button>
        {/* `labelled`, so the actions follow the Beschriftung setting; the
            description truncates instead. */}
        <div className="flex items-center gap-1.5">
          <IconBadge
            labelled
            icon={<IconArrowUp width={16} height={16} />}
            hue={index}
            title={cx('settings.schedule.moveUp')}
            aria-label={cx('settings.schedule.moveUp')}
            disabled={index === 0}
            onClick={() => onMove(-1)}
          />
          <IconBadge
            labelled
            icon={<IconArrowDown width={16} height={16} />}
            hue={index}
            title={cx('settings.schedule.moveDown')}
            aria-label={cx('settings.schedule.moveDown')}
            disabled={last}
            onClick={() => onMove(1)}
          />
          <IconBadge
            labelled
            icon={<IconTrash width={16} height={16} />}
            hue={index}
            title={cx('settings.schedule.remove')}
            aria-label={cx('settings.schedule.remove')}
            onClick={onRemove}
          />
        </div>
      </div>

      {/* Repeated on a collapsed row, so the reason is not hidden behind a click. */}
      {!open && error && (
        <p className="pb-2 text-xs text-statusFail">{cx('settings.schedule.rowError', { row: index + 1, error })}</p>
      )}

      {open && (
        <div className="glim-well mb-3 flex flex-col gap-4 p-4">
          {error && <p className="text-xs text-statusFail">{cx('settings.schedule.rowError', { row: index + 1, error })}</p>}

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label={cx('settings.schedule.name')}>
              <TextInput
                value={entry.name ?? ''}
                placeholder={cx('settings.schedule.namePlaceholder')}
                onChange={(e) => onChange({ name: e.target.value })}
              />
            </Field>
            <Field label={cx('settings.schedule.action')}>
              <ActionSelect
                value={entry.action}
                actions={actions}
                onChange={(a) => onChange({ action: a })}
                label={actionLabel}
              />
            </Field>
          </div>

          <DayPicker days={entry.days} labels={labels} onChange={(days) => onChange({ days })} cx={cx} />

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label={cx('settings.schedule.start')}>
              <TimePicker
                label={cx('settings.schedule.start')}
                value={entry.start}
                onChange={(start) => onChange({ start })}
              />
            </Field>
            <Field label={cx('settings.schedule.end')} hint={cx('settings.schedule.endHint')}>
              <TimePicker
                label={cx('settings.schedule.end')}
                value={entry.end}
                onChange={(end) => onChange({ end })}
              />
            </Field>
          </div>

          {entry.action === 'limit' && (
            <Field label={cx('settings.schedule.limit')}>
              <RateField value={entry.limit ?? 0} onChange={(v) => onChange({ limit: v })} unitLabel={t('queue.limitUnit')} />
            </Field>
          )}

          {entry.disabled && <p className="text-xs text-carbon-textMuted">{cx('settings.schedule.disabledOff')}</p>}
        </div>
      )}
    </li>
  );
}

function ActionSelect({
  value,
  actions,
  onChange,
  label,
}: {
  value: ScheduleAction;
  actions: ScheduleAction[];
  onChange: (a: ScheduleAction) => void;
  label: (a: ScheduleAction) => string;
}) {
  // A value the menu does not list stays an option of its own rather than
  // being swapped for the first known one.
  const options = actions.includes(value) ? actions : [value, ...actions];
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="glim-select appearance-none pe-6 w-full rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-sm text-carbon-text
        outline-none transition-shadow focus:shadow-[0_0_0_2px_var(--focus-ring)]"
    >
      {options.map((a) => (
        <option key={a} value={a}>
          {label(a)}
        </option>
      ))}
    </select>
  );
}

/**
 * The time picker replaces a native <input type="time">, whose spinner cannot
 * be styled. A compact "HH:MM" button opens a portaled popover with an hour
 * column and a minute column in five-minute steps, placed off the trigger and
 * clamped into the viewport.
 *
 * The window scroll listener ignores scrolls inside the panel, or the columns
 * would close it, and nothing focuses or scrolls before the position is
 * measured, or the browser's own scroll would trip that listener. Only one
 * picker is open at a time.
 */
const MINUTE_STEP = 5;

/** The open picker's close function, called when another opens. */
let closeOpenTimePicker: (() => void) | null = null;

function pad2(n: number): string {
  return String(n).padStart(2, '0');
}

function TimePicker({ value, onChange, label }: { value: string; onChange: (v: string) => void; label: string }) {
  const clock = parseClock(value);
  const hour = clock?.h ?? 0;
  const minute = clock?.m ?? 0;

  const [open, setOpen] = useState(false);
  // null until the position is measured; see the note above.
  const [at, setAt] = useState<{ left: number; top: number } | null>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);

  // A stored minute off the step stays reachable and visible.
  const minutes = useMemo(() => {
    const list: number[] = [];
    for (let m = 0; m < 60; m += MINUTE_STEP) list.push(m);
    if (!list.includes(minute)) list.push(minute);
    return list.sort((a, b) => a - b);
  }, [minute]);

  const close = useCallback(() => {
    setOpen(false);
    setAt(null);
  }, []);

  function toggle() {
    if (open) {
      close();
      return;
    }
    closeOpenTimePicker?.();
    closeOpenTimePicker = close;
    setOpen(true);
  }

  useEffect(() => {
    if (!open) return;
    return () => {
      if (closeOpenTimePicker === close) closeOpenTimePicker = null;
    };
  }, [open, close]);

  // Measured with the panel's real size, which depends on the columns and the
  // scrollbar gutter.
  useLayoutEffect(() => {
    if (!open) return;
    const place = () => {
      const r = trigger.current?.getBoundingClientRect();
      const box = panel.current?.getBoundingClientRect();
      if (!r || !box) return;
      const margin = 8;
      const left = Math.min(Math.max(r.left, margin), Math.max(margin, window.innerWidth - box.width - margin));
      const below = r.bottom + 6;
      const top =
        below + box.height <= window.innerHeight - margin || r.top - 6 - box.height < margin
          ? below
          : r.top - 6 - box.height;
      setAt({ left, top });
    };
    place();
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (e: PointerEvent) => {
      const target = e.target as Node;
      if (panel.current?.contains(target) || trigger.current?.contains(target)) return;
      close();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        close();
        trigger.current?.focus();
      }
    };
    // Capturing, to see scrolls in any ancestor; the panel's own are ignored.
    const onScroll = (e: Event) => {
      if (panel.current?.contains(e.target as Node)) return;
      close();
    };
    document.addEventListener('pointerdown', onPointerDown, true);
    document.addEventListener('keydown', onKey);
    window.addEventListener('scroll', onScroll, true);
    window.addEventListener('resize', close);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown, true);
      document.removeEventListener('keydown', onKey);
      window.removeEventListener('scroll', onScroll, true);
      window.removeEventListener('resize', close);
    };
  }, [open, close]);

  const shown = clock ? `${pad2(hour)}:${pad2(minute)}` : value;

  return (
    <>
      <button
        ref={trigger}
        type="button"
        // A clock reads left to right in every language.
        dir="ltr"
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={label}
        onClick={toggle}
        // The size of the field it replaces.
        className="glim-num w-full rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-1.5 text-start text-sm
          text-carbon-text outline-none transition-shadow focus:shadow-[0_0_0_2px_var(--focus-ring)]"
      >
        {shown}
      </button>
      {open &&
        createPortal(
          <div
            ref={panel}
            dir="ltr"
            role="dialog"
            aria-label={label}
            className="glim-card fixed z-[2147483647] flex gap-1 p-1"
            style={{
              left: at?.left ?? 0,
              top: at?.top ?? 0,
              // Hidden for the frame before it is measured.
              visibility: at ? undefined : 'hidden',
            }}
          >
            <TimeColumn
              name="HH"
              values={HOURS}
              value={hour}
              ready={at !== null}
              onPick={(h) => onChange(`${pad2(h)}:${pad2(minute)}`)}
            />
            <TimeColumn
              name="MM"
              values={minutes}
              value={minute}
              ready={at !== null}
              onPick={(m) => onChange(`${pad2(hour)}:${pad2(m)}`)}
            />
          </div>,
          document.body,
        )}
    </>
  );
}

const HOURS = Array.from({ length: 24 }, (_, i) => i);

/**
 * TimeColumn is one column of the time picker. `name` is "HH" or "MM", format
 * tokens that read the same in every language. Arrow keys select as they move,
 * Home and End jump to the ends, left and right switch columns, and a roving
 * tabindex keeps the column one tab stop.
 */
function TimeColumn({
  name,
  values,
  value,
  ready,
  onPick,
}: {
  name: string;
  values: number[];
  value: number;
  ready: boolean;
  onPick: (v: number) => void;
}) {
  const list = useRef<HTMLDivElement>(null);

  /**
   * Centres the chosen option in this column only, by scrollTop, since
   * scrollIntoView would also scroll the page and close the popover.
   */
  const centre = useCallback((el: HTMLElement) => {
    const box = list.current;
    if (!box) return;
    const r = el.getBoundingClientRect();
    const b = box.getBoundingClientRect();
    box.scrollTop += r.top - b.top - (b.height - r.height) / 2;
  }, []);

  // Only once placed, or scrolling to the option would move the page.
  useEffect(() => {
    if (!ready) return;
    const el = list.current?.querySelector('[aria-selected="true"]');
    if (el instanceof HTMLElement) centre(el);
  }, [ready, centre]);

  function step(delta: number) {
    const i = values.indexOf(value);
    const next = values[Math.min(values.length - 1, Math.max(0, (i < 0 ? 0 : i) + delta))];
    if (next === undefined || next === value) return;
    onPick(next);
    // Focus follows the selection, without scrolling the page.
    const el = list.current?.querySelector(`[data-value="${next}"]`);
    if (el instanceof HTMLElement) {
      el.focus({ preventScroll: true });
      centre(el);
    }
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLDivElement>) {
    const sideways = e.key === 'ArrowLeft' || e.key === 'ArrowRight';
    if (sideways) {
      const panel = e.currentTarget.parentElement;
      const columns = panel ? Array.from(panel.children) : [];
      const mine = columns.indexOf(e.currentTarget);
      const other = columns[mine === 0 ? 1 : 0];
      const focus = other?.querySelector('[aria-selected="true"]');
      if (focus instanceof HTMLElement) {
        e.preventDefault();
        focus.focus();
      }
      return;
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      step(1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      step(-1);
    } else if (e.key === 'Home') {
      e.preventDefault();
      step(-values.length);
    } else if (e.key === 'End') {
      e.preventDefault();
      step(values.length);
    }
  }

  return (
    <div
      ref={list}
      role="listbox"
      aria-label={name}
      onKeyDown={onKeyDown}
      className="glim-num h-44 w-14 overflow-y-auto rounded-[var(--radius-control)] bg-carbon-surface2 p-1"
    >
      {values.map((v) => {
        const on = v === value;
        return (
          <button
            key={v}
            type="button"
            role="option"
            data-value={v}
            aria-selected={on}
            tabIndex={on ? 0 : -1}
            onClick={() => onPick(v)}
            className={`block w-full rounded-[var(--radius-control)] px-1.5 py-1 text-center text-sm transition-colors ${
              on
                ? 'bg-accent text-accentContrast'
                : 'text-carbon-textSub hover:bg-carbon-surface3 hover:text-carbon-text'
            }`}
          >
            {pad2(v)}
          </button>
        );
      })}
    </div>
  );
}

/**
 * DayPicker offers the presets (select="one") and the weekday strip
 * (select="many") through Tabs, so both get keyboard handling, RTL and the
 * rainbow position.
 */
function DayPicker({
  days,
  labels,
  onChange,
  cx,
}: {
  days: number[];
  labels: string[];
  onChange: (next: number[]) => void;
  cx: Cx;
}) {
  const preset = presetOf(days);
  const presets: { id: 'every' | 'weekdays' | 'weekends'; days: number[] }[] = [
    { id: 'every', days: PRESET_EVERYDAY },
    { id: 'weekdays', days: PRESET_WEEKDAYS },
    { id: 'weekends', days: PRESET_WEEKENDS },
  ];
  const chosen = useMemo(() => new Set(days.map(String)), [days]);
  return (
    <FieldGroup label={cx('settings.schedule.days')} hint={cx('settings.schedule.daysHint')}>
      <div className="flex flex-wrap items-center gap-1.5">
        <Tabs
          select="one"
          variant="well"
          size="sm"
          className="w-fit"
          label={cx('settings.schedule.days')}
          // null while the days match no preset, so only the readout is lit.
          active={preset === 'custom' ? null : preset}
          onSelect={(id) => onChange(presets.find((p) => p.id === id)?.days ?? PRESET_EVERYDAY)}
          items={presets.map((p) => ({ id: p.id, label: cx(`settings.schedule.preset.${p.id}`) }))}
        />
        {/* Custom is a readout beside the track rather than a segment, since
            there is no single array it could set. */}
        <span
          className={`${segBase} inline-flex h-8 items-center px-2.5 text-xs ${
            preset === 'custom' ? segOn : 'bg-carbon-surface2 text-carbon-textMuted'
          }`}
        >
          {cx('settings.schedule.preset.custom')}
        </span>
      </div>
      <Tabs
        select="many"
        variant="well"
        size="sm"
        className="w-fit"
        label={cx('settings.schedule.days')}
        active={chosen}
        onSelect={(id) => {
          const d = Number(id);
          onChange(days.includes(d) ? days.filter((x) => x !== d) : [...days, d].sort((a, b) => a - b));
        }}
        items={labels.map((label, d) => ({ id: String(d), label }))}
      />
    </FieldGroup>
  );
}

/**
 * RateField keeps the typed amount in local state, like QueueBar's speed
 * field, so switching the unit does not rewrite the number being typed.
 */
function RateField({
  value,
  onChange,
  unitLabel,
}: {
  value: number;
  onChange: (bytes: number) => void;
  unitLabel: string;
}) {
  const initial = splitRate(value);
  const [text, setText] = useState(fmtRateValue(initial.value));
  const [unit, setUnit] = useState<RateUnit>(initial.unit);

  const commit = (nextText: string, nextUnit: RateUnit) => {
    const bytes = joinRate(Math.max(0, Number(nextText.replace(',', '.')) || 0), nextUnit);
    const settled = splitRate(bytes);
    setText(fmtRateValue(settled.value));
    setUnit(settled.unit);
    onChange(bytes);
  };

  return (
    <div className="flex items-center gap-2">
      <TextInput
        dir="ltr"
        inputMode="decimal"
        value={text}
        onChange={(e) => setText(e.target.value)}
        onBlur={() => commit(text, unit)}
      />
      <select
        value={unit}
        aria-label={unitLabel}
        onChange={(e) => commit(text, e.target.value as RateUnit)}
        className="glim-select appearance-none pe-6 rounded-[var(--radius-control)] bg-carbon-surface2 px-2 py-2 text-sm text-carbon-text
          outline-none transition-shadow focus:shadow-[0_0_0_2px_var(--focus-ring)]"
      >
        {RATE_UNITS.map((u) => (
          <option key={u.label} value={u.label}>
            {u.label}
          </option>
        ))}
      </select>
    </div>
  );
}
