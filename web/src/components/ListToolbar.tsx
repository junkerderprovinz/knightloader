// The controls around a download list: filters, selection actions and clean-up
// entries. Both lists mount the same pieces and pass in the quick filters that
// suit their rows.
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { PriorityGlyph } from '../lib/icons';
import {
  type ApiOptions,
  type BulkResult,
  type CleanupClass,
  type PriorityChoice,
  type QueueMove,
  type QueueState,
  type Task,
  type TaskOptionsPatch,
  type TaskStatus,
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
  setPackage,
  setQueue as armStopMark,
  setTaskOptions,
  startTasks,
  undoDelete,
} from '../lib/api';
import { fmtBytes } from '../lib/format';
import { happened } from '../lib/countdown';
import { useDialogMute, type DialogId } from '../lib/dialogmute';
import { useToast } from '../lib/toast';
import { useT, type TranslationKey } from '../lib/i18n';
import { Button, Field, Modal, NumberInput, TextInput } from './ui';
import { PackageMoveDialog } from './PackageActions';
import { retryPending } from './RetryCountdown';
import { SelectionReach } from './SelectionReach';
import {
  ContextMenu,
  anchorBelow,
  useContextMenu,
  type MenuAnchor,
  type MenuGroup,
  type MenuItem,
} from './ContextMenu';
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

/**
 * useQueueVerbs fetches, on mount rather than when the menu opens, the
 * server's priority choices and which task carries the stop mark.
 */
export function useQueueVerbs(base: string) {
  const [choices, setChoices] = useState<PriorityChoice[]>([]);
  const [queue, setQueue] = useState<QueueState | null>(null);

  useEffect(() => {
    let live = true;
    void priorityChoices().then(
      (p) => {
        if (live) setChoices(p);
      },
      () => {
        // The priority entry is left out of the menu.
      },
    );
    void fetchQueue(base).then(
      (q) => {
        if (live) setQueue(q);
      },
      () => {
        // The list already reports an unreachable peer.
      },
    );
    return () => {
      live = false;
    };
  }, [base]);

  // Only one task carries the mark, so the answer replaces the old state.
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

export type QueueVerbs = ReturnType<typeof useQueueVerbs>;

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
  | 'unchecked'
  | 'stalled';

export interface QuickFilter {
  id: QuickFilterId;
  label: TranslationKey;
  match: (t: Task) => boolean;
}

export const QUICK_FILTERS: QuickFilter[] = [
  // Extracting counts as running, since the download is not over yet.
  { id: 'running', label: 'filter.running', match: (t) => t.status === 'running' || t.status === 'extracting' },
  { id: 'queued', label: 'filter.queued', match: (t) => t.status === 'queued' },
  { id: 'paused', label: 'filter.paused', match: (t) => t.status === 'paused' },
  { id: 'finished', label: 'filter.finished', match: (t) => t.status === 'done' },
  { id: 'failed', label: 'filter.failed', match: (t) => t.status === 'error' },
  { id: 'online', label: 'filter.online', match: (t) => t.online === 'online' },
  { id: 'offline', label: 'filter.offline', match: (t) => t.online === 'offline' },
  // Separate from "offline": the host would not say, so these are worth
  // checking again rather than deleting.
  { id: 'uncheckable', label: 'filter.uncheckable', match: (t) => t.online === 'uncheckable' },
  { id: 'unchecked', label: 'filter.unchecked', match: (t) => !t.online },
  { id: 'disabled', label: 'filter.disabled', match: (t) => !t.enabled },
  { id: 'held', label: 'filter.held', match: (t) => !!t.hold },
  // A stalled row still counts as running everywhere else, so it gets its own
  // filter. happened() rather than `!!`, because a never-stalled task carries
  // Go's zero time, which is a truthy string.
  { id: 'stalled', label: 'filter.stalled', match: (t) => happened(t.stalledSince) },
];

/**
 * offeredQuickFilters counts each filter's matches and offers only those with
 * any, keeping an active filter at zero so its chip does not vanish. Exported
 * for pages that render the chips elsewhere.
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

/** The nine states a download list is actually filtered by. */
export const DOWNLOAD_FILTERS: QuickFilterId[] = [
  'running',
  'queued',
  'paused',
  'finished',
  'failed',
  'offline',
  'disabled',
  'held',
  'stalled',
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
 * matchesQuickFilters is a union: two filters on show both kinds, since the
 * states exclude each other.
 */
export function matchesQuickFilters(t: Task, active: Set<QuickFilterId>): boolean {
  if (active.size === 0) return true;
  return QUICK_FILTERS.some((f) => active.has(f.id) && f.match(t));
}

interface Weight {
  count: number;
  files: number;
  bytes: number;
}

/**
 * weigh counts what a removal would erase, in bytes already on disk rather
 * than announced sizes.
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
 * ConfirmRemove states what a removal takes and how much, before anything
 * goes. Removing rows can be undone by pasting again, erasing files cannot;
 * the labels and counts tell them apart, not a colour (GlimStone 1.12.0).
 */
function ConfirmRemove({
  title,
  what,
  weight,
  allowFiles,
  note,
  mute,
  reach,
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
  /** Which "do not show this again" switch this dialog carries, if any. */
  mute?: DialogId;
  /**
   * How many rows about to go are off screen, and the press that narrows the
   * removal to the visible ones. Only for a hand-picked selection: a clean-up
   * class comes from the server's preview and must not be narrowed.
   */
  reach?: { hidden: number; onReduce: () => void };
  onCancel: () => void;
  onConfirm: (withFiles: boolean) => void;
}) {
  const { t } = useT();
  const files = allowFiles && weight.files > 0;

  return (
    <Modal
      title={title}
      onClose={onCancel}
      mute={mute}
      footer={
        <>
          {/* Grouped at the end in order of how far each goes. Cancel is the
              way out and carries the close glyph through the label engine;
              the two answers are words. */}
          <span className="flex-1" />
          <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onCancel} />
          <Button kind="secondary" onClick={() => onConfirm(false)}>
            {t('remove.fromList')}
          </Button>
          {files && (
            <Button kind="secondary" onClick={() => onConfirm(true)}>
              {t('remove.withFiles')}
            </Button>
          )}
        </>
      }
    >
      <div className="flex flex-col gap-2 text-sm text-carbon-textSub">
        {what && <p>{what}</p>}
        {/* Above the count it qualifies. */}
        {reach && (
          <SelectionReach mode="removal" total={weight.count} hidden={reach.hidden} onReduce={reach.onReduce} />
        )}
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

/**
 * TaskOptionsDialog edits a selection's folder, archive password and
 * connection count. Only changed fields are sent, so a selection whose values
 * disagree opens empty without wiping them on save; a chunk count of 0 would
 * mean "back to the rules".
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
  // The shared value, or empty when the selection disagrees.
  const agreed = (pick: (x: Task) => string) => {
    const first = tasks.length > 0 ? pick(tasks[0]) : '';
    return tasks.every((x) => pick(x) === first) ? first : '';
  };
  // For the count, disagreement opens on 0, "no override".
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
          {/* The forward button ends the row, so the error goes first. */}
          {error && <span className="min-w-0 text-statusFail text-sm">{error}</span>}
          <span className="flex-1" />
          <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
          <Button onClick={apply}>{t('settings.save')}</Button>
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
      {/* The engine's bound, as in the rule editor and settings. */}
      <Field label={t('task.chunks')} hint={t('task.chunksHint')}>
        <NumberInput value={chunks} min={0} max={16} onValue={setChunks} />
      </Field>
    </Modal>
  );
}

/**
 * useRemoval takes a selection off a list and binds Del and Shift+Del.
 * Removing rows happens at once, with an undo in the toast, since the server
 * keeps them for a while (app.RemoveTasksUndoable); erasing files always asks
 * first with the file count and bytes. The toast's count also reveals rows
 * removed while hidden by a filter.
 */
export function useRemoval({
  all,
  selected,
  base,
  drawn,
  onDone,
}: {
  /** Every task this instance holds, so the byte total covers rows the filters hid. */
  all: Task[];
  selected: Set<string>;
  base: string;
  /**
   * The ids the list is drawing (lib/selectionReach.ts). Without it the hidden
   * counts are simply not shown.
   */
  drawn?: ReadonlySet<string>;
  onDone: () => void;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const dialogs = useDialogMute();
  const [ask, setAsk] = useState<string[] | null>(null);

  // An expired token answers 200 with nothing restored, which is "too late"
  // rather than a failure.
  const undoRemoval = useCallback(
    async (token: string) => {
      try {
        const r = await undoDelete(token, base);
        if (r.count > 0) toast(t('remove.undone', { n: r.count }), 'ok');
        else toast(t('remove.undoTooLate'), 'info');
      } catch (e) {
        toast(t('list.failed', { error: message(e) }), 'fail');
      }
    },
    [base, t, toast],
  );

  const removeNow = useCallback(
    async (ids: string[], withFiles = false) => {
      if (ids.length === 0) return;
      // Counted before the request, while the rows are still drawn.
      const unseen = drawn ? ids.filter((id) => !drawn.has(id)).length : 0;
      try {
        const r: BulkResult = await deleteTasks(ids, withFiles, base);
        // No token when files were erased, so no undo; the toast stays open as
        // long as the server keeps the rows (undoMs).
        const token = r.undo;
        toast(
          unseen > 0
            ? t('remove.doneHidden', { n: r.count }).replace('{hidden}', String(unseen))
            : t('remove.done', { n: r.count }),
          'ok',
          'action-done',
          token ? { label: t('remove.undo'), run: () => undoRemoval(token), holdMs: r.undoMs } : undefined,
        );
        onDone();
      } catch (e) {
        toast(t('list.failed', { error: message(e) }), 'fail');
      }
    },
    [base, drawn, onDone, t, toast, undoRemoval],
  );

  // With the dialog muted, this deletes with files at once, as the mute agreed.
  const askWithFiles = useCallback(
    (ids: string[]) => {
      if (ids.length === 0) return;
      if (dialogs.isMuted('remove')) {
        void removeNow(ids, true);
        return;
      }
      setAsk(ids);
    },
    [dialogs, removeNow],
  );

  // Ignored while typing, or editing a search would delete downloads.
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
      mute="remove"
      reach={
        drawn && {
          hidden: ask.filter((id) => !drawn.has(id)).length,
          // Narrows the dialog's own ids, not the page selection, so a
          // cancelled removal leaves the selection intact.
          onReduce: () => setAsk(ask.filter((id) => drawn.has(id))),
        }
      }
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

// The clean-up classes come from the server once per session, so the menu
// never offers a class the server lacks.
let optionsOnce: Promise<ApiOptions> | null = null;

function cleanupClasses(): Promise<CleanupClass[]> {
  if (!optionsOnce) optionsOnce = fetchOptions();
  return optionsOnce.then(
    (o) => o.cleanupClasses ?? [],
    (e) => {
      // A failed load is retried next time.
      optionsOnce = null;
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

// Classes that never offer to erase files. Clearing finished downloads is a
// daily action, and one wrong click would erase a finished library.
const KEEPS_FILES = new Set<string>(['finished']);

/**
 * useCleanup fetches the server's clean-up classes, previews what one would
 * take, then confirms. Each caller (the bar, the menu, the downloads command)
 * keeps its own instance.
 */
export function useCleanup(all: Task[]) {
  const { t } = useT();
  const { toast } = useToast();
  const dialogs = useDialogMute();
  const [classes, setClasses] = useState<CleanupClass[] | null>(null);
  const [confirm, setConfirm] = useState<{ cls: CleanupClass; ids: string[] } | null>(null);

  const load = useCallback(async (): Promise<CleanupClass[]> => {
    const list = await cleanupClasses();
    setClasses(list);
    return list;
  }, []);

  // Declared before preview(), which calls it when the dialog is muted.
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

  // The class picks the rows, so the preview's count is what gets confirmed.
  const preview = useCallback(
    async (cls: CleanupClass): Promise<void> => {
      try {
        const r = await cleanupPreview(cls);
        if (r.count === 0) {
          toast(t('cleanup.nothing', { what: classLabel(cls, t) }), 'info');
          return;
        }
        // Muted, it runs without files, the conservative answer.
        if (dialogs.isMuted('cleanup')) {
          void run(cls, false);
          return;
        }
        setConfirm({ cls, ids: r.ids });
      } catch (e) {
        toast(t('cleanup.failed', { error: message(e) }), 'fail');
      }
    },
    [dialogs, run, t, toast],
  );

  const dialog = confirm && (
    <ConfirmRemove
      title={classLabel(confirm.cls, t)}
      what={CLEANUP_WHAT[confirm.cls] ? t(CLEANUP_WHAT[confirm.cls]!) : undefined}
      weight={weigh(all, confirm.ids)}
      allowFiles={!KEEPS_FILES.has(confirm.cls)}
      note={KEEPS_FILES.has(confirm.cls) ? t('cleanup.finishedKeepsFiles') : undefined}
      mute="cleanup"
      onCancel={() => setConfirm(null)}
      onConfirm={(withFiles) => void run(confirm.cls, withFiles)}
    />
  );

  return { classes, load, preview, dialog };
}

export type CleanupState = ReturnType<typeof useCleanup>;

/** cleanupItems turns the server's clean-up classes into menu entries. */
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

/**
 * MenuTarget is what a right-click landed on: a link row or the More button
 * (the selection), a package header (the same verbs over the package, plus the
 * fold), or empty space (entries for the list itself).
 */
export type MenuTarget =
  | { kind: 'selection' }
  | { kind: 'package'; name: string }
  | { kind: 'list' };

// The menu's accessible name per target, so a package menu does not claim to
// act on "the selected downloads".
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
   * Whether the list is the local instance. Clean-up is not forwarded to peers,
   * so on a peer's list those entries are left out.
   */
  local: boolean;
}

/**
 * MOVE_STATES mirrors movable() in internal/app/app_queue.go: every status but
 * done and error, which have no place left in the wait order.
 * check-queue-reach.mjs keeps the two in step.
 */
export const MOVE_STATES: readonly TaskStatus[] = [
  'collected',
  'queued',
  'running',
  'paused',
  'extracting',
];

/**
 * PRIORITY_STATES is every status, since SetPriorityIn filters none. On a done
 * or failed task the priority takes effect after a restart, which keeps it
 * (TestPriorityOnAFinishedTaskSurvivesItsRestart). A separate list from
 * MOVE_STATES because the server answers the two questions differently.
 */
export const PRIORITY_STATES: readonly TaskStatus[] = [
  'collected',
  'queued',
  'running',
  'paused',
  'extracting',
  'done',
  'error',
];

/**
 * STOP_MARK_STATES are the statuses that still have the done transition ahead,
 * which is when the mark fires (app_dispatch.go). An extracting task's download
 * is already over.
 */
export const STOP_MARK_STATES: readonly TaskStatus[] = ['collected', 'queued', 'running', 'paused'];

/**
 * queueMenuGroup builds the wait-order entries for a selection: move, priority
 * and the stop mark. The context menu and Downloads.tsx's "Queue order" badge
 * share it so they offer the same verbs through the same calls. `queue` comes
 * from useQueueVerbs.
 */
export function queueMenuGroup({
  chosen,
  ids,
  base,
  t,
  fail,
  queue,
}: {
  chosen: Task[];
  ids: string[];
  base: string;
  t: (key: TranslationKey, vars?: Record<string, string | number>) => string;
  fail: (e: unknown) => void;
  queue: QueueVerbs;
}): MenuGroup {
  const guard = (run: () => Promise<unknown>) => () => {
    void run().catch(fail);
  };
  const some = (p: (x: Task) => boolean) => chosen.some(p);

  // Separate gates for moving and priority; see MOVE_STATES and PRIORITY_STATES.
  const queueGroup: MenuGroup = { id: 'queue', items: [] };
  if (some((x) => MOVE_STATES.includes(x.status))) {
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
  }

  // A submenu, as in JDownloader. The check marks the value the whole selection
  // shares, if any. The rungs are the server's own choices.
  if (queue.choices.length > 0 && some((x) => PRIORITY_STATES.includes(x.status))) {
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

  // There is one stop mark, so it is offered for a single row only and toggles.
  if (chosen.length === 1 && STOP_MARK_STATES.includes(chosen[0].status)) {
    const only = chosen[0];
    const armed = queue.stopMark === only.id;
    queueGroup.items.push({
      id: 'stopMark',
      label: t(armed ? 'queue.stopMarkOn' : 'queue.stopMark'),
      icon: <IconStopMark />,
      onSelect: () => void queue.mark(armed ? '' : only.id).catch(fail),
    });
  }

  return queueGroup;
}

/**
 * taskMenuGroups builds the verbs for a selection of any size. An entry that
 * applies to none of the selected rows is left out rather than greyed.
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
  // Start admits staged links and can lift a manual halt. Forcing puts links
  // already in the queue ahead of everything else, and is refused while the
  // queue is stopped. It stays offered on forced links, to reclaim the front.
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
  // No bulk pause route, so each running task is paused on its own.
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
  // Runs the already scheduled retry now, spending it rather than granting a
  // new one. Sent for the waiting rows only, since restart would also start
  // finished downloads over.
  const waitingIds = chosen.filter(retryPending).map((x) => x.id);
  if (waitingIds.length > 0)
    transport.items.push({
      id: 'retryNow',
      label: t('task.retry.skip'),
      icon: <IconBolt />,
      // How many selected rows are waiting, when not all of them are.
      detail: waitingIds.length < chosen.length ? String(waitingIds.length) : undefined,
      onSelect: () => void restartTasks(waitingIds, base),
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

  const queueGroup = queueMenuGroup({ chosen, ids, base, t, fail, queue });

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

  // Both open TaskOptionsDialog, focused on the named box.
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

  // Not coloured as faults; the glyph, label and position say enough.
  const gone: MenuGroup = {
    id: 'remove',
    items: [
      {
        id: 'remove',
        label: t('task.remove'),
        detail: 'Del',
        icon: <IconTrash width={14} height={14} />,
        onSelect: () => void removal.removeNow(ids),
      },
      {
        id: 'removeFiles',
        label: t('task.removeWithFiles'),
        detail: 'Shift+Del',
        icon: <IconTrashFiles />,
        onSelect: () => removal.askWithFiles(ids),
      },
    ],
  };

  return [transport, queueGroup, state, options, gone];
}

/** targetTaskId reads the `data-task-id` of the row a right-click landed on. */
export function targetTaskId(e: { target: EventTarget | null }): string | null {
  const el = e.target instanceof Element ? e.target.closest('[data-task-id]') : null;
  return el?.getAttribute('data-task-id') ?? null;
}

/**
 * targetPackage returns the name of the package header a right-click landed
 * on, or null. The ungrouped package's name is the empty string.
 */
export function targetPackage(e: { target: EventTarget | null }): string | null {
  const el = e.target instanceof Element ? e.target.closest('[data-package-row]') : null;
  if (!el) return null;
  return el.getAttribute('data-package-row') ?? '';
}

/**
 * ListMenu is the page's one menu: the same groups whether it was opened by
 * right-click on a link, on a package header, on empty space, or from the
 * selection strip's More button. It stays mounted while nothing is open so the
 * dialogs it raises outlive it; the menu closes before an entry runs, and a
 * dialog would otherwise open underneath it.
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
  // The rows whose package is being renamed/merged right now - see the
  // 'movePackage' entry below and PackageMoveDialog, which this shares with
  // the selection row's own folder badge rather than reimplementing.
  const [movePkg, setMovePkg] = useState<Task[] | null>(null);

  const chosen = useMemo(() => all.filter((x) => selected.has(x.id)), [all, selected]);
  // Every package name on screen, for the dialogue's datalist: moving into a
  // package that already exists should be a pick, not a re-typing exercise.
  const knownPackages = useMemo(
    () => [...new Set(all.map((x) => x.package).filter((p) => p !== ''))].sort(),
    [all],
  );

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
          // A cross, not a struck-through tick: the row above sets the
          // selection and the two have to be told apart at a glance.
          icon: <IconClose />,
          onSelect: list.onSelectNone,
        });
      groups.push({ id: 'select', items: whole });
    }

    // Whole-list folding sits on the empty space, not beside a package's own
    // fold, so "this one" and "all of them" are never one word apart.
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
      // Moving the selection into a package a person names themselves, first
      // in the list of things to do with a selection because that is what
      // JDownloader's own right-click opens with. The folder glyph in the
      // selection row above the list offers the same thing and nobody finds it
      // there.
      {
        id: 'organise',
        items: [
          {
            id: 'movePackage',
            label: t('pkg.moveTitle'),
            icon: <IconFolder width={14} height={14} />,
            onSelect: () => setMovePkg(chosen),
          },
        ],
      },
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
      {movePkg && (
        <PackageMoveDialog
          count={movePkg.length}
          suggestion={movePkg[0]?.package ?? ''}
          known={knownPackages}
          onClose={() => setMovePkg(null)}
          onApply={(name) => {
            setMovePkg(null);
            setPackage(movePkg.map((x) => x.id), name, base).catch(fail);
          }}
        />
      )}
      {cleanup.dialog}
    </>
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
