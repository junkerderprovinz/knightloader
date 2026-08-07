// The column registry for the download list, plus the pure functions that turn a
// stored layout back into a live one.
//
// A column is four things at once — a header, a cell, a sort order and an id in
// somebody's saved layout. Describing all four in one entry is what stops a
// column sorting by one value and printing another, which is the failure people
// report as "the sort is broken" and nobody can reproduce.
//
// This file is .tsx and not .ts because a cell renderer is markup. Splitting the
// renderers into the list component would leave the registry describing columns
// it cannot draw, which is the drift the registry exists to prevent.

import { useState, type ReactNode } from 'react';
import { setEnabled, type Task } from '../lib/api';
import { fmtBytes, fmtDate, fmtEta, fmtSpeed, pct } from '../lib/format';
import type { TranslationKey } from '../lib/i18n';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { IconCheck } from '../lib/icons';
import { ProgressBar } from './ProgressBar';
import { ResolverBadge, StatusPill } from './StatusPill';

export type ColumnId =
  | 'enabled'
  | 'name'
  | 'size'
  | 'progress'
  | 'speed'
  | 'eta'
  | 'status'
  | 'host'
  | 'added'
  | 'finished'
  | 'comment'
  | 'resolver'
  | 'source';

/**
 * Which list a layout belongs to. The collector shows staged links and the
 * downloads list shows transfers, so they do not want the same columns — and a
 * single shared layout would mean hiding "speed" on the collector hides it on
 * the list where it is the point.
 */
export type ListProfile = 'downloads' | 'collector';

export type Translate = (key: TranslationKey, vars?: Record<string, string | number>) => string;

export interface CellContext {
  t: Translate;
  /** The instance this list is showing, for the cells that can act on a row. */
  base: string;
}

export interface ColumnDef {
  id: ColumnId;
  labelKey: TranslationKey;
  /** Default width in CSS pixels; what the user drags overrides it. */
  width: number;
  minWidth: number;
  align: 'start' | 'end';
  /** Tabular digits, for a value that changes while somebody is looking at it. */
  numeric?: boolean;
  /** A path or a URL, which reads left-to-right even in a right-to-left interface. */
  ltr?: boolean;
  /** false only for the one column a list cannot be without. */
  hideable: boolean;
  /** Ascending order. Absent means the column cannot be sorted by. */
  compare?: (a: Task, b: Task) => number;
  render: (task: Task, ctx: CellContext) => ReactNode;
  /** What the package header shows in this column; absent means nothing. */
  aggregate?: (items: Task[], ctx: CellContext) => ReactNode;
}

// --- Shared cell furniture -------------------------------------------------

/** The selection mark: a filled square, as everywhere else in GlimStone. */
export function Checkbox({
  checked,
  onChange,
  label,
}: {
  checked: boolean;
  onChange: () => void;
  label?: string;
}) {
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={checked}
      aria-label={label}
      title={label}
      onClick={onChange}
      className={`grid h-4.5 w-4.5 shrink-0 place-items-center rounded-[var(--radius-control)] transition-colors ${
        checked ? 'bg-accent text-accentContrast' : 'bg-carbon-surface3/60 text-transparent hover:bg-carbon-surface3'
      }`}
      style={{ height: '1.125rem', width: '1.125rem' }}
    >
      <IconCheck width={12} height={12} />
    </button>
  );
}

/**
 * The per-link on/off switch — deliberately a switch and not a checkbox, because
 * the checkbox one cell to its left means "selected" and the two would otherwise
 * be the same mark twice in the same row.
 *
 * It shows what the server last said, never what was just clicked: the value
 * comes back over the websocket a moment later, and a control that reports
 * success locally while the request failed is how a link somebody switched off
 * downloads anyway.
 */
export function EnabledSwitch({
  ids,
  on,
  base,
}: {
  ids: string[];
  on: boolean;
  base: string;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  const label = t(on ? 'task.disable' : 'task.enable');

  async function flip() {
    if (busy || ids.length === 0) return;
    setBusy(true);
    try {
      await setEnabled(ids, !on, base);
    } catch (err) {
      // The server's own sentence names what refused; a generic failure here
      // would leave a switch that visibly did nothing and no way to find out why.
      toast(err instanceof Error && err.message ? err.message : t('task.switchFailed'), 'fail');
    } finally {
      setBusy(false);
    }
  }

  // The switch is deliberately NOT the accent when it is on. Every row is
  // enabled by default, so an accent-filled pill per row spends the one colour
  // that means "something is happening here" on the most ordinary fact on the
  // page — a column of gold next to a single gold progress bar, and the bar
  // stops reading as the thing that matters. On is the quiet state; off is the
  // exception, and the exception is what earns the ink.
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={label}
      title={label}
      disabled={busy}
      onClick={flip}
      className={`relative h-3.5 w-7 shrink-0 rounded-[var(--radius-pill)] transition-colors disabled:opacity-40 ${
        on ? 'bg-carbon-surface3' : 'bg-statusFailBg'
      }`}
    >
      {/* left-0 is load-bearing: without it the knob starts from its static
          position, which the button's inherited text-align centres, and the knob
          then slides out past the track. */}
      <span
        className={`absolute left-0 top-0.5 h-2.5 w-2.5 rounded-[var(--radius-pill)] shadow-sm transition-[translate] duration-150 ${
          on ? 'translate-x-4 bg-carbon-textSub' : 'translate-x-0.5 bg-statusFail'
        }`}
      />
    </button>
  );
}

function NameCell({ task, t }: { task: Task; t: Translate }) {
  // A pending automatic retry is not the same as a dead task, and saying so
  // stops people restarting something that is already about to restart.
  const retrying = task.status === 'error' && !!task.nextTry;
  return (
    <div className="min-w-0">
      <div dir="ltr" className="truncate text-start text-[13.5px] text-carbon-text">
        {task.name || task.url}
      </div>
      {task.error && (
        <div className="mt-0.5 flex items-center gap-2 text-[11px]">
          <span className="truncate text-statusFail">{task.error}</span>
          {retrying && <span className="shrink-0 text-carbon-textMuted">· {t('task.retryPending')}</span>}
        </div>
      )}
    </div>
  );
}

function ProgressCell({ loaded, size, done, active }: { loaded: number; size: number; done: boolean; active: boolean }) {
  const p = pct(loaded, size, done);
  return (
    <div className="flex items-center gap-2">
      <div className="min-w-0 flex-1">
        <ProgressBar percent={p} active={active} indeterminate={!done && size <= 0} tone={done ? 'ok' : 'accent'} />
      </div>
      <span className="glim-num w-9 shrink-0 text-end text-[11px] text-carbon-textMuted">{p}%</span>
    </div>
  );
}

function StatusCell({ task, t }: { task: Task; t: Translate }) {
  return (
    <span className="inline-flex min-w-0 items-center gap-2">
      <StatusPill status={task.status} />
      {/* An availability verdict is worth showing only while nothing has been
          attempted yet; once a transfer is running, the status is the answer. */}
      {task.status === 'collected' && task.online === 'online' && (
        <span className="truncate text-[11px] text-statusOk">{t('task.online')}</span>
      )}
      {task.status === 'collected' && task.online === 'offline' && (
        <span className="truncate text-[11px] text-statusFail">{t('task.offline')}</span>
      )}
      {task.status === 'collected' && task.online === 'uncheckable' && (
        <span className="truncate text-[11px] text-carbon-textMuted">{t('task.uncheckable')}</span>
      )}
      {/* Only a real verdict is shown. An unverified download stays unmarked,
          because a tick that also means "not checked" is worse than none. */}
      {task.checksum === 'ok' && (
        <span title={t('task.checksumOk')} className="shrink-0 text-statusOk">
          <IconCheck width={13} height={13} />
        </span>
      )}
      {task.checksum === 'failed' && (
        <span title={t('task.checksumFail')} className="shrink-0 text-[11px] font-semibold text-statusFail">
          !
        </span>
      )}
    </span>
  );
}

// --- Values a column needs that the task does not carry directly -----------

const label = (t: Task): string => t.name || t.url;

// A URL is parsed once per link and kept, because the host column parses it for
// every row of every repaint otherwise. The cap is there so a session that has
// seen a hundred thousand links does not keep them all.
const hostCache = new Map<string, string>();

/**
 * hostOf is the file host, which is not the resolver: through a debrid service
 * every row would otherwise claim the same origin.
 *
 * Until the server carries `host`, it comes from the URL that was pasted rather
 * than from wherever the bytes end up coming from — the same rule the server
 * side will follow, so the column does not change its answer when it lands.
 */
export function hostOf(task: Task): string {
  if (task.host) return task.host;
  if (!task.url) return '';
  let h = hostCache.get(task.url);
  if (h === undefined) {
    try {
      h = new URL(task.url).hostname.replace(/^www\./, '');
    } catch {
      h = '';
    }
    if (hostCache.size > 5000) hostCache.clear();
    hostCache.set(task.url, h);
  }
  return h;
}

// Sorting by status alphabetically tells nobody anything; sorting by where a
// task is in its life does. Fault last, because that is what people sort to find.
const STATUS_RANK: Record<Task['status'], number> = {
  running: 0,
  extracting: 1,
  queued: 2,
  paused: 3,
  collected: 4,
  done: 5,
  error: 6,
};

// Unknown sorts last ascending rather than first: a row that cannot say how long
// it has left is not "about to finish".
const ETA_UNKNOWN = Number.MAX_SAFE_INTEGER;

function etaSeconds(t: Task): number {
  if (t.speed <= 0 || t.size <= 0 || t.loaded >= t.size) return ETA_UNKNOWN;
  return (t.size - t.loaded) / t.speed;
}

const cmpText = (a: string, b: string): number => a.localeCompare(b);

const sum = (items: Task[], pick: (t: Task) => number): number => items.reduce((s, x) => s + pick(x), 0);

// --- The registry ----------------------------------------------------------

export const COLUMNS: ColumnDef[] = [
  {
    id: 'enabled',
    labelKey: 'columns.enabled',
    width: 56,
    minWidth: 48,
    align: 'start',
    hideable: true,
    compare: (a, b) => Number(a.enabled) - Number(b.enabled),
    render: (task, ctx) => <EnabledSwitch ids={[task.id]} on={task.enabled} base={ctx.base} />,
    // A package is on when every link in it is; clicking then switches the whole
    // package the other way, which is the only reading that has one meaning.
    aggregate: (items, ctx) => (
      <EnabledSwitch ids={items.map((x) => x.id)} on={items.every((x) => x.enabled)} base={ctx.base} />
    ),
  },
  {
    id: 'name',
    labelKey: 'columns.name',
    width: 340,
    minWidth: 160,
    align: 'start',
    hideable: false,
    compare: (a, b) => cmpText(label(a), label(b)),
    render: (task, ctx) => <NameCell task={task} t={ctx.t} />,
    // No aggregate: in a package row this cell is the tree control, which is
    // list state the registry has no access to.
  },
  {
    id: 'size',
    labelKey: 'columns.size',
    width: 100,
    minWidth: 72,
    align: 'end',
    numeric: true,
    hideable: true,
    compare: (a, b) => a.size - b.size,
    render: (task) => fmtBytes(task.size),
    aggregate: (items) => fmtBytes(sum(items, (x) => x.size)),
  },
  {
    id: 'progress',
    labelKey: 'columns.progress',
    width: 170,
    minWidth: 110,
    align: 'start',
    hideable: true,
    compare: (a, b) => pct(a.loaded, a.size, a.status === 'done') - pct(b.loaded, b.size, b.status === 'done'),
    render: (task) => (
      <ProgressCell
        loaded={task.loaded}
        size={task.size}
        done={task.status === 'done'}
        active={task.status !== 'error'}
      />
    ),
    aggregate: (items) => {
      const size = sum(items, (x) => x.size);
      const loaded = sum(items, (x) => x.loaded);
      return (
        <ProgressCell
          loaded={loaded}
          size={size}
          done={items.every((x) => x.status === 'done')}
          active={items.some((x) => x.status !== 'error')}
        />
      );
    },
  },
  {
    id: 'speed',
    labelKey: 'columns.speed',
    width: 104,
    minWidth: 76,
    align: 'end',
    numeric: true,
    hideable: true,
    compare: (a, b) => a.speed - b.speed,
    render: (task) => fmtSpeed(task.speed),
    aggregate: (items) => fmtSpeed(sum(items, (x) => (x.status === 'running' ? x.speed : 0))),
  },
  {
    id: 'eta',
    labelKey: 'columns.eta',
    width: 88,
    minWidth: 64,
    align: 'end',
    numeric: true,
    hideable: true,
    compare: (a, b) => etaSeconds(a) - etaSeconds(b),
    render: (task) => fmtEta(task.loaded, task.size, task.speed),
    aggregate: (items) =>
      fmtEta(
        sum(items, (x) => x.loaded),
        sum(items, (x) => x.size),
        sum(items, (x) => (x.status === 'running' ? x.speed : 0)),
      ),
  },
  {
    id: 'status',
    labelKey: 'columns.status',
    width: 148,
    minWidth: 90,
    align: 'start',
    hideable: true,
    compare: (a, b) => STATUS_RANK[a.status] - STATUS_RANK[b.status],
    render: (task, ctx) => <StatusCell task={task} t={ctx.t} />,
  },
  {
    id: 'host',
    labelKey: 'columns.host',
    width: 150,
    minWidth: 80,
    align: 'start',
    ltr: true,
    hideable: true,
    compare: (a, b) => cmpText(hostOf(a), hostOf(b)),
    render: (task) => hostOf(task),
  },
  {
    id: 'added',
    labelKey: 'columns.added',
    width: 150,
    minWidth: 100,
    align: 'start',
    numeric: true,
    hideable: true,
    compare: (a, b) => cmpText(a.createdAt, b.createdAt),
    render: (task) => fmtDate(task.createdAt),
  },
  {
    id: 'finished',
    labelKey: 'columns.finished',
    width: 150,
    minWidth: 100,
    align: 'start',
    numeric: true,
    hideable: true,
    // Unfinished tasks carry Go's zero timestamp, which sorts before every real
    // one — so ascending puts "not finished" first, which is where it belongs.
    compare: (a, b) => cmpText(a.finishedAt ?? '', b.finishedAt ?? ''),
    render: (task) => fmtDate(task.finishedAt),
  },
  {
    id: 'comment',
    labelKey: 'columns.comment',
    width: 200,
    minWidth: 90,
    align: 'start',
    hideable: true,
    compare: (a, b) => cmpText(a.comment ?? '', b.comment ?? ''),
    render: (task) => task.comment ?? '',
  },
  {
    id: 'resolver',
    labelKey: 'columns.resolver',
    width: 124,
    minWidth: 76,
    align: 'start',
    hideable: true,
    compare: (a, b) => cmpText(a.resolver, b.resolver),
    render: (task) => <ResolverBadge resolver={task.resolver} />,
    aggregate: (items) => {
      const one = new Set(items.map((x) => x.resolver));
      return one.size === 1 ? <ResolverBadge resolver={items[0].resolver} /> : null;
    },
  },
  {
    id: 'source',
    labelKey: 'columns.source',
    width: 260,
    minWidth: 110,
    align: 'start',
    ltr: true,
    hideable: true,
    compare: (a, b) => cmpText(a.source ?? '', b.source ?? ''),
    render: (task) => task.source ?? '',
  },
];

export const COLUMN_BY_ID = new Map<ColumnId, ColumnDef>(COLUMNS.map((c) => [c.id, c]));

/** The order a list has before anybody drags anything. */
export const DEFAULT_ORDER: ColumnId[] = COLUMNS.map((c) => c.id);

/**
 * The one column that stretches. Its stored width becomes a flex ratio rather
 * than a pixel count, so dragging it still works and still persists — it just
 * competes for the leftover room instead of demanding an exact slice of it.
 */
const FLEX_COLUMN: ColumnId = 'name';

/**
 * What each list starts with switched off. The collector holds links nobody has
 * started, so a speed and a finished-at column there are three empty cells per
 * row pretending to be information.
 *
 * The downloads set is also cut to what FITS. Measured on the live instance at a
 * 1400px window: every column on by default came to 1812px against 1112px of
 * room, so the table opened 700px scrolled off its own right edge and the last
 * two columns were only reachable by dragging sideways. A default that does not
 * fit reads as a broken layout, not as a rich one — the rest are one click away
 * in the header menu, which is the point of having the menu.
 */
export const DEFAULT_HIDDEN: Record<ListProfile, ColumnId[]> = {
  downloads: ['comment', 'source', 'added', 'finished', 'resolver'],
  collector: ['progress', 'speed', 'eta', 'finished', 'comment', 'added'],
};

// --- The stored layout, and surviving an update ----------------------------

/**
 * What goes into the UI state store. Ids, never indices: an update that adds or
 * removes a column would otherwise shift every width onto the wrong column.
 *
 * `order` lists every column the stored layout knew about, hidden ones included
 * — that membership is what tells a later build which columns are new to this
 * layout and which the user deliberately switched off.
 */
export interface ColumnLayout {
  order: ColumnId[];
  hidden: ColumnId[];
  widths: Partial<Record<ColumnId, number>>;
}

export interface ResolvedLayout {
  /** Every column this build has, in the user's order — hidden ones included. */
  order: ColumnDef[];
  /** The ones actually drawn, in order. */
  visible: ColumnDef[];
  hidden: Set<ColumnId>;
  /** Only the widths somebody dragged; everything else takes its default. */
  widths: Partial<Record<ColumnId, number>>;
  widthOf: (id: ColumnId) => number;
}

const isKnown = (id: string): id is ColumnId => COLUMN_BY_ID.has(id as ColumnId);

/**
 * mergeOrder is the whole point of storing ids.
 *
 * A stored layout has to survive an update in both directions. A column this
 * build no longer has is dropped rather than left in the order as a hole; a
 * column this build added is not in the stored list at all, and is seated where
 * the built-in order puts it relative to the columns that are — so an update
 * that adds one column does not throw away a layout somebody spent an evening
 * arranging, and does not append the new column at the far right where nobody
 * scrolls to find it.
 */
function mergeOrder(stored: ColumnId[] | undefined): ColumnId[] {
  const kept = (stored ?? []).filter(isKnown);
  // Nothing recognisable stored: either a first run or a layout from a build
  // that shares no column with this one. Either way the defaults are the answer.
  if (kept.length === 0) return [...DEFAULT_ORDER];

  const out: ColumnId[] = [];
  for (const id of kept) if (!out.includes(id)) out.push(id);

  for (let i = 0; i < DEFAULT_ORDER.length; i++) {
    const id = DEFAULT_ORDER[i];
    if (out.includes(id)) continue;
    // Seat it after the nearest default predecessor that is already placed, so
    // two new neighbouring columns also keep their order relative to each other.
    let at = 0;
    for (let k = i - 1; k >= 0; k--) {
      const p = out.indexOf(DEFAULT_ORDER[k]);
      if (p >= 0) {
        at = p + 1;
        break;
      }
    }
    out.splice(at, 0, id);
  }
  return out;
}

function mergeHidden(profile: ListProfile, stored: ColumnLayout | null | undefined): Set<ColumnId> {
  const knew = new Set((stored?.order ?? []).filter(isKnown));
  if (knew.size === 0) return new Set(DEFAULT_HIDDEN[profile]);
  const hidden = new Set((stored?.hidden ?? []).filter(isKnown));
  // A column the stored layout never saw takes this build's default. One that
  // ships hidden must not appear on every existing install at once just because
  // it is new, and one that ships visible must not stay invisible forever
  // because an old layout happens not to mention it.
  for (const id of DEFAULT_HIDDEN[profile]) if (!knew.has(id)) hidden.add(id);
  for (const c of COLUMNS) if (!c.hideable) hidden.delete(c.id);
  return hidden;
}

export function resolveLayout(profile: ListProfile, stored: ColumnLayout | null | undefined): ResolvedLayout {
  const order = mergeOrder(stored?.order).map((id) => COLUMN_BY_ID.get(id)!);
  const hidden = mergeHidden(profile, stored);
  const widths: Partial<Record<ColumnId, number>> = {};
  for (const [id, w] of Object.entries(stored?.widths ?? {})) {
    if (isKnown(id) && typeof w === 'number' && Number.isFinite(w)) widths[id] = w;
  }
  const widthOf = (id: ColumnId): number => {
    const def = COLUMN_BY_ID.get(id);
    if (!def) return 0;
    return Math.max(def.minWidth, Math.round(widths[id] ?? def.width));
  };
  const visible = order.filter((c) => !hidden.has(c.id));
  return { order, visible, hidden, widths, widthOf };
}

/** toStored is what resolveLayout resolved, in the shape the store keeps. */
export function toStored(r: ResolvedLayout): ColumnLayout {
  return { order: r.order.map((c) => c.id), hidden: [...r.hidden], widths: { ...r.widths } };
}

/** moveColumn puts `id` immediately before or after `target`. */
export function moveColumn(order: ColumnId[], id: ColumnId, target: ColumnId, after: boolean): ColumnId[] {
  if (id === target) return order;
  const without = order.filter((x) => x !== id);
  const at = without.indexOf(target);
  if (at < 0) return order;
  without.splice(after ? at + 1 : at, 0, id);
  return without;
}

// The leading gutter holds the selection mark and the trailing one the row's
// actions. Neither is a column: they cannot be hidden, sorted by or dragged, and
// putting them in the registry would only mean writing "except these two"
// everywhere the registry is used.
export const GUTTER_SELECT = '2.5rem';
export const GUTTER_ACTIONS = '9.5rem';

/**
 * gridTemplate builds the track list every row shares.
 *
 * It is handed to the rows through one custom property rather than to each row
 * separately, so a column drag repaints the table by touching a single element
 * instead of re-rendering several hundred rows per pointer move.
 */
/**
 * The track list. Every column is its stored pixel width except the FLEX one,
 * which takes whatever is left over.
 *
 * That column is the name, and making it flexible rather than fixed is what
 * stops the table opening scrolled off its own right edge: with all widths
 * fixed, a trailing spacer soaked up any surplus but nothing absorbed a
 * shortfall, so a window narrower than the sum simply cut the last columns off.
 * Measured at 1400px: 276px over, with a 340px name column sitting next to a
 * spacer doing nothing. The name is the right one to give: it is the column
 * people widen the window FOR, it truncates gracefully, and its own minimum
 * keeps it readable when the flex runs out and the table does start scrolling.
 */
export function gridTemplate(visible: ColumnDef[], widthOf: (id: ColumnId) => number): string {
  // Exactly one track per rendered cell, and no spare. The rows are a grid with
  // no explicit row count, so one track too few silently wraps the last cell
  // onto a second grid line — which does not look like a layout bug, it looks
  // like the rows are simply tall, and every row grew from 38px to 74px before
  // anybody counted the tracks. The flexible column is never hideable, so there
  // is no case where a spacer is needed to soak up the surplus.
  const tracks = visible.map((c) =>
    c.id === FLEX_COLUMN ? `minmax(${c.minWidth}px, ${widthOf(c.id)}fr)` : `${widthOf(c.id)}px`,
  );
  return [GUTTER_SELECT, ...tracks, GUTTER_ACTIONS].join(' ');
}

// --- Sorting, which is a view and nothing more -----------------------------

export interface SortState {
  id: ColumnId;
  dir: 'asc' | 'desc';
}

/** Clicking a header cycles ascending → descending → back to the queue order. */
export function nextSort(current: SortState | null, id: ColumnId): SortState | null {
  if (!current || current.id !== id) return { id, dir: 'asc' };
  if (current.dir === 'asc') return { id, dir: 'desc' };
  return null;
}

export function comparatorFor(sort: SortState | null): ((a: Task, b: Task) => number) | null {
  if (!sort) return null;
  const cmp = COLUMN_BY_ID.get(sort.id)?.compare;
  if (!cmp) return null;
  return sort.dir === 'asc' ? cmp : (a, b) => cmp(b, a);
}

/**
 * applySort orders the rows inside each package and then the packages against
 * each other, comparing each package by the row that sorts first in it — so
 * "biggest first" puts the package holding the biggest file at the top and does
 * not silently mean "the package whose name happens to sort first".
 *
 * Nothing here touches the queue. Array sort is stable, so rows that compare
 * equal stay in the order they came in, which is the order they will run in.
 */
export function applySort(groups: [string, Task[]][], sort: SortState | null): [string, Task[]][] {
  const cmp = comparatorFor(sort);
  if (!cmp) return groups;
  const sorted: [string, Task[]][] = groups.map(([name, items]) => [name, [...items].sort(cmp)]);
  return sorted.sort((a, b) => {
    if (a[1].length === 0) return b[1].length === 0 ? 0 : 1;
    if (b[1].length === 0) return -1;
    return cmp(a[1][0], b[1][0]);
  });
}
