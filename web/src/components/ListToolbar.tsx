// The furniture around a download list: what narrows it, what acts on a
// selection, and the clean-up entries that work out a selection themselves.
//
// Both lists mount the same pieces. The collector had no search field at all and
// the download list had a blind substring match over two fields, which is the
// same feature built twice and badly; here it is built once and the pages pass
// in which quick filters make sense for the rows they hold.
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import type { SVGProps } from 'react';
import {
  type ApiOptions,
  type BulkResult,
  type CleanupClass,
  type PriorityChoice,
  type QueueMove,
  type QueueState,
  type Task,
  type TaskOptionsPatch,
  cleanupPreview,
  deleteTasks,
  fetchOptions,
  priorityChoices,
  fetchQueue,
  pause,
  queueForce,
  queueMove,
  queuePriority,
  recheckTasks,
  restartTasks,
  resume,
  runCleanup,
  setEnabled,
  setForced,
  setHold,
  setQueue as armStopMark,
  setTaskOptions,
  startTasks,
} from '../lib/api';
import { fmtBytes } from '../lib/format';
import { useToast } from '../lib/toast';
import { useT, type TranslationKey } from '../lib/i18n';
import { Button, Field, InfoBubble, Modal, NumberInput, TextInput } from './ui';
import { Tabs } from './Tabs';
import {
  ContextMenu,
  anchorBelow,
  useContextMenu,
  type MenuAnchor,
  type MenuGroup,
  type MenuItem,
} from './ContextMenu';
import { SearchField, type SearchQuery } from './SearchField';
import {
  IconArrowDown,
  IconArrowUp,
  IconBolt,
  IconBottom,
  IconCheck,
  IconChevronDown,
  IconChevronUp,
  IconClose,
  IconFolder,
  IconKey,
  IconPause,
  IconPin,
  IconPlay,
  IconPower,
  IconPriority,
  IconRetry,
  IconSearch,
  IconStopMark,
  IconTop,
  IconTrash,
  IconTrashFiles,
} from '../lib/icons';

// The nine glyphs this file used to draw for itself are gone: seven of them now
// live in lib/icons.tsx with the rest of the set, and two (the key and the power
// symbol) turned out to be second, stroked drawings of ones that were already
// there. Eight of the nine were stroked outlines, which is what that file's
// header forbids and what jdp's report was pointing at ("Rechtsklick menü soll
// auch glyphen bekommen (siehe GS)"): one menu row could carry a hairline
// chevron while the row under it carried a solid trash can. Only the More
// button's glyph stays here, because it is toolbar furniture, not a menu entry.
const IconMore = (p: SVGProps<SVGSVGElement>) => (
  <svg
    width={16}
    height={16}
    viewBox="0 0 20 20"
    fill="currentColor"
    className="shrink-0"
    aria-hidden
    {...p}
  >
    <circle cx="5" cy="10" r="1.4" />
    <circle cx="10" cy="10" r="1.4" />
    <circle cx="15" cy="10" r="1.4" />
  </svg>
);

// --- The queue's own state ------------------------------------------------


/**
 * useQueueVerbs holds the two things the queue entries need that neither the
 * list nor the menu already has: the priorities this server implements, and
 * which of these downloads is marked "finish this one, then stop".
 *
 * Both are fetched when the page mounts, never when the menu opens. A
 * right-click that waits for a request before it can draw its entries is a menu
 * whose bottom half appears after you have read past it.
 */
function useQueueVerbs(base: string) {
  const [choices, setChoices] = useState<PriorityChoice[]>([]);
  const [queue, setQueue] = useState<QueueState | null>(null);

  useEffect(() => {
    let live = true;
    void priorityChoices().then(
      (p) => {
        if (live) setChoices(p);
      },
      () => {
        /* the priority entry stays out of the menu; nothing else is affected */
      },
    );
    void fetchQueue(base).then(
      (q) => {
        if (live) setQueue(q);
      },
      () => {
        /* an unreachable peer is already reported by the list itself */
      },
    );
    return () => {
      live = false;
    };
  }, [base]);

  // The mark is one task, so arming a second one disarms the first — which is
  // why this replaces rather than adds, and why the answer is kept: it is the
  // only way the menu knows which row is currently marked.
  const mark = useCallback(
    async (id: string) => {
      setQueue(await armStopMark({ stopMark: id }, base));
    },
    [base],
  );

  return {
    choices,
    halted: queue?.halted ?? false,
    stopMark: queue?.stopMark ?? '',
    mark,
  };
}

type QueueVerbs = ReturnType<typeof useQueueVerbs>;

// --- Quick filters --------------------------------------------------------

export type QuickFilterId =
  | 'running'
  | 'queued'
  | 'paused'
  | 'finished'
  | 'failed'
  | 'offline'
  | 'disabled'
  | 'held'
  | 'online'
  | 'uncheckable'
  | 'unchecked';

export interface QuickFilter {
  id: QuickFilterId;
  label: TranslationKey;
  match: (t: Task) => boolean;
}

export const QUICK_FILTERS: QuickFilter[] = [
  // Extracting is part of running: the download is not over, and a row that
  // vanished from "running" while it unpacks reads as a finished download.
  { id: 'running', label: 'filter.running', match: (t) => t.status === 'running' || t.status === 'extracting' },
  { id: 'queued', label: 'filter.queued', match: (t) => t.status === 'queued' },
  { id: 'paused', label: 'filter.paused', match: (t) => t.status === 'paused' },
  { id: 'finished', label: 'filter.finished', match: (t) => t.status === 'done' },
  { id: 'failed', label: 'filter.failed', match: (t) => t.status === 'error' },
  { id: 'online', label: 'filter.online', match: (t) => t.online === 'online' },
  { id: 'offline', label: 'filter.offline', match: (t) => t.online === 'offline' },
  // Deliberately its own filter and not folded into "offline": the host was
  // asked and would not say, which is the state people need to find in order to
  // check those links again rather than delete them.
  { id: 'uncheckable', label: 'filter.uncheckable', match: (t) => t.online === 'uncheckable' },
  { id: 'unchecked', label: 'filter.unchecked', match: (t) => !t.online },
  { id: 'disabled', label: 'filter.disabled', match: (t) => !t.enabled },
  { id: 'held', label: 'filter.held', match: (t) => !!t.hold },
];

/**
 * offeredQuickFilters is ListToolbar's own filter-chip logic, exported so a
 * page that renders these chips somewhere OTHER than ListToolbar's own row
 * (Collector.tsx's badge row, jdp 2026-08-25: "können wir die nicht in der
 * zeile der quadratischen icons platzieren") can reuse the exact same
 * counting/visibility rule without a second, drifting copy of it. A filter
 * with nothing to match is left off the menu - eight always-visible chips
 * reading zero are eight things to read past on the way to the two that
 * mean something - but one that is switched ON stays even at zero, or
 * turning a filter on could make its own chip disappear out from under it.
 */
export function offeredQuickFilters(
  filterIds: QuickFilterId[],
  tasks: Task[],
  active: Set<QuickFilterId>,
): { f: QuickFilter; n: number }[] {
  const defs = filterIds.map((id) => QUICK_FILTERS.find((f) => f.id === id)).filter((f): f is QuickFilter => !!f);
  return defs
    .map((f) => ({ f, n: tasks.reduce((s, x) => s + (f.match(x) ? 1 : 0), 0) }))
    .filter(({ f, n }) => n > 0 || active.has(f.id));
}

/** The eight states a download list is actually filtered by. */
export const DOWNLOAD_FILTERS: QuickFilterId[] = [
  'running',
  'queued',
  'paused',
  'finished',
  'failed',
  'offline',
  'disabled',
  'held',
];

/** The collector's, where every row is staged and the question is what a check said. */
export const COLLECTOR_FILTERS: QuickFilterId[] = [
  'online',
  'offline',
  'uncheckable',
  'unchecked',
  'disabled',
  'held',
];

/**
 * matchesQuickFilters is a union, not an intersection: two filters on means
 * "show me both kinds". Intersecting them would make every second click empty
 * the list, since nothing is queued and finished at once.
 */
export function matchesQuickFilters(t: Task, active: Set<QuickFilterId>): boolean {
  if (active.size === 0) return true;
  return QUICK_FILTERS.some((f) => active.has(f.id) && f.match(t));
}

// --- Weighing a selection before it is destroyed --------------------------

interface Weight {
  count: number;
  files: number;
  bytes: number;
}

/**
 * weigh counts what a removal would actually erase.
 *
 * Bytes on disk, not the announced size: a queued 40 GB download has written
 * nothing, and warning about 40 GB that do not exist is how people learn to
 * click straight through the dialog.
 */
function weigh(all: Task[], ids: string[]): Weight {
  const want = new Set(ids);
  let files = 0;
  let bytes = 0;
  for (const t of all) {
    if (!want.has(t.id) || t.loaded <= 0) continue;
    files++;
    bytes += t.loaded;
  }
  return { count: ids.length, files, bytes };
}

/**
 * ConfirmRemove states what is about to go and how much of it, before anything
 * goes. The two exits are deliberately unlike each other: taking rows off a list
 * is reversible by pasting the links again, erasing the files is not.
 */
function ConfirmRemove({
  title,
  what,
  weight,
  allowFiles,
  note,
  onCancel,
  onConfirm,
}: {
  title: string;
  /** What a clean-up class selects, when the rows were not chosen by hand. */
  what?: string;
  weight: Weight;
  allowFiles: boolean;
  /** Why the files cannot be deleted from here, when they cannot. */
  note?: string;
  onCancel: () => void;
  onConfirm: (withFiles: boolean) => void;
}) {
  const { t } = useT();
  const files = allowFiles && weight.files > 0;

  return (
    <Modal
      title={title}
      onClose={onCancel}
      footer={
        <>
          <Button kind="secondary" onClick={() => onConfirm(false)}>
            {t('remove.fromList')}
          </Button>
          {files && (
            <Button
              kind="danger"
              className="bg-statusFailBg"
              icon={<IconTrashFiles width={16} height={16} />}
              onClick={() => onConfirm(true)}
            >
              {t('remove.withFiles')}
            </Button>
          )}
          <span className="flex-1" />
          <Button kind="ghost" onClick={onCancel}>
            {t('common.cancel')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-2 text-sm text-carbon-textSub">
        {what && <p>{what}</p>}
        <p className="glim-num text-carbon-text">{t('remove.count', { n: weight.count })}</p>
        <p className="glim-num">
          {weight.files === 0
            ? t('remove.noFiles')
            : files
              ? t('remove.filesGone', { files: weight.files, bytes: fmtBytes(weight.bytes) })
              : t('remove.filesKept')}
        </p>
        {note && <p className="text-carbon-textMuted">{note}</p>}
      </div>
    </Modal>
  );
}

// --- The per-task overrides -----------------------------------------------

/**
 * TaskOptionsDialog edits the three things a link can be told on its own: where
 * its file goes, the password its archive needs, and how many connections it is
 * pulled over.
 *
 * It takes a selection rather than a task, because the row's own folder button
 * and the context menu's two entries are the same dialog on one row and on
 * forty. That also fixes the rule that matters here: a field is sent ONLY if it
 * was changed. Sending all three every time would let a selection whose members
 * disagree open with empty boxes and wipe every override in it on save - and for
 * the connection count that is not a blank field but a 0, which the server reads
 * as a deliberate "hand it back to the rules".
 */
export function TaskOptionsDialog({
  tasks,
  base,
  focus = 'dir',
  onClose,
}: {
  tasks: Task[];
  base: string;
  /** Which box the caller came for. */
  focus?: 'dir' | 'password';
  onClose: () => void;
}) {
  const { t } = useT();
  // One agreed value, or nothing. An empty box that is left alone changes
  // nothing, so "they disagree" and "it is unset" behave identically here.
  const agreed = (pick: (x: Task) => string) => {
    const first = tasks.length > 0 ? pick(tasks[0]) : '';
    return tasks.every((x) => pick(x) === first) ? first : '';
  };
  // The same rule for the count, where "nothing" is 0. That is not a fallback
  // standing in for a real value: 0 is what the field means anyway, so a
  // selection that disagrees opens on "no override" and only changes the rows
  // if somebody types a number.
  const agreedNum = (pick: (x: Task) => number) => {
    const first = tasks.length > 0 ? pick(tasks[0]) : 0;
    return tasks.every((x) => pick(x) === first) ? first : 0;
  };
  const [dir, setDir] = useState(() => agreed((x) => x.dir ?? ''));
  const [password, setPassword] = useState(() => agreed((x) => x.password ?? ''));
  const [chunks, setChunks] = useState(() => agreedNum((x) => x.chunks ?? 0));
  const [initial] = useState(() => ({
    dir: agreed((x) => x.dir ?? ''),
    password: agreed((x) => x.password ?? ''),
    chunks: agreedNum((x) => x.chunks ?? 0),
  }));
  const [error, setError] = useState('');

  async function apply() {
    const opts: TaskOptionsPatch = {};
    if (dir !== initial.dir) opts.dir = dir;
    if (password !== initial.password) opts.password = password;
    if (chunks !== initial.chunks) opts.chunks = chunks;
    if (Object.keys(opts).length === 0) {
      onClose();
      return;
    }
    const r = await setTaskOptions(
      tasks.map((x) => x.id),
      opts,
      base,
    );
    if (!r.ok) {
      setError(await r.text());
      return;
    }
    onClose();
  }

  const title =
    tasks.length === 1 ? tasks[0].name || tasks[0].url : `${tasks.length} ${t('select.count')}`;

  return (
    <Modal
      title={title}
      onClose={onClose}
      footer={
        <>
          <Button onClick={apply}>{t('settings.save')}</Button>
          {error && <span className="text-statusFail text-sm">{error}</span>}
        </>
      }
    >
      <Field label={t('task.folder')} hint={t('settings.downloadDirHint')}>
        <TextInput
          dir="ltr"
          autoFocus={focus === 'dir'}
          value={dir}
          spellCheck={false}
          onChange={(e) => setDir(e.target.value)}
        />
      </Field>
      <Field label={t('task.password')}>
        <TextInput
          autoFocus={focus === 'password'}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </Field>
      {/* max is the engine's bound, the same one the rule editor and the
          settings page offer. Three spinners that stop at three different
          numbers would be three different accounts of what the app can do. */}
      <Field label={t('task.chunks')} hint={t('task.chunksHint')}>
        <NumberInput value={chunks} min={0} max={16} onValue={setChunks} />
      </Field>
    </Modal>
  );
}

// --- Removing a selection -------------------------------------------------

/**
 * useRemoval is the one place a selection is taken off a list, and the one place
 * Del and Shift+Del are bound.
 *
 * Removing rows the user picked themselves happens immediately: they named the
 * rows, and the files are untouched. Erasing the files always asks first, and
 * names the file count and the bytes while asking. A clean-up class is the other
 * way round again — see runClass below — because there the app picked the rows.
 */
export function useRemoval({
  all,
  selected,
  base,
  onDone,
}: {
  /** Every task this instance holds, so the byte total covers rows the filters hid. */
  all: Task[];
  selected: Set<string>;
  base: string;
  onDone: () => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [ask, setAsk] = useState<string[] | null>(null);

  const removeNow = useCallback(
    async (ids: string[], withFiles = false) => {
      if (ids.length === 0) return;
      try {
        const r: BulkResult = await deleteTasks(ids, withFiles, base);
        toast(t('remove.done', { n: r.count }), 'ok');
        onDone();
      } catch (e) {
        toast(t('list.failed', { error: message(e) }), 'fail');
      }
    },
    [base, onDone, t, toast],
  );

  const askWithFiles = useCallback((ids: string[]) => {
    if (ids.length > 0) setAsk(ids);
  }, []);

  // Del is only the download list's key while nothing is being typed into.
  // Without the guard, editing a search query deletes downloads.
  useEffect(() => {
    if (selected.size === 0) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Delete' || e.altKey || e.ctrlKey || e.metaKey) return;
      const el = e.target as HTMLElement | null;
      if (el && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName))) return;
      e.preventDefault();
      const ids = [...selected];
      if (e.shiftKey) askWithFiles(ids);
      else void removeNow(ids);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [selected, askWithFiles, removeNow]);

  const dialog = ask && (
    <ConfirmRemove
      title={t('remove.title')}
      weight={weigh(all, ask)}
      allowFiles
      onCancel={() => setAsk(null)}
      onConfirm={(withFiles) => {
        setAsk(null);
        void removeNow(ask, withFiles);
      }}
    />
  );

  return { removeNow, askWithFiles, dialog };
}

export type Removal = ReturnType<typeof useRemoval>;

// --- The clean-up classes, shared by the bar and the menu ------------------

// The clean-up entries come from the server, once per session. Building the menu
// from the client's own list would offer whatever this build was compiled with,
// and an entry the server does not implement is a button that answers 400 when
// it is pressed.
let optionsOnce: Promise<ApiOptions> | null = null;

function cleanupClasses(): Promise<CleanupClass[]> {
  if (!optionsOnce) optionsOnce = fetchOptions();
  return optionsOnce.then(
    (o) => o.cleanupClasses ?? [],
    (e) => {
      optionsOnce = null; // a failed load must not poison the next attempt
      throw e;
    },
  );
}

const CLEANUP_LABEL: Partial<Record<string, TranslationKey>> = {
  finished: 'cleanup.finished',
  offline: 'cleanup.offline',
  disabled: 'cleanup.disabled',
  duplicates: 'cleanup.duplicates',
  incompleteArchives: 'cleanup.incompleteArchives',
};

const CLEANUP_WHAT: Partial<Record<string, TranslationKey>> = {
  finished: 'cleanup.what.finished',
  offline: 'cleanup.what.offline',
  disabled: 'cleanup.what.disabled',
  duplicates: 'cleanup.what.duplicates',
  incompleteArchives: 'cleanup.what.incompleteArchives',
};

/**
 * A finished download's files are the reason it was downloaded. The class that
 * tidies them off the list is the one people run daily, and one absent-minded
 * click on the wrong button would erase a finished library — so this entry does
 * not offer the destructive exit at all, rather than offering it and asking
 * nicely.
 */
const KEEPS_FILES = new Set<string>(['finished']);

/**
 * useCleanup is the clean-up flow, once: fetch the classes the server actually
 * implements, preview what one would take, then confirm.
 *
 * The bar under the list and the right-click menu each run their own instance
 * of this hook, and Downloads.tsx now holds a third — one for its own command
 * surface's "clear finished" entry (lib/commands/downloads.ts), published
 * through lib/commands/pageContext.ts so a command's run() can call this same
 * preview() and the confirm dialog it raises is this same dialog. A third
 * instance is the existing pattern, not a new one: every caller keeps its own
 * `classes`/`confirm` state, and a shared one would be how the menu ends up
 * offering a class the server does not have, or removing without previewing
 * first.
 */
export function useCleanup(all: Task[]) {
  const { t } = useT();
  const { toast } = useToast();
  const [classes, setClasses] = useState<CleanupClass[] | null>(null);
  const [confirm, setConfirm] = useState<{ cls: CleanupClass; ids: string[] } | null>(null);

  const load = useCallback(async (): Promise<CleanupClass[]> => {
    const list = await cleanupClasses();
    setClasses(list);
    return list;
  }, []);

  // The preview and the confirmation are one gesture: the class picks the rows,
  // so the count is the only thing that can tell the user what they are about to
  // agree to. Nothing is removed by opening this.
  const preview = useCallback(
    async (cls: CleanupClass): Promise<void> => {
      try {
        const r = await cleanupPreview(cls);
        if (r.count === 0) {
          toast(t('cleanup.nothing', { what: classLabel(cls, t) }), 'info');
          return;
        }
        setConfirm({ cls, ids: r.ids });
      } catch (e) {
        toast(t('cleanup.failed', { error: message(e) }), 'fail');
      }
    },
    [t, toast],
  );

  const run = useCallback(
    async (cls: CleanupClass, withFiles: boolean): Promise<void> => {
      setConfirm(null);
      try {
        const r = await runCleanup(cls, withFiles);
        toast(t('remove.done', { n: r.count }), 'ok');
      } catch (e) {
        toast(t('cleanup.failed', { error: message(e) }), 'fail');
      }
    },
    [t, toast],
  );

  const dialog = confirm && (
    <ConfirmRemove
      title={classLabel(confirm.cls, t)}
      what={CLEANUP_WHAT[confirm.cls] ? t(CLEANUP_WHAT[confirm.cls]!) : undefined}
      weight={weigh(all, confirm.ids)}
      allowFiles={!KEEPS_FILES.has(confirm.cls)}
      note={KEEPS_FILES.has(confirm.cls) ? t('cleanup.finishedKeepsFiles') : undefined}
      onCancel={() => setConfirm(null)}
      onConfirm={(withFiles) => void run(confirm.cls, withFiles)}
    />
  );

  return { classes, load, preview, dialog };
}

/**
 * Named the same way Removal is (this file, above): the return shape a
 * caller outside this file needs to spell out, first needed by
 * lib/commands/pageContext.ts so a page can publish its own useCleanup()
 * instance to the command registry without that file re-deriving the shape
 * via `ReturnType<typeof useCleanup>` itself.
 */
export type CleanupState = ReturnType<typeof useCleanup>;

/** cleanupItems turns the classes the server offers into menu entries. Exported
 *  for Collector.tsx's own badge-triggered cleanup menu (jdp, 2026-08-24:
 *  "Aufräumen ... als badge"), which needs the same entries ListActionBar's
 *  own text-button trigger already builds from this, just behind a different
 *  visual trigger - never a second, separately-maintained item list. */
export function cleanupItems(
  classes: CleanupClass[],
  t: (key: TranslationKey) => string,
  preview: (cls: CleanupClass) => void,
): MenuItem[] {
  return classes.map((cls) => ({
    id: cls,
    label: classLabel(cls, t),
    icon: <IconTrash width={14} height={14} />,
    onSelect: () => preview(cls),
  }));
}

// --- The menu a list offers -----------------------------------------------

/**
 * What a right-click landed on. The three readings a download list has:
 *
 *   selection  a link row, or the More button — act on what is selected
 *   package    a package header — the same verbs, over the whole package, plus
 *              the fold
 *   list       empty space — nothing is selected and the entries are the ones
 *              that belong to the list itself
 */
export type MenuTarget =
  | { kind: 'selection' }
  | { kind: 'package'; name: string }
  | { kind: 'list' };

/**
 * The menu's accessible name, one per reading. A screen reader announces this
 * before the entries, so it is the only chance to say what the menu is about to
 * act on — and "the selected downloads" is a lie on a package header nobody
 * selected.
 */
const MENU_LABEL: Record<MenuTarget['kind'], TranslationKey> = {
  selection: 'menu.label',
  package: 'menu.packageLabel',
  list: 'list.actions',
};

/** What the page knows about its own list, for the entries that act on all of it. */
export interface ListContext {
  /** The package names on screen, in view order. */
  packages: string[];
  collapsed: ReadonlySet<string>;
  onCollapse: (names: string[]) => void;
  onExpand: (names: string[]) => void;
  onSelectAll: () => void;
  onSelectNone: () => void;
  /**
   * Whether clean-up would act on the instance being shown. It is never
   * forwarded to a peer, so on somebody else's list the entries are left out
   * rather than quietly acting on the wrong machine.
   */
  local: boolean;
}

/**
 * PriorityGlyph draws one rung of the queue's priority ladder: filled triangles
 * pointing the way that rung moves a download, one per step away from default,
 * and a single flat bar for default itself.
 *
 * jdp: "im priorität menü fehlen glyphen." The gutter was left empty there on
 * purpose, because that column was carrying the tick for the priority the
 * selection already sits at, and a glyph in it would have buried the one thing
 * the submenu has to answer. Both fit once they stop sharing a column: the
 * glyph names what the row IS, on the left, and which row is IN FORCE is said
 * at the other end of the row instead - full ink plus a trailing tick, which is
 * what MenuItem.checked paints. Neither has to be given up.
 *
 * Triangles rather than arrows, and the count rather than the size, for the
 * same reason IconPriority is not an arrow: the four entries one submenu away
 * own the arrows, and those move a task one place, while this puts it on a
 * rung. Three stacked wedges read as "as far as it goes" at 14px where three
 * lengths of one arrow do not. The rung is read off the server's own value
 * (-3..3, clamped), not off the id, so a ladder that renames its steps or
 * offers fewer of them still draws.
 */
function PriorityGlyph({ steps }: { steps: number }) {
  const n = Math.min(3, Math.abs(steps));
  const up = steps > 0;
  // 5 tall per wedge, 1 of air between them, centred in the 20-unit box the
  // rest of the icon set is drawn in.
  const top = (20 - (n * 6 - 1)) / 2;
  return (
    <svg
      viewBox="0 0 20 20"
      width={14}
      height={14}
      fill="currentColor"
      className="shrink-0"
      aria-hidden
      focusable="false"
    >
      {n === 0 ? (
        <rect x="4.5" y="9.1" width="11" height="1.8" rx=".9" />
      ) : (
        Array.from({ length: n }, (_, i) => {
          const y = top + i * 6;
          return <path key={i} d={up ? `M10 ${y}L15 ${y + 5}H5Z` : `M5 ${y}H15L10 ${y + 5}Z`} />;
        })
      )}
    </svg>
  );
}

/**
 * taskMenuGroups builds the verbs for a selection — the same ones whether the
 * selection is one link, a package, or forty rows picked by hand.
 *
 * An entry that cannot act on any of the selected rows is left out instead of
 * shown greyed: a menu of nine dead verbs is a menu nobody reads to the end of.
 */
function taskMenuGroups({
  chosen,
  ids,
  base,
  t,
  fail,
  removal,
  queue,
  onOptions,
}: {
  chosen: Task[];
  ids: string[];
  base: string;
  t: (key: TranslationKey, vars?: Record<string, string | number>) => string;
  fail: (e: unknown) => void;
  removal: Removal;
  /** The priorities this server offers and where the stop mark currently sits. */
  queue: QueueVerbs;
  onOptions: (focus: 'dir' | 'password') => void;
}): MenuGroup[] {
  const some = (p: (x: Task) => boolean) => chosen.some(p);
  const guard = (run: () => Promise<unknown>) => () => {
    void run().catch(fail);
  };

  const transport: MenuGroup = { id: 'transport', items: [] };
  if (some((x) => x.status === 'collected'))
    transport.items.push({
      id: 'start',
      label: t('task.start'),
      icon: <IconPlay width={14} height={14} />,
      onSelect: () => void startTasks(ids, base),
    });
  // Start and force are a pair and now sit as one, because read apart they look
  // like two clocks. jdp: "was ist der unterschied zwischen starten und jetzt
  // starten? Soll es nicht besser die Option Auswahl starten und Alle starten
  // geben?"
  //
  // They are not two clocks, they are a verb and an override. Start admits
  // links that are only staged into the queue, and it is the one entry here
  // that can lift a halt somebody set by hand. Forcing admits nothing: it takes
  // a selection the queue has ALREADY accepted and puts it in front of
  // everything else waiting, switching it on and un-parking it on the way, and
  // it refuses outright while the queue is stopped, because a per-link button
  // is not where a decision about the whole box gets undone. So the override is
  // named after the order it overrides rather than after the clock, and it is
  // put next to the start it overrides instead of three groups down among the
  // on/off flags, where the contrast was impossible to see.
  //
  // His two proposed labels are not free: "Auswahl starten" and "Alle starten"
  // are already collector.startSelected / collector.startAll, which answer how
  // MUCH of the collector goes in, not where in the order it lands.
  //
  // Forcing is offered even on links that are already forced, because pressing
  // it again is how you claim the front a second time after somebody else's
  // links have been pushed ahead.
  if (some((x) => x.status !== 'done'))
    transport.items.push({
      id: 'force',
      label: t('menu.forceStart'),
      icon: <IconBolt />,
      detail: queue.halted ? t('menu.queueStopped') : undefined,
      disabled: queue.halted,
      onSelect: guard(() => queueForce({ ids }, base)),
    });
  if (some((x) => !!x.forced))
    transport.items.push({
      id: 'unforce',
      label: t('menu.unforce'),
      icon: <IconBolt />,
      onSelect: guard(() => setForced(ids, false, base)),
    });
  // Stopping is per task on the wire — there is no bulk pause route — so this
  // pauses exactly the ones that are running rather than asking the server to
  // work out which those were.
  if (some((x) => x.status === 'running' || x.status === 'extracting'))
    transport.items.push({
      id: 'pause',
      label: t('task.pause'),
      icon: <IconPause width={14} height={14} />,
      onSelect: () => {
        for (const x of chosen) if (x.status === 'running' || x.status === 'extracting') void pause(x.id, base);
      },
    });
  if (some((x) => x.status === 'paused'))
    transport.items.push({
      id: 'resume',
      label: t('task.resume'),
      icon: <IconPlay width={14} height={14} />,
      onSelect: () => {
        for (const x of chosen) if (x.status === 'paused') void resume(x.id, base);
      },
    });
  if (some((x) => x.status === 'done' || x.status === 'error'))
    transport.items.push({
      id: 'restart',
      label: t('task.restart'),
      icon: <IconRetry width={14} height={14} />,
      onSelect: () => void restartTasks(ids, base),
    });
  transport.items.push({
    id: 'recheck',
    label: t('task.recheck'),
    icon: <IconSearch width={14} height={14} />,
    onSelect: () => void recheckTasks(ids, base),
  });

  // Queue order only means something while something is still waiting to run.
  // A package of finished downloads has no position to raise.
  const queueGroup: MenuGroup = { id: 'queue', items: [] };
  const waiting = (x: Task) =>
    x.status === 'queued' || x.status === 'paused' || x.status === 'collected';
  if (some(waiting)) {
    // Four steps, one route. The old pair of entries here called setPriority
    // with a fixed 1 and -1, which is not a step at all: pressing "raise" twice
    // left a task exactly where the first press put it, and pressing it on a
    // task the Packagizer had already set to highest silently demoted it.
    const step = (where: QueueMove) => guard(() => queueMove({ ids }, where, base));
    queueGroup.items.push({
      id: 'move',
      label: t('menu.move'),
      icon: <IconTop width={14} height={14} />,
      submenu: [
        {
          id: 'steps',
          items: [
            { id: 'top', label: t('task.moveTop'), icon: <IconTop width={14} height={14} />, onSelect: step('top') },
            { id: 'up', label: t('task.moveUp'), icon: <IconArrowUp width={14} height={14} />, onSelect: step('up') },
            { id: 'down', label: t('task.moveDown'), icon: <IconArrowDown width={14} height={14} />, onSelect: step('down') },
            { id: 'bottom', label: t('task.moveBottom'), icon: <IconBottom width={14} height={14} />, onSelect: step('bottom') },
          ],
        },
      ],
    });

    // Behind one word, the way JDownloader keeps it: seven more entries in a
    // menu that already has a dozen would bury everything under them. The tick
    // marks the value the whole selection is already at — a selection that
    // disagrees gets no tick rather than the first row's answer. `checked`
    // rather than a tick in the icon slot, so every rung can carry its own
    // glyph as well; see PriorityGlyph for why both are needed at once.
    if (queue.choices.length > 0) {
      const agreed = chosen.every((x) => x.priority === chosen[0].priority)
        ? chosen[0].priority
        : undefined;
      queueGroup.items.push({
        id: 'priority',
        label: t('menu.priority'),
        icon: <IconPriority />,
        submenu: [
          {
            id: 'values',
            items: queue.choices.map((p) => ({
              id: p.id,
              label: t(`priority.${p.id}` as TranslationKey),
              icon: <PriorityGlyph steps={p.value} />,
              checked: p.value === agreed,
              onSelect: guard(() => queuePriority({ ids }, p.value, base)),
            })),
          },
        ],
      });
    }
  }

  // The stop mark is one task, so it is offered on one row and never on forty:
  // an entry that silently picked the first of a selection would arm a mark on
  // a download nobody pointed at. There is exactly one mark in the app and this
  // toggles it — arming a second would only move it.
  if (chosen.length === 1 && (waiting(chosen[0]) || chosen[0].status === 'running')) {
    const only = chosen[0];
    const armed = queue.stopMark === only.id;
    queueGroup.items.push({
      id: 'stopMark',
      label: t(armed ? 'queue.stopMarkOn' : 'queue.stopMark'),
      icon: <IconStopMark />,
      onSelect: () => void queue.mark(armed ? '' : only.id).catch(fail),
    });
  }

  const state: MenuGroup = { id: 'state', items: [] };
  if (some((x) => !x.enabled))
    state.items.push({
      id: 'enable',
      label: t('menu.enable'),
      icon: <IconPower />,
      onSelect: guard(() => setEnabled(ids, true, base)),
    });
  if (some((x) => x.enabled))
    state.items.push({
      id: 'disable',
      label: t('menu.disable'),
      icon: <IconPower />,
      onSelect: guard(() => setEnabled(ids, false, base)),
    });
  if (some((x) => !x.hold))
    state.items.push({
      id: 'hold',
      label: t('menu.hold'),
      icon: <IconPin />,
      onSelect: guard(() => setHold(ids, true, base)),
    });
  if (some((x) => !!x.hold))
    state.items.push({
      id: 'release',
      label: t('menu.release'),
      icon: <IconPin />,
      onSelect: guard(() => setHold(ids, false, base)),
    });

  // Where the files go and what unlocks the archive: the two overrides a link
  // carries of its own. Both open the one dialog, with the cursor in the box
  // the entry names.
  const options: MenuGroup = {
    id: 'options',
    items: [
      {
        id: 'dir',
        label: t('menu.setFolder'),
        icon: <IconFolder width={14} height={14} />,
        onSelect: () => onOptions('dir'),
      },
      {
        id: 'password',
        label: t('task.password'),
        icon: <IconKey />,
        onSelect: () => onOptions('password'),
      },
    ],
  };

  const gone: MenuGroup = {
    id: 'remove',
    items: [
      {
        id: 'remove',
        label: t('task.remove'),
        detail: 'Del',
        icon: <IconTrash width={14} height={14} />,
        danger: true,
        onSelect: () => void removal.removeNow(ids),
      },
      {
        id: 'removeFiles',
        label: t('task.removeWithFiles'),
        detail: 'Shift+Del',
        icon: <IconTrashFiles />,
        danger: true,
        onSelect: () => removal.askWithFiles(ids),
      },
    ],
  };

  return [transport, queueGroup, state, options, gone];
}

/**
 * targetTaskId finds the row a right-click landed on.
 *
 * It reads `data-task-id`, which the row component sets. Falling back to the
 * current selection when nothing is under the pointer is deliberate: the More
 * button raises the same menu.
 */
export function targetTaskId(e: { target: EventTarget | null }): string | null {
  const el = e.target instanceof Element ? e.target.closest('[data-task-id]') : null;
  return el?.getAttribute('data-task-id') ?? null;
}

/**
 * targetPackage finds the package header a right-click landed on, and answers
 * with its name — which is legitimately the empty string for the ungrouped
 * package, so "not on a header at all" has to be null rather than falsy.
 */
export function targetPackage(e: { target: EventTarget | null }): string | null {
  const el = e.target instanceof Element ? e.target.closest('[data-package-row]') : null;
  if (!el) return null;
  return el.getAttribute('data-package-row') ?? '';
}

/**
 * ListMenu is the page's one menu: the same groups whether it was opened by
 * right-click on a link, on a package header, on empty space, or from the
 * selection strip's More button.
 *
 * It is mounted even while nothing is open, because the dialogs it raises must
 * outlive the menu itself — the menu closes before an entry runs, which is what
 * stops a dialog opening underneath it.
 */
export function ListMenu({
  anchor,
  onClose,
  all,
  selected,
  base,
  removal,
  target = { kind: 'selection' },
  list,
  extraGroups = [],
}: {
  anchor: MenuAnchor | null;
  onClose: () => void;
  all: Task[];
  selected: Set<string>;
  base: string;
  removal: Removal;
  /** What the pointer landed on. Defaults to the selection, which is what More means. */
  target?: MenuTarget;
  list?: ListContext;
  /** Groups another wave contributes, appended after the standard ones. */
  extraGroups?: MenuGroup[];
}) {
  const { t } = useT();
  const { toast } = useToast();
  const fail = useCallback((e: unknown) => toast(t('list.failed', { error: message(e) }), 'fail'), [t, toast]);
  const cleanup = useCleanup(all);
  const queue = useQueueVerbs(base);
  const [options, setOptions] = useState<{ tasks: Task[]; focus: 'dir' | 'password' } | null>(null);

  const chosen = useMemo(() => all.filter((x) => selected.has(x.id)), [all, selected]);

  // Fetched once when the page mounts, not when the menu opens: a right-click
  // that has to wait for a request before it can draw its entries is a menu
  // whose bottom half appears after you have already read past it.
  const { load } = cleanup;
  useEffect(() => {
    void load().catch(() => {
      /* the bar under the list says so when it is pressed; a menu does not nag */
    });
  }, [load]);

  const groups: MenuGroup[] = [];

  if (list) {
    const folded = list.packages.filter((n) => list.collapsed.has(n));
    const open = list.packages.filter((n) => !list.collapsed.has(n));

    // The package under the pointer folds from its own menu, which is the entry
    // people look for after they have found the twisty once.
    if (target.kind === 'package') {
      const name = target.name;
      const isFolded = list.collapsed.has(name);
      groups.push({
        id: 'fold',
        items: [
          {
            id: 'fold',
            label: t(isFolded ? 'task.expand' : 'task.collapse'),
            icon: isFolded ? <IconChevronDown /> : <IconChevronUp />,
            onSelect: () => (isFolded ? list.onExpand([name]) : list.onCollapse([name])),
          },
        ],
      });
    }

    // Empty space is about the list itself: nothing is selected there, and the
    // verbs that need a selection would all be missing anyway.
    if (target.kind === 'list') {
      const whole: MenuItem[] = [];
      if (list.packages.length > 0)
        whole.push({
          id: 'selectAll',
          label: t('select.all'),
          icon: <IconCheck width={14} height={14} />,
          onSelect: list.onSelectAll,
        });
      if (selected.size > 0)
        whole.push({
          id: 'selectNone',
          label: t('select.none'),
          detail: String(selected.size),
          // The cross that clears a thing, the same one every dismissable
          // surface in the app uses, against the tick that sets one directly
          // above it. Not a second tick with a slash through it: the two rows
          // are opposites and must be told apart at a glance, not read.
          icon: <IconClose />,
          onSelect: list.onSelectNone,
        });
      groups.push({ id: 'select', items: whole });
    }

    // Whole-list folding belongs to the empty space, not beside a package's own
    // fold: the two are one word apart in the reading, and a menu that has to be
    // read twice to tell "this one" from "all of them" is a menu that needs its
    // own explanation. The count stays in the detail column — the label says
    // which set, the number says how big it is.
    if (list.packages.length > 1 && target.kind === 'list') {
      const fold: MenuItem[] = [];
      if (open.length > 0)
        fold.push({
          id: 'collapseAll',
          label: t('menu.collapseAll'),
          detail: String(open.length),
          icon: <IconChevronUp />,
          onSelect: () => list.onCollapse(open),
        });
      if (folded.length > 0)
        fold.push({
          id: 'expandAll',
          label: t('menu.expandAll'),
          detail: String(folded.length),
          icon: <IconChevronDown />,
          onSelect: () => list.onExpand(folded),
        });
      groups.push({ id: 'foldAll', items: fold });
    }
  }

  if (chosen.length > 0) {
    groups.push(
      ...taskMenuGroups({
        chosen,
        ids: chosen.map((x) => x.id),
        base,
        t,
        fail,
        removal,
        queue,
        onOptions: (focus) => setOptions({ tasks: chosen, focus }),
      }),
      ...extraGroups,
    );
  }

  // Behind one word, the way JDownloader keeps it: five more entries in a menu
  // that already has a dozen would bury the ones that act on the selection.
  if (list?.local && cleanup.classes && cleanup.classes.length > 0) {
    groups.push({
      id: 'cleanup',
      items: [
        {
          id: 'cleanup',
          label: t('cleanup.menu'),
          icon: <IconTrash width={14} height={14} />,
          submenu: [{ id: 'classes', items: cleanupItems(cleanup.classes, t, (cls) => void cleanup.preview(cls)) }],
        },
      ],
    });
  }

  return (
    <>
      {anchor && (
        <ContextMenu
          anchor={anchor}
          groups={groups}
          label={t(MENU_LABEL[target.kind])}
          onClose={onClose}
        />
      )}
      {options && (
        <TaskOptionsDialog
          tasks={options.tasks}
          base={base}
          focus={options.focus}
          onClose={() => setOptions(null)}
        />
      )}
      {cleanup.dialog}
    </>
  );
}

// --- The toolbar above the list -------------------------------------------

/**
 * ListToolbar narrows what is on screen: the search field, the quick filters and
 * the readout that says how much of the list survived them.
 *
 * A filter nothing matches is not offered. Eight always-visible chips reading
 * zero are eight things to read past on the way to the two that mean something —
 * but one that is switched on stays, or turning a filter on could make its own
 * chip disappear.
 */
export function ListToolbar({
  search,
  onSearch,
  filters,
  active,
  onActive,
  tasks,
  shown,
  right,
  hideSearch = false,
}: {
  search: SearchQuery;
  onSearch: (next: SearchQuery) => void;
  /** Which quick filters this list offers, in order. */
  filters: QuickFilterId[];
  active: Set<QuickFilterId>;
  onActive: (next: Set<QuickFilterId>) => void;
  /** The list before search and filters — where the counts come from. */
  tasks: Task[];
  /** How many rows survived them. */
  shown: number;
  right?: ReactNode;
  /**
   * Skips the inline SearchField entirely. Collector.tsx moved its own search
   * field into a badge-toggled row of its own (jdp, 2026-08-24: "das
   * suchfeld soll auch als quadratischer badge neben die andren vier
   * badges") — Downloads.tsx never passes this and keeps the field inline
   * exactly as before.
   */
  hideSearch?: boolean;
}) {
  const { t } = useT();

  const offered = useMemo(() => offeredQuickFilters(filters, tasks, active), [filters, tasks, active]);

  const narrowed = active.size > 0 || search.text.trim() !== '';

  function toggle(id: QuickFilterId): void {
    const next = new Set(active);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    onActive(next);
  }

  return (
    <div className="flex flex-wrap items-center gap-2" role="group" aria-label={t('list.controls')}>
      {!hideSearch && <SearchField value={search} onChange={onSearch} className="max-w-md flex-1" />}

      {/* The same strip as the settings tabs and the corner picker, in its
          multi-select reading: two filters on means "show me both kinds". These
          were hand-built buttons that happened to share three class names with
          the settings rail, which is how the two drifted apart in the first
          place — one keyboard-navigable, one not. */}
      {offered.length > 0 && (
        <Tabs
          select="many"
          size="sm"
          label={t('filter.label')}
          active={active}
          onSelect={(id) => toggle(id as QuickFilterId)}
          items={offered.map(({ f, n }) => ({ id: f.id, label: t(f.label), badge: n }))}
          after={
            active.size > 0 && (
              <Button kind="ghost" className="px-2 py-1 text-xs" onClick={() => onActive(new Set())}>
                {t('filter.clear')}
              </Button>
            )
          }
        />
      )}

      {narrowed && (
        <span className="glim-num text-xs text-carbon-textMuted">
          {t('search.shown', { n: shown, total: tasks.length })}
        </span>
      )}

      <span className="flex-1" />
      {right}
    </div>
  );
}

// --- The strip that appears with a selection ------------------------------

/**
 * SelectionStrip is the bulk-action bar. Every verb on it can act on what is
 * selected right now; the rest are not greyed out, they are simply not there.
 *
 * The long tail lives behind More, which opens the very same menu as a
 * right-click — so the menu is discoverable by people who never right-click, and
 * the strip stays a strip instead of becoming a wall of buttons.
 */
export function SelectionStrip({
  all,
  selected,
  onSelected,
  removal,
  onMore,
  children,
}: {
  all: Task[];
  selected: Set<string>;
  onSelected: (next: Set<string>) => void;
  removal: Removal;
  onMore: (at: MenuAnchor) => void;
  /** Actions only one of the two lists has: queue order, start, package moves. */
  children?: ReactNode;
}) {
  const { t } = useT();
  const chosen = useMemo(() => all.filter((x) => selected.has(x.id)), [all, selected]);
  if (chosen.length === 0) return null;

  const ids = chosen.map((x) => x.id);
  const onDisk = chosen.some((x) => x.loaded > 0);

  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="glim-num text-sm text-carbon-textSub">
        {chosen.length} {t('select.count')}
      </span>
      <Button kind="ghost" className="px-2.5 text-xs" onClick={() => onSelected(new Set())}>
        {t('select.none')}
      </Button>
      {children}
      <span className="flex-1" />
      <Button
        kind="ghost"
        className="px-2.5 text-xs"
        icon={<IconMore />}
        onClick={(e) => onMore(anchorBelow(e.currentTarget))}
      >
        {t('menu.more')}
      </Button>
      <Button
        kind="danger"
        className="px-2.5 text-xs"
        icon={<IconTrash width={15} height={15} />}
        onClick={() => void removal.removeNow(ids)}
      >
        {t('task.remove')}
      </Button>
      {/* Only when there is something on disk to erase, and never with the same
          treatment as the line above it. */}
      {onDisk && (
        <Button
          kind="danger"
          className="bg-statusFailBg px-2.5 text-xs"
          icon={<IconTrashFiles width={16} height={16} />}
          onClick={() => removal.askWithFiles(ids)}
        >
          {t('task.removeWithFiles')}
        </Button>
      )}
      <InfoBubble tip={t('remove.keys')} />
    </div>
  );
}

// --- The bar under the list -----------------------------------------------

/**
 * ListActionBar sits under the list and is always there, including when the list
 * is empty — which is exactly the moment a new user is looking for the way to
 * add something.
 */
export function ListActionBar({
  all,
  selected,
  onSelected,
  visible,
  local,
  children,
}: {
  all: Task[];
  selected: Set<string>;
  onSelected: (next: Set<string>) => void;
  /** The rows on screen right now — what "select all" means with a filter on. */
  visible: Task[];
  /**
   * Whether the list being shown belongs to this instance. Clean-up is not
   * forwarded to a peer, so on somebody else's list the entry would quietly act
   * on the wrong machine.
   */
  local: boolean;
  children?: ReactNode;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const menu = useContextMenu();
  const cleanup = useCleanup(all);

  const allChosen = visible.length > 0 && visible.every((x) => selected.has(x.id));

  async function openCleanup(el: HTMLButtonElement | null): Promise<void> {
    try {
      await cleanup.load();
      menu.openAt(anchorBelow(el));
    } catch {
      toast(t('list.optionsFailed'), 'fail');
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-2" role="group" aria-label={t('list.actions')}>
      <Button
        kind="ghost"
        className="px-2.5 text-xs"
        icon={<IconCheck width={15} height={15} />}
        disabled={visible.length === 0}
        onClick={() => onSelected(allChosen ? new Set() : new Set(visible.map((x) => x.id)))}
      >
        {allChosen ? t('select.none') : t('select.all')}
      </Button>

      <Button
        kind="ghost"
        className="px-2.5 text-xs"
        disabled={!local}
        onClick={(e) => void openCleanup(e.currentTarget)}
      >
        {t('cleanup.menu')}
        <IconChevronDown width={16} height={16} />
      </Button>
      {!local && <InfoBubble tip={t('cleanup.localOnly')} />}

      <span className="flex-1" />
      {children}

      {menu.anchor && cleanup.classes && (
        <ContextMenu
          anchor={menu.anchor}
          label={t('cleanup.menuLabel')}
          onClose={menu.close}
          groups={[{ id: 'cleanup', items: cleanupItems(cleanup.classes, t, (cls) => void cleanup.preview(cls)) }]}
        />
      )}

      {cleanup.dialog}
    </div>
  );
}

/**
 * classLabel names a clean-up class. A class this build has no label for is
 * shown by its own id rather than hidden: a newer server offering an entry we
 * cannot name is still an entry that works.
 */
function classLabel(cls: string, t: (key: TranslationKey) => string): string {
  const key = CLEANUP_LABEL[cls];
  return key ? t(key) : cls;
}

/** message is the server's own sentence when there is one, since these routes refuse with a reason. */
function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}
