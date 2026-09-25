// The column registry for the download list, plus the pure functions that turn a
// stored layout back into a live one.
//
// A column is four things at once: a header, a cell, a sort order and an id in
// somebody's saved layout. Describing all four in one entry is what stops a
// column sorting by one value and printing another, which is the failure people
// report as "the sort is broken" and nobody can reproduce.
//
// This file is .tsx and not .ts because a cell renderer is markup. Splitting the
// renderers into the list component would leave the registry describing columns
// it cannot draw, which is the drift the registry exists to prevent.

import { useEffect, useState, type ReactNode } from 'react';
import {
  fetchOptions,
  priorityChoices,
  setEnabled,
  setTaskOptions,
  type Availability,
  type ExtractJob,
  type Task,
} from '../lib/api';
import { DIRECT_ID, endpointOf, useConnections } from '../lib/connections';
import { fmtBytes, fmtDate, fmtDateFull, fmtEta, fmtPct, fmtSpeed, pct } from '../lib/format';
import type { TranslationKey } from '../lib/i18n';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { IconBolt, IconCheck, IconPin, IconPower, IconRetry, IconStopMark, PriorityGlyph } from '../lib/icons';
import { hostOf } from '../lib/searchQuery';
import { adviceFor } from '../lib/failureAdvice';
import { FailureAdvice } from './FailureAdvice';
import { HosterIcon } from './HosterIcon';
import { ProgressBar } from './ProgressBar';
import { ResolverBadge, StatusPill, UnpackPill, unpackLabel, unpackState, type UnpackState } from './StatusPill';
import { RetryNote, retryPending } from './RetryCountdown';
import { useShake } from '../lib/useShake';
import { useTooltip } from './ui';
import {
  NO_PRESET_MENUS,
  VariantDropdown,
  audioPickers,
  audioSummary,
  presetMenusOf,
  probedFormats,
  videoPickers,
  videoSummary,
  type PickerProps,
  type PresetMenus,
} from './VariantPicker';

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
 * downloads list shows transfers, so they do not want the same columns, and one
 * shared layout would mean hiding "speed" on the collector hides it on the list
 * where it is the point.
 */
export type ListProfile = 'downloads' | 'collector';

export type Translate = (key: TranslationKey, vars?: Record<string, string | number>) => string;

// The name column is the tree column, and these four numbers are the package
// row's leading furniture. They live beside the column that has to leave room
// for them, because the indent and the column's own minimum width are one
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
 * How far a link's name is indented inside its package, in pixels. It is
 * exactly the width of the furniture in front of a package's name, so a link's
 * name starts where its package's name starts and the twisty and the folder
 * hang to the left of both, which is the shape of every tree anyone arriving
 * from JDownloader has used.
 */
export const TREE_INDENT = CELL_PAD + TWISTY_BOX + TREE_GAP + FOLDER_GLYPH + TREE_GAP;

/**
 * What is left for the name itself once the indent is paid. The name column's
 * minimum is this plus the indent, so widening the indent cannot quietly narrow
 * the text. 120px is about sixteen characters, the point at which a file name
 * still tells you which file it is.
 */
const NAME_TEXT_FLOOR = 120;

export interface CellContext {
  t: Translate;
  /** The instance this list is showing, for the cells that can act on a row. */
  base: string;
  /**
   * Which of the two lists is drawing this cell. One column can mean two
   * different things in two lists, and the status column is that case: a staged
   * link has no transfer state worth a word, while a running one has nothing to
   * say about availability. Two registry entries would mean two ids, two stored
   * widths and a layout that forgets itself when a list changes which it uses.
   */
  profile: ListProfile;
  /**
   * The removal question for a whole package, asked by the page rather than by
   * the row. The button does not delete: it hands the package's ids to the same
   * question a multiple selection asks, with its count, its file choice and its
   * undo. A second removal path would be a second place for that question to
   * drift.
   *
   * Optional, because not every list has a toolbar that asks it. Without one
   * the button does not appear at all.
   */
  onRemovePackage?: (ids: string[]) => void;
  /**
   * The latest unpacking of each file's archive, keyed by task id, with every
   * part of a set pointing at its archive's job (Archives.tsx's
   * extractionsByTask). Absent in the collector, where nothing is unpacked.
   */
  extractions?: ReadonlyMap<string, ExtractJob>;
  /** The task the queue halts after (QueueState.stopMark), when one is marked. */
  stopMark?: string;
  /**
   * Whether the Enabled column is drawn. A switched-off row wears a mark of its
   * own only where that column's switch is not there to show it.
   */
  switchShown?: boolean;
}

export interface ColumnDef {
  id: ColumnId;
  labelKey: TranslationKey;
  /** The header, where one list calls this column something else. */
  labelByProfile?: Partial<Record<ListProfile, TranslationKey>>;
  /**
   * The lists this column exists in at all. Absent means both.
   *
   * Stronger than DEFAULT_HIDDEN: hidden is a preference somebody can undo from
   * the header menu, this is the column not being part of that list. Used for
   * `enabled`, which is the collector's "take this link along when I press
   * start"; once a link is in the queue the switch that means something is
   * pause. Merely hiding it would leave a menu entry that switches on a control
   * with no honest meaning where it sits.
   */
  onlyIn?: ListProfile[];
  /** Default width in CSS pixels; what the user drags overrides it. */
  width: number;
  /**
   * The default width where one list draws the column differently, the same
   * per-list shape `labelByProfile` takes for the header. Widths a user drags
   * are stored per list (`list.columns.<profile>`), so this is only the
   * starting point.
   */
  widthByProfile?: Partial<Record<ListProfile, number>>;
  /** How far the column gives way in a narrow window, and how far it can be dragged. */
  minWidth: number;
  /** The floor where one list's cell can go narrower than the other's. */
  minWidthByProfile?: Partial<Record<ListProfile, number>>;
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

/**
 * Tip is a piece of on-screen text that carries the house bubble instead of the
 * operating system's own. ui.tsx pulled `title` out of Button and IconBadge for
 * the same reason; these are the same call sites on raw elements. A native
 * `title` draws the OS balloon, in the OS font, at the pointer and on the OS's
 * timing, right beside the house bubble the control next to it opens.
 *
 * It lives in this module for the same reason `hostOf` is re-exported from it:
 * this is where the list, the collector's facets and the collector's own cards
 * take their shared furniture from.
 */
export function Tip({
  tip,
  className,
  dir,
  label,
  children,
}: {
  /** The whole string, of which `children` is usually the truncated half. */
  tip: ReactNode;
  className?: string;
  /** A path or a URL, which reads left-to-right in a right-to-left interface. */
  dir?: 'ltr';
  /**
   * The accessible name, for the call sites whose children are a glyph and
   * nothing else. Truncated text needs none, since the whole string is in the
   * DOM, but a drawing has no text to be read. Set together with `role="img"`,
   * or a screen reader announces a name on an element it has no reason to stop
   * at.
   */
  label?: string;
  /** Absent where the element is the drawing, such as the availability dot. */
  children?: ReactNode;
}) {
  const t = useTooltip<HTMLSpanElement>(tip);
  // role/tabIndex dropped: these sit in rows and cells that carry their own
  // focus model (listKeyboard's roving tabindex), and a tab stop per truncated
  // string would put a dozen of them in every row. The full string is in the
  // DOM either way, so a screen reader reads it whole; the bubble exists for
  // the eye, which is what CSS truncation cuts.
  const { role: _role, tabIndex: _tabIndex, ...hover } = t.triggerProps;
  return (
    <>
      <span dir={dir} className={className} role={label ? 'img' : undefined} aria-label={label} {...hover}>
        {children}
      </span>
      {t.node}
    </>
  );
}

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
  // The house bubble, never the OS balloon; see Tip above. A glyph-only control
  // needs a tooltip unconditionally, since there is no other way to know what
  // it does.
  const tip = useTooltip<HTMLButtonElement>(label);
  const { role: _role, tabIndex: _tabIndex, ...hover } = tip.triggerProps;
  return (
    <>
      <button
        type="button"
        role="checkbox"
        aria-checked={checked}
        aria-label={label}
        {...(label ? hover : undefined)}
        onClick={onChange}
        className={`grid h-4.5 w-4.5 shrink-0 place-items-center rounded-[var(--radius-control)] transition-colors ${
          checked ? 'bg-accent text-accentContrast' : 'bg-carbon-surface3/60 text-transparent hover:bg-carbon-surface3'
        }`}
        style={{ height: '1.125rem', width: '1.125rem' }}
      >
        <IconCheck width={12} height={12} />
      </button>
      {label && tip.node}
    </>
  );
}

/**
 * The per-link on/off switch, a switch and not a checkbox because the checkbox
 * one cell to its left means "selected" and the two would otherwise be the same
 * mark twice in the same row.
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
  // Both halves of GlimStone's failure feedback: the toast carries the
  // sentence, and this counter makes the switch itself say that it refused.
  const [shake, setShake] = useState(0);
  const shakeRef = useShake<HTMLButtonElement>(shake);
  const label = t(on ? 'task.disable' : 'task.enable');
  const tip = useTooltip<HTMLButtonElement>(label);
  const { role: _role, tabIndex: _tabIndex, ...hover } = tip.triggerProps;

  async function flip() {
    if (busy || ids.length === 0) return;
    setBusy(true);
    try {
      await setEnabled(ids, !on, base);
    } catch (err) {
      // The server's own sentence names what refused; a generic failure here
      // would leave a switch that visibly did nothing and no way to find out why.
      toast(err instanceof Error && err.message ? err.message : t('task.switchFailed'), 'fail');
      setShake((n) => n + 1);
    } finally {
      setBusy(false);
    }
  }

  // The switch is not the accent when it is on. Every row is enabled by
  // default, so an accent-filled pill per row spends the one colour that means
  // "something is happening here" on the most ordinary fact on the page, and a
  // column of gold beside a single gold progress bar stops the bar reading as
  // the thing that matters.
  //
  // Off is not the fault colour either: a link somebody switched off is a
  // decision, not a failure, and a status colour on a control people press all
  // day is a colour they stop reading. Both halves are carbon tones, and what
  // tells them apart is the knob's side and the step between the two grounds.
  return (
    <>
      <button
        type="button"
        role="switch"
        aria-checked={on}
        aria-label={label}
        {...hover}
        ref={(el) => {
          shakeRef.current = el;
          hover.ref.current = el;
        }}
        disabled={busy}
        onClick={flip}
        className={`relative h-3.5 w-7 shrink-0 rounded-[var(--radius-pill)] transition-colors disabled:opacity-40 ${
          on ? 'bg-carbon-surface3' : 'bg-carbon-surface2'
        }`}
      >
        {/* start-0 is load-bearing: without it the knob starts from its static
            position, which the button's inherited text-align centres, and the
            knob then slides out past the track. */}
        <span
          className={`absolute start-0 top-0.5 h-2.5 w-2.5 rounded-[var(--radius-pill)] shadow-sm transition-[translate] duration-150 ${
            on ? 'translate-x-4 rtl:-translate-x-4 bg-carbon-textSub' : 'translate-x-0.5 rtl:-translate-x-0.5 bg-carbon-textMuted'
          }`}
        />
      </button>
      {tip.node}
    </>
  );
}

// What each typed failure is called on screen.
//
// Only the reasons this build knows are in here. The server's reason is an open
// string, so a newer backend can settle a task with a value this interface has
// never heard of, and an unrecognised one gets no label rather than the raw
// token: the worth of a label is that it is a word somebody can act on, and
// "reason: hoster_soft_limit" is not one.
//
// Exported so the failure chips (ErrorCauses.tsx) name a cause with the word
// the row beside them uses. A second copy is how a chip ends up saying "Hoster
// limit" over rows labelled something else.
export const reasonKey: Record<string, TranslationKey> = {
  gone: 'task.reason.gone',
  auth: 'task.reason.auth',
  limit: 'task.reason.limit',
  unavailable: 'task.reason.unavailable',
  network: 'task.reason.network',
  diskFull: 'task.reason.diskFull',
  unsupported: 'task.reason.unsupported',
  captcha: 'task.reason.captcha',
  cancelled: 'task.reason.cancelled',
  // Named by the backend that hit them rather than by the shared classifier:
  // only the process that read the whole of yt-dlp's output can tell "the site
  // thinks we are a bot" from "the site said 403".
  botCheck: 'task.reason.botCheck',
  membersOnly: 'task.reason.membersOnly',
  geoBlocked: 'task.reason.geoBlocked',
  drm: 'task.reason.drm',
  extractorBroken: 'task.reason.extractorBroken',
};

// The availability chip, one entry per verdict the server can send.
//
// '' is absent, and that absence is what the fourth state is for: a link nobody
// has checked says nothing at all, where 'uncheckable' is a link that was
// checked and whose host would not answer. Drawing them the same way left a
// Real-Debrid or JD link looking untouched forever.
//
// 'uncheckable' is the quiet shade, never the fail colour and never the accent.
// It is not activity and it is not a dead link, and painting it red is how
// somebody is talked into deleting a link that is fine.
const availChip: Record<Exclude<Availability, ''>, { key: TranslationKey; tone: string }> = {
  online: { key: 'task.online', tone: 'text-statusOk' },
  offline: { key: 'task.offline', tone: 'text-statusFail' },
  uncheckable: { key: 'task.uncheckable', tone: 'text-carbon-textMuted' },
};

// The same verdict as availChip, as a dot rather than a word, for the name cell
// that shows it beside the name itself rather than only in the Status column.
// The solid-dot tokens StatusPill's own status dot uses, not the softer
// text-status* wash availChip reads: a 6px dot needs the fully saturated colour
// to read at all, where a word has its own text weight to carry it.
const availDot: Record<Exclude<Availability, ''>, string> = {
  online: 'bg-statusOkSolid',
  offline: 'bg-statusFailSolid',
  uncheckable: 'bg-statusNeutralSolid',
};

/**
 * Which connection is carrying this download. It answers what a task is on and
 * not what it was asked for: the server writes the id when it hands the
 * download to a backend, so a task pointed at a proxy that was busy shows the
 * connection that took it, where an echo of the request would agree with the
 * settings page and disagree with the traffic.
 *
 * An unrouted task therefore shows nothing rather than "Direct". An empty id is
 * "nobody has decided yet", while the direct gateway is a decision with an id
 * of its own, and the blanks fill in as tasks start. Shared by the column cell,
 * the row tooltip and the task detail panel, so a connection resolves to the
 * same words in all three.
 */
export function useConnectionLabel(task: Task, t: Translate, base: string): { text: string; hint: string } | null {
  const rows = useConnections(base);
  const id = task.connection ?? '';
  if (!id) return null;

  const row = rows.get(id);
  const direct = t('settings.connections.kind.direct');
  // Three ways to have no endpoint to print, and they are not the same thing.
  // The gateway and a direct row both mean "out over this machine", so both
  // read as the word; an id with no row at all is a connection that was deleted
  // or a list that has not arrived yet, and it keeps the raw id, which can be
  // matched against the settings page by hand where a blank cell cannot.
  const endpoint = row ? endpointOf(row) : '';
  const text = endpoint || (id === DIRECT_ID || row ? direct : id);
  // A direct row is a rule the user wrote to keep certain hosts off every proxy,
  // and two of them in one list read identically. The hosts it claims go in the
  // hint, which is the only thing that tells them apart.
  const hint = endpoint || row?.filter?.join(', ') || text;
  return { text, hint };
}

/** One labelled fact in the row tooltip: the category on its own line and the
 *  value on the next, so a long value wraps instead of forcing a ragged right
 *  edge beside a short label. */
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
 * RowTooltipContent is everything about one row in a single hover rather than
 * several cell tooltips. Six of this table's columns ship hidden by default
 * (see DEFAULT_HIDDEN) purely for width, so this is the one place to read them
 * without opening the column menu and giving up width elsewhere for a column
 * that stays blank on most rows. Nothing here is fetched: every field arrived
 * with the task. See useTooltip in components/ui.tsx for the hover mechanics.
 */
function RowTooltipContent({ task, t, base }: { task: Task; t: Translate; base: string }) {
  const connection = useConnectionLabel(task, t, base);
  // The same signal the Peers/Seeds/Ratio columns and ResolverBadge key off:
  // the torrent resolver's own Info().ID (internal/resolver/torrent).
  const isTorrent = task.resolver === 'torrent';
  const name = task.name || task.url;
  // Only worth its own line when it says something the name above did not; a
  // task with no name already shows the URL as its name.
  const showUrl = !!task.name && task.name !== task.url;
  const host = hostOf(task);
  const added = fmtDateFull(task.createdAt);
  const finished = fmtDateFull(task.finishedAt);
  const changed = fmtDateFull(task.changedAt);
  // fmtDateFull decides whether a retry is pending, not the field: a settled
  // error arrives with Go's zero time rather than an empty nextTry, and the
  // formatter answers '' for that. Testing this string is right where testing
  // task.nextTry would not be.
  const retryAt = task.status === 'error' ? fmtDateFull(task.nextTry) : '';

  return (
    <div className="flex flex-col gap-2">
      {/* text-xs, the scale's 12px dense row: the type scale is a fixed
          four-row table (20/14/12/11), and a fifth size is a bug to fix rather
          than a row to add. */}
      <div dir="ltr" className="break-words text-xs font-semibold text-carbon-text">
        {name}
      </div>
      <div className="flex flex-col gap-1.5 border-t border-carbon-border/60 pt-2">
        {showUrl && (
          <TooltipField label={t('task.tooltip.url')} ltr>
            {task.url}
          </TooltipField>
        )}
        {host && (
          <TooltipField label={t('columns.host')} ltr>
            {host}
          </TooltipField>
        )}
        <TooltipField label={t('columns.resolver')}>
          <ResolverBadge resolver={task.resolver} mode={task.mode} />
        </TooltipField>
        {/* Peers, seeds and ratio have their own columns, hidden by default
            like the other low-traffic ones, so they are here for the same
            reason connection, added, finished, comment and source are.
            Uploaded and "still seeding" go no further than this bubble,
            which carries the full peer and seed detail the three columns
            leave out. */}
        {isTorrent && (
          <TooltipField label={t('task.tooltip.swarm')}>
            {t('task.tooltip.swarmDetail', {
              peers: task.peers ?? 0,
              seeds: task.seeds ?? 0,
              ratio: fmtRatio(task.ratio),
            })}
            {task.seeding ? ` · ${t('task.tooltip.seeding')}` : ''}
            {task.uploaded ? ` · ${t('task.tooltip.uploaded')} ${fmtBytes(task.uploaded)}` : ''}
          </TooltipField>
        )}
        {task.infoHash && (
          <TooltipField label={t('task.tooltip.infoHash')} ltr>
            {task.infoHash}
          </TooltipField>
        )}
        {task.trackers && task.trackers.length > 0 && (
          <TooltipField label={t('task.tooltip.trackers')} ltr>
            {task.trackers.join(', ')}
          </TooltipField>
        )}
        {connection && (
          <TooltipField label={t('columns.connection')} ltr>
            {connection.text}
          </TooltipField>
        )}
        {added && (
          <TooltipField label={t('columns.added')}>
            {added}
          </TooltipField>
        )}
        {finished && (
          <TooltipField label={t('columns.finished')}>
            {finished}
          </TooltipField>
        )}
        {retryAt && (
          <TooltipField label={t('task.retryPending')}>
            <RetryNote task={task} form="full" />
            {retryAt}
          </TooltipField>
        )}
        {changed && (
          <TooltipField label={t('task.tooltip.changed')}>
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

/**
 * The priority ladder, once per session, shared by every row on screen. Forty
 * rows must not be forty requests for one short list, and priorityChoices() is
 * memoised for this.
 */
export function usePriorityNames(): Map<number, string> {
  const [names, setNames] = useState<Map<number, string>>(new Map());
  useEffect(() => {
    let live = true;
    void priorityChoices().then(
      (choices) => {
        if (live) setNames(new Map(choices.map((c) => [c.value, c.id])));
      },
      () => {
        /* no ladder, no name: the badge falls back to the raw number */
      },
    );
    return () => {
      live = false;
    };
  }, []);
  return names;
}

/**
 * sharedPriority is the priority a whole package carries, or 0 when its links
 * do not agree. Same shape as the Enabled column's aggregate: a package says
 * something about itself only when every row in it says the same thing.
 */
export function sharedPriority(items: Task[]): number {
  if (items.length === 0) return 0;
  const first = items[0].priority;
  return items.every((x) => x.priority === first) ? first : 0;
}

/**
 * PriorityTag marks a link whose priority somebody changed. Nothing is drawn at
 * the default, because a tag on every row is furniture and the point is that
 * this row is not like the others. A column would have been hidden by default
 * like the other low-traffic ones, leaving the feature invisible.
 */
export function PriorityTag({ value, names, t }: { value: number; names: Map<number, string>; t: Translate }) {
  if (!value) return null;
  const id = names.get(value);
  const label = id ? t(`priority.${id}` as TranslationKey) : String(value);
  // A value outside the enum still gets a mark rather than vanishing: the queue
  // orders any integer (see clampPriority), so a row set by a rule or by a
  // newer client must not look like an ordinary one.
  return (
    // The glyph alone, with the name as the tooltip and the accessible name,
    // and the same drawing the right-click menu uses, so one control does not
    // name a rung with wedges while the row beside it uses a different mark. Up
    // is the accent and down is muted: a raised link is the one somebody wants
    // to spot in a long list.
    //
    // --accent-ink, not --accent: a colour on a `background` is --accent, a
    // colour on `color`, `fill` or `stroke` against the page is --accent-ink.
    // This glyph is drawn in the text colour on the card's own ground, where
    // the flat accent is Sunflower on white in the light theme.
    <Tip
      tip={label}
      label={label}
      className={`inline-flex shrink-0 leading-none ${value > 0 ? 'text-accentInk' : 'text-carbon-textMuted'}`}
    >
      <PriorityGlyph steps={value} />
    </Tip>
  );
}

/**
 * RowMarks shows what the right-click menu leaves on a row: the stop mark,
 * Start now, Hold and Switch off, each as the glyph of its menu entry with the
 * state's name in the bubble. On a package row `items` is its links, and it
 * wears a mark when every link carries it, as with PriorityTag. The stop mark
 * is the exception: one link carries it, and a folded package would hide
 * where the queue is going to stop.
 */
export function RowMarks({ items, ctx }: { items: Task[]; ctx: CellContext }) {
  const { t, stopMark, switchShown } = ctx;
  // The two that change what the queue does next take --accent-ink, as a
  // raised priority does; the two that park a row stay in the quiet ink.
  const marks: { id: string; label: string; icon: ReactNode; ink: string }[] = [];
  // The server clears the mark as its download finishes, which on a peer's
  // list only the row itself reports.
  if (stopMark && items.some((x) => x.id === stopMark && x.status !== 'done'))
    marks.push({ id: 'stop', label: t('queue.stopMarkOn'), icon: <IconStopMark />, ink: 'text-accentInk' });
  if (items.every((x) => !!x.forced))
    marks.push({ id: 'forced', label: t('task.forced'), icon: <IconBolt />, ink: 'text-accentInk' });
  if (items.every((x) => !!x.hold))
    marks.push({ id: 'held', label: t('task.held'), icon: <IconPin />, ink: 'text-carbon-textSub' });
  if (!switchShown && items.every((x) => !x.enabled))
    marks.push({ id: 'off', label: t('task.waiting.disabled'), icon: <IconPower />, ink: 'text-carbon-textSub' });
  return marks.map((m) => (
    <Tip
      key={m.id}
      tip={m.label}
      label={m.label}
      className={`inline-flex shrink-0 leading-none [&_svg]:h-3.5 [&_svg]:w-3.5 ${m.ink}`}
    >
      {m.icon}
    </Tip>
  ));
}

function NameCell({ task, ctx }: { task: Task; ctx: CellContext }) {
  const { t, base } = ctx;
  // A pending automatic retry is not the same as a dead task, and saying so
  // stops people restarting something that is already about to restart.
  //
  // The shared predicate rather than a reading of its own: NextTry is a
  // time.Time, so a task waiting for nothing arrives carrying
  // "0001-01-01T00:00:00Z" and a `!!task.nextTry` is true. Every reader of a
  // failed row agrees by calling retryStateOf, and a corrected copy here would
  // still claim a pending retry on a row that gave up.
  const retrying = retryPending(task);
  const reason = task.reason ? reasonKey[task.reason] : undefined;
  const advice = adviceFor(task.reason);
  const [whyOpen, setWhyOpen] = useState(false);
  // The row's rich tooltip lives on this cell: it is the one that truncates
  // first (see TREE_INDENT) and the one hover that can afford to say more than
  // the string on screen. No native `title` beside it, or the browser's own
  // delayed tooltip would stack on top of this one over the same box.
  const tip = useTooltip<HTMLDivElement>(<RowTooltipContent task={task} t={t} base={base} />);
  // The "open the advice" button's own bubble. Its visible text is the failure
  // reason, so the tooltip says what pressing it does, which is the one case a
  // labelled control still earns one.
  const openTip = useTooltip<HTMLButtonElement>(t('failure.open'));
  const { role: _openRole, tabIndex: _openTabIndex, ...openHover } = openTip.triggerProps;
  const priorityNames = usePriorityNames();
  return (
    <div className="min-w-0">
      <div className="flex min-w-0 items-center gap-1.5">
        <RowMarks items={[task]} ctx={ctx} />
        <PriorityTag value={task.priority} names={priorityNames} t={t} />
        {/* text-sm is the scale's body row; a half-pixel value is not a step
            the four-row table has. */}
        <div dir="ltr" {...tip.triggerProps} className="min-w-0 truncate text-start text-sm text-carbon-text">
          {/* task.ext is a display-only hint (see core.Task.Ext), never
              appended to task.name itself: Name stays the resolved-versus-
              placeholder sentinel the backend's rename and probe guards key
              on. Only shown once a real name has resolved, so a bare URL
              placeholder gets no extension tacked onto it. */}
          {task.name && task.name !== task.url && task.ext ? `${task.name}.${task.ext}` : task.name || task.url}
        </div>
      </div>
      {tip.node}
      {whyOpen && reason && (
        <FailureAdvice task={task} base={base} reasonLabel={reason} onClose={() => setWhyOpen(false)} />
      )}
      {task.error && (
        <div className="mt-0.5 flex items-center gap-1.5 text-[11px]">
          {/* The typed cause leads the line as a tag rather than a second
              sentence: a column reading "disk full" four times is one fact
              about this box, where four hoster sentences that each mean it are
              four things to read. Quiet eyebrow type and never the accent,
              since a settled failure is not activity, and no box of its own,
              because the shade step is the separation.

              Capped at 45% rather than left to size itself: a tag free to run
              the width of the cell would squeeze the sentence to nothing in a
              narrow column. The sentence keeps the larger half, and the tag
              truncates with its own tooltip. */}
          {reason &&
            (advice ? (
              // No stopPropagation: TaskList's control selector begins with
              // `button`, so the row's click-to-select, its double-click and
              // its dragstart all skip this.
              <>
                <button
                  type="button"
                  {...openHover}
                  className="glim-eyebrow max-w-[45%] shrink-0 truncate rounded-[var(--radius-pill)] bg-carbon-surface2 px-1.5 py-0.5
                    transition-colors hover:bg-carbon-surface3 hover:text-carbon-textSub"
                  onClick={() => setWhyOpen(true)}
                >
                  {t(reason)}
                </button>
                {openTip.node}
              </>
            ) : (
              <Tip tip={t(reason)} className="glim-eyebrow max-w-[45%] shrink-0 truncate">
                {t(reason)}
              </Tip>
            ))}
          {/* `min-w-0` and no `flex-1`: it sizes to its text and is the only
              thing on the line that may shrink, so it takes the whole shortfall
              and the glyph stays beside the sentence it belongs to instead of
              being pushed to the far edge of a wide column. Prose that cannot
              shrink beside text that can is a race the text loses, and a row
              that then says nothing about why it failed is the one row on the
              page somebody has to act on. The tool's own line stays in the
              bubble: the plain-language sentence translates the failure, it
              does not replace the evidence. */}
          <Tip tip={task.error} className="min-w-0 truncate text-statusFail">
            {advice ? t(advice.line) : task.error}
          </Tip>
          {/* The retry note is a glyph: fixed width, never competing, and still
              carrying the whole sentence for the pointer and the screen reader.
              Not the accent, because a retry that has not happened yet is
              waiting rather than activity. */}
          {retrying && (
            <Tip tip={t('task.retryPending')} label={t('task.retryPending')} className="shrink-0 text-carbon-textMuted">
              <IconRetry width={11} height={11} />
            </Tip>
          )}
        </div>
      )}
    </div>
  );
}

function ProgressCell({
  loaded,
  size,
  done,
  active,
  // Whether bytes are actually moving right now. A row waiting in a stopped
  // queue has no size either, and looping a bar over it said "working" about a
  // queue that was switched off.
  live,
}: {
  loaded: number;
  size: number;
  done: boolean;
  active: boolean;
  live: boolean;
}) {
  const p = pct(loaded, size, done);
  return (
    <div className="flex items-center gap-2">
      <div className="min-w-0 flex-1">
        {/* `live` answers both of the bar's "is anything happening" questions,
            because it is the same question: a bar with no size behind it loops,
            a bar with one breathes at its front edge, and a row in a stopped
            queue does neither. Passing it to only one of them is what left
            every finished and paused row pulsing. */}
        <ProgressBar
          percent={p}
          active={active}
          indeterminate={live && !done && size <= 0}
          moving={live}
          tone={done ? 'ok' : 'accent'}
        />
      </div>
      <span className="glim-num w-9 shrink-0 text-end text-[11px] text-carbon-textMuted">{fmtPct(p)}</span>
    </div>
  );
}

// AvailDot is StatusCell's own dot, pulled out so the status column's package
// row can paint the same shape over a whole package's aggregate verdict.
function AvailDot({ avail, title, mixed }: { avail: Availability | undefined; title?: string; mixed?: boolean }) {
  return (
    // A dot with no text of its own needs the bubble and the name
    // unconditionally; see Tip.
    <Tip
      tip={title}
      label={title}
      className={`inline-block h-2 w-2 shrink-0 rounded-[var(--radius-pill)] ${
        // Mixed is the package's third answer and outranks the verdict below. A
        // folder with one dead link among nine good ones is neither green nor
        // red, and painting it either way hides the row somebody has to act on.
        mixed ? 'bg-statusWarnSolid' : avail ? availDot[avail] : 'bg-carbon-textMuted/40'
      }`}
    />
  );
}

// packageAvailStatus folds every item's verdict into the one worth showing on
// their shared package row: any dead link outranks everything else, the same
// way packageStatus's error check does, all-online is the other unambiguous
// case, uncheckable is worth a glance even mixed with plain unchecked, and
// undefined is the neutral dot a package nobody has checked yet shows.
function packageAvailStatus(items: Task[]): Availability | undefined {
  if (items.some((x) => x.online === 'offline')) return 'offline';
  if (items.length > 0 && items.every((x) => x.online === 'online')) return 'online';
  if (items.some((x) => x.online === 'uncheckable')) return 'uncheckable';
  return undefined;
}

/** True when a package holds both a working link and a dead one. */
function packageAvailMixed(items: Task[]): boolean {
  return items.some((x) => x.online === 'online') && items.some((x) => x.online === 'offline');
}

/**
 * AvailCell is the collector's own column: is this link there or not. The dot
 * carries the availability verdict, and the one waiting reason a staged row can
 * have follows it in words. Every staged row carries the same task.status, so a
 * state word there would read "collected" on all of them.
 */
function AvailCell({ task, t }: { task: Task; t: Translate }) {
  // The typed cause carries the detail, as a tooltip rather than a second word
  // on the line. "Host would not say" is the verdict and it is what the column
  // is for; whether the host was rate-limiting us or simply down is the next
  // question, and it belongs one hover away, not in the width of the cell.
  const why = task.reason ? reasonKey[task.reason] : undefined;
  const avail = task.online;
  const dot = <AvailDot avail={avail} title={why ? t(why) : avail ? t(availChip[avail].key) : undefined} />;
  if (task.waiting !== 'premium') return dot;
  // The one waiting reason a staged row carries: what starting it would run
  // into, which is news here and not after the click.
  return (
    <span className={STATUS_LINE}>
      {dot}
      <Tip tip={t('task.waiting.premium')} className="min-w-0 truncate text-[11px] text-carbon-textMuted">
        {t('task.waiting.premium')}
      </Tip>
    </span>
  );
}

/**
 * waitingKey names each reason a queued task is not running. Typed as
 * TranslationKey for the same reason reasonKey is: a lookup through it stays
 * something t() accepts, so a renamed key is a compile error rather than a row
 * printing its own key. An unknown value falls back at the call site, because a
 * newer server may send a reason this build has never heard of.
 */
const waitingKey: Partial<Record<NonNullable<Task['waiting']>, TranslationKey>> = {
  slot: 'task.waiting.slot',
  host: 'task.waiting.host',
  forced: 'task.waiting.forced',
  disabled: 'task.waiting.disabled',
  hold: 'task.waiting.hold',
  captcha: 'task.waiting.captcha',
  account: 'task.waiting.account',
  halted: 'task.waiting.halted',
  // A reason with no entry here reads as "all slots busy", which sends somebody
  // to raise the concurrency limit against a queue that is not short of slots.
  disk: 'task.waiting.disk',
  volumeCap: 'task.waiting.volumeCap',
  module: 'task.waiting.module',
  premium: 'task.waiting.premium',
};

// A full slot count and a stopped queue hold every waiting row alike, and the
// toolbar already says so; written on each row they tell one row from another
// nothing.
const queueWideWaiting = new Set<NonNullable<Task['waiting']>>(['slot', 'halted']);

// max-w-full is what makes the truncate inside truncate. Without it this
// inline-flex takes its content's width and the cell does the clipping instead,
// which cuts mid-word with no ellipsis and no way to read the rest.
const STATUS_LINE = 'inline-flex min-w-0 max-w-full items-center gap-2';

function StatusCell({ task, t, unpack }: { task: Task; t: Translate; unpack: Unpacking | null }) {
  // A staged row can still turn up in the download list's history views, where
  // the availability dot is the only honest reading: the transfer has not begun
  // and the row has no state of its own yet.
  if (task.status === 'collected') return <AvailCell task={task} t={t} />;
  return (
    <span className={STATUS_LINE}>
      {unpack ? <UnpackStatus unpack={unpack} t={t} /> : <StatusPill status={task.status} />}
      {/* What the backend is doing, when "running" is not the whole truth: JD
          can report "Captcha recognition (rapidgator.net)" on a package while
          this column says running with no bytes moving. Beside the status
          rather than in the metadata line, because it answers "why is nothing
          moving", which is the question somebody asks while looking at the
          status. Muted and truncating, since it is a sentence from somebody
          else's program and may be long. */}
      {task.note && (
        <Tip tip={task.note} className="min-w-0 truncate text-[11px] text-carbon-textMuted">
          {task.note}
        </Tip>
      )}
      {/* Why a queued row is not running: the slot count is full, this host is
          at its ceiling, the account behind the only backend that claims the
          link is benched. Without it ten queued rows all say "waiting" and
          telling four different situations apart means reasoning about the
          settings page.

          Only when there is no note: a backend's sentence about what it is
          doing right now is more specific than our reason for not having
          started it, and two greys on one line stop the cell being readable. */}
      {!task.note && task.waiting && !queueWideWaiting.has(task.waiting) && (
        // A bubble for the same reason task.note has one: this column is narrow
        // by default and several of these reasons are longer in German than the
        // space they get, so the ellipsis needs somewhere to lead.
        <Tip
          tip={t(waitingKey[task.waiting] ?? 'task.waiting.slot')}
          className="min-w-0 truncate text-[11px] text-carbon-textMuted"
        >
          {t(waitingKey[task.waiting] ?? 'task.waiting.slot')}
        </Tip>
      )}
      {/* What the row is waiting for after a failure, in the same slot and the
          same grey as the note above it. A queued row's waiting reason and a
          failed row's countdown can never both be on screen, so they do not
          compete for the line. Compact here because this column is 148px by
          default and 90px at its floor; the whole sentence is one hover away in
          the row tooltip. */}
      {!task.note && <RetryNote task={task} form="compact" />}
      {/* Only a real verdict is shown. An unverified download stays unmarked,
          because a tick that also means "not checked" is worse than none. */}
      {task.checksum === 'ok' && (
        <Tip tip={t('task.checksumOk')} label={t('task.checksumOk')} className="shrink-0 text-statusOk">
          <IconCheck width={13} height={13} />
        </Tip>
      )}
      {task.checksum === 'failed' && (
        <Tip
          tip={t('task.checksumFail')}
          label={t('task.checksumFail')}
          className="shrink-0 text-[11px] font-semibold text-statusFail"
        >
          !
        </Tip>
      )}
    </span>
  );
}

/** An unpacking as one row reads it. */
interface Unpacking {
  state: UnpackState;
  /** The job, while the server still holds it. After a restart the row has only the state. */
  job?: ExtractJob;
  /** Why it failed, or why moving the files afterwards did. */
  error?: string;
}

// app.extractErrorPrefix, which marks the unpacking's own error on a task.
const EXTRACT_ERROR = 'extract: ';

// A file downloading again has left its archive's last unpacking behind, so
// only a finished row takes it up. Without a job it reads how its last
// unpacking ended, which the server keeps on every part (core.Task.Unpack).
function unpackingOf(task: Task, ctx: CellContext): Unpacking | null {
  if (task.status !== 'done' && task.status !== 'extracting') return null;
  const job = ctx.extractions?.get(task.id);
  const state = job && unpackState(job);
  if (job && state) return { state, job, error: job.error };
  if (!task.unpack) return null;
  const error = task.error?.startsWith(EXTRACT_ERROR) ? task.error.slice(EXTRACT_ERROR.length) : undefined;
  return { state: task.unpack, error };
}

// Go wraps an error with ": " at every level it climbs, so the innermost cause
// is the last piece: "Film.part1.rar: rardecode: bad block header" in short is
// "bad block header".
const shortCause = (error: string): string => error.slice(error.lastIndexOf(': ') + 1).trim();

/**
 * UnpackStatus stands in a finished row's status slot while its archive is
 * being unpacked and afterwards. A failure carries its innermost cause beside
 * the word, and the bubble holds the rest: the archive open now, what has come
 * out of it, how many parts the set has and the whole error. The package
 * header adds how many of its archives are unpacked.
 */
function UnpackStatus({
  unpack,
  t,
  tally,
}: {
  unpack: Unpacking;
  t: Translate;
  tally?: { done: number; total: number };
}) {
  const { job, state } = unpack;
  const percent = state === 'running' && job?.size ? pct(job.unpacked ?? 0, job.size, false) : undefined;
  // "Needs a password" says what the library's sentence says, and says it in
  // the reader's language.
  const error = state === 'password' ? '' : (unpack.error ?? '');
  const cause = error && shortCause(error);
  const facts = job
    ? [
        job.files > 0 ? `${job.files} ${t(job.files === 1 ? 'task.file' : 'task.files')}` : '',
        job.files > 0 ? fmtBytes(job.bytes) : '',
        job.volumes > 1 ? t('archive.volumes', { volumes: job.volumes }) : '',
      ].filter(Boolean)
    : [];
  // The word first, since a narrow column shows only the start of it.
  const detail = (
    <span className="flex flex-col gap-0.5">
      <span className="font-medium">{unpackLabel(state, percent, t)}</span>
      {job && <span dir="ltr">{job.name}</span>}
      {job?.archive && job.archive !== job.name && <span dir="ltr">{job.archive}</span>}
      {facts.length > 0 && <span className="glim-num">{facts.join(' · ')}</span>}
      {tally && <span>{t('archive.tally', { done: tally.done, total: tally.total })}</span>}
      {error && <span>{error}</span>}
    </span>
  );
  return (
    <Tip tip={detail} className={STATUS_LINE}>
      <UnpackPill state={state} percent={percent} />
      {tally && (
        <span className="glim-num shrink-0 text-[11px] text-carbon-textMuted">{`${tally.done}/${tally.total}`}</span>
      )}
      {/* flex-1 from a zero basis: the cause takes what the word leaves and
          never squeezes the word itself. */}
      {cause && <span className="min-w-0 flex-1 truncate text-[11px] text-carbon-textMuted">{cause}</span>}
    </Tip>
  );
}

/** The host column: its logo, then its name. Blank rows draw neither. */
function HostCell({ host }: { host: string }) {
  if (!host) return null;
  return (
    <span className="flex min-w-0 items-center gap-1.5">
      <HosterIcon host={host} size={16} />
      <span className="truncate">{host}</span>
    </span>
  );
}

// The column's read of useConnectionLabel: text truncated to the cell, hint in
// the house bubble, since this box is not sitting under the row's own tooltip
// the way the name column is.
function ConnectionCell({ task, t, base }: { task: Task; t: Translate; base: string }) {
  const label = useConnectionLabel(task, t, base);
  if (!label) return null;
  return (
    <Tip dir="ltr" tip={label.hint} className="block truncate text-[11px] text-carbon-textMuted">
      {label.text}
    </Tip>
  );
}

const label = (t: Task): string => t.name || t.url;

// hostOf lives in lib/searchQuery.ts, cache and all, and is re-exported here
// because this is the module the collector's facets and stats import it from.
// The column and the `host:` search term are one question, and two answers to
// it is how a row is filed under one host and found under another.
export { hostOf } from '../lib/searchQuery';

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
 * packageStatus is the one word a package header can truthfully show.
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

// The order in which a package's archives speak for it: a failure first, for
// the reason packageStatus puts one first, then the archive being worked on,
// then the ones waiting, and "Unpacked" only once nothing else is left.
const UNPACK_RANK: UnpackState[] = ['error', 'password', 'running', 'queued', 'done'];

/**
 * packageUnpacking is the package header's summary of its archives, and null
 * while a download in it is still under way or has failed, which is the more
 * pressing thing to say. `done` and `total` count archives, not files: a set of
 * twenty parts is one archive.
 */
export function packageUnpacking(
  items: Task[],
  ctx: CellContext,
): (Unpacking & { done: number; total: number }) | null {
  const status = packageStatus(items);
  if (status !== 'done' && status !== 'extracting') return null;
  const byArchive = new Map<string, Unpacking>();
  for (const x of items) {
    const u = unpackingOf(x, ctx);
    // Without a job a set is counted once, by its first part.
    if (u?.job) byArchive.set(`job ${u.job.id}`, u);
    else if (u && (x.archivePart ?? 0) <= 1) byArchive.set(`task ${x.id}`, u);
  }
  const all = [...byArchive.values()];
  if (all.length === 0) return null;
  const lead = all.reduce((a, b) => (UNPACK_RANK.indexOf(b.state) < UNPACK_RANK.indexOf(a.state) ? b : a));
  return { ...lead, done: all.filter((u) => u.state === 'done').length, total: all.length };
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

/** Same signal as RowTooltipContent's isTorrent: internal/resolver/torrent's Info().ID. */
const isTorrentTask = (t: Task): boolean => t.resolver === 'torrent';

/**
 * fmtRatio prints uploaded over downloaded to two places, never scientific
 * notation and never blank for a real zero: a fresh torrent that has not
 * uploaded a byte is "0.00", the same way core.TorrentStats treats peers and
 * seeds.
 */
function fmtRatio(r: number | undefined): string {
  return (r ?? 0).toFixed(2);
}

// core.Task.Variant, decoded the way variantEncode and variantDecode
// (app_ytdlp_variants.go) encode it: "<kind>" or "<kind>:<sub>". kind is one of
// the five rows expandYtdlpVariants creates for a yt-dlp-routed link, fixed
// when the row was created and never edited here. sub is the pick on a video
// or an audio row, the only two kinds this column's pickers edit (see
// VariantPicker.tsx for what it holds).
//
// setTaskName (app_tasks.go) propagates one resolved title to every
// URL-sharing sibling, so all five of a link's rows show the same Name. This is
// the one column where they read differently from each other.
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
 * One shared fetch backs every row's pickers rather than one per row: the
 * menus are the same handful of ids for the whole table, and forty rows calling
 * fetchOptions() on mount would be forty identical requests. Module-scoped
 * rather than threaded through CellContext, so this column stays
 * self-contained instead of widening what every other cell's context carries
 * for menus only this column reads. They are the full menus, for a row no
 * probe has answered for yet.
 */
let ytdlpMenus: Promise<PresetMenus> | null = null;
function loadYtdlpMenus() {
  if (!ytdlpMenus) {
    ytdlpMenus = fetchOptions().then(presetMenusOf, () => NO_PRESET_MENUS);
  }
  return ytdlpMenus;
}

function useYtdlpMenus() {
  const [menus, setMenus] = useState<PresetMenus>(NO_PRESET_MENUS);
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
  // One counter each, never one shared between the two pickers: a shared nonce
  // would shake the picker nobody touched.
  const [shakeFirst, setShakeFirst] = useState(0);
  const [shakeSecond, setShakeSecond] = useState(0);
  const menus = useYtdlpMenus();

  const kind = variantKindOf(task);
  if (!kind) return null;
  const sub = variantSubOf(task);
  const label = ctx.t(VARIANT_KIND_LABEL_KEY[kind] ?? VARIANT_KIND_LABEL_KEY.video);
  // Past the collector the pick is settled, so the download list says what is
  // being fetched instead of offering to change it.
  if (ctx.profile === 'downloads') return <VariantSummary task={task} kind={kind} sub={sub} label={label} />;

  async function change(opts: { variantQuality: string; audioBitrate?: string }, first: boolean) {
    setBusy(true);
    try {
      await setTaskOptions([task.id], opts, ctx.base);
    } catch (err) {
      // Both halves of GlimStone's failure feedback: the sentence goes to the
      // toast, and the control that was pressed shakes so the refusal is
      // visible where the eye already is.
      toast(err instanceof Error && err.message ? err.message : ctx.t('task.switchFailed'), 'fail');
      (first ? setShakeFirst : setShakeSecond)((n) => n + 1);
    } finally {
      setBusy(false);
    }
  }

  // A probe fills the menus with what this source offers. Until one answers,
  // the full menus stand in, the way every other "empty means no opinion"
  // field here does, and a format picked from them is a wish the probe
  // resolves later.
  let pair: [PickerProps, PickerProps | null] | null = null;
  if (kind === 'video') {
    const probed = probedFormats(task.availableVideoFormats);
    const p = videoPickers({
      pick: sub,
      formats: probed ?? menus.videoFormats,
      tracks: probed ? (task.availableVideoTracks ?? []) : null,
      caps: task.availableQualities?.length ? task.availableQualities : menus.qualities,
      t: ctx.t,
      onPick: (pick, from) => void change({ variantQuality: pick }, from === 'format'),
    });
    pair = [p.format, p.quality];
  } else if (kind === 'audio') {
    const probed = probedFormats(task.availableAudioFormats);
    const p = audioPickers({
      pick: sub,
      bitrate: task.audioBitrate ?? '',
      formats: probed ?? menus.audioFormats,
      // Before a probe the full list stands in, and nothing is a conversion yet.
      convertTo: probed ? menus.audioFormats : [],
      tracks: probed ? (task.availableAudioTracks ?? []) : null,
      conversions: task.availableAudioBitrates?.length ? task.availableAudioBitrates : menus.audioBitrates,
      t: ctx.t,
      onPick: (pick, bitrate, from) => void change({ variantQuality: pick, audioBitrate: bitrate }, from === 'format'),
    });
    pair = [p.format, p.bitrate];
  }

  return (
    // Wrapping rather than shrinking: somebody who narrows this column past
    // what two pickers need gets them on two lines, which is readable, instead
    // of two boxes with one letter in each, which is not.
    <span className="flex min-w-0 flex-wrap items-center gap-1.5 text-[11px] text-carbon-textMuted">
      <span className="shrink-0">{label}</span>
      {pair && <VariantDropdown picker={pair[0]} busy={busy} shake={shakeFirst} />}
      {pair?.[1] && <VariantDropdown picker={pair[1]} busy={busy} shake={shakeSecond} />}
    </span>
  );
}

/**
 * VariantSummary is the download list's reading of a variant row: which of
 * the link's rows it is and what it is fetched as, "Video 1080p60 WebM (VP9)"
 * or "Audio Opus 160 kbit/s". The rows of a link start out under one name, so
 * without it the audio row is one more line with the video's title.
 */
function VariantSummary({ task, kind, sub, label }: { task: Task; kind: string; sub: string; label: string }) {
  const { t } = useT();
  let picked = '';
  if (kind === 'video') picked = videoSummary(sub, t);
  if (kind === 'audio') picked = audioSummary(sub, task.audioBitrate ?? '', t);
  return (
    <Tip tip={picked ? `${label} ${picked}` : label} className="block min-w-0 truncate text-[11px] text-carbon-textMuted">
      {label}
      {picked && <span className="text-carbon-textSub"> {picked}</span>}
    </Tip>
  );
}

export const COLUMNS: ColumnDef[] = [
  {
    id: 'enabled',
    labelKey: 'columns.enabled',
    // Collector only; see ColumnDef.onlyIn for why this is not default-hidden.
    onlyIn: ['collector'],
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
    // The name takes whatever the other columns leave (see gridTemplate), so
    // until somebody drags it the only width it has of its own is the floor.
    width: TREE_INDENT + NAME_TEXT_FLOOR,
    minWidth: TREE_INDENT + NAME_TEXT_FLOOR,
    align: 'start',
    hideable: false,
    compare: (a, b) => cmpText(label(a), label(b)),
    render: (task, ctx) => <NameCell task={task} ctx={ctx} />,
    // No aggregate: in a package row this cell is the tree control, which is
    // list state the registry has no access to.
  },
  {
    id: 'size',
    labelKey: 'columns.size',
    // Size, progress, speed, time left, status and host are as wide as what
    // their cells show, and the rest goes to the name. The measurements are in
    // check-column-widths.mjs.
    width: 80,
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
    width: 128,
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
        live={task.status === 'running' || task.status === 'extracting'}
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
          live={items.some((x) => x.status === 'running' || x.status === 'extracting')}
        />
      );
    },
  },
  {
    id: 'speed',
    labelKey: 'columns.speed',
    width: 88,
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
    width: 84,
    minWidth: 76,
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
    // Two lists, two honest meanings for one stored column; see CellContext's
    // own `profile`.
    labelByProfile: { collector: 'columns.availability' },
    width: 136,
    minWidth: 90,
    align: 'center',
    hideable: true,
    compare: (a, b) => STATUS_RANK[a.status] - STATUS_RANK[b.status],
    render: (task, ctx) =>
      ctx.profile === 'collector' ? (
        <AvailCell task={task} t={ctx.t} />
      ) : (
        <StatusCell task={task} t={ctx.t} unpack={unpackingOf(task, ctx)} />
      ),
    // A package that shows nothing in the status column looks like a spacer. It
    // gets the same pill as a link, over the whole package, or in the collector
    // the same availability dot its rows show, with mixed as its own colour.
    // Once its downloads are in, its archives speak for it.
    aggregate: (items, ctx) => {
      if (ctx.profile === 'collector' || packageStatus(items) === 'collected') {
        return <AvailDot avail={packageAvailStatus(items)} mixed={packageAvailMixed(items)} />;
      }
      const unpack = packageUnpacking(items, ctx);
      if (!unpack) return <StatusPill status={packageStatus(items)} />;
      return (
        <UnpackStatus
          unpack={unpack}
          t={ctx.t}
          tally={unpack.total > 1 ? { done: unpack.done, total: unpack.total } : undefined}
        />
      );
    },
  },
  {
    id: 'host',
    labelKey: 'columns.host',
    // The logo in front of the name is 16px plus its gap, which the width has
    // to pay before the host's name gets any.
    width: 120,
    minWidth: 96,
    align: 'start',
    ltr: true,
    hideable: true,
    compare: (a, b) => cmpText(hostOf(a), hostOf(b)),
    // The host's logo beside its name, the same lazy, self-cached icon the
    // account picker draws: the instance fetches it once and serves it from
    // disk, so five hundred rows over twenty hosts are twenty requests.
    render: (task) => <HostCell host={hostOf(task)} />,
    // Only when the whole package came from one host. "3 hosts" in a column of
    // host names is a different kind of value in the same column.
    aggregate: (items) => {
      const one = new Set(items.map(hostOf));
      return one.size === 1 ? <HostCell host={hostOf(items[0])} /> : null;
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
    // one, so ascending puts "not finished" first, where it belongs.
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
    render: (task) => <ResolverBadge resolver={task.resolver} mode={task.mode} />,
    aggregate: (items) => {
      const one = new Set(items.map((x) => x.resolver));
      return one.size === 1 ? <ResolverBadge resolver={items[0].resolver} mode={items[0].mode} /> : null;
    },
  },
  {
    id: 'variant',
    labelKey: 'columns.variant',
    // Sized for the row every yt-dlp package has, the video row with its
    // format and quality pickers. 231 carries it on one line in English and
    // German, the widest pick included; in a language with a longer word for
    // Auto (287 in Lithuanian) the quality picker wraps, which it can since the
    // pickers are shrink-0 and the cell flex-wrap. Measurements are in
    // check-column-widths.mjs.
    //
    // minWidth is about the widest single control, because a picker cannot
    // shrink and the cell clips rather than squeezes it: the widest measured is
    // the Persian bitrate picker at 320 kbit/s, 144px with the padding.
    width: 231,
    minWidth: 144,
    // The download list shows one line of text instead of the pickers, and it
    // truncates into its tooltip, so neither the pickers' width nor their floor
    // applies there. The column is blank on every row that is not a yt-dlp link.
    widthByProfile: { downloads: 104 },
    minWidthByProfile: { downloads: 96 },
    align: 'start',
    hideable: true,
    compare: (a, b) => cmpText(variantKindOf(a), variantKindOf(b)),
    render: (task, ctx) => <VarianteCell task={task} ctx={ctx} />,
    // No aggregate: a package almost always mixes kinds, its video, audio and
    // thumbnail rows all sharing one package, so there is no single variant a
    // package header could show for all of them.
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
  // Peers, seeds and ratio are blank on every non-torrent row rather than "0":
  // zero peers is a true reading for a torrent and meaningless noise for an
  // HTTP download, the same distinction core.TorrentStats draws. Hidden by
  // default below, like six other low-traffic columns, and readable regardless
  // through the row tooltip.
  {
    id: 'peers',
    labelKey: 'columns.peers',
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
    labelKey: 'columns.seeds',
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
    labelKey: 'columns.ratio',
    width: 84,
    minWidth: 60,
    align: 'end',
    numeric: true,
    hideable: true,
    compare: (a, b) => (a.ratio ?? 0) - (b.ratio ?? 0),
    render: (task) => (isTorrentTask(task) ? fmtRatio(task.ratio) : ''),
    // No aggregate, matching added, finished, comment and source above: a
    // package's ratio is neither the sum nor the mean of its members' ratios in
    // any sense somebody reading the header would recognise.
  },
];

export const COLUMN_BY_ID = new Map<ColumnId, ColumnDef>(COLUMNS.map((c) => [c.id, c]));

/** The order a list has before anybody drags anything. */
export const DEFAULT_ORDER: ColumnId[] = COLUMNS.map((c) => c.id);

/**
 * Where the progress column sits, which is not the same answer in both lists.
 * In the download list it is last, so the bar has the trailing edge of the row
 * to itself and every fixed-width value column lines up before it. In the
 * collector nothing has started, so the column ships hidden and its position
 * never comes up.
 */
function defaultOrderFor(profile: ListProfile): ColumnId[] {
  const own = DEFAULT_ORDER.filter((id) => belongsTo(id, profile));
  if (profile !== 'downloads') return own;
  return [...own.filter((id) => id !== 'progress'), 'progress'];
}

/** Whether a column exists in this list at all; see ColumnDef.onlyIn. */
export function belongsTo(id: ColumnId, profile: ListProfile): boolean {
  const only = COLUMN_BY_ID.get(id)?.onlyIn;
  return !only || only.includes(profile);
}

/**
 * The shape of a stored layout, bumped when a shipped default changes in a way
 * an existing layout would otherwise swallow. mergeOrder keeps whatever order
 * somebody arranged, which also means a new default order reaches nobody who
 * has ever touched this table. Each stamp names the one change that is not a
 * preference to keep: at ORDER_RESEATED_AT the order is re-seated once, and a
 * column in SHOWN_SINCE is taken out of a hidden set stored before its
 * version. Everything else survives untouched.
 */
export const LAYOUT_VERSION = 3;

/** The version whose order every older layout takes once; see resolveLayout. */
const ORDER_RESEATED_AT = 2;

/**
 * Columns a list shows by default where an earlier build hid them, with the
 * version that changed it. A hidden set stored before that version was written
 * while the column was hidden for everybody, so it says nothing about anybody
 * wanting it gone and the new default wins once. From that version on, hiding
 * it is somebody's own choice and stays.
 */
const SHOWN_SINCE: Record<ListProfile, Partial<Record<ColumnId, number>>> = {
  downloads: { variant: 3 },
  collector: {},
};

/**
 * What each list starts with switched off. The collector holds links nobody has
 * started, so a speed and a finished-at column there are empty cells per row
 * pretending to be information.
 *
 * Both sets are also cut towards what fits: with every column on, the downloads
 * table opens several hundred pixels scrolled off its own right edge, and a
 * default that does not fit reads as a broken layout rather than a rich one.
 * The rest are one click away in the header menu. Every column left on in
 * downloads carries a value on every row except 'variant' (below), and each
 * set fits its card in a 1280px window (check-column-widths.mjs).
 *
 * `connection` ships hidden in both because it is empty until somebody
 * configures a connection, and peers, seeds and ratio because they are blank on
 * every row that is not a torrent.
 */
export const DEFAULT_HIDDEN: Record<ListProfile, ColumnId[]> = {
  // 'variant' is on in both lists, although it is blank on every row that is
  // not a yt-dlp link. In the collector a link's five rows want a pick; in the
  // download list they all carry the link's title, and this column is the only
  // thing that tells the audio row from the video row and says what quality
  // each is being fetched at.
  downloads: ['comment', 'source', 'added', 'finished', 'resolver', 'connection', 'peers', 'seeds', 'ratio'],
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

/**
 * What goes into the UI state store. Ids, never indices: an update that adds or
 * removes a column would otherwise shift every width onto the wrong column.
 *
 * `order` lists every column the stored layout knew about, hidden ones
 * included. That membership is what tells a later build which columns are new
 * to this layout and which the user switched off.
 */
export interface ColumnLayout {
  order: ColumnId[];
  hidden: ColumnId[];
  widths: Partial<Record<ColumnId, number>>;
  /** Absent on every layout written before the stamp existed; see LAYOUT_VERSION. */
  v?: number;
}

export interface ResolvedLayout {
  /** Every column this build has, in the user's order, hidden ones included. */
  order: ColumnDef[];
  /** The ones actually drawn, in order. */
  visible: ColumnDef[];
  hidden: Set<ColumnId>;
  /** Only the widths somebody dragged; everything else takes its default. */
  widths: Partial<Record<ColumnId, number>>;
  widthOf: (id: ColumnId) => number;
  minWidthOf: (id: ColumnId) => number;
}

const isKnown = (id: string): id is ColumnId => COLUMN_BY_ID.has(id as ColumnId);

/**
 * mergeOrder is what storing ids buys: a stored layout survives an update in
 * both directions. A column this build no longer has is dropped rather than
 * left as a hole, and one this build added is seated where the built-in order
 * puts it relative to the columns that are stored, so an update neither throws
 * away an arrangement nor appends the new column at the far right where nobody
 * scrolls to find it.
 */
function mergeOrder(profile: ListProfile, stored: ColumnId[] | undefined): ColumnId[] {
  const base = defaultOrderFor(profile);
  // A column this list does not have is dropped from a stored layout the same
  // way a column this build no longer has is: a layout written before
  // `enabled` became collector-only would otherwise keep drawing it.
  const kept = (stored ?? []).filter(isKnown).filter((id) => belongsTo(id, profile));
  // Nothing recognisable stored: either a first run or a layout from a build
  // that shares no column with this one. Either way the defaults are the answer.
  if (kept.length === 0) return [...base];

  const out: ColumnId[] = [];
  for (const id of kept) if (!out.includes(id)) out.push(id);

  for (let i = 0; i < base.length; i++) {
    const id = base[i];
    if (out.includes(id)) continue;
    // Seat it after the nearest default predecessor that is already placed, so
    // two new neighbouring columns also keep their order relative to each other.
    let at = 0;
    for (let k = i - 1; k >= 0; k--) {
      const p = out.indexOf(base[k]);
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
  for (const [id, since] of Object.entries(SHOWN_SINCE[profile])) {
    if ((stored?.v ?? 0) < (since ?? 0)) hidden.delete(id as ColumnId);
  }
  for (const c of COLUMNS) if (!c.hideable) hidden.delete(c.id);
  return hidden;
}

export function resolveLayout(profile: ListProfile, stored: ColumnLayout | null | undefined): ResolvedLayout {
  // A layout from before the stamp takes this build's order once; everything
  // that is genuinely a preference (which columns are off, how wide they are)
  // comes along unchanged. See LAYOUT_VERSION.
  const current = (stored?.v ?? 0) >= ORDER_RESEATED_AT;
  const order = mergeOrder(profile, current ? stored?.order : undefined).map((id) => COLUMN_BY_ID.get(id)!);
  const hidden = mergeHidden(profile, stored);
  const widths: Partial<Record<ColumnId, number>> = {};
  for (const [id, w] of Object.entries(stored?.widths ?? {})) {
    if (isKnown(id) && typeof w === 'number' && Number.isFinite(w)) widths[id] = w;
  }
  const minWidthOf = (id: ColumnId): number => {
    const def = COLUMN_BY_ID.get(id);
    return def ? (def.minWidthByProfile?.[profile] ?? def.minWidth) : 0;
  };
  const widthOf = (id: ColumnId): number => {
    const def = COLUMN_BY_ID.get(id);
    if (!def) return 0;
    // A width somebody dragged first, then this list's own default, then the
    // shared one. The per-list default is only a starting point: it is read
    // before anything is stored, and the store already keeps widths per list.
    return Math.max(minWidthOf(id), Math.round(widths[id] ?? def.widthByProfile?.[profile] ?? def.width));
  };
  const visible = order.filter((c) => !hidden.has(c.id));
  return { order, visible, hidden, widths, widthOf, minWidthOf };
}

/** toStored is what resolveLayout resolved, in the shape the store keeps. */
export function toStored(r: ResolvedLayout): ColumnLayout {
  return { order: r.order.map((c) => c.id), hidden: [...r.hidden], widths: { ...r.widths }, v: LAYOUT_VERSION };
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

/**
 * gridTemplate builds the track list every row shares, handed to them through
 * one custom property, so a column drag repaints the table by touching a single
 * element instead of re-rendering several hundred rows per pointer move. `drag`
 * is a width still under the pointer, drawn as if it were stored.
 *
 * The name takes the width the other columns leave. It is the one cell that is
 * never wide enough, while the others hold values of a known length. When the
 * window is too narrow, the name gives way to its minimum first, then every
 * column at its default width gives way to its own, and only then does the
 * table scroll: a name truncates gracefully, and a scrolled table puts the
 * row's actions out of sight at its far end. A width somebody dragged stays.
 *
 * Once the name has been dragged it stops filling, or dragging it narrower
 * would change nothing, and the last column takes the rest from its own width
 * up. It is the one dragged width that still gives way, for the reason above.
 */
export function gridTemplate(layout: ResolvedLayout, drag?: { id: ColumnId; width: number }): string {
  const dragged = (id: ColumnId) => drag?.id === id || layout.widths[id] !== undefined;
  const nameFills = !dragged('name');
  const last = layout.visible.length - 1;
  // One track per column and one for the row's action badges after them. The
  // rows are a grid with no explicit row count, so one track too few wraps the
  // last cell onto a second grid line, which reads as the rows simply being
  // tall rather than as a layout fault. The header and a folder row without
  // actions leave the last track empty, which costs nothing.
  const columns = layout.visible.map((c, i) => {
    const min = layout.minWidthOf(c.id);
    const width = drag?.id === c.id ? drag.width : layout.widthOf(c.id);
    if (c.id === 'name' && nameFills) return `minmax(${min}px, 1fr)`;
    if (!nameFills && i === last) return `minmax(${width}px, 1fr)`;
    return dragged(c.id) && c.id !== 'name' ? `${width}px` : `minmax(${min}px, ${width}px)`;
  });
  return [...columns, ACTIONS_TRACK].join(' ');
}

/**
 * The width of the actions track: as many square badges as the busiest row
 * carries and the 4px gaps between them. That is three, a collected link's
 * start, recheck and remove, or a failed one's skip, restart and remove. A
 * fixed width rather than `auto`, since every row is a grid of its own and an
 * `auto` track would size to each row's badges and shift the columns before
 * it from row to row.
 */
const ACTIONS_TRACK = 'calc(3 * var(--btn-h) + 2 * 0.25rem)';

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
 * each other, comparing each package by the row that sorts first in it, so
 * "biggest first" puts the package holding the biggest file at the top rather
 * than the package whose name happens to sort first.
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
