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
  type Task,
  cleanupPreview,
  deleteTasks,
  fetchOptions,
  recheckTasks,
  restartTasks,
  runCleanup,
  setEnabled,
  setForced,
  setHold,
  startTasks,
} from '../lib/api';
import { fmtBytes } from '../lib/format';
import { useToast } from '../lib/toast';
import { useT, type TranslationKey } from '../lib/i18n';
import { Button, InfoBubble, Modal, segBase, segOn, segOff } from './ui';
import {
  ContextMenu,
  anchorBelow,
  useContextMenu,
  type MenuAnchor,
  type MenuGroup,
} from './ContextMenu';
import { SearchField, type SearchQuery } from './SearchField';
import { IconCheck, IconPlay, IconRetry, IconSearch, IconTrash } from '../lib/icons';

// A handful of glyphs the shared set does not carry yet. They live here rather
// than in lib/icons.tsx because that file belongs to another lane this wave;
// they follow its geometry so they can be lifted across unchanged.
const glyph = (p: SVGProps<SVGSVGElement>) => ({
  width: 16,
  height: 16,
  viewBox: '0 0 20 20',
  fill: 'none',
  className: 'shrink-0',
  'aria-hidden': true,
  ...p,
});

const IconChevronDown = (p: SVGProps<SVGSVGElement>) => (
  <svg {...glyph(p)} stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round">
    <path d="M6 8l4 4 4-4" />
  </svg>
);

const IconMore = (p: SVGProps<SVGSVGElement>) => (
  <svg {...glyph(p)} fill="currentColor">
    <circle cx="5" cy="10" r="1.4" />
    <circle cx="10" cy="10" r="1.4" />
    <circle cx="15" cy="10" r="1.4" />
  </svg>
);

// Removing the rows and erasing the files are different acts with very different
// regret profiles, so they never share a glyph. This is the trash with the
// contents struck out.
const IconTrashFiles = (p: SVGProps<SVGSVGElement>) => (
  <svg {...glyph(p)} stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
    <path d="M4 5.5h12M8 5.5V4a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v1.5M6 5.5l.7 10a1 1 0 0 0 1 .9h4.6a1 1 0 0 0 1-.9l.7-10" />
    <path d="M8.4 9l3.2 4M11.6 9l-3.2 4" />
  </svg>
);

const IconPower = (p: SVGProps<SVGSVGElement>) => (
  <svg {...glyph(p)} stroke="currentColor" strokeWidth={1.6} strokeLinecap="round">
    <path d="M10 3v6" />
    <path d="M6.1 5.7a5 5 0 1 0 7.8 0" />
  </svg>
);

const IconPin = (p: SVGProps<SVGSVGElement>) => (
  <svg {...glyph(p)} stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
    <path d="M8 3h4l-.7 4.2 2.7 2.4H6l2.7-2.4z" />
    <path d="M10 9.6V17" />
  </svg>
);

const IconBolt = (p: SVGProps<SVGSVGElement>) => (
  <svg {...glyph(p)} fill="currentColor">
    <path d="M11.3 2.4 4.8 11.2h3.6L7.6 17.6 15.2 8.8h-3.7z" />
  </svg>
);

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

interface QuickFilter {
  id: QuickFilterId;
  label: TranslationKey;
  match: (t: Task) => boolean;
}

const QUICK_FILTERS: QuickFilter[] = [
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
            <Button kind="danger" className="bg-statusFailBg" icon={<IconTrashFiles />} onClick={() => onConfirm(true)}>
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

// --- The menu a list offers on a selection --------------------------------

/**
 * taskMenuGroups builds the standard groups for a selection. Later waves add
 * their own groups beside these rather than editing this list — the menu shell
 * takes any number of them.
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
}: {
  chosen: Task[];
  ids: string[];
  base: string;
  t: (key: TranslationKey, vars?: Record<string, string | number>) => string;
  fail: (e: unknown) => void;
  removal: Removal;
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
  if (some((x) => !x.forced && x.status !== 'done'))
    state.items.push({
      id: 'force',
      label: t('menu.force'),
      icon: <IconBolt />,
      onSelect: guard(() => setForced(ids, true, base)),
    });
  if (some((x) => !!x.forced))
    state.items.push({
      id: 'unforce',
      label: t('menu.unforce'),
      icon: <IconBolt />,
      onSelect: guard(() => setForced(ids, false, base)),
    });

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

  return [transport, state, gone];
}

/**
 * targetTaskId finds the row a right-click landed on.
 *
 * It reads `data-task-id`, which the row component sets. While no row carries
 * one the menu falls back to the current selection, which is the useful answer
 * anyway: nothing here has to change when the attribute appears.
 */
export function targetTaskId(e: { target: EventTarget | null }): string | null {
  const el = e.target instanceof Element ? e.target.closest('[data-task-id]') : null;
  return el?.getAttribute('data-task-id') ?? null;
}

/**
 * ListMenu is the page's one menu: the same groups whether it was opened by
 * right-click or by the selection strip's More button.
 */
export function ListMenu({
  anchor,
  onClose,
  all,
  selected,
  base,
  removal,
  extraGroups = [],
}: {
  anchor: MenuAnchor | null;
  onClose: () => void;
  all: Task[];
  selected: Set<string>;
  base: string;
  removal: Removal;
  /** Groups another wave contributes, appended after the standard ones. */
  extraGroups?: MenuGroup[];
}) {
  const { t } = useT();
  const { toast } = useToast();
  const fail = useCallback((e: unknown) => toast(t('list.failed', { error: message(e) }), 'fail'), [t, toast]);

  const chosen = useMemo(() => all.filter((x) => selected.has(x.id)), [all, selected]);
  if (!anchor || chosen.length === 0) return null;

  const groups = [
    ...taskMenuGroups({ chosen, ids: chosen.map((x) => x.id), base, t, fail, removal }),
    ...extraGroups,
  ];
  return <ContextMenu anchor={anchor} groups={groups} label={t('menu.label')} onClose={onClose} />;
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
}) {
  const { t } = useT();

  const offered = useMemo(() => {
    const defs = filters
      .map((id) => QUICK_FILTERS.find((f) => f.id === id))
      .filter((f): f is QuickFilter => !!f);
    return defs
      .map((f) => ({ f, n: tasks.reduce((s, x) => s + (f.match(x) ? 1 : 0), 0) }))
      .filter(({ f, n }) => n > 0 || active.has(f.id));
  }, [filters, tasks, active]);

  const narrowed = active.size > 0 || search.text.trim() !== '';

  function toggle(id: QuickFilterId): void {
    const next = new Set(active);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    onActive(next);
  }

  return (
    <div className="flex flex-wrap items-center gap-2" role="group" aria-label={t('list.controls')}>
      <SearchField value={search} onChange={onSearch} className="max-w-md flex-1" />

      {offered.length > 0 && (
        <div className="glim-well flex flex-wrap items-center gap-0.5 p-1" role="group" aria-label={t('filter.label')}>
          {offered.map(({ f, n }) => (
            <button
              key={f.id}
              type="button"
              aria-pressed={active.has(f.id)}
              onClick={() => toggle(f.id)}
              className={`${segBase} flex items-center gap-1.5 px-2.5 py-1 text-xs ${
                active.has(f.id) ? segOn : segOff
              }`}
            >
              {t(f.label)}
              <span className="glim-num opacity-70">{n}</span>
            </button>
          ))}
          {active.size > 0 && (
            <Button kind="ghost" className="px-2 py-1 text-xs" onClick={() => onActive(new Set())}>
              {t('filter.clear')}
            </Button>
          )}
        </div>
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
          icon={<IconTrashFiles />}
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
  const [classes, setClasses] = useState<CleanupClass[] | null>(null);
  const [confirm, setConfirm] = useState<{ cls: CleanupClass; ids: string[] } | null>(null);

  const allChosen = visible.length > 0 && visible.every((x) => selected.has(x.id));

  async function openCleanup(el: HTMLButtonElement | null): Promise<void> {
    try {
      const list = classes ?? (await cleanupClasses());
      setClasses(list);
      menu.openAt(anchorBelow(el));
    } catch {
      toast(t('list.optionsFailed'), 'fail');
    }
  }

  // The preview and the confirmation are one gesture: the class picks the rows,
  // so the count is the only thing that can tell the user what they are about to
  // agree to. Nothing is removed by opening this.
  async function preview(cls: CleanupClass): Promise<void> {
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
  }

  async function run(cls: CleanupClass, withFiles: boolean): Promise<void> {
    setConfirm(null);
    try {
      const r = await runCleanup(cls, withFiles);
      toast(t('remove.done', { n: r.count }), 'ok');
    } catch (e) {
      toast(t('cleanup.failed', { error: message(e) }), 'fail');
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
        <IconChevronDown />
      </Button>
      {!local && <InfoBubble tip={t('cleanup.localOnly')} />}

      <span className="flex-1" />
      {children}

      {menu.anchor && classes && (
        <ContextMenu
          anchor={menu.anchor}
          label={t('cleanup.menuLabel')}
          onClose={menu.close}
          groups={[
            {
              id: 'cleanup',
              items: classes.map((cls) => ({
                id: cls,
                label: classLabel(cls, t),
                icon: <IconTrash width={14} height={14} />,
                onSelect: () => void preview(cls),
              })),
            },
          ]}
        />
      )}

      {confirm && (
        <ConfirmRemove
          title={classLabel(confirm.cls, t)}
          what={CLEANUP_WHAT[confirm.cls] ? t(CLEANUP_WHAT[confirm.cls]!) : undefined}
          weight={weigh(all, confirm.ids)}
          allowFiles={!KEEPS_FILES.has(confirm.cls)}
          note={KEEPS_FILES.has(confirm.cls) ? t('cleanup.finishedKeepsFiles') : undefined}
          onCancel={() => setConfirm(null)}
          onConfirm={(withFiles) => void run(confirm.cls, withFiles)}
        />
      )}
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
