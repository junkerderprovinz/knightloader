import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Button,
  Card,
  Field,
  FieldGroup,
  PageHeader,
  SectionTitle,
  TextInput,
  segBase,
  segOff,
  segOn,
} from '../../components/ui';
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
import { NeutralSwitch } from './controls';

/**
 * The timetable editor: a table of windows, each pausing, resuming or
 * capping the queue for as long as it is open.
 *
 * OUTSIDE THE SETTINGS DRAFT, ON PURPOSE. Every other sub-page in this shell
 * reads and writes settings.SettingsPage's one shared draft, saved wholesale
 * by the shell's own Save bar - see context.tsx's own doc comment on why
 * ("dropping a field... would delete somebody's rule set"). This page does
 * not: it reads and writes only PUT /api/schedule, a dedicated route this
 * wave adds specifically so a timetable save can never be the stale side of
 * that shared draft, and can never carry an unrelated field along with it.
 * See routes_schedule.go's doc comment for the full reasoning and for the
 * one direction of the race that choice does NOT close. One consequence
 * worth being explicit about: this page's own unsaved edits do not feed the
 * shell's sticky "Unsaved changes" bar at the bottom of every other settings
 * page, because they were never part of what that bar watches. This page
 * carries its own, directly below the table.
 *
 * ORDER IS MEANING, NOT LAYOUT. schedule.Schedule.At applies every window
 * that covers the current moment in order, last write to a field wins - the
 * same rule a rule set applies. A broad "pause every night" above a narrow
 * "except for the download I queue by hand at midnight" is two rows; the same
 * two rows the other way round pause right over the exception. Move up/down
 * is therefore not a cosmetic reorder control, and it says so once, in the
 * list's own hint, rather than on every row.
 *
 * THE NEXT-EXECUTION COLUMN IS A HINT, NOT THE ENGINE'S OWN ANSWER.
 * ScheduleState.next (the aggregate this page polls for, see StateBanner) is
 * the one DST-correct, cumulative answer - internal/schedule/schedule.go
 * spends a long comment on why that question is hard. What is shown per row
 * here is a much narrower, locally-computed question - "when does THIS row's
 * own window next open" - answered with plain calendar arithmetic in the
 * reader's own local time. It is close enough to be useful while a row is
 * being edited and is never treated as authoritative; the banner above the
 * table is.
 */

// Mirrors schedule.Action. Left open rather than a closed union, matching the
// rest of this app's server-named-enum fields (Reason, Origin in lib/api.ts):
// the three below are everything this build ships, and an unrecognised value
// from a newer server still renders (as its own raw string) instead of
// failing to compile.
type ScheduleAction = 'pause' | 'resume' | 'limit' | (string & {});

const KNOWN_ACTIONS: ScheduleAction[] = ['pause', 'resume', 'limit'];

/** Mirrors schedule.Entry. Days are 0 = Sunday .. 6 = Saturday, exactly
 *  time.Weekday's own numbering and, not coincidentally, JavaScript's
 *  Date.getDay() - chosen so this page never converts between the two. */
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
 * saveSchedule posts the whole ordered table to the dedicated route (never to
 * PUT /api/settings - see the page-level doc comment) and reads back either
 * the applied state or, for a 400, which rows were refused and why. Not
 * routed through lib/api.ts's shared `json()` helper: that helper's error
 * parsing is built for the single-sentence {error,code,params} envelope PUT
 * /api/settings answers with, and this route's refusal is a LIST of
 * {row,error} pairs a flat sentence cannot carry.
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
 * The strings this page needs, keyed by where they are going.
 *
 * i18n for this wave lands in one later, dedicated pass across every locale
 * at once (the same one-writer-per-wave rule locales/* has followed since
 * Wave 1) - see Connections.tsx's identical table and useCx for the
 * precedent this mirrors. The lookup asks the real catalogue first, so the
 * day these keys land in en.ts this table stops being consulted.
 */
const PENDING = {
  'settings.schedule.title': 'Schedule',
  'settings.schedule.subtitle': 'Pause, resume or cap the download speed on a timetable.',
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
  'settings.schedule.save': 'Save timetable',
  'settings.schedule.discard': 'Discard',
  'settings.schedule.unsaved': 'Unsaved changes to the timetable',
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

/** Short weekday names in the reader's own language, indexed 0 = Sunday.
 *  Pinned to UTC on both sides - the anchor date and the formatter - so the
 *  browser's own timezone can never shift which calendar day (and so which
 *  weekday name) index 0 resolves to for a reader in, say, UTC+14. */
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

/** Mirrors rule.covers in internal/schedule/schedule.go, in the reader's own
 *  local time - see the page doc comment for why this is a display hint and
 *  not a second implementation of the engine. */
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
  // Wraps past midnight: belongs to the day it opened on, exactly as the Go
  // evaluator treats it - testing today's date for the tail too would also
  // mark the morning BEFORE this window's own start as active.
  if (days.includes(d) && nowM >= startM) return true;
  return days.includes((d + 6) % 7) && nowM < endM;
}

/** The instant an active window closes; null when it is not active now. */
function activeUntil(entry: ScheduleEntry, now: Date): Date | null {
  if (!isActiveNow(entry, now)) return null;
  const start = parseClock(entry.start);
  const end = parseClock(entry.end);
  if (!start || !end) return null;
  const startM = start.h * 60 + start.m;
  const endM = end.h * 60 + end.m;
  const nowM = now.getHours() * 60 + now.getMinutes();
  const until = new Date(now);
  // Wrapping and still on the starting side of midnight: the end is
  // tomorrow's clock face, not today's.
  if (endM <= startM && nowM >= startM) until.setDate(until.getDate() + 1);
  until.setHours(end.h, end.m, 0, 0);
  return until;
}

/** When this row's window next OPENS - a narrower question than
 *  ScheduleState.next, see the page doc comment. Walks at most a week and a
 *  day ahead, which is always enough: within eight consecutive calendar days
 *  every weekday value occurs at least twice. */
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

/** loaded.entries -> editable rows, with a fresh client-only React key per
 *  row (the server has no id for one, and re-using array position would key
 *  two different rows identically the moment one is deleted, letting an
 *  edit in one land in another - see Connections.tsx's identical freshID
 *  for the same trap) and Days normalised to a real array. Days has no
 *  `omitempty` tag on the Go side, so a hand-edited settings.json missing the
 *  key, or explicit null, arrives here as JSON null rather than being
 *  dropped - Validate refuses saving such a row but does not retroactively
 *  fix one already on disk, and every helper below assumes a real array.
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

  const { data: loaded, failed, loading, setData: setLoaded, reload } = useResource<ScheduleState>(fetchSchedule);

  const [rows, setRows] = useState<Row[] | null>(null);
  useEffect(() => {
    if (loaded) setRows(toRows(loaded.entries));
  }, [loaded]);

  // The aggregate state banner is polled independently of `rows`, and never
  // writes into it: a poll landing while the table has unsaved edits must
  // not discard them, which is exactly the failure this page's own doc
  // comment says the shared settings draft has for every OTHER field.
  const [live, setLive] = useState<Pick<ScheduleState, 'state' | 'next'> | null>(null);
  useEffect(() => {
    if (loaded) setLive({ state: loaded.state, next: loaded.next });
  }, [loaded]);
  useEffect(() => {
    const iv = setInterval(() => {
      fetchSchedule()
        .then((s) => setLive({ state: s.state, next: s.next }))
        .catch(() => {
          /* the banner keeps its last known answer rather than blanking on one failed poll */
        });
    }, 30_000);
    return () => clearInterval(iv);
  }, []);

  // Drives every row's live next-execution/active-now text. 30s is plenty for
  // a column whose finest unit is a minute - this is a hint beside a form,
  // not a countdown clock.
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
        /* the fixed fallback above still lets the page work */
      },
    );
    return () => {
      alive = false;
    };
  }, []);

  const [openKey, setOpenKey] = useState('');
  const [saving, setSaving] = useState(false);
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({});
  const [saveError, setSaveError] = useState('');

  const dirty =
    rows !== null &&
    loaded !== null &&
    JSON.stringify(rows.map((r) => r.entry)) !== JSON.stringify(loaded.entries);

  const write = useCallback((next: Row[]) => {
    setRows(next);
    setRowErrors({});
    setSaveError('');
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
    if (!rows) return;
    setSaving(true);
    setSaveError('');
    setRowErrors({});
    try {
      const result = await saveSchedule(rows.map((r) => r.entry));
      if (result.ok) {
        setLoaded(result.state);
        setLive({ state: result.state.state, next: result.state.next });
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
        setSaveError(cx('settings.schedule.saveFailed', { error: result.error }));
      }
    } finally {
      setSaving(false);
    }
  }

  if (loading) return <LoadingState label={t('common.loading')} />;
  if (failed || rows === null) {
    return <ErrorState message={t('common.loadFailed')} retry={reload} retryLabel={t('common.retry')} />;
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={cx('settings.schedule.title')} subtitle={cx('settings.schedule.subtitle')} />

      <StateBanner live={live} cx={cx} locale={locale} />

      <Card className="flex flex-col gap-4">
        <SectionTitle
          right={
            <Button icon={<IconPlus width={16} height={16} />} onClick={add}>
              {cx('settings.schedule.add')}
            </Button>
          }
        >
          {cx('settings.schedule.listTitle')}
        </SectionTitle>

        {rows.length > 1 && <p className="text-xs text-carbon-textMuted">{cx('settings.schedule.orderHint')}</p>}

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

      {dirty && (
        <div className="glim-card sticky bottom-0 flex items-center gap-3 p-4">
          <span className="text-sm text-carbon-textSub">{cx('settings.schedule.unsaved')}</span>
          <span className="flex-1" />
          {saveError && <span className="text-sm text-statusFail">{saveError}</span>}
          <Button kind="ghost" onClick={() => loaded && write(toRows(loaded.entries))} disabled={saving}>
            {cx('settings.schedule.discard')}
          </Button>
          <Button onClick={onSave} disabled={saving}>
            {cx('settings.schedule.save')}
          </Button>
        </div>
      )}
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

/** What the timetable says right now, and when that next changes - read
 *  straight from the server's own cumulative, DST-correct evaluation
 *  (ScheduleState.state/.next), never recomputed here. */
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
    <Card className="flex items-center gap-3">
      <span className={`h-2 w-2 shrink-0 rounded-full ${active ? 'bg-accent' : 'bg-carbon-textMuted'}`} aria-hidden />
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="text-sm text-carbon-text">{nowText}</span>
        <span className="text-xs text-carbon-textMuted">{changeText}</span>
      </div>
    </Card>
  );
}

function actionIcon(action: ScheduleAction) {
  if (action === 'pause') return <IconPause width={14} height={14} />;
  if (action === 'resume') return <IconPlay width={14} height={14} />;
  if (action === 'limit') return <IconSliders width={14} height={14} />;
  return <IconClock width={14} height={14} />;
}

function daysSummary(days: number[], labels: string[]): string {
  // Filtered rather than trusted as 0..6: Validate refuses an out-of-range
  // weekday on SAVE, but GET does not re-check a row already on disk, so a
  // hand-edited settings.json can still hand this a value with nothing at
  // that index in `labels`.
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

  const description = entry.name?.trim() || `${actionLabel(entry.action)} · ${entry.start}–${entry.end}`;
  const preset = presetOf(entry.days);

  return (
    <li className={last ? '' : 'border-b border-carbon-border/60'}>
      <div className="group grid grid-cols-[auto_1fr_auto] items-center gap-3 py-2.5">
        <NeutralSwitch
          on={!entry.disabled}
          onChange={(v) => onChange({ disabled: !v })}
          name={cx('settings.schedule.use')}
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
        <div className="flex items-center gap-0.5">
          <Button
            kind="ghost"
            icon={<IconArrowUp width={14} height={14} />}
            aria-label={cx('settings.schedule.moveUp')}
            disabled={index === 0}
            onClick={() => onMove(-1)}
          />
          <Button
            kind="ghost"
            icon={<IconArrowDown width={14} height={14} />}
            aria-label={cx('settings.schedule.moveDown')}
            disabled={last}
            onClick={() => onMove(1)}
          />
          <Button
            kind="danger"
            icon={<IconTrash width={14} height={14} />}
            aria-label={cx('settings.schedule.remove')}
            onClick={onRemove}
          />
        </div>
      </div>

      {/* Repeated below the fold too - a row collapsed straight after a
          failed save (or one a reader jumps to from elsewhere) must not
          leave the only copy of the reason hidden behind a click. */}
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
              <TextInput type="time" dir="ltr" value={entry.start} onChange={(e) => onChange({ start: e.target.value })} />
            </Field>
            <Field label={cx('settings.schedule.end')} hint={cx('settings.schedule.endHint')}>
              <TextInput type="time" dir="ltr" value={entry.end} onChange={(e) => onChange({ end: e.target.value })} />
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
  // A value this build does not recognise (an older row, or a newer server)
  // is kept as an option of its own rather than silently swapped for the
  // first known one - switching a saved action on the strength of a menu
  // that simply had nothing else to offer would be exactly the kind of
  // save-time surprise this whole editor exists to avoid.
  const options = actions.includes(value) ? actions : [value, ...actions];
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="w-full rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-sm text-carbon-text
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
  return (
    <FieldGroup label={cx('settings.schedule.days')} hint={cx('settings.schedule.daysHint')}>
      <div className="flex flex-wrap items-center gap-1.5">
        {presets.map((p) => (
          <button
            key={p.id}
            type="button"
            aria-pressed={preset === p.id}
            onClick={() => onChange(p.days)}
            className={`${segBase} px-2.5 py-1 text-xs ${preset === p.id ? segOn : segOff}`}
          >
            {cx(`settings.schedule.preset.${p.id}`)}
          </button>
        ))}
        {/* Custom is a readout, not a button of its own: there is no single
            array it could set, and the seven toggles below already do the job
            "pick whichever days" describes. It stays in the same row and the
            same visual language as the three real presets - matching the
            "every day / weekdays / weekends / custom" set as asked for - and
            lights up on its own the moment the toggles below no longer match
            any of the three. */}
        <span className={`${segBase} px-2.5 py-1 text-xs ${preset === 'custom' ? segOn : 'text-carbon-textMuted/60'}`}>
          {cx('settings.schedule.preset.custom')}
        </span>
      </div>
      <div className="flex flex-wrap gap-1" role="group" aria-label={cx('settings.schedule.days')}>
        {labels.map((label, d) => {
          const on = days.includes(d);
          return (
            <button
              key={d}
              type="button"
              aria-pressed={on}
              onClick={() => onChange(on ? days.filter((x) => x !== d) : [...days, d].sort((a, b) => a - b))}
              className={`${segBase} h-8 min-w-9 px-1.5 text-xs ${on ? segOn : segOff}`}
            >
              {label}
            </button>
          );
        })}
      </div>
    </FieldGroup>
  );
}

/**
 * Value-plus-unit, the same idea as QueueBar's own speed limit field and for
 * the same reason: switching the unit must not rewrite the number the user
 * just typed, so the shown amount is local state, only re-derived from the
 * canonical bytes value (via splitRate) once a change has actually
 * committed - never on every keystroke, or a field mid-way through "1.5"
 * would fight the person typing it. Unlike QueueBar's, a "commit" here is
 * local only (the row is not saved until the page's own Save is pressed), so
 * it can afford to run on every change instead of waiting for blur/Enter.
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
        className="rounded-[var(--radius-control)] bg-carbon-surface2 px-2 py-2 text-sm text-carbon-text
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
