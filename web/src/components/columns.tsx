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

import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { fetchOptions, setEnabled, setTaskOptions, type Availability, type Task } from '../lib/api';
import { DIRECT_ID, endpointOf, useConnections } from '../lib/connections';
import { fmtBytes, fmtDate, fmtEta, fmtSpeed, pct } from '../lib/format';
import type { TranslationKey } from '../lib/i18n';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { IconCheck, IconRetry } from '../lib/icons';
import { ProgressBar } from './ProgressBar';
import { ResolverBadge, StatusPill } from './StatusPill';
import { useTooltip } from './ui';


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
  | 'source'
  | 'variant'
  | 'peers'
  | 'seeds'
  | 'ratio';

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
  align: 'start' | 'center' | 'end';
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

// The same verdict as availChip, as a dot rather than a word — for NameCell
// below, which shows it beside the name itself rather than only in the
// Status column (jdp, 2026-08-25: "Der status der links soll auch per
// eingefärbtem icon angezeigt werden (als online oder offline)"). The same
// solid-dot tokens StatusPill's own status dot already uses, not the softer
// text-status* wash availChip reads: a 6px dot needs the fully saturated
// colour to read at all, where a word has its own text weight to carry it.
const availDot: Record<Exclude<Availability, ''>, string> = {
  online: 'bg-statusOkSolid',
  offline: 'bg-statusFailSolid',
  uncheckable: 'bg-statusNeutralSolid',
};

// --- The row tooltip --------------------------------------------------------
//
// New UI text for this wave, kept out of en.ts on purpose: the locale files
// are 9E's own lane and it runs after 9A-9D land (build-plan.md section 8's
// Wave 9 note). Same arrangement as components/CollectorFacets.tsx and
// pages/settings/Captcha.tsx - cx() asks t() first, so the day these two
// keys land for real in en.ts (and in every other locale) this table stops
// being consulted and can be deleted without touching anything else here.
const PENDING = {
  'task.tooltip.url': 'URL',
  'task.tooltip.changed': 'Last changed',
  // build-plan.md's 11.5E (torrent/magnet support) additions. columns.peers/
  // columns.seeds/columns.ratio are deliberately NOT in this table - it only
  // backs cx(), and the column header/menu row (TaskList.tsx, ColumnMenu.tsx)
  // call t() on ColumnDef.labelKey directly, with no fallback of their own -
  // see the labelKey casts below for where that gap actually lives and why it
  // cannot be closed from this file.
  'task.tooltip.infoHash': 'Info hash',
  'task.tooltip.trackers': 'Trackers',
  'task.tooltip.swarm': 'Peers / seeds / ratio',
  'task.tooltip.swarmDetail': '{peers} peers, {seeds} seeds, ratio {ratio}',
  'task.tooltip.uploaded': 'Uploaded',
  'task.tooltip.seeding': 'Still seeding',
} as const;

type PendingKey = keyof typeof PENDING;

function useCx() {
  const { t } = useT();
  return useCallback(
    (key: PendingKey, vars?: Record<string, string | number>) => {
      // The cast is the whole point: these keys are not in the union yet. It
      // is narrow - only keys in PENDING can be passed - and it goes with
      // the table.
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      let s: string = translated ?? PENDING[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [t],
  );
}

/**
 * The row tooltip's own date formatting, spelled out in full rather than
 * fmtDate's short column form (lib/format.ts): the column is short because
 * it has to fit a cell, the tooltip has the room a cell never does, and
 * repeating the column's own short form in its tooltip would tell a reader
 * nothing the column had not already said. No formatter cache like fmtDate
 * keeps, deliberately: that cache exists because a finished-at COLUMN builds
 * one per row on every repaint of a few hundred rows, where this runs once,
 * when a hover actually opens a tooltip - which is not that.
 */
function fmtDateFull(iso: string | undefined): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() <= 1) return '';
  return new Intl.DateTimeFormat(document.documentElement.lang || undefined, {
    dateStyle: 'full',
    timeStyle: 'medium',
  }).format(d);
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
 *
 * Shared by the column cell and the row tooltip below, so a task's connection
 * resolves to the same words in both rather than two computations that could
 * drift apart. Null for an unrouted task - "nobody has decided yet" has no
 * text to show wherever it is asked from.
 */
function useConnectionLabel(task: Task, t: Translate, base: string): { text: string; hint: string } | null {
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
  // hint, which is the only thing that tells them apart.
  const hint = endpoint || row?.filter?.join(', ') || text;
  return { text, hint };
}

/** One labelled fact in the row tooltip - a category on its own line, the value on the next so a long one wraps instead of forcing a ragged right edge beside a short label. */
function TooltipField({ label, children, ltr }: { label: string; children: ReactNode; ltr?: boolean }) {
  return (
    <div className="min-w-0">
      <div className="glim-eyebrow">{label}</div>
      <div dir={ltr ? 'ltr' : undefined} className="break-words text-carbon-text">
        {children}
      </div>
    </div>
  );
}

/**
 * RowTooltipContent is "everything about this row" in one hover rather than
 * several separate cell tooltips, because six of this table's columns ship
 * hidden by default (see DEFAULT_HIDDEN below) purely for width - the table
 * does not fit with all of them on, not because host, backend, connection,
 * added, finished and comment stop mattering on the rows where a column is
 * off. This is the one place to read them without opening the column menu
 * and giving up width elsewhere for a column that stays blank on most rows
 * anyway. Nothing here is fetched: every field already arrived on the task
 * with the row - see useTooltip in components/ui.tsx for the hover mechanics.
 */
function RowTooltipContent({ task, t, base }: { task: Task; t: Translate; base: string }) {
  const cx = useCx();
  const connection = useConnectionLabel(task, t, base);
  // Same signal the Peers/Seeds/Ratio columns and ResolverBadge already key
  // off - the torrent resolver's own Info().ID (internal/resolver/torrent).
  const isTorrent = task.resolver === 'torrent';
  const name = task.name || task.url;
  // Only worth its own line when it says something the name above did not -
  // a task with no name already shows the URL as its name.
  const showUrl = !!task.name && task.name !== task.url;
  const host = hostOf(task);
  const added = fmtDateFull(task.createdAt);
  const finished = fmtDateFull(task.finishedAt);
  const changed = fmtDateFull(task.changedAt);
  // Only shown while a retry is actually pending - a settled error carries no
  // nextTry, and the icon it sits beside already says "retrying automatically";
  // the exact moment is the one part of that sentence the row has no room for.
  const retryAt = task.status === 'error' ? fmtDateFull(task.nextTry) : '';

  return (
    <div className="flex flex-col gap-2">
      <div dir="ltr" className="break-words text-[12.5px] font-semibold text-carbon-text">
        {name}
      </div>
      <div className="flex flex-col gap-1.5 border-t border-carbon-border/60 pt-2">
        {showUrl && (
          <TooltipField label={cx('task.tooltip.url')} ltr>
            {task.url}
          </TooltipField>
        )}
        {host && (
          <TooltipField label={t('columns.host')} ltr>
            {host}
          </TooltipField>
        )}
        <TooltipField label={t('columns.resolver')}>
          <ResolverBadge resolver={task.resolver} />
        </TooltipField>
        {/* Peers/Seeds/Ratio also have their own columns (hidden by default,
            like six other low-traffic ones already are - see DEFAULT_HIDDEN
            below), so this is here for the same reason connection/added/
            finished/comment/source are: readable without opening the column
            menu and giving up width elsewhere for a column blank on every
            non-torrent row. Uploaded and "still seeding" go no further than
            here - the spec (docs/torrent-support.md) asks for "full peer/seed
            detail" in the tooltip specifically, a fuller picture than the
            three columns alone give. */}
        {isTorrent && (
          <TooltipField label={cx('task.tooltip.swarm')} ltr>
            {cx('task.tooltip.swarmDetail', {
              peers: task.peers ?? 0,
              seeds: task.seeds ?? 0,
              ratio: fmtRatio(task.ratio),
            })}
            {task.seeding ? ` · ${cx('task.tooltip.seeding')}` : ''}
            {task.uploaded ? ` · ${cx('task.tooltip.uploaded')} ${fmtBytes(task.uploaded)}` : ''}
          </TooltipField>
        )}
        {task.infoHash && (
          <TooltipField label={cx('task.tooltip.infoHash')} ltr>
            {task.infoHash}
          </TooltipField>
        )}
        {task.trackers && task.trackers.length > 0 && (
          <TooltipField label={cx('task.tooltip.trackers')} ltr>
            {task.trackers.join(', ')}
          </TooltipField>
        )}
        {connection && (
          <TooltipField label={t('columns.connection')} ltr>
            {connection.text}
          </TooltipField>
        )}
        {added && (
          <TooltipField label={t('columns.added')} ltr>
            {added}
          </TooltipField>
        )}
        {finished && (
          <TooltipField label={t('columns.finished')} ltr>
            {finished}
          </TooltipField>
        )}
        {retryAt && (
          <TooltipField label={t('task.retryPending')} ltr>
            {retryAt}
          </TooltipField>
        )}
        {changed && (
          <TooltipField label={cx('task.tooltip.changed')} ltr>
            {changed}
          </TooltipField>
        )}
        {task.comment && <TooltipField label={t('columns.comment')}>{task.comment}</TooltipField>}
        {task.source && (
          <TooltipField label={t('columns.source')} ltr>
            {task.source}
          </TooltipField>
        )}
      </div>
    </div>
  );
}

function NameCell({ task, t, base }: { task: Task; t: Translate; base: string }) {
  // A pending automatic retry is not the same as a dead task, and saying so
  // stops people restarting something that is already about to restart.
  const retrying = task.status === 'error' && !!task.nextTry;
  const reason = task.reason ? reasonKey[task.reason] : undefined;
  // The row's own rich tooltip lives on this cell rather than a plain
  // `title`: it is the one cell that truncates first (see TREE_INDENT
  // above), and the one hover that can afford to say more than the string
  // already on screen - host, backend, connection, added/finished down to
  // the second, whichever of DEFAULT_HIDDEN's six columns happen to be off
  // right now. A native `title` is deliberately NOT also set on this element
  // - the two would be hovering the exact same box, and the browser's own
  // delayed tooltip would eventually stack on top of this one.
  const tip = useTooltip<HTMLDivElement>(<RowTooltipContent task={task} t={t} base={base} />);
  // Same gating as StatusCell's own chip: a verdict is only worth a glance
  // while nothing has been attempted yet, and once a transfer starts the
  // status itself is the answer (see that cell's own doc comment).
  const avail = task.status === 'collected' && task.online ? task.online : undefined;
  return (
    <div className="min-w-0">
      <div className="flex min-w-0 items-center gap-1.5">
        {avail && (
          <span
            role="img"
            title={t(availChip[avail].key)}
            aria-label={t(availChip[avail].key)}
            className={`h-1.5 w-1.5 shrink-0 rounded-[var(--radius-pill)] ${availDot[avail]}`}
          />
        )}
        <div dir="ltr" {...tip.triggerProps} className="min-w-0 truncate text-start text-[13.5px] text-carbon-text">
          {/* task.ext is a display-only best-effort hint (core.Task.Ext's
              own doc comment), never appended to task.name itself - Name
              stays the resolved-vs-placeholder sentinel every rename/probe
              guard in the backend already keys on. Only shown once a real
              name has resolved: a bare URL placeholder gets no extension
              tacked onto it. */}
          {task.name && task.name !== task.url && task.ext ? `${task.name}.${task.ext}` : task.name || task.url}
        </div>
      </div>
      {tip.node}
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

// AvailDot is StatusCell's own dot, pulled out so PackageStatusCell below -
// the status column's PACKAGE row, jdp 2026-08-25's own follow-up ("auf dem
// ordner wird in der spalte immer noch gesammelt angezeigt") - can paint the
// exact same shape over a whole package's aggregate verdict instead of one
// task's.
function AvailDot({ avail, title }: { avail: Availability | undefined; title?: string }) {
  return (
    <span
      title={title}
      className={`inline-block h-2 w-2 shrink-0 rounded-[var(--radius-pill)] ${
        avail ? availDot[avail] : 'bg-carbon-textMuted/40'
      }`}
    />
  );
}

// packageAvailStatus folds every item's own verdict into the one worth
// showing on their shared package row: any dead link outranks everything
// else (the same "bad news wins" rule packageStatus's own error check
// already follows), all-online is the other unambiguous case, uncheckable
// is worth a glance even mixed with plain unchecked, and undefined (the
// neutral dot) is what a package nobody has checked yet - or one that
// mixes online and offline links with nothing worse - shows.
function packageAvailStatus(items: Task[]): Availability | undefined {
  if (items.some((x) => x.online === 'offline')) return 'offline';
  if (items.length > 0 && items.every((x) => x.online === 'online')) return 'online';
  if (items.some((x) => x.online === 'uncheckable')) return 'uncheckable';
  return undefined;
}

function StatusCell({ task, t }: { task: Task; t: Translate }) {
  // The typed cause carries the detail, as a tooltip rather than a second word
  // on the line. "Host would not say" is the verdict and it is what the column
  // is for; whether the host was rate-limiting us or simply down is the next
  // question, and it belongs one hover away, not in the width of the cell.
  const why = task.reason ? reasonKey[task.reason] : undefined;
  // Every row in the Collector carries the same task.status, so StatusPill's
  // own dot+word here would always read "collected" in grey - the one thing
  // that actually varies while a link is staged is the availability check,
  // which this cell shows instead: just the dot, coloured by that verdict
  // rather than by a status that never changes (jdp, 2026-08-25: "status-
  // spalte soll nicht gesammelt text anzeigen sondern nur der status
  // punkt... der soll grün wenn der link online ist... oder rot wenn
  // offline"). Once a task leaves "collected" the availability question is
  // moot - the transfer's own status IS the answer - so StatusPill's usual
  // dot+word takes back over below, unchanged.
  if (task.status === 'collected') {
    const avail = task.online;
    return <AvailDot avail={avail} title={why ? t(why) : avail ? t(availChip[avail].key) : undefined} />;
  }
  return (
    <span className="inline-flex min-w-0 items-center gap-2">
      <StatusPill status={task.status} />
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

// The column's own read of useConnectionLabel above - text truncated to the
// cell, hint carried as a native title since this box, unlike the name
// column, is not already sitting under the row's own rich tooltip.
function ConnectionCell({ task, t, base }: { task: Task; t: Translate; base: string }) {
  const label = useConnectionLabel(task, t, base);
  if (!label) return null;
  return (
    <span dir="ltr" title={label.hint} className="block truncate text-[11px] text-carbon-textMuted">
      {label.text}
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

/** Same signal as RowTooltipContent's own isTorrent - internal/resolver/torrent's Info().ID. */
const isTorrentTask = (t: Task): boolean => t.resolver === 'torrent';

/**
 * fmtRatio prints uploaded-over-downloaded to two places, never scientific
 * notation and never blank for a real zero - a fresh torrent that has not
 * uploaded a byte yet genuinely is "0.00", the same "zero is a true statement
 * for a torrent" rule core.TorrentStats' own doc comment states for peers and
 * seeds.
 */
function fmtRatio(r: number | undefined): string {
  return (r ?? 0).toFixed(2);
}

// --- The "Variante" column --------------------------------------------------
//
// core.Task.Variant, decoded the same way variantEncode/variantDecode
// (app_ytdlp_variants.go) encode it: "<kind>" or "<kind>:<sub>". kind is one
// of the five rows expandYtdlpVariants always creates for a yt-dlp-routed
// link - video/audio/thumbnail/subtitle/description - fixed the moment that
// row was created, never edited here. sub is a quality preset on a video
// row or an audio format on an audio row, the only two kinds this column's
// own picker edits (jdp, 2026-08-25's locked answer: "Video (Auflösung...),
// Audio (Format/Bitrate...)" - a thumbnail/subtitle/description row gets no
// picker, just its own kind label).
//
// This column is not only a nicety: setTaskName (app_tasks.go) propagates
// one resolved title to every URL-sharing sibling, so all five of a link's
// own rows show the exact same Name. This is the one column where they read
// differently from each other at all.
export function variantKindOf(task: Task): string {
  const v = task.variant ?? '';
  const i = v.indexOf(':');
  return i === -1 ? v : v.slice(0, i);
}

function variantSubOf(task: Task): string {
  const v = task.variant ?? '';
  const i = v.indexOf(':');
  return i === -1 ? '' : v.slice(i + 1);
}

export const VARIANT_KIND_LABEL_KEY: Record<string, TranslationKey> = {
  video: 'columns.variant.video',
  audio: 'columns.variant.audio',
  thumbnail: 'columns.variant.thumbnail',
  subtitle: 'columns.variant.subtitle',
  description: 'columns.variant.description',
};

/**
 * One shared fetch backs every row's own picker, not one per row: the menu
 * is the same handful of ids for the whole table, and forty rows each
 * calling fetchOptions() on mount would be forty identical requests for the
 * same short list. Module-scoped rather than threaded through CellContext,
 * so this column stays self-contained the way peers/seeds/ratio's own
 * isTorrentTask does, instead of widening what every OTHER cell's context
 * has to carry for a menu only this one column reads.
 */
let ytdlpMenus: Promise<{ qualities: string[]; audioFormats: string[] }> | null = null;
function loadYtdlpMenus() {
  if (!ytdlpMenus) {
    ytdlpMenus = fetchOptions().then(
      (o) => ({ qualities: o.ytdlpQualities ?? [], audioFormats: o.ytdlpAudioFormats ?? [] }),
      () => ({ qualities: [], audioFormats: [] }),
    );
  }
  return ytdlpMenus;
}

function useYtdlpMenus() {
  const [menus, setMenus] = useState<{ qualities: string[]; audioFormats: string[] }>({
    qualities: [],
    audioFormats: [],
  });
  useEffect(() => {
    let live = true;
    void loadYtdlpMenus().then((m) => {
      if (live) setMenus(m);
    });
    return () => {
      live = false;
    };
  }, []);
  return menus;
}

function VarianteCell({ task, ctx }: { task: Task; ctx: CellContext }) {
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  const menus = useYtdlpMenus();

  const kind = variantKindOf(task);
  if (!kind) return null;
  const sub = variantSubOf(task);
  const label = ctx.t(VARIANT_KIND_LABEL_KEY[kind] ?? VARIANT_KIND_LABEL_KEY.video);
  // A video row's own probe (task.availableQualities, core.Task's own doc
  // comment) narrows the menu to what this specific source genuinely
  // offers (jdp, 2026-08-25: "man soll nur die varianten auswählen können
  // die wirklich verfügbar sind") - falling back to the full static menu
  // whenever nothing has probed yet, exactly like every other "empty means
  // no opinion" field this feature already follows. Audio format has no
  // equivalent narrowing: -x --audio-format transcodes via ffmpeg, which
  // works for any of the fixed formats regardless of what codec the source
  // itself uses, so restricting that menu by source codec would hide
  // choices that actually work fine.
  const options =
    kind === 'video'
      ? task.availableQualities?.length
        ? task.availableQualities
        : menus.qualities
      : kind === 'audio'
        ? menus.audioFormats
        : null;

  async function change(value: string) {
    setBusy(true);
    try {
      await setTaskOptions([task.id], { variantQuality: value }, ctx.base);
    } catch (err) {
      toast(err instanceof Error && err.message ? err.message : ctx.t('task.switchFailed'), 'fail');
    } finally {
      setBusy(false);
    }
  }

  return (
    <span className="flex min-w-0 items-center gap-1.5 text-[11px] text-carbon-textMuted">
      <span className="shrink-0">{label}</span>
      {options && options.length > 0 && (
        <select
          value={sub || options[0]}
          disabled={busy}
          onChange={(e) => void change(e.target.value)}
          onClick={(e) => e.stopPropagation()}
          className="min-w-0 rounded-[var(--radius-control)] bg-carbon-surface3/60 px-1 py-0.5 text-[11px] text-carbon-text disabled:opacity-40"
        >
          {options.map((o) => (
            <option key={o} value={o}>
              {o}
            </option>
          ))}
        </select>
      )}
    </span>
  );
}

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
    render: (task, ctx) => <NameCell task={task} t={ctx.t} base={ctx.base} />,
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
    align: 'center',
    hideable: true,
    compare: (a, b) => STATUS_RANK[a.status] - STATUS_RANK[b.status],
    render: (task, ctx) => <StatusCell task={task} t={ctx.t} />,
    // A package that shows nothing in the status column is a package that looks
    // like a spacer. It gets the same pill as a link, over the whole package -
    // or, while every one of its links is still sitting in the collector, the
    // same availability dot StatusCell's own row shows instead (jdp,
    // 2026-08-25: "auf dem ordner wird in der spalte immer noch gesammelt
    // angezeigt" - this aggregate branch was the one place still missed when
    // StatusCell itself was fixed, since a folded package's own row never
    // calls StatusCell at all).
    aggregate: (items) =>
      packageStatus(items) === 'collected' ? (
        <AvailDot avail={packageAvailStatus(items)} />
      ) : (
        <StatusPill status={packageStatus(items)} />
      ),
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
    id: 'variant',
    labelKey: 'columns.variant',
    width: 132,
    minWidth: 90,
    align: 'start',
    hideable: true,
    compare: (a, b) => cmpText(variantKindOf(a), variantKindOf(b)),
    render: (task, ctx) => <VarianteCell task={task} ctx={ctx} />,
    // No aggregate: a package almost always mixes kinds (its own video,
    // audio, thumbnail... rows all share one package), so there is no
    // single variant a package header could honestly show.
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
  // Peers/Seeds/Ratio - build-plan.md's 11.5E. Blank on every non-torrent row
  // rather than "0": zero peers is a true, useful reading for a torrent and
  // meaningless noise for an HTTP download, the same distinction
  // core.TorrentStats' own doc comment draws. Hidden by default below
  // (DEFAULT_HIDDEN), the same treatment six other low-traffic columns
  // already get, and readable regardless via the row tooltip above.
  //
  // labelKey is cast the same way System.tsx/Scripts.tsx's own PENDING keys
  // are, but with a real difference worth being explicit about: those are
  // consumed through this file's own cx() (or their page's), which supplies
  // an English fallback when the catalogue has no entry yet. These three are
  // consumed by TaskList.tsx and ColumnMenu.tsx calling t(col.labelKey)
  // directly, with no fallback of their own - neither file is this wave's to
  // edit, and en.ts is 11.5F's (the translate phase, which lands right after
  // this lane). Until 11.5F adds 'columns.peers'/'columns.seeds'/
  // 'columns.ratio', t() returns undefined for these three specifically
  // (i18n.tsx: `dict[key] ?? en[key]`, both undefined for a key neither
  // object has) and React renders that as nothing - an empty header cell and
  // an empty column-menu row, not a raw dotted key. Self-heals the moment
  // 11.5F lands; see this wave's own report.
  {
    id: 'peers',
    labelKey: 'columns.peers' as unknown as TranslationKey,
    width: 76,
    minWidth: 56,
    align: 'end',
    numeric: true,
    hideable: true,
    compare: (a, b) => (a.peers ?? 0) - (b.peers ?? 0),
    render: (task) => (isTorrentTask(task) ? String(task.peers ?? 0) : ''),
    aggregate: (items) => {
      const torrents = items.filter(isTorrentTask);
      return torrents.length > 0 ? String(sum(torrents, (x) => x.peers ?? 0)) : null;
    },
  },
  {
    id: 'seeds',
    labelKey: 'columns.seeds' as unknown as TranslationKey,
    width: 76,
    minWidth: 56,
    align: 'end',
    numeric: true,
    hideable: true,
    compare: (a, b) => (a.seeds ?? 0) - (b.seeds ?? 0),
    render: (task) => (isTorrentTask(task) ? String(task.seeds ?? 0) : ''),
    aggregate: (items) => {
      const torrents = items.filter(isTorrentTask);
      return torrents.length > 0 ? String(sum(torrents, (x) => x.seeds ?? 0)) : null;
    },
  },
  {
    id: 'ratio',
    labelKey: 'columns.ratio' as unknown as TranslationKey,
    width: 84,
    minWidth: 60,
    align: 'end',
    numeric: true,
    hideable: true,
    compare: (a, b) => (a.ratio ?? 0) - (b.ratio ?? 0),
    render: (task) => (isTorrentTask(task) ? fmtRatio(task.ratio) : ''),
    // No aggregate, matching added/finished/comment/source just above: a
    // package's ratio is not a sum or a mean of its members' ratios in any
    // sense somebody reading the header would recognise as "the" ratio.
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
// Peers/seeds/ratio join both lists' hidden set, on top of everything the two
// long comments above already argued for: they are blank on every row that is
// not a torrent, which today is every row on every instance that has never
// added a magnet link or a .torrent file, and a column of blanks earns its
// keep even less than `connection` (which at least resolves once one proxy is
// configured) does before this feature has been used even once.
export const DEFAULT_HIDDEN: Record<ListProfile, ColumnId[]> = {
  // 'variant' stays visible here (unlike its own downloads-list default just
  // below): it is only blank once a link is already routed and past
  // choosing a quality, where a resolver+status pair already says what
  // downloads mostly wants to know. The collector is exactly where a
  // yt-dlp-routed link's five rows appear and want a quality picked, so
  // hiding it there by default would hide the "Variante" feature itself.
  downloads: [
    'comment',
    'source',
    'added',
    'finished',
    'resolver',
    'connection',
    'variant',
    'peers',
    'seeds',
    'ratio',
  ],
  collector: [
    'progress',
    'speed',
    'eta',
    'finished',
    'comment',
    'added',
    'source',
    'resolver',
    'connection',
    'peers',
    'seeds',
    'ratio',
  ],
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
