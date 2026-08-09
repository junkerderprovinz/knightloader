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
import { setEnabled, type Availability, type Task } from '../lib/api';
import { DIRECT_ID, endpointOf, useConnections } from '../lib/connections';
import { fmtBytes, fmtDate, fmtEta, fmtSpeed, pct } from '../lib/format';
import type { TranslationKey } from '../lib/i18n';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { IconCheck, IconRetry } from '../lib/icons';
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
  | 'connection'
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

// --- The tree column -------------------------------------------------------
//
// The name column is the tree column, and these four numbers are the package
// row's leading furniture. They live here, beside the column that has to leave
// room for them, because the indent and the column's own minimum width are one
// decision: a floor that does not include the indent is a floor for a name that
// is no longer there.

/** The name cell's own leading padding. */
const CELL_PAD = 8;
/** The tree control's hit target (`h-6 w-6` on the package row). */
const TWISTY_BOX = 24;
/** `gap-1.5` between twisty, folder and name. */
const TREE_GAP = 6;
/** The package glyph. Exported so the row that draws it uses this very number. */
export const FOLDER_GLYPH = 16;

/**
 * How far a link's name is indented inside its package, in pixels.
 *
 * Not a taste number: it is exactly the width of the furniture in front of a
 * package's NAME, so a link's name starts where its package's name starts and
 * the twisty and the folder hang to the left of both — the shape of every tree
 * anyone arriving from JDownloader has used.
 *
 * It was 36px, and 36 is less than 60: measured on the live instance, a link's
 * name began 24px BEFORE its own package's name. That reads as the link being
 * the outer level and the package the inner one — the tree upside down — and no
 * test can see it, because nothing about it is wrong except where it is.
 */
export const TREE_INDENT = CELL_PAD + TWISTY_BOX + TREE_GAP + FOLDER_GLYPH + TREE_GAP;

/**
 * What is left for the name itself once the indent is paid.
 *
 * The name column's minimum is this PLUS the indent, so widening the indent
 * cannot quietly narrow the text: the tree got 24px deeper and the floor moved
 * 24px with it, which is why a name reads exactly as well after that change as
 * before it. 120px is about sixteen characters — the point at which a file name
 * still tells you which file it is.
 */
const NAME_TEXT_FLOOR = 120;

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

// What each typed failure is called on screen.
//
// Only the reasons this build knows are in here. The server's reason is an open
// string on purpose — a newer backend can settle a task with a value this
// interface has never heard of — and an unrecognised one gets no label rather
// than the raw token: the whole worth of the label is that it is a word somebody
// can act on, and "reason: hoster_soft_limit" is not one.
const reasonKey: Record<string, TranslationKey> = {
  gone: 'task.reason.gone',
  auth: 'task.reason.auth',
  limit: 'task.reason.limit',
  unavailable: 'task.reason.unavailable',
  network: 'task.reason.network',
  diskFull: 'task.reason.diskFull',
  unsupported: 'task.reason.unsupported',
  captcha: 'task.reason.captcha',
  cancelled: 'task.reason.cancelled',
};

// The availability chip, one entry per verdict the server can send.
//
// '' is deliberately absent, and that absence is the whole point of the fourth
// state: a link nobody has checked says nothing at all, and 'uncheckable' is a
// link that WAS checked and whose host would not answer. Drawing them the same
// way is what left a Real-Debrid or JD link looking untouched forever.
//
// 'uncheckable' is the quiet shade, never the fail colour and never the accent.
// It is not activity and it is not a dead link, and painting it red is the same
// mistake in the interface that filing a transport error as offline is in the
// probe: it is how somebody is talked into deleting a link that is fine.
const availChip: Record<Exclude<Availability, ''>, { key: TranslationKey; tone: string }> = {
  online: { key: 'task.online', tone: 'text-statusOk' },
  offline: { key: 'task.offline', tone: 'text-statusFail' },
  uncheckable: { key: 'task.uncheckable', tone: 'text-carbon-textMuted' },
};

function NameCell({ task, t }: { task: Task; t: Translate }) {
  // A pending automatic retry is not the same as a dead task, and saying so
  // stops people restarting something that is already about to restart.
  const retrying = task.status === 'error' && !!task.nextTry;
  const reason = task.reason ? reasonKey[task.reason] : undefined;
  return (
    <div className="min-w-0">
      {/* Its own title, because the list's generic one cannot reach it: the row
          gives a cell its text as a tooltip only when the cell renders a plain
          string, and this one renders a component. Without it the name column
          at its narrowest is truncated text with no way at all to read the rest
          — and this is the column that truncates first, because it is the one
          that gives up its width to the others. */}
      <div dir="ltr" title={task.name || task.url} className="truncate text-start text-[13.5px] text-carbon-text">
        {task.name || task.url}
      </div>
      {task.error && (
        <div className="mt-0.5 flex items-center gap-1.5 text-[11px]">
          {/* The typed cause leads the line, as a tag rather than a second
              sentence: a column reading DISK FULL four times is a fact about
              this box, where four hoster sentences that each mean it are four
              things to read. It is the quiet eyebrow type and never the accent —
              a settled failure is not activity — and it carries no box of its
              own, because the shade step is the separation.

              It is capped rather than left to size itself. This line has already
              lost one fight to an element that would not shrink (see below), and
              a tag free to run the width of the cell would squeeze the sentence
              to nothing in a narrow column. At 45% the sentence always keeps the
              larger half, and the tag truncates with its own tooltip. */}
          {reason && (
            <span title={t(reason)} className="glim-eyebrow max-w-[45%] shrink-0 truncate">
              {t(reason)}
            </span>
          )}
          {/* The sentence wins the room, and `flex-1 min-w-0` is what gives it
              to it. The pending-retry note used to sit here as `shrink-0` prose,
              and prose that cannot shrink beside text that can is a race the
              text always loses: measured on the live instance at 1440, the
              German note wanted 142px of a 116px line, so the error span was
              squeezed to ZERO and the note itself was still cut off mid-word by
              the cell's own overflow. The row then said nothing about why it
              had failed — on the one row on the page somebody has to act on. */}
          {/* `min-w-0` and no `flex-1`: it sizes to its text and is the only
              thing on the line that may shrink, so it takes the whole shortfall
              and the glyph stays beside the sentence it belongs to instead of
              being pushed to the far edge of a wide column. */}
          <span title={task.error} className="min-w-0 truncate text-statusFail">
            {task.error}
          </span>
          {/* So the note is a glyph now: fixed width, never competing, and it
              still carries the whole sentence for the pointer and the screen
              reader. It is deliberately not the accent — a retry that has not
              happened yet is waiting, not activity. */}
          {retrying && (
            <span
              role="img"
              aria-label={t('task.retryPending')}
              title={t('task.retryPending')}
              className="shrink-0 text-carbon-textMuted"
            >
              <IconRetry width={11} height={11} />
            </span>
          )}
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
  // An availability verdict is worth showing only while nothing has been
  // attempted yet; once a transfer is running, the status is the answer.
  const chip = task.status === 'collected' && task.online ? availChip[task.online] : undefined;
  // The typed cause carries the detail, as a tooltip rather than a second word
  // on the line. "Host would not say" is the verdict and it is what the column
  // is for; whether the host was rate-limiting us or simply down is the next
  // question, and it belongs one hover away, not in the width of the cell.
  const why = task.reason ? reasonKey[task.reason] : undefined;
  return (
    <span className="inline-flex min-w-0 items-center gap-2">
      <StatusPill status={task.status} />
      {chip && (
        <span title={why ? t(why) : undefined} className={`truncate text-[11px] ${chip.tone}`}>
          {t(chip.key)}
        </span>
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

/**
 * Which connection is carrying this download.
 *
 * The column answers what a task IS ON, not what it was asked for. The server
 * writes the id when it hands the download to a backend, so a task pointed at a
 * proxy that was busy or switched off shows the connection that actually took
 * it, which is the only version of this column worth having. One that echoed the
 * request would agree with the settings page and disagree with the traffic.
 *
 * That is also why an unrouted task shows NOTHING rather than "Direct". An empty
 * id is "nobody has decided yet"; the direct gateway is a decision, with an id of
 * its own, and a collector full of rows announcing a decision that has not been
 * made is worse than a column of blanks. The blanks fill in as tasks start.
 *
 * Quiet type, never the accent: which way the bytes came in is metadata about a
 * finished fact, the same weight as the backend badge beside it. It is also the
 * reason there is no icon here. A glyph per row in a column that is blank for
 * most of them is decoration where the eye is scanning for names.
 *
 * The word for the gateway is the connection page's own key rather than a new
 * one. There is exactly one right word for it, and a second key would be that
 * word maintained twice across forty-two locales, drifting in some of them.
 */
function ConnectionCell({ task, t, base }: { task: Task; t: Translate; base: string }) {
  const rows = useConnections(base);
  const id = task.connection ?? '';
  if (!id) return null;

  const row = rows.get(id);
  const direct = t('settings.connections.kind.direct');
  // Three ways to have no endpoint to print, and they are not the same thing.
  // The gateway and a direct ROW both mean "out over this machine", so both read
  // as the word; an id with no row at all is a connection that was deleted, or a
  // list that has not arrived yet, and it keeps the raw id because that can be
  // matched against the settings page by hand where a blank cell cannot.
  const endpoint = row ? endpointOf(row) : '';
  const text = endpoint || (id === DIRECT_ID || row ? direct : id);
  // A direct row is a rule the user wrote to keep certain hosts off every proxy,
  // and two of them in one list read identically. The hosts it claims go in the
  // tooltip, which is the only thing that tells them apart.
  const hint = endpoint || row?.filter?.join(', ') || text;
  return (
    <span dir="ltr" title={hint} className="block truncate text-[11px] text-carbon-textMuted">
      {text}
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

/**
 * packageStatus is the one word a package header can honestly show.
 *
 * A failure anywhere wins, whatever else the package is doing: nine finished
 * files and one dead link is not a finished package, and a header that says
 * "Done" hides the one row somebody has to act on. Otherwise it is the least
 * settled state in the package, so a package with one running link reads as
 * running rather than as the queue its other nine links are still sitting in.
 */
export function packageStatus(items: Task[]): Task['status'] {
  if (items.length === 0) return 'queued';
  if (items.some((x) => x.status === 'error')) return 'error';
  let best = items[0].status;
  for (const x of items) if (STATUS_RANK[x.status] < STATUS_RANK[best]) best = x.status;
  return best;
}

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
    minWidth: TREE_INDENT + NAME_TEXT_FLOOR,
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
    // A package that shows nothing in the status column is a package that looks
    // like a spacer. It gets the same pill as a link, over the whole package.
    aggregate: (items) => <StatusPill status={packageStatus(items)} />,
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
    // Only when the whole package came from one host. "3 hosts" in a column of
    // host names is a different kind of value in the same column.
    aggregate: (items) => {
      const one = new Set(items.map(hostOf));
      return one.size === 1 ? hostOf(items[0]) : null;
    },
  },
  {
    id: 'connection',
    labelKey: 'columns.connection',
    width: 160,
    minWidth: 90,
    align: 'start',
    // The endpoint is a URL and reads left to right even in Arabic or Hebrew.
    ltr: true,
    hideable: true,
    // By id, not by the label the cell draws. A comparator is not a component
    // and has no instance to resolve an id against, and sorting this column is
    // for putting the rows that share a connection together, which the id does
    // exactly, whatever order it happens to put the groups in.
    compare: (a, b) => cmpText(a.connection ?? '', b.connection ?? ''),
    render: (task, ctx) => <ConnectionCell task={task} t={ctx.t} base={ctx.base} />,
    // Only when the whole package went out the same way. A package split across
    // two proxies has no single answer, and picking one of them would say
    // something untrue about the other's links.
    aggregate: (items, ctx) => {
      const one = new Set(items.map((x) => x.connection ?? ''));
      if (one.size !== 1 || !items[0].connection) return null;
      return <ConnectionCell task={items[0]} t={ctx.t} base={ctx.base} />;
    },
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
 * BOTH sets are also cut towards what FITS, and that is the test each one has to
 * pass on its own. Measured on the live instance at a 1400px window: every
 * column on by default came to 1812px against 1112px of room, so the downloads
 * table opened 700px scrolled off its own right edge and the last two columns
 * were only reachable by dragging sideways. A default that does not fit reads as
 * a broken layout, not as a rich one — the rest are one click away in the header
 * menu, which is the point of having the menu.
 *
 * Downloads is cut as far as it can honestly go and still does NOT fit: measured
 * again at 1440, its eight columns want 1188px against 1152px of room, so the
 * name column sits at its 180px floor and the table scrolls 36px inside its own
 * card (196px at 1280). Every column left on it carries a value on every row —
 * size, progress, speed, eta, status, host are the download table JDownloader
 * shows and the muscle memory expects — so the remaining shortfall is a widths
 * decision, not a which-columns one, and it is deliberately not being made here
 * by whoever last touched this file. The page itself never scrolls sideways;
 * only the table does, which is what the overflow container is for.
 *
 * The collector was cut by relevance only and never measured, and it failed the
 * same test the moment anybody looked: at 1280 its default set came to 1234px
 * against 982px of room. The whole 252px shortfall was `source`, 260px wide and
 * empty on every row of a list whose links were pasted — while the name column,
 * the one thing on the row that says WHICH file this is, sat pinned at its
 * 180px floor with fifteen characters of a file name showing. Hiding source
 * gives that 260px back to the name and the table fits, which is also what the
 * downloads set already decided about the same column.
 *
 * `connection` ships hidden in both, and the arithmetic above is only half the
 * reason. The other half is that it is empty for everybody: an instance with no
 * connections configured, which is every instance until somebody adds one,
 * routes nothing, so the column is 160px of blank on every row, and 160px is
 * taken from the name column that is already pinned at its floor. It is one
 * click away for the people who have a list of proxies and want to see which of
 * them is carrying what, which is exactly who the column is for.
 */
export const DEFAULT_HIDDEN: Record<ListProfile, ColumnId[]> = {
  downloads: ['comment', 'source', 'added', 'finished', 'resolver', 'connection'],
  collector: ['progress', 'speed', 'eta', 'finished', 'comment', 'added', 'source', 'connection'],
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
