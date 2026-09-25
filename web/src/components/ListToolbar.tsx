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
  apiBase,
  cleanupPreview,
  connectWS,
  deleteTasks,
  fetchOptions,
  priorityChoices,
  fetchQueue,
  pauseTasks,
  queueForce,
  queueMove,
  queuePriority,
  recheckTasks,
  restartTasks,
  resumeTasks,
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
import { readShortcutOverrides } from '../lib/commands/overrides';
import { formatShortcut } from '../lib/commands/shortcuts';
import { Button, Field, IconBadge, Modal, NumberInput, TextInput } from './ui';
import { PathInput } from './FolderPicker';
import { PackageMoveDialog } from './PackageActions';
import { RenameLinkDialog, RenamePackageDialog } from './RenameDialog';
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
  IconEdit,
  IconFolder,
  IconKey,
  IconMore,
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
 * server's priority choices and which task carries the stop mark. A page
 * holds one and hands it to its right-click menu, so the row that wears the
 * mark and the menu entry that sets it read the same state.
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

  // This instance's server announces every change to the mark, including the
  // ones nobody pressed here: another window setting it, or the marked
  // download finishing. A peer's stream is not ours to read; there a finished
  // download is recognised by its row instead (RowMarks).
  useEffect(() => {
    if (base !== apiBase('')) return;
    return connectWS((type, data) => type === 'queue' && setQueue(data as QueueState), ['queue']);
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
          {error && <span dir="auto" className="min-w-0 text-statusFail text-sm">{error}</span>}
          <span className="flex-1" />
          <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
          <Button onClick={apply}>{t('settings.save')}</Button>
        </>
      }
    >
      <Field label={t('task.folder')} hint={t('settings.downloadDirHint')}>
        <PathInput
          autoFocus={focus === 'dir'}
          value={dir}
          title={t('task.folder')}
          local={base === '/api'}
          onValue={setDir}
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
            ? t('remove.doneHidden', { n: r.count, hidden: unseen })
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

/** RENAME_SHORTCUT is the rename commands' default key, JDownloader's own. */
export const RENAME_SHORTCUT = 'f2';

type RenameTarget = { kind: 'link'; id: string } | { kind: 'package'; name: string };

/**
 * useRename opens the rename window for one link or one package, from the
 * menu or from the page's rename command. The window reads its rows on every
 * render, so a download that finishes while it is open changes what it says.
 */
export function useRename({
  all,
  base,
  members,
  command,
}: {
  all: Task[];
  base: string;
  /** Every link of a package in the page's list; see ListContext.members. */
  members: (pkg: string) => Task[];
  /** The page's rename command, whose binding the menu entry shows. */
  command: string;
}) {
  const { t } = useT();
  const [target, setTarget] = useState<RenameTarget | null>(null);
  const openLink = useCallback((id: string) => setTarget({ kind: 'link', id }), []);
  const openPackage = useCallback((name: string) => setTarget({ kind: 'package', name }), []);
  const close = useCallback(() => setTarget(null), []);

  // What has the keyboard decides, a package header or a link row, and
  // otherwise a selection of exactly one link. The ungrouped rows have no
  // package name to change.
  const fromKeyboard = useCallback(
    (selection: readonly string[]) => {
      const focus = { target: document.activeElement };
      const pkg = targetPackage(focus);
      if (pkg !== null) {
        if (pkg !== '') openPackage(pkg);
        return;
      }
      const id = targetTaskId(focus) ?? (selection.length === 1 ? selection[0] : null);
      if (id) openLink(id);
    },
    [openLink, openPackage],
  );

  const binding = readShortcutOverrides()[command] ?? RENAME_SHORTCUT;

  let dialog: ReactNode = null;
  if (target?.kind === 'link') {
    const task = all.find((x) => x.id === target.id);
    if (task) dialog = <RenameLinkDialog key={task.id} task={task} base={base} onClose={close} />;
  } else if (target?.kind === 'package') {
    dialog = (
      <RenamePackageDialog
        key={target.name}
        name={target.name}
        tasks={members(target.name)}
        base={base}
        onClose={close}
      />
    );
  }

  return { openLink, openPackage, fromKeyboard, shortcut: binding ? formatShortcut(binding, t) : undefined, dialog };
}

export type Rename = ReturnType<typeof useRename>;

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
 * MenuTarget is what a right-click landed on: a link row (the selection), a
 * package header (the same verbs over the package, plus the fold), or empty
 * space (entries for the list itself).
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
  /**
   * Every link of a package in this list, rows a filter hides included. A
   * package header stands for all of them, so its menu pauses, switches and
   * renames all of them.
   */
  members: (pkg: string) => Task[];
}

/**
 * FORCE_STATES are the statuses "start now" is offered for: the ones still
 * waiting for their turn. A running download has no turn left to skip, and a
 * failed one needs a restart, which ForceDownload does not do.
 */
export const FORCE_STATES: readonly TaskStatus[] = ['collected', 'queued', 'paused'];

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
  scope,
  base,
  t,
  fail,
  removal,
  queue,
  onOptions,
}: {
  chosen: Task[];
  ids: string[];
  /**
   * What pausing and the switches act on: the selection, or a whole package
   * when the menu came from its header.
   */
  scope: Task[];
  base: string;
  t: (key: TranslationKey, vars?: Record<string, string | number>) => string;
  fail: (e: unknown) => void;
  removal: Removal;
  /** The priorities this server offers and where the stop mark currently sits. */
  queue: QueueVerbs;
  onOptions: (focus: 'dir' | 'password') => void;
}): MenuGroup[] {
  const some = (p: (x: Task) => boolean) => chosen.some(p);
  const idsInScope = (p: (x: Task) => boolean) => scope.filter(p).map((x) => x.id);
  const scopeIds = scope.map((x) => x.id);
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
  // Start admits staged links and can lift a manual halt. Start now puts the
  // waiting ones ahead of everything else and past the limit of concurrent
  // downloads, and on a stopped queue it starts them while everything else
  // waits. It stays offered on forced links, to reclaim the front.
  if (some((x) => FORCE_STATES.includes(x.status)))
    transport.items.push({
      id: 'force',
      label: t('menu.forceStart'),
      icon: <IconBolt />,
      onSelect: guard(() => queueForce({ ids }, base)),
    });
  if (some((x) => !!x.forced))
    transport.items.push({
      id: 'unforce',
      label: t('menu.unforce'),
      icon: <IconBolt />,
      onSelect: guard(() => setForced(ids, false, base)),
    });
  // Waiting links are paused too, or the slots the running ones free would
  // start them. Unpacking is past pausing: the download is over.
  const pausable = idsInScope((x) => x.status === 'running' || x.status === 'queued');
  if (pausable.length > 0)
    transport.items.push({
      id: 'pause',
      label: t('task.pause'),
      icon: <IconPause width={14} height={14} />,
      onSelect: guard(() => pauseTasks(pausable, base)),
    });
  const resumable = idsInScope((x) => x.status === 'paused');
  if (resumable.length > 0)
    transport.items.push({
      id: 'resume',
      label: t('task.resume'),
      icon: <IconPlay width={14} height={14} />,
      onSelect: guard(() => resumeTasks(resumable, base)),
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
  if (scope.some((x) => !x.enabled))
    state.items.push({
      id: 'enable',
      label: t('menu.enable'),
      icon: <IconPower />,
      onSelect: guard(() => setEnabled(scopeIds, true, base)),
    });
  if (scope.some((x) => x.enabled))
    state.items.push({
      id: 'disable',
      label: t('menu.disable'),
      icon: <IconPower />,
      onSelect: guard(() => setEnabled(scopeIds, false, base)),
    });
  if (scope.some((x) => !x.hold))
    state.items.push({
      id: 'hold',
      label: t('menu.hold'),
      icon: <IconPin />,
      onSelect: guard(() => setHold(scopeIds, true, base)),
    });
  if (scope.some((x) => !!x.hold))
    state.items.push({
      id: 'release',
      label: t('menu.release'),
      icon: <IconPin />,
      onSelect: guard(() => setHold(scopeIds, false, base)),
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
      removeWithFilesItem(ids, removal, t),
    ],
  };

  return [transport, queueGroup, state, options, gone];
}

/** The entry that erases files, the same in the right-click menu and under More. */
function removeWithFilesItem(ids: string[], removal: Removal, t: (key: TranslationKey) => string): MenuItem {
  return {
    id: 'removeFiles',
    label: t('task.removeWithFiles'),
    detail: 'Shift+Del',
    icon: <IconTrashFiles />,
    onSelect: () => removal.askWithFiles(ids),
  };
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
 * ListMenu is the page's right-click menu: the same groups whether it was
 * opened on a link, on a package header or on empty space. It stays mounted
 * while nothing is open so the dialogs it raises outlive it; the menu closes
 * before an entry runs, and a dialog would otherwise open underneath it.
 */
export function ListMenu({
  anchor,
  onClose,
  all,
  selected,
  base,
  removal,
  queue,
  target = { kind: 'selection' },
  list,
  rename,
  extraGroups = [],
}: {
  anchor: MenuAnchor | null;
  onClose: () => void;
  all: Task[];
  selected: Set<string>;
  base: string;
  removal: Removal;
  /** The page's useQueueVerbs, whose stop mark the rows show. */
  queue: QueueVerbs;
  /** What the pointer landed on. Defaults to the selection. */
  target?: MenuTarget;
  list?: ListContext;
  /** The page's rename window, whose dialog the page renders. */
  rename?: Rename;
  /** Groups another wave contributes, appended after the standard ones. */
  extraGroups?: MenuGroup[];
}) {
  const { t } = useT();
  const { toast } = useToast();
  const fail = useCallback((e: unknown) => toast(t('list.failed', { error: message(e) }), 'fail'), [t, toast]);
  const cleanup = useCleanup(all);
  const [options, setOptions] = useState<{ tasks: Task[]; focus: 'dir' | 'password' } | null>(null);
  // The rows being moved into a package right now. PackageMoveDialog is also
  // the window of the selection row's More menu, shared rather than rebuilt.
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
    // Renaming and moving into a package open the menu, as moving does in
    // JDownloader's own right-click. A package header renames its package and
    // a link row the link, when it is the only one selected: one name for
    // several files would point them all at one destination. Moving is also
    // under More in the selection row above the list, which is not where
    // people look for it.
    let renameThis: (() => void) | null = null;
    if (rename && target.kind === 'package' && target.name !== '') {
      const name = target.name;
      renameThis = () => rename.openPackage(name);
    } else if (rename && target.kind === 'selection' && chosen.length === 1) {
      const id = chosen[0].id;
      renameThis = () => rename.openLink(id);
    }
    const organise: MenuItem[] = [];
    if (renameThis)
      organise.push({
        id: 'rename',
        label: t('rename.menu'),
        detail: rename?.shortcut,
        icon: <IconEdit />,
        onSelect: renameThis,
      });
    organise.push({
      id: 'movePackage',
      label: t('pkg.moveTitle'),
      icon: <IconFolder width={14} height={14} />,
      onSelect: () => setMovePkg(chosen),
    });

    // A header stands for its whole package, rows a filter hides included,
    // unless a larger selection holds it; then the selection is what the
    // menu acts on.
    const wholePackage =
      target.kind === 'package' && list && chosen.every((x) => (x.package || '') === target.name)
        ? list.members(target.name)
        : null;

    groups.push(
      { id: 'organise', items: organise },
      ...taskMenuGroups({
        chosen,
        ids: chosen.map((x) => x.id),
        scope: wholePackage ?? chosen,
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
 * SelectionMore is the selection row's last badge and the menu under it, for
 * the verbs too rare to earn a badge of their own: the caller's groups, then
 * deleting the files, offered while something selected has bytes on disk.
 * Deleting stays apart from Remove, which acts at once and can be undone.
 */
export function SelectionMore({
  groups,
  chosen,
  removal,
  hue,
}: {
  groups: MenuGroup[];
  chosen: Task[];
  removal: Removal;
  hue?: number;
}) {
  const { t } = useT();
  const menu = useContextMenu();
  const onDisk = chosen.some((x) => x.loaded > 0);
  const all: MenuGroup[] = [
    ...groups,
    { id: 'removeFiles', items: onDisk ? [removeWithFilesItem(chosen.map((x) => x.id), removal, t)] : [] },
  ];
  if (!all.some((g) => g.items.length > 0)) return null;

  return (
    <>
      <IconBadge
        labelled
        hue={hue}
        icon={<IconMore width={16} height={16} />}
        title={t('menu.more')}
        aria-label={t('menu.more')}
        aria-haspopup="menu"
        aria-expanded={!!menu.anchor}
        onClick={(e) => menu.openAt(anchorBelow(e.currentTarget))}
      />
      {menu.anchor && <ContextMenu anchor={menu.anchor} label={t('menu.label')} onClose={menu.close} groups={all} />}
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
