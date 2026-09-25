import {
  Fragment,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type DragEvent,
  type KeyboardEvent,
  type PointerEvent,
} from 'react';
import { createPortal } from 'react-dom';
import {
  priorityChoices,
  type PriorityChoice,
  type Task,
  type TaskOptionsPatch,
  type YtdlpHosterPreset,
  type YtdlpVariantKind,
  YTDLP_VARIANT_KINDS,
  fetchHosterPreset,
  fetchOptions,
  saveHosterPreset,
} from '../lib/api';
import { hueVars } from '../lib/appearance';
import {
  pause,
  resume,
  remove,
  startTasks,
  restartTasks,
  recheckTasks,
  setPackage,
  setTaskOptions,
  reorderTasks,
  // Aliased: TaskProperties further down owns local useState setters called
  // setPriority/setForced, and a module-scope import of the same two names
  // would read as those inside it.
  setPriority as setTaskPriority,
  setForced as setTaskForced,
} from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { useUIState } from '../lib/uistate';
import {
  Button,
  Card,
  Field,
  FieldGroup,
  ErrorCard,
  IconBadge,
  InfoBubble,
  LoadingCard,
  Modal,
  SectionTitle,
  TextArea,
  TextInput,
  Toggle,
  useTooltip,
} from './ui';
import { Tabs } from './Tabs';
import { PathInput } from './FolderPicker';
import {
  NO_PRESET_MENUS,
  VariantDropdown,
  presetMenusOf,
  presetPickers,
  type PickerProps,
  type PresetMenus,
} from './VariantPicker';
import { ColumnMenu } from './ColumnMenu';
import {
  COLUMN_BY_ID,
  FOLDER_GLYPH,
  Tip,
  TREE_INDENT,
  VARIANT_KIND_LABEL_KEY,
  applySort,
  gridTemplate,
  moveColumn,
  nextSort,
  resolveLayout,
  sharedPriority,
  toStored,
  usePriorityNames,
  variantKindOf,
  PriorityTag,
  type CellContext,
  type ColumnDef,
  type ColumnId,
  type ColumnLayout,
  type ListProfile,
  type ResolvedLayout,
  type SortState,
} from './columns';
import { RetrySkipBadge } from './RetryCountdown';
import { TaskDetailPanel } from './taskdetail/TaskDetailPanel';
import { useListKeyboard } from './listKeyboard';
import { rowKey, useRowWindow, type ListRow, type RowDragKey } from './listRows';
import {
  aimAt,
  carriedOffsets,
  moveRefusal,
  pastThreshold,
  previewOrder,
  sameUnit,
  selectedBlock,
  stackOffsets,
  type BlockRow,
  type MoveRefusal,
  type RowSlot,
} from './rowDrag';
import {
  HOLD_MS,
  HOLD_SLOP_PX,
  LIFT,
  SETTLE,
  SHIFT,
  liftScale,
  sameOrder,
  settleMs,
  translateOf,
} from './dragLift';
import {
  IconPause,
  IconPlay,
  IconTrash,
  IconRetry,
  IconFolder,
  IconFolderOpen,
  IconSearch,
  IconSettings,
  IconArrowUp,
  IconArrowDown,
  IconClose,
} from '../lib/icons';

export interface Selection {
  ids: Set<string>;
  toggle: (id: string) => void;
  /** Replaces the whole selection, which is the write path a plain click and a
   *  Shift-range both need (TaskListCard's selectUnit) and which a per-id
   *  toggle cannot express without the caller working out the diff itself. */
  set: (ids: Set<string>) => void;
}

// A stable empty default: useUIState leaves the fallback out of its
// dependencies, so a fresh [] on every render would make every subscriber think
// the value changed.
const NO_COLLAPSED: string[] = [];

// Every row is the same grid, and the track list reaches it through one custom
// property set on the table. That is what lets a column drag repaint by touching
// a single element instead of re-rendering several hundred rows per pointer move.
const ROW_GRID: CSSProperties = { gridTemplateColumns: 'var(--kl-cols)' };

// A row's drag style when no drag is in flight: nothing at all.
//
// Empty rather than `{ translate: 'none' }`, because React clears an inline
// style property by seeing it disappear from the style object, and spelling out
// 'none' would leave it on the element for the rest of the session, overriding
// whatever the stylesheet and the landing slide say about it. Shared
// constants, so neither the style nor the map is rebuilt per row.
const NO_SLIDE: CSSProperties = {};
const NO_OFFSETS = new Map<string, number>();

// How long a dropped order is drawn after the server took it, as the phone's
// DragList does: long enough for the tick that carries it, short enough that an
// order the server settled differently is not contradicted for long.
const HOLD_ORDER_MS = 3000;

/**
 * inOrder is the list redrawn in `order`, the order a drop promised, or null
 * when the two hold different tasks.
 */
function inOrder(groups: [string, Task[]][], order: readonly string[]): [string, Task[]][] | null {
  const rank = new Map(order.map((id, at) => [id, at] as const));
  const flat = groups.flatMap(([, items]) => items);
  if (flat.length !== rank.size || flat.some((x) => !rank.has(x.id))) return null;
  return groupByPackage([...flat].sort((a, b) => (rank.get(a.id) ?? 0) - (rank.get(b.id) ?? 0)));
}

/** The drawn rows of a strip: the two window spacers and the probes carry no key. */
function drawnRows(strip: HTMLElement): HTMLElement[] {
  return Array.from(strip.querySelectorAll<HTMLElement>('[data-row-key]'));
}

/**
 * The cell holding a row's action badges, in the last grid track (see
 * gridTemplate). It paints no ground, so the row's own fill, selection tint
 * and rainbow wash run through it, and it pushes its badges to the trailing
 * edge, so the trash of every row lines up however many badges stand before
 * it.
 */
const ACTIONS_CELL = 'flex items-center justify-end gap-1';

/**
 * useCollapsedPackages is the folded set, and the only thing that knows where
 * it is kept. The list card holds it and so does the page, because the
 * right-click menu folds packages too and the menu belongs to the page. Both
 * read the same field of the same store, which notifies every subscriber on
 * write, so the twisty and the menu entry cannot disagree about what is open.
 *
 * Folded packages are keyed by name, which is the only identity the wire model
 * carries: core.Task has a package name and no package id. SetPackage rewrites
 * the name, so a rename or a Packagizer re-package makes a folded package come
 * back expanded. Fixing that needs a stable package id on the task.
 */
export function useCollapsedPackages(profile: ListProfile = 'downloads') {
  const [stored, setStored] = useUIState<string[]>(`list.collapsed.${profile}`, NO_COLLAPSED);
  const collapsed = useMemo(() => new Set(stored), [stored]);

  const collapse = useCallback(
    (names: string[]) => {
      const next = new Set(stored);
      for (const n of names) next.add(n);
      setStored([...next]);
    },
    [stored, setStored],
  );

  // Named, never "clear the lot": expanding what is on screen must not unfold
  // the packages a search is currently hiding.
  const expand = useCallback(
    (names: string[]) => {
      const next = new Set(stored);
      for (const n of names) next.delete(n);
      setStored([...next]);
    },
    [stored, setStored],
  );

  const toggle = useCallback(
    (name: string) => {
      if (collapsed.has(name)) expand([name]);
      else collapse([name]);
    },
    [collapsed, collapse, expand],
  );

  return { collapsed, collapse, expand, toggle };
}

/**
 * The tree control, as a filled triangle rather than a chevron: Swing's JTree
 * draws exactly this, which is what a package row looks like in JDownloader,
 * and it is the one mark on the row that says there is something inside. It
 * points along the reading direction when shut and downward when open, so in an
 * Arabic or Hebrew interface it points the way that interface reads.
 */
function Twisty({ open }: { open: boolean }) {
  const rtl = typeof document !== 'undefined' && document.documentElement.dir === 'rtl';
  return (
    <svg viewBox="0 0 16 16" width={11} height={11} aria-hidden focusable="false">
      <path
        d="M5.5 2.8 11.6 8l-6.1 5.2z"
        fill="currentColor"
        style={{
          transformOrigin: '8px 8px',
          transform: open ? 'rotate(90deg)' : rtl ? 'rotate(180deg)' : 'none',
        }}
      />
    </svg>
  );
}

function TaskRow({
  task,
  base,
  ctx,
  columns,
  selection,
  index,
  dnd,
  onOpenProperties,
  current,
  onKeyDown,
  level,
  posinset,
  setsize,
}: {
  task: Task;
  base: string;
  ctx: CellContext;
  columns: ColumnDef[];
  selection?: Selection;
  /** Position in the rendered list, which is the rainbow palette position. */
  index: number;
  /** Press, sweep-select and move-by-drag; see TaskListCard, the one place it
   *  is built and the one place that knows what a press means. */
  dnd: RowDnD;
  /** Opens the properties panel for what a double-click selected. A plain
   *  click opening it made a quick multi-select impossible, with the panel
   *  flashing open and shut on every intermediate click. The double-click's
   *  leading press has already marked this row by the time this fires, so
   *  there are no modifier keys to read here. */
  onOpenProperties?: () => void;
  /** True for the one row that owns the list's tab stop. Everything focusable
   *  inside a row reads it too, or Tab would still walk four hundred badges. */
  current: boolean;
  onKeyDown: (e: KeyboardEvent<HTMLElement>) => void;
  /** The tree's own sibling-set numbers, off the model and never off the
   *  window; see ListRow. */
  level: number;
  posinset: number;
  setsize: number;
}) {
  const { t } = useT();
  const collected = task.status === 'collected';
  const settled = task.status === 'done' || task.status === 'error';
  const unit: RowDragKey = { kind: 'task', id: task.id };

  // In rainbow mode the row owns a colour, and everything inside it that paints
  // activity, the progress fill above all, reads it through --accent without
  // knowing the mode exists. A running row counts as active, so the reactive
  // reading still shows colour where work is happening.
  //
  // The colour comes from the row's position, not from a hash of its id. A hash
  // survives rows above finishing, which sounds better until three rows land in
  // the same bucket and two neighbours share a colour, which is the one thing
  // the mode exists to prevent. By position, eight adjacent rows always differ.
  return (
    <div
      // What a right-click landed on. Without it the menu can only ever act on
      // whatever happened to be selected already, which is how the wrong
      // download gets deleted.
      data-task-id={task.id}
      // What the window measures. Separate from data-task-id above because a
      // folder header carries the same pair and has no task id to be found by;
      // see useRowWindow's measureRows.
      data-row-key={rowKey({ kind: 'task', id: task.id })}
      data-row-kind="task"
      role="treeitem"
      // Roving tabindex: one row is the tab stop, not five thousand.
      tabIndex={current ? 0 : -1}
      aria-selected={selection ? selection.ids.has(task.id) : undefined}
      aria-level={level}
      aria-posinset={posinset}
      aria-setsize={setsize}
      onKeyDown={onKeyDown}
      // The live drag preview slides this row with a translate rather than
      // rendering it somewhere else in the list; see TaskListCard's
      // previewOffsets. With no drag in flight the property is absent and the
      // row sits where the document flow puts it.
      style={
        {
          ...hueVars(index),
          ...ROW_GRID,
          ...dnd.slide(unit),
        } as CSSProperties
      }
      // One press, and the row does not know what it means. Selecting, sweeping
      // a selection over several rows and moving the selection are all the same
      // pointerdown, told apart by what the pointer does next; see
      // TaskListCard's gesture section, which owns every bit of that.
      //
      // No `draggable` attribute: a native HTML5 drag takes the pointer over as
      // soon as it starts, so a press that might still turn out to be a
      // selection sweep cannot be one while the row is draggable.
      onPointerDown={(e) => dnd.press(e, unit)}
      // A double-click and not a second press-and-hold: it cannot be confused
      // with a move, because it never travels the five pixels that start one.
      onDoubleClick={(e) => {
        if (e.target instanceof Element && e.target.closest(CONTROL)) return;
        onOpenProperties?.();
      }}
      // select-none, unconditionally: a press-and-drag that starts over the
      // row's own text draws a blue text selection across half the table while
      // the row sweep runs underneath it. The package header carries it too.
      //
      // has-[:focus-visible] is the keyboard's hover, and the fill has to
      // arrive with it.
      className={`glim-hue glim-tint select-none ${task.status === 'running' ? 'glim-active' : ''} ${dnd.look(unit)} ${
        selection?.ids.has(task.id) ? 'glim-row-selected' : ''
      } relative grid items-center px-3 py-2 transition-colors
        hover:bg-carbon-hover/50 has-[:focus-visible]:bg-carbon-hover/50`}
    >
      {columns.map((col) => {
        const node = col.render(task, ctx);
        return (
          <div
            key={col.id}
            dir={col.ltr ? 'ltr' : undefined}
            // The name cell is the tree column, so a link inside a package is
            // indented under it, which is the second half of what makes the
            // header above read as a container rather than as another row in
            // bold. Written as its own padding pair rather than as `px-2 ps-6`,
            // because two utilities setting the same edge leave the result to
            // stylesheet order.
            //
            // TREE_INDENT is the exact width of the package row's leading
            // furniture (see columns.tsx), so a link's name starts where its
            // package's name starts. A smaller value starts it before the
            // package's, which reads as the tree upside down.
            style={col.id === 'name' ? { paddingInlineStart: `${TREE_INDENT}px` } : undefined}
            // text-xs is the scale's dense row, which is what a table cell takes.
            className={`min-w-0 truncate text-xs text-carbon-textSub ${
              col.id === 'name' ? 'pe-2' : 'px-2'
            } ${col.align === 'end' ? 'text-end' : col.align === 'center' ? 'text-center' : 'text-start'} ${col.numeric ? 'glim-num' : ''}`}
          >
            {/* A column that renders plain text carries that text in the house
                bubble, so a name too long for its width is readable without
                widening the column first. Never a native `title` and the
                operating system's balloon beside it; see columns.tsx's Tip. */}
            {typeof node === 'string' ? (
              <Tip tip={node} className="block truncate">
                {node}
              </Tip>
            ) : (
              node
            )}
          </div>
        );
      })}

      {/* The actions stand in a track of their own at the row's end and are
          there on every row at rest (GlimStone rule 6): a touch screen has no
          hover to reveal them, and an action nobody can see is one nobody knows
          is there. Floating over the row instead, they would cover its size,
          speed and status cells whenever they showed. The badges are quiet, so
          forty rows do not read as a wall of tiles, and the context menu on the
          row offers every one of these verbs too.
          The badges are hued per slot rather than per row position, so a row's
          play badge is always the same hue and Recheck is always the next one,
          the same "position is the identity" rule every other badge set in this
          app follows. A hash of the task id would repaint a badge a different
          colour every time its row moved. Trash takes a hue as well: a badge in
          solid red beside neutral siblings reads as the inconsistency. */}
      <div className={ACTIONS_CELL}>
        {collected && (
          <IconBadge
            quiet
            hue={0}
            // Roving tabindex reaches inside the row too: without it Tab walks
            // every badge of every drawn row and the one-stop list is decorative.
            tabIndex={current ? 0 : -1}
            icon={<IconPlay width={16} height={16} />}
            title={t('task.start')}
            aria-label={t('task.start')}
            onClick={() => startTasks([task.id], base)}
          />
        )}
        {task.status === 'running' && (
          <IconBadge
            quiet
            hue={0}
            tabIndex={current ? 0 : -1}
            icon={<IconPause width={16} height={16} />}
            title={t('task.pause')}
            aria-label={t('task.pause')}
            onClick={() => pause(task.id, base)}
          />
        )}
        {task.status === 'paused' && (
          <IconBadge
            quiet
            hue={0}
            tabIndex={current ? 0 : -1}
            icon={<IconPlay width={16} height={16} />}
            title={t('task.resume')}
            aria-label={t('task.resume')}
            onClick={() => resume(task.id, base)}
          />
        )}
        <div className="flex items-center gap-1">
          {collected && (
            <IconBadge
              quiet
              hue={1}
              tabIndex={current ? 0 : -1}
              icon={<IconSearch width={16} height={16} />}
              title={t('task.recheck')}
              aria-label={t('task.recheck')}
              onClick={() => recheckTasks([task.id], base)}
            />
          )}
          {/* Before Restart and never instead of it. This one appears only
              while a wait is running, and it spends the retry that was already
              scheduled rather than granting a new one; Restart beside it is the
              plain "run this again" for a row that is finished or done waiting.
              No guard at the call site: RetrySkipBadge draws nothing unless a
              retry is pending, which is narrower than `settled`. */}
          <RetrySkipBadge task={task} base={base} focusable={current} />
          {settled && (
            <IconBadge
              quiet
              hue={3}
              tabIndex={current ? 0 : -1}
              icon={<IconRetry width={16} height={16} />}
              title={t('task.restart')}
              aria-label={t('task.restart')}
              onClick={() => restartTasks([task.id], base)}
            />
          )}
          <IconBadge
            quiet
            hue={4}
            tabIndex={current ? 0 : -1}
            icon={<IconTrash width={16} height={16} />}
            title={t('task.remove')}
            aria-label={t('task.remove')}
            onClick={() => remove(task.id, base)}
          />
        </div>
      </div>

    </div>
  );
}

/**
 * The package header's name cell: the folder, the tree control, the name and
 * the file count, which together are what makes a package look like a package
 * rather than like a row somebody made bold.
 *
 * The online figure is a ratio and never a pair of counts: "3 of 5 online" is
 * true while a check is still running, whereas "3 online, 2 offline" claims the
 * other two were asked and said no.
 */
function PackageName({
  name,
  items,
  collapsed,
  onToggle,
  focusable,
}: {
  name: string;
  items: Task[];
  collapsed: boolean;
  onToggle: () => void;
  /** Whether this row owns the list's tab stop; see TaskRow's `current`. The
   *  twisty is a real button and would otherwise be its own tab stop on every
   *  drawn package. */
  focusable: boolean;
}) {
  const { t } = useT();
  const priorityNames = usePriorityNames();
  const done = items.filter((x) => x.status === 'done').length;
  const label = t(collapsed ? 'task.expand' : 'task.collapse');
  // The twisty draws a glyph and nothing else, so it needs a tooltip
  // unconditionally, and it takes the house bubble rather than the OS balloon
  // the native attribute draws (see columns.tsx's Tip).
  const tip = useTooltip<HTMLButtonElement>(label);
  const { role: _role, tabIndex: _tabIndex, ...hover } = tip.triggerProps;

  const count = `${items.length} ${items.length === 1 ? t('task.file') : t('task.files')}${
    done > 0 ? ` · ${done} ${t('overview.done').toLowerCase()}` : ''
  }`;

  return (
    // @container, because what follows the name is whole or gone, never
    // shredded. `truncate` is the right tool for a value that still means
    // something cut short and the wrong one for a two-word label, where "2
    // files" becomes "2 f…". Sized against this row rather than the viewport,
    // since the name column is dragged and hidden independently of the window.
    <div className="@container flex min-w-0 items-center gap-1.5">
      <button
        type="button"
        onClick={onToggle}
        tabIndex={focusable ? 0 : -1}
        aria-expanded={!collapsed}
        aria-label={label}
        {...hover}
        className="grid h-6 w-6 shrink-0 place-items-center rounded-[var(--radius-control)] text-carbon-textSub
          transition-colors hover:bg-carbon-surface3 hover:text-carbon-text"
      >
        <Twisty open={!collapsed} />
      </button>
      {tip.node}
      {/* Furniture, never the accent: every package has one, and a column of
          gold folders would spend the one colour that means "something is
          happening here" on the most ordinary fact on the page. */}
      {collapsed ? (
        <IconFolder width={FOLDER_GLYPH} height={FOLDER_GLYPH} className="shrink-0 text-carbon-textMuted" />
      ) : (
        <IconFolderOpen width={FOLDER_GLYPH} height={FOLDER_GLYPH} className="shrink-0 text-carbon-textMuted" />
      )}
      {/* The package wears the mark when every link in it agrees, the same rule
          the Enabled column's aggregate follows. Without it the mark exists
          only on expanded rows, so a collapsed package would hide the thing it
          is there to announce. Rows that disagree show nothing here and keep
          their own marks inside. */}
      <PriorityTag value={sharedPriority(items)} names={priorityNames} t={t} />
      {/* The name wins the room: everything after it shrinks and the name does
          not, below its own floor. With the counts pinned instead, a package
          called "Season One" in a narrow column renders as "S · 3 files", and
          the name is the one thing on the row nobody can do without. */}
      <Tip
        tip={`${name || t('task.ungrouped')} - ${count}`}
        className="min-w-[5rem] flex-1 truncate text-sm font-semibold text-carbon-text"
      >
        {name || t('task.ungrouped')}
      </Tip>
      {/* The count hangs in the name's bubble as well, so a column too narrow
          to show it has hidden nothing that cannot be got at. No online ratio
          beside it: the package's aggregate dot in the Status column already
          says that, and saying it twice is how a package reads differently
          from its own status cell. */}
      <span className="glim-num hidden shrink-0 whitespace-nowrap text-[11px] text-carbon-textSub @[13rem]:inline">
        {count}
      </span>
    </div>
  );
}

/**
 * The gear badge a yt-dlp-routed package's header carries. It opens that host's
 * variant preset: which of the five rows the collector shows for this host's
 * links, the ones already in it included, and the default quality and audio
 * format a new link's rows start on. Per host rather than per package (GET and
 * POST /api/ytdlp/preset), so a package with more than one host shows the
 * badge for whichever host its variant rows share.
 */
function HosterPresetButton({ host, base, focusable }: { host: string; base: string; focusable: boolean }) {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  const label = `${t('settings.resolvers.variantDefaults')} · ${host}`;
  return (
    <>
      <IconBadge
        hue={0}
        tabIndex={focusable ? 0 : -1}
        icon={<IconSettings width={16} height={16} />}
        title={label}
        aria-label={label}
        onClick={() => setOpen(true)}
      />
      {open &&
        createPortal(<HosterPresetDialog host={host} base={base} onClose={() => setOpen(false)} />, document.body)}
    </>
  );
}

// The two failures here are reported two different ways, by the test
// GlimStone's failure-feedback section asks: does a fresh click of the same
// button replace this message?
//
//   a failed save  yes. The sentence goes to a toast and the button that was
//                  pressed shakes. Left standing in the footer instead, it
//                  never clears itself and an hour-old failure looks as
//                  current as a fresh one.
//   a failed load  no. Nothing was clicked and there is no control to shake:
//                  the dialog has nothing to show, which is a standing fact
//                  about its contents rather than a refused action, so it
//                  stays where the contents would have been, in an ErrorCard.
function HosterPresetDialog({ host, base, onClose }: { host: string; base: string; onClose: () => void }) {
  const { t } = useT();
  const { toast } = useToast();
  const [preset, setPreset] = useState<YtdlpHosterPreset | null>(null);
  const [menus, setMenus] = useState<PresetMenus>(NO_PRESET_MENUS);
  const [saving, setSaving] = useState(false);
  const [loadError, setLoadError] = useState('');
  // The Save button's failure counter, so a repeated refusal shakes it again.
  const [shake, setShake] = useState(0);

  useEffect(() => {
    let live = true;
    void Promise.all([fetchHosterPreset(host, base), fetchOptions()]).then(
      ([p, o]) => {
        if (!live) return;
        setPreset(p);
        setMenus(presetMenusOf(o));
      },
      (err) => {
        if (live) setLoadError(err instanceof Error && err.message ? err.message : String(err));
      },
    );
    return () => {
      live = false;
    };
  }, [host, base]);

  function toggleVariant(kind: YtdlpVariantKind) {
    setPreset((p) => {
      if (!p) return p;
      const on = p.variants.includes(kind);
      return { ...p, variants: on ? p.variants.filter((v) => v !== kind) : [...p.variants, kind] };
    });
  }

  async function save() {
    if (!preset) return;
    setSaving(true);
    try {
      await saveHosterPreset(host, preset, base);
      onClose();
    } catch (err) {
      toast(err instanceof Error && err.message ? err.message : String(err), 'fail');
      setShake((n) => n + 1);
    } finally {
      setSaving(false);
    }
  }

  const pickers =
    preset && presetPickers({ preset, menus, t, onChange: (fields) => setPreset((p) => (p ? { ...p, ...fields } : p)) });
  const pairs: Partial<Record<YtdlpVariantKind, (PickerProps | null)[]>> = pickers
    ? { video: [pickers.video.format, pickers.video.quality], audio: [pickers.audio.format, pickers.audio.bitrate] }
    : {};

  return (
    <Modal
      title={`${t('settings.resolvers.variantDefaults')} · ${host}`}
      onClose={onClose}
      footer={
        <>
          <Button kind="ghost" labelled icon={<IconClose />} title={t('common.cancel')} onClick={onClose} />
          <Button shake={shake} onClick={() => void save()} disabled={!preset || saving}>
            {t('settings.save')}
          </Button>
        </>
      }
    >
      {loadError ? (
        <ErrorCard nested message={loadError} />
      ) : !preset ? (
        <LoadingCard nested label={t('common.loading')} />
      ) : (
        <FieldGroup label={t('columns.variant')} hint={t('collector.hosterPresetHint', { host })}>
          {/* The row the table on the Resolvers page shows for this host,
              turned on its side: each variant with the format and quality it
              starts with, and its switch at the end of the line. One grid, so
              the pickers of the video and audio lines stand in columns. */}
          <div className="grid auto-rows-[minmax(var(--btn-h),auto)] grid-cols-[auto_minmax(0,1fr)_minmax(0,1fr)_auto] items-center gap-2">
            {YTDLP_VARIANT_KINDS.map((kind, k) => {
              const kindLabel = t(VARIANT_KIND_LABEL_KEY[kind]);
              const pair = menus.qualities.length > 0 ? pairs[kind] : undefined;
              return (
                <Fragment key={kind}>
                  <span className="text-sm text-carbon-text">{kindLabel}</span>
                  {[0, 1].map((i) => {
                    const p = pair?.[i];
                    return p ? <VariantDropdown key={i} picker={p} width="fill" /> : <span key={i} />;
                  })}
                  <Toggle
                    hideLabel
                    label={kindLabel}
                    checked={preset.variants.includes(kind)}
                    onChange={() => toggleVariant(kind)}
                    hue={k}
                  />
                </Fragment>
              );
            })}
          </div>
        </FieldGroup>
      )}
    </Modal>
  );
}

// Anything that is itself a control keeps its own click. Everything else on a
// package header folds it, which is what people try first and what JDownloader
// does.
const CONTROL = 'button, a, input, select, textarea, [role="switch"], [role="checkbox"], .glim-info';

// PackageRow is the folder header: a plain block inside the list card, not a
// nested card. Its totals are over every link in the package, folded or not: a
// collapsed package that stops counting turns the header into a number that
// changes when you click a chevron.
//
// It draws the header and nothing else. Its links are siblings of it in the
// table rather than children (see ListRow and TaskListCard's `rows`): the table
// is one flat run of rows, because only a flat run can be windowed, and
// windowing is what keeps a list of several thousand links usable. The tree is
// still a tree, drawn by the indent on the name cell rather than by the nesting
// of the elements (see TREE_INDENT).
function PackageRow({
  name,
  items,
  base,
  ctx,
  columns,
  selection,
  collapsed,
  onToggleCollapsed,
  divider,
  dnd,
  onOpenProperties,
  current,
  onKeyDown,
  level,
  posinset,
  setsize,
}: {
  name: string;
  items: Task[];
  base: string;
  ctx: CellContext;
  columns: ColumnDef[];
  selection?: Selection;
  collapsed: boolean;
  onToggleCollapsed: () => void;
  /** The rule between one package and the next. `divide-y` is not available to
   *  a flat run of rows, where a divider between every pair would draw a line
   *  under every link, so the seam is a property of the header that opens a
   *  package and the first row of the table has none. */
  divider: boolean;
  /** See TaskRow's identical prop: one press, and this row does not know what
   *  it means. */
  dnd: RowDnD;
  /** TaskRow's onOpenProperties, for a double-click on the package header. */
  onOpenProperties?: () => void;
  /** See TaskRow's identical prop. */
  current: boolean;
  onKeyDown: (e: KeyboardEvent<HTMLElement>) => void;
  level: number;
  posinset: number;
  setsize: number;
}) {
  const allSelected = selection && items.every((x) => selection.ids.has(x.id));
  const unit: RowDragKey = { kind: 'package', name };
  const ytdlpHost = items.find((x) => variantKindOf(x) && x.host)?.host;

  return (
    <div
      // The name is the only identity a package has on the wire, and it is
      // legitimately empty for the ungrouped one, so the attribute is present
      // and empty rather than absent and the page tells the two apart.
      data-package-row={name}
      // See TaskRow's own pair: what useRowWindow measures this row by.
      data-row-key={rowKey({ kind: 'package', name })}
      data-row-kind="package"
      role="treeitem"
      tabIndex={current ? 0 : -1}
      aria-selected={selection ? !!allSelected : undefined}
      aria-expanded={!collapsed}
      aria-level={level}
      aria-posinset={posinset}
      aria-setsize={setsize}
      onKeyDown={onKeyDown}
      // Slid out of the way by a drag in flight exactly like a link row; see
      // TaskRow's identical style above and previewOffsets for the arithmetic.
      // A folder header is a row like any other here, so the folders a drag
      // passes step aside while the pointer is still down.
      style={{
        ...ROW_GRID,
        ...dnd.slide(unit),
      }}
      // See TaskRow's identical pair. A press on a folder header selects the
      // whole folder; the twisty button beside the name matches CONTROL, so it
      // keeps folding and unfolding on its own click.
      onPointerDown={(e) => dnd.press(e, unit)}
      onDoubleClick={(e) => {
        if (e.target instanceof Element && e.target.closest(CONTROL)) return;
        onOpenProperties?.();
      }}
      // A colour step, not a rule: the header sits on the quiet surface and the
      // links inside it sit on the card, which is the whole of the weight
      // difference between a container and its contents. The resting ground is
      // surface2 at 80% over the card, and the selected state paints over it
      // with .glim-row-selected.
      //
      // The hover goes up the ramp (rule 21), and both the flat tones fail it.
      // --carbon-surface2 is the tone the resting mix is made of and moves
      // ΔL* 1.8 against the link row's 3.8, which is not a hover anybody sees.
      // Flat --carbon-surface3 is what the Aktiv switch's on track and the
      // hoster badge are filled with, so a row painted with it swallows them
      // whole. Moving the same 80% plane one step keeps a step under everything
      // standing on the row. Both are named as custom properties so that
      // web/check-hover-ramp.mjs can compare them, which it cannot do with two
      // arbitrary `bg-[...]` values.
      className={`relative grid cursor-pointer select-none items-center ${
        allSelected ? 'glim-row-selected' : ''
      } ${divider ? 'border-t border-carbon-border/60' : ''} px-3 py-2.5 transition-colors
        bg-[var(--row-ground)]
        [--row-ground:color-mix(in_srgb,var(--carbon-surface2)_80%,var(--carbon-surface))]
        [--row-raised:color-mix(in_srgb,var(--carbon-surface3)_80%,var(--carbon-surface))]
        hover:[--row-ground:var(--row-raised)]
        has-[:focus-visible]:[--row-ground:var(--row-raised)] ${dnd.look(unit)}`}
    >
      {columns.map((col) => (
        <div
          key={col.id}
          className={`min-w-0 truncate px-2 text-[12px] text-carbon-textSub ${
            col.align === 'end' ? 'text-end' : col.align === 'center' ? 'text-center' : 'text-start'
          } ${col.numeric ? 'glim-num' : ''}`}
        >
          {col.id === 'name' ? (
            <PackageName
              name={name}
              items={items}
              collapsed={collapsed}
              onToggle={onToggleCollapsed}
              focusable={current}
            />
          ) : (
            col.aggregate?.(items, ctx)
          )}
        </div>
      ))}

      {/* The folder row's actions, in the same trailing track as a link row's
          and visible at rest for the same reason.

          The gear is for a package whose variant rows share a host
          (variantKindOf is '' for anything not yt-dlp-routed) and collector
          only: what it opens is which variants to keep and at what quality,
          which is a decision about a link before it is fetched. */}
      {(ytdlpHost && ctx.profile === 'collector') || ctx.onRemovePackage ? (
        <div className={ACTIONS_CELL}>
          {ytdlpHost && ctx.profile === 'collector' && (
            <HosterPresetButton host={ytdlpHost} base={base} focusable={current} />
          )}
          {/* Does not delete itself: it asks the same question a multiple
              selection asks, with its count, its file choice and its undo. A
              removal path of its own here would be a second place for that
              question to drift. */}
          {ctx.onRemovePackage && (
            <IconBadge
              quiet
              hue={4}
              tabIndex={current ? 0 : -1}
              icon={<IconTrash width={16} height={16} />}
              title={ctx.t('task.remove')}
              aria-label={ctx.t('task.remove')}
              onClick={() => ctx.onRemovePackage?.(items.map((x) => x.id))}
            />
          )}
        </div>
      ) : null}
    </div>
  );
}

/** The bundle TaskRow and PackageRow share, built once per render in TaskListCard. */
interface RowDnD {
  /** The row's drag class: LIFT for a row a move in flight is carrying (the
   *  whole selection, so it can be several), SETTLE while it lands, SHIFT for
   *  the others while they make room, and none at rest. */
  look: (unit: RowDragKey) => string;
  /** The press. Everything it can turn into, a click, a selection sweep or a
   *  move, is decided by TaskListCard; see its gesture section. */
  press: (e: PointerEvent<HTMLElement>, unit: RowDragKey) => void;
  /** How far this row has to slide to show where the move in flight would put
   *  it, as the inline style that does it, or an empty object when no move is
   *  running. Every link row and every folder header spreads this into its own
   *  style; see TaskListCard's previewOffsets for where the numbers come from. */
  slide: (unit: RowDragKey) => CSSProperties;
}

/** The three fields TaskListCard's own selectUnit reads off a press. */
type SelectMods = { ctrlKey: boolean; metaKey: boolean; shiftKey: boolean };
const PLAIN: SelectMods = { ctrlKey: false, metaKey: false, shiftKey: false };

/**
 * A press that has not finished being one thing or another yet. `mode` is
 * settled at the press itself and never changes afterwards, which is what makes
 * the gesture learnable: what a press will do is decided by what was under it,
 * not by how far it later travels. The distance only decides whether it happens
 * at all (see pastThreshold).
 */
interface Gesture {
  /** 'select' sweeps a range, 'move' carries the selection. */
  mode: 'select' | 'move';
  /** The row the press landed on. */
  unit: RowDragKey;
  /** Its place in the on-screen order, which is where the sweep measures its
   *  own direction from. Not the same as the range anchor: a Shift-press lands
   *  somewhere the anchor is not. */
  fromIndex: number;
  pointerId: number;
  /** Where the press was, in client coordinates. The threshold measures from
   *  here, never from the previous move. */
  fromX: number;
  fromY: number;
  /** Where the pointer is now. Read by the edge scroll, which has to know where
   *  the pointer is at a moment when the pointer is not moving. */
  atX: number;
  atY: number;
  /** The keys held at the press. A modifier taken away mid-sweep does not
   *  change what the sweep is doing, the same way letting go of Shift halfway
   *  through a Shift-click does not undo it. */
  mods: SelectMods;
  /** The selection as the press found it: what Escape puts back, and what a
   *  Ctrl-sweep adds to. */
  before: ReadonlySet<string>;
  /** Whether the pointer has travelled far enough for this to be a gesture at
   *  all rather than a click. */
  live: boolean;
  /** The rows a move is carrying, in drawn order. Empty until `live`. */
  block: RowDragKey[];
  /** The last row a sweep selected up to, so a pointer wandering inside one row
   *  does not rewrite the selection sixty times a second. */
  sweptTo: number | null;
  /** The aim a move would commit right now. Kept here as well as in state
   *  because the drop reads it in the same tick the last move wrote it. */
  over: { target: RowDragKey; after: boolean } | null;
  /** A finger, which arms a move by holding still; see holdRow. */
  touch: boolean;
  /** The hold's timer while it runs, else 0. */
  hold: number;
  /** Whether a held finger has moved since it armed. One that has not opens
   *  the row's menu on release instead of dropping anything. */
  travelled: boolean;
  /** Why a hold that armed may not move its rows, or null. Such a hold still
   *  opens the row's menu, and says why only if the finger tries to drag. */
  refused: MoveRefusal | null;
  /** The press in the strip's own coordinates, taken when a move begins: the
   *  carried rows move by the pointer's travel from here. */
  originY: number;
  /** The carried rows' lift scale, read off the first one placed; 0 until then. */
  scale: number;
}

/** Which drag unit a drawn row element stands for, or null for anything that is
 *  not a row (the two window spacers, the keyboard's own probe). */
function unitOfRow(el: HTMLElement): RowDragKey | null {
  if (el.dataset.taskId !== undefined) return { kind: 'task', id: el.dataset.taskId };
  if (el.dataset.packageRow !== undefined) return { kind: 'package', name: el.dataset.packageRow };
  return null;
}

// How close to the edge of the scrolling box the pointer has to come before the
// list starts moving under it, and how fast it moves at the very edge.
//
// The native HTML5 drag this gesture replaced got the edge scroll from the
// browser, and without it a folder can only be moved as far as one screen. 56px
// is a little over a row and a half, so the band is reachable without being
// somewhere the pointer sits by accident; 18px a frame is roughly a screenful
// every two seconds at the edge, eased in across the band so entering it does
// not lurch.
const EDGE_BAND_PX = 56;
const EDGE_SPEED_PX = 18;

/** The box that actually scrolls this list, or null when it is the page. */
function scrollerOf(el: HTMLElement | null): HTMLElement | null {
  for (let n = el?.parentElement ?? null; n; n = n.parentElement) {
    const overflow = getComputedStyle(n).overflowY;
    if ((overflow === 'auto' || overflow === 'scroll') && n.scrollHeight > n.clientHeight + 1) return n;
  }
  return null;
}

/**
 * The 8px grab strip on a column's trailing edge. Its own component only so it
 * can hold a hook: it carries the house bubble rather than a native `title`
 * (see columns.tsx's Tip), and a hook cannot live inside the header's `map()`.
 * The handle is invisible furniture with no text of its own, which is the case
 * GlimStone says needs a tooltip unconditionally.
 */
function ResizeHandle({
  hint,
  onResize,
  onReset,
}: {
  hint: string;
  onResize: (phase: 'start' | 'move' | 'end', e: PointerEvent<HTMLElement>) => void;
  onReset: () => void;
}) {
  const tip = useTooltip<HTMLSpanElement>(hint);
  // role/tabIndex dropped: this element already states role="separator", and
  // the "note" role the hook hands out would replace it.
  const { role: _role, tabIndex: _tabIndex, ...hover } = tip.triggerProps;
  return (
    <>
      <span
        role="separator"
        aria-orientation="vertical"
        aria-label={hint}
        {...hover}
        onPointerDown={(e) => onResize('start', e)}
        onPointerMove={(e) => onResize('move', e)}
        onPointerUp={(e) => onResize('end', e)}
        // A drag the browser takes away (a context menu, a window switch)
        // must still settle the width, or the table keeps a hand-painted
        // track list that the next render silently undoes.
        onPointerCancel={(e) => onResize('end', e)}
        onDoubleClick={onReset}
        className="absolute inset-y-0 end-0 z-10 w-2 cursor-col-resize touch-none"
      >
        {/* The visible line, drawn inside the 8px grab area rather than as a
            border on the cell: a border would sit at the edge of the column,
            and the thing to aim at is the handle. Transparent until the header
            is hovered, and always ignoring the pointer so it never eats the
            drag it advertises. */}
        <span
          aria-hidden
          className="pointer-events-none absolute inset-y-1 end-[3px] w-px bg-transparent
            transition-colors group-hover/header:bg-carbon-border"
        />
      </span>
      {tip.node}
    </>
  );
}

/**
 * The header: column labels, and the three things you can do to a column.
 *
 * The label is the draggable element and the resize handle is its sibling rather
 * than a child, because a handle inside the drag source turns every resize into
 * a half-started reorder.
 */
function Header({
  layout,
  profile,
  sort,
  onSort,
  onReorder,
  onResize,
  onResizeReset,
  onMenu,
}: {
  layout: ResolvedLayout;
  profile: ListProfile;
  sort: SortState | null;
  onSort: (id: ColumnId) => void;
  onReorder: (id: ColumnId, target: ColumnId, after: boolean) => void;
  onResize: (id: ColumnId, phase: 'start' | 'move' | 'end', e: PointerEvent<HTMLElement>) => void;
  onResizeReset: (id: ColumnId) => void;
  onMenu: (at: { x: number; y: number }) => void;
}) {
  const { t } = useT();
  const [dragId, setDragId] = useState<ColumnId | null>(null);

  function drop(e: DragEvent<HTMLDivElement>, target: ColumnDef) {
    e.preventDefault();
    const from = dragId;
    setDragId(null);
    if (!from || from === target.id) return;
    const r = e.currentTarget.getBoundingClientRect();
    const middle = r.left + r.width / 2;
    const after = document.documentElement.dir === 'rtl' ? e.clientX < middle : e.clientX > middle;
    onReorder(from, target.id, after);
  }

  return (
    <div
      style={ROW_GRID}
      onContextMenu={(e) => {
        e.preventDefault();
        onMenu({ x: e.clientX, y: e.clientY });
      }}
      // group/header: hovering anywhere on the header lights up every column
      // boundary at once. Per-handle hover would not do, because the handle is
      // 8px wide and invisible and nobody can hover what they are looking for.
      className="group/header relative grid items-center border-b border-carbon-border/60 px-3 py-1 select-none"
    >
      {layout.visible.map((col) => {
        const sorted = sort?.id === col.id ? sort.dir : null;
        const sortable = !!col.compare;
        return (
          <div
            key={col.id}
            className={`relative flex items-center ${dragId === col.id ? 'opacity-50' : ''}`}
            onDragOver={(e) => dragId && e.preventDefault()}
            onDrop={(e) => drop(e, col)}
          >
            <button
              type="button"
              draggable
              onDragStart={(e) => {
                setDragId(col.id);
                e.dataTransfer.effectAllowed = 'move';
                // Firefox starts no drag at all without payload on the transfer.
                e.dataTransfer.setData('text/plain', col.id);
              }}
              onDragEnd={() => setDragId(null)}
              onClick={() => sortable && onSort(col.id)}
              aria-sort={sorted === 'asc' ? 'ascending' : sorted === 'desc' ? 'descending' : undefined}
              className={`flex min-w-0 flex-1 items-center gap-1 px-2 py-1.5 text-[11px] font-semibold uppercase
                tracking-wide transition-colors ${
                  col.align === 'end' ? 'justify-end' : col.align === 'center' ? 'justify-center' : 'justify-start'
                } ${sorted ? 'text-carbon-text' : 'text-carbon-textMuted hover:text-carbon-textSub'}`}
            >
              {/* One list can call a column something else; see CellContext's
                  `profile` for why that is one column and not two. */}
              <span className="truncate">{t(col.labelByProfile?.[profile] ?? col.labelKey)}</span>
              {sorted === 'asc' && <IconArrowUp width={11} height={11} className="shrink-0" />}
              {sorted === 'desc' && <IconArrowDown width={11} height={11} className="shrink-0" />}
            </button>

            {/* Double-click gives a column its built-in width back, which is the
                only way out of a column dragged down to its minimum. */}
            <ResizeHandle
              hint={t('columns.resizeHint')}
              onResize={(phase, e) => onResize(col.id, phase, e)}
              onReset={() => onResizeReset(col.id)}
            />
          </div>
        );
      })}

      {/* The bubble explaining the header, right-click for the column menu and
          click a label to sort, sits on the card's title badge, where
          TaskListCard renders it. This row holds nothing but the column labels
          and their resize handles. */}
    </div>
  );
}

/** Which box was edited. Nothing else is sent; see TaskProperties. */
type PropField = 'name' | 'dir' | 'comment' | 'priority' | 'autoExtract';

/**
 * The priorities, from the server, low to high so the strip reads as a scale
 * rather than as a list somebody happened to order that way. The server's list
 * is the list, so the panel and the right-click menu cannot give two answers to
 * how many priorities there are; priorityChoices() fetches it once per session
 * and both callers read the same copy.
 */
function usePriorities(): { id: string; label: TranslationKey }[] {
  const [choices, setChoices] = useState<PriorityChoice[]>([]);
  useEffect(() => {
    let live = true;
    void priorityChoices().then(
      (p) => {
        if (live) setChoices(p);
      },
      () => {
        /* the strip stays empty rather than offering a guess */
      },
    );
    return () => {
      live = false;
    };
  }, []);
  // Reversed: the server sends highest first, because that is the order a menu
  // reads best in, and a slider reads the other way.
  return choices
    .slice()
    .reverse()
    .map((p) => ({ id: String(p.value), label: `priority.${p.id}` as TranslationKey }));
}

/** The tri-state as a tab id. `undefined` is "inherit", never "off". */
const extractId = (t: Task): string =>
  t.autoExtract === undefined ? 'inherit' : t.autoExtract ? 'on' : 'off';

/**
 * agree reads one field off the whole selection and answers with the value they
 * share, or null when they do not, which is a third answer rather than an empty
 * one.
 */
function agree<T>(tasks: Task[], pick: (t: Task) => T): T | null {
  if (tasks.length === 0) return null;
  const first = pick(tasks[0]);
  return tasks.every((x) => pick(x) === first) ? first : null;
}

/**
 * TaskProperties edits what is selected, one row or forty, through the same
 * panel and the same request.
 *
 * A field is sent only if it was edited: not if it differs from what was
 * loaded, and not if it is non-empty. A selection whose rows disagree opens
 * with an empty box, and sending that would write nothing over forty comments,
 * folders and passwords in one click. So the boxes carry a placeholder rather
 * than a value, and `touched` is set by the change handler rather than derived
 * by comparison, which keeps "they disagree" and "I cleared this on purpose"
 * apart; both look like an empty string on the wire.
 *
 * `ids` is the whole selection and `tasks` the part of it this list can see,
 * since a quick filter can hide a selected row. A hidden row can only make the
 * panel show a value as agreed when it is not, which costs a placeholder and
 * never a value.
 */
export function TaskProperties({
  ids,
  tasks,
  base,
  autoFocus = false,
}: {
  ids: string[];
  tasks: Task[];
  base: string;
  /** Set only where the keyboard opened this panel. It renders below the list
   *  card, which on a five-thousand-row list is thousands of pixels down the
   *  page, so Enter would otherwise open a panel nobody can see. A double-click
   *  never sets it: a mouse user's focus is theirs. */
  autoFocus?: boolean;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const priorities = usePriorities();

  // Read once, at mount. The panel is remounted whenever the selection changes
  // (see the key in TaskListCard), so this is the only moment these values are
  // the ones the user is looking at; reading them again on every render would
  // pull a half-typed box back to what the server last broadcast.
  const [start] = useState(() => ({
    name: tasks.length === 1 ? tasks[0].name : '',
    dir: agree(tasks, (x) => x.dir ?? ''),
    comment: agree(tasks, (x) => x.comment ?? ''),
    priority: agree(tasks, (x) => String(x.priority ?? 0)),
    autoExtract: agree(tasks, extractId),
  }));

  const [name, setName] = useState(start.name);
  const [dir, setDir] = useState(start.dir ?? '');
  const [comment, setComment] = useState(start.comment ?? '');
  const [priority, setPriority] = useState(start.priority);
  const [extract, setExtract] = useState(start.autoExtract);
  const [touched, setTouched] = useState<Set<PropField>>(() => new Set());
  const [busy, setBusy] = useState(false);
  // The Save button's own failure counter, read as its `key`. A refused save
  // reports through the two channels every action here uses, the server's
  // sentence in a toast and the pressed control shaking, never as a sentence
  // left standing beside the button, which nothing ever clears.
  const [shake, setShake] = useState(0);

  const panelRef = useRef<HTMLElement>(null);
  useEffect(() => {
    if (!autoFocus) return;
    panelRef.current?.focus({ preventScroll: true });
    panelRef.current?.scrollIntoView({ block: 'nearest' });
  }, [autoFocus]);

  function edit<T>(field: PropField, set: (v: T) => void): (v: T) => void {
    return (v) => {
      set(v);
      setTouched((s) => (s.has(field) ? s : new Set(s).add(field)));
    };
  }

  // The explanation and, where the rows disagree, the sentence that says why the
  // box is empty. One helper, so no field can carry the placeholder without also
  // carrying its reason.
  const hint = (text: string, mixed: boolean) => (mixed ? `${text} ${t('props.mixedHint')}` : text);
  const placeholder = (mixed: boolean) => (mixed ? t('props.mixed') : undefined);

  async function apply(): Promise<void> {
    const opts: TaskOptionsPatch = {};
    if (touched.has('name')) opts.name = name;
    if (touched.has('dir')) opts.dir = dir;
    if (touched.has('comment')) opts.comment = comment;
    if (touched.has('priority') && priority !== null) opts.priority = Number(priority);
    // null, not undefined: "inherit the global switch" is a value the server has
    // to be told, and undefined would be dropped by JSON.stringify along with
    // every field the user left alone.
    if (touched.has('autoExtract') && extract !== null) {
      opts.autoExtract = extract === 'inherit' ? null : extract === 'on';
    }

    setBusy(true);
    const r = await setTaskOptions(ids, opts, base);
    setBusy(false);
    if (!r.ok) {
      // These routes refuse with a sentence, and the sentence is why refusing
      // is useful: a rename that could not happen has a reason, and hiding it
      // behind "save failed" leaves the row promising a name the folder does
      // not have. It goes to the toast, which is where a failure is read.
      toast((await r.text()).trim() || t('list.optionsFailed'), 'fail');
      setShake((n) => n + 1);
      return;
    }
    setTouched(new Set());
    toast(t('settings.saved'), 'ok');
  }

  return (
    // The right-click belongs to the browser in here. The page above this puts a
    // context menu on the whole list area and calls preventDefault on every
    // reading of it, which inside a text box means no paste entry.
    <section
      ref={panelRef}
      tabIndex={-1}
      aria-label={t('props.title')}
      onContextMenu={(e) => e.stopPropagation()}
    >
      <Card className="flex flex-col gap-4">
        <SectionTitle
          right={
            <span className="glim-num text-xs text-carbon-textMuted">
              {ids.length} {t('select.count')}
            </span>
          }
        >
          {t('props.title')}
        </SectionTitle>

        {/* Only over a single row, and left out rather than greyed out over
            several: a name is an identity, not a property. Forty rows given one
            name are forty downloads pointed at one destination, which the
            server refuses for the same reason. */}
        {ids.length === 1 && (
          <Field label={t('props.name')} hint={t('props.nameHint')}>
            <TextInput
              value={name}
              spellCheck={false}
              onChange={(e) => edit('name', setName)(e.target.value)}
            />
          </Field>
        )}

        {/* One row, not two stacked full-width fields: the same grid-cols-2
            pattern the priority and auto-extract row below uses. Comment is a
            single-row TextArea sharing TextInput's inputClass, so it lands at
            the same height as the folder field beside it, and it stays resize-y
            so a longer comment can be grown by hand rather than reserving the
            space for one. */}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label={t('task.folder')}
            hint={hint(t('settings.downloadDirHint'), start.dir === null)}
          >
            <PathInput
              value={dir}
              placeholder={placeholder(start.dir === null)}
              title={t('task.folder')}
              local={base === '/api'}
              onValue={edit('dir', setDir)}
            />
          </Field>

          <Field label={t('props.comment')} hint={hint(t('props.commentHint'), start.comment === null)}>
            <TextArea
              rows={1}
              value={comment}
              placeholder={placeholder(start.comment === null)}
              onChange={(e) => edit('comment', setComment)(e.target.value)}
            />
          </Field>
        </div>

        {/* A set of controls, so FieldGroup and not Field: a <label> hands its
            click to the first thing inside it, which here would pick a priority
            every time somebody read the caption.

            Priority and auto-extract read as one decision, so they share a grid
            row in the well variant (Tabs.tsx), the tight segmented-control
            treatment the Look page's shape and theme pickers use.

            The strip wraps and never scrolls: the `sm` track measures its own
            labels rather than pinning every segment, and Tabs itself wraps. An
            `overflow-x-auto` wrapper around it would put the horizontal
            scrollbar the language rules out back one level up. */}
        <div className="grid gap-4 sm:grid-cols-2">
          <FieldGroup
            label={t('props.priority')}
            hint={hint(t('props.priorityHint'), start.priority === null)}
          >
            <Tabs
              variant="well"
              size="sm"
              label={t('props.priority')}
              active={priority}
              onSelect={edit('priority', setPriority)}
              items={priorities.map((p) => ({ id: p.id, label: t(p.label) }))}
            />
          </FieldGroup>

          <FieldGroup
            label={t('props.autoExtract')}
            hint={hint(t('props.autoExtractHint'), start.autoExtract === null)}
          >
            <Tabs
              variant="well"
              size="sm"
              label={t('props.autoExtract')}
              active={extract}
              onSelect={edit('autoExtract', setExtract)}
              items={[
                { id: 'inherit', label: t('props.inherit') },
                { id: 'on', label: t('props.on') },
                { id: 'off', label: t('props.off') },
              ]}
            />
          </FieldGroup>
        </div>

        <div className="flex items-center gap-3">
          <Button
            shake={shake}
            disabled={touched.size === 0 || busy}
            onClick={() => void apply()}
          >
            {t('settings.save')}
          </Button>
        </div>
      </Card>
    </section>
  );
}

// TaskListCard holds every package group on one surface.
export function TaskListCard({
  groups,
  base,
  selection,
  profile = 'downloads',
  title,
  hue,
  revealKey,
  onRemovePackage,
}: {
  groups: [string, Task[]][];
  base: string;
  selection?: Selection;
  /**
   * Which stored layout this list uses. The collector and the downloads list
   * want different columns, and one shared layout would mean switching a column
   * off on the list where it is useless switches it off where it is the point.
   */
  profile?: ListProfile;
  /** This card's title badge, pre-translated by the caller the way every other
   *  shared component here takes its label text: Collector.tsx and
   *  Downloads.tsx each want a name distinct from their own page heading,
   *  rather than this file guessing which page it is from `profile`. */
  title: string;
  /** This card's rainbow position, independent of the hues the caller's hero
   *  row or badge row already used; see Collector.tsx and Downloads.tsx for
   *  why each picks a different number. */
  hue?: number;
  /**
   * "Scroll to this row, once." A row key plus a nonce ("task:abc#7"), never a
   * bare id: the guard below handles each distinct value exactly once, so
   * without the nonce revealing the same row twice running is a no-op the
   * second time. See lib/reveal.ts.
   */
  revealKey?: string;
  /** See CellContext.onRemovePackage: the page asks the question, not the row. */
  onRemovePackage?: (ids: string[]) => void;
}) {
  const { t } = useT();
  // Only the row move reports through this so far (see dropBlock and
  // beginGesture): the queue refuses a reorder with a sentence, and a move that
  // is refused in silence reads as a move the app never received.
  const { toast } = useToast();

  const [stored, setStored] = useUIState<ColumnLayout | null>(`list.columns.${profile}`, null);
  const [storedSort, setSort] = useUIState<SortState | null>(`list.sort.${profile}`, null);
  const { collapsed, collapse, expand, toggle } = useCollapsedPackages(profile);
  const [menuAt, setMenuAt] = useState<{ x: number; y: number } | null>(null);
  // The properties panel's visibility, kept apart from the selection: a single
  // click, a Ctrl-click and a Shift-range select without opening the panel, and
  // only a double-click opens it (see onOpenProperties). Reset to closed on
  // every new selection, so it always takes a fresh double-click to reopen for
  // whatever is selected now.
  const [propertiesOpen, setPropertiesOpen] = useState(false);
  // Whether the panel about to appear should take the focus. True only on the
  // Enter path: on a five-thousand-row list the panel renders thousands of
  // pixels below the row that opened it, so "Enter opens the properties" is
  // otherwise true and invisible. Never on the double-click path, where a mouse
  // user has not asked for their focus to be moved.
  const [propertiesAutoFocus, setPropertiesAutoFocus] = useState(false);

  const tableRef = useRef<HTMLDivElement>(null);
  // The rows themselves, without the header row above them or the spacer below:
  // what useRowWindow measures, and what a drag snapshot reads. Separate from
  // tableRef because the header would otherwise count as part of the strip's own
  // top edge, and the window's arithmetic is in the strip's coordinates.
  const stripRef = useRef<HTMLDivElement>(null);
  const drag = useRef<{ id: ColumnId; startX: number; startWidth: number; width: number } | null>(null);

  const layout = useMemo(() => resolveLayout(profile, stored), [profile, stored]);
  const ctx = useMemo<CellContext>(
    () => ({ t, base, profile, onRemovePackage }),
    [t, base, profile, onRemovePackage],
  );

  // A sort on a column that is currently hidden is ignored rather than cleared,
  // so showing the column again brings the order back with it. Applying it while
  // its column is invisible would be a list in an order with nothing on screen
  // to explain it.
  const sort = storedSort && !layout.hidden.has(storedSort.id) ? storedSort : null;
  const sorted = useMemo(() => applySort(groups, sort), [groups, sort]);
  // The order the last drop promised, drawn until the server's own order
  // agrees (see holdOrder). Without it the rows that just landed would jump
  // back to where they were and forward again a tick later. Everything below
  // reads `view`, so a drag that starts in the meantime aims at what is drawn.
  const [pending, setPending] = useState<{ order: string[]; token: number } | null>(null);
  const view = useMemo(
    () => (pending && !sort ? (inOrder(sorted, pending.order) ?? sorted) : sorted),
    [sorted, sort, pending],
  );

  // The table, flattened: every folder header and, while that folder is open,
  // its own links, in the order they are drawn. Everything downstream reads
  // this rather than walking the packages again, so the window, the Shift-range
  // and the rows on screen cannot describe three different lists. See ListRow.
  const rows = useMemo<ListRow[]>(() => {
    const out: ListRow[] = [];
    let hue = 0;
    // The sibling-set numbers a screen reader reads out, counted here where the
    // grouping is still in hand: a folder is one of the folders, a link is one
    // of its own folder's links. Counted off the window instead they would say
    // "3 of 40" on a list of five thousand, which is worse than no answer.
    const packages = view.length;
    let pkgAt = 0;
    for (const [name, items] of view) {
      const folded = collapsed.has(name);
      out.push({
        kind: 'package',
        key: rowKey({ kind: 'package', name }),
        name,
        items,
        collapsed: folded,
        divider: out.length > 0,
        level: 1,
        posinset: ++pkgAt,
        setsize: packages,
      });
      if (!folded) {
        items.forEach((x, i) =>
          out.push({
            kind: 'task',
            key: rowKey({ kind: 'task', id: x.id }),
            task: x,
            index: hue++,
            level: 2,
            posinset: i + 1,
            setsize: items.length,
          }),
        );
      }
    }
    return out;
  }, [view, collapsed]);

  // selectableOrder is the flat, on-screen order a Shift-click's range walks:
  // `rows` above with each entry's own ids attached. A collapsed package
  // contributes only itself, so Shift-clicking across one selects the whole
  // folded package as a single step.
  //
  // Every row the list holds, never only the ones the window has drawn: a range
  // that stopped at the edge of the viewport would select a different set
  // depending on how far somebody had scrolled.
  const selectableOrder = useMemo(
    () =>
      rows.map((r) =>
        r.kind === 'package'
          ? { kind: 'package' as const, key: r.name, ids: r.items.map((x) => x.id) }
          : { kind: 'task' as const, key: r.task.id, ids: [r.task.id] },
      ),
    [rows],
  );

  // The last unit clicked plain or with Ctrl or Cmd, which is what a
  // Shift-click measures its range from. An index into selectableOrder rather
  // than a remembered key, so a Shift-click still works after the list has
  // re-sorted or re-filtered. A Shift-click does not move it (see selectUnit),
  // the same as in Explorer: clicking further away with Shift held extends or
  // shrinks the same range rather than starting a new one.
  const selectAnchor = useRef<number | null>(null);

  /** Every id between two places in the on-screen order, both ends included.
   *  Shared by the Shift-range and the press-and-sweep, which have to agree
   *  about what a range is or the mouse contradicts itself. */
  function rangeIds(a: number, b: number): Set<string> {
    const lo = Math.min(a, b);
    const hi = Math.max(a, b);
    const range = new Set<string>();
    for (let i = lo; i <= hi; i++) selectableOrder[i]?.ids.forEach((id) => range.add(id));
    return range;
  }

  function selectUnit(kind: 'task' | 'package', key: string, ids: string[], e: SelectMods): void {
    if (!selection) return;
    const index = selectableOrder.findIndex((u) => u.kind === kind && u.key === key);
    if (index < 0) return;
    if (e.shiftKey && selectAnchor.current !== null) {
      selection.set(rangeIds(selectAnchor.current, index));
      return; // The anchor itself does not move; see selectAnchor above.
    }
    if (e.ctrlKey || e.metaKey) {
      const next = new Set(selection.ids);
      const allIn = ids.every((id) => next.has(id));
      ids.forEach((id) => (allIn ? next.delete(id) : next.add(id)));
      selection.set(next);
      selectAnchor.current = index;
      return;
    }
    selection.set(new Set(ids));
    selectAnchor.current = index;
  }

  // The rows the properties panel shows values from. The ids it writes come
  // straight off the selection, which is a larger set whenever a quick filter
  // is hiding one of them; see TaskProperties for why the two may differ.
  const chosenIds = selection?.ids;
  const chosen = useMemo(
    () => (chosenIds ? view.flatMap(([, items]) => items).filter((x) => chosenIds.has(x.id)) : []),
    [view, chosenIds],
  );
  // Every new selection closes the panel again. selectUnit always hands `set()`
  // a fresh Set, so this fires on a plain click, a Ctrl-click and a Shift-range
  // alike rather than only on a change in which ids are in it.
  useEffect(() => setPropertiesOpen(false), [chosenIds]);

  // One press, three meanings, which is JDownloader's gesture, and Swing's
  // before it:
  //
  //   press on an unmarked row, then drag  sweep a selection over the rows the
  //                                        pointer passes,
  //   press and let go                     mark that one row,
  //   press on a marked row, then drag     move the whole marking.
  //
  // It could not stay on HTML5 drag, which takes the pointer over the instant
  // it starts: dragstart arrives before the pointer has travelled far enough to
  // tell a sweep from a move. On pointer events the strip can capture the
  // pointer instead, so a row sliding out from under a still one cannot swallow
  // the drop, and the hit test reads the pointer's own Y (see aimAt). What that
  // costs is paid back below: the browser's edge scrolling during a drag (see
  // EDGE_BAND_PX), and a press that must not be swallowed by a text selection
  // or by the native drag of an <img> inside a row.
  //
  // A finger dragged down a list means scroll, so touch has one meaning of its
  // own: holding still on a row arms a move, as on the phone (mobile's
  // DragList). A stylus counts as a mouse here.
  //
  // What a move looks like is GlimStone's drag lift (dragLift.ts): the carried
  // rows float under the pointer as one block, the others slide aside as it
  // passes, and on release the block slides into its gap, or back where it
  // came from after Escape or a drop that changes nothing.
  //
  // Moving is only offered in queue-order view, since a client-side sort is a
  // view and never the queue itself (see applySort). A move attempted under a
  // sort is refused out loud rather than being quietly impossible, and
  // selecting is never refused.
  const dndEnabled = !sort;
  // The rows a move in flight is carrying, in drawn order, or null when nothing
  // is being moved. Several units, because a move carries the whole marking.
  const [rowDrag, setRowDrag] = useState<RowDragKey[] | null>(null);
  // The row currently aimed at mid-move and which half of it, updated on every
  // pointer move rather than only at the drop. That is what lets the other rows
  // step aside live instead of snapping into their new order once the mouse is
  // released.
  const [dragOver, setDragOver] = useState<{ target: RowDragKey; after: boolean } | null>(null);
  // The landing: the keys of the rows that were carried, while they slide into
  // their gap or back home and the others slide with them. Empty when only the
  // others move, as when the server's order replaces a promised one.
  const [settle, setSettle] = useState<ReadonlySet<string> | null>(null);
  const gesture = useRef<Gesture | null>(null);
  // Whether the finger on the list armed a hold, until it lifts; see the
  // touchend listener further down.
  const heldTouch = useRef(false);
  const edgeScroll = useRef<{ frame: number; box: HTMLElement | null }>({ frame: 0, box: null });
  // Where each drawn row was painted before a change of order, so the next
  // commit can slide it from there into its new place.
  const flipFrom = useRef<Map<string, number> | null>(null);
  const pendingRef = useRef(pending);
  pendingRef.current = pending;
  const pendingToken = useRef(0);

  // A frozen snapshot of every row's position, taken once when a move starts.
  // Without it, "which row, which half" comes from whichever DOM element the
  // browser delivered the event to, and that element moves the moment the
  // preview slides it, feeding its own output back in as its next input.
  //
  // It is the one geometry the whole move runs on: the list keeps rendering its
  // resting order while the pointer is down and every row is slid to its
  // previewed place by a translate (previewOffsets below), so the flow the
  // snapshot measured stays the true one throughout.
  //
  // In the strip's own coordinates rather than the viewport's, so it survives
  // the list scrolling under the pointer, and in DOM order, because
  // previewOffsets stacks rows back up in that order and needs the gap between
  // each pair.
  //
  // On a long list these are the window's rows (see useRowWindow), which is the
  // right set: every row a move can aim at is one somebody can see. A move that
  // edge-scrolls far enough to draw rows the snapshot never measured makes the
  // preview bail out whole, and the drop still lands where the last aim pointed.
  const rowSlotsRef = useRef<RowSlot[]>([]);

  function snapshotSlots(): void {
    const root = stripRef.current;
    if (!root) {
      rowSlotsRef.current = [];
      return;
    }
    // Layout offsets and not client boxes, which would count a landing slide
    // still in flight. The strip is every row's offsetParent.
    const slots: RowSlot[] = [];
    root.querySelectorAll<HTMLElement>('[data-task-id],[data-package-row]').forEach((el) => {
      const unit = unitOfRow(el);
      if (!unit) return;
      slots.push({ unit, top: el.offsetTop, bottom: el.offsetTop + el.offsetHeight });
    });
    rowSlotsRef.current = slots;
  }

  /** The pointer's Y in the frame snapshotSlots measured in. */
  function stripY(clientY: number): number {
    const root = stripRef.current;
    return root ? clientY - root.getBoundingClientRect().top : clientY;
  }

  // Every task on screen, flattened out of the package groups in display order:
  // the same tasks `chosen` reads off `view` above rather than the raw `groups`
  // prop, so a drag position always matches what is drawn.
  const flatTasks = useMemo(() => view.flatMap(([, items]) => items), [view]);
  const taskById = useMemo(() => new Map(flatTasks.map((x) => [x.id, x] as const)), [flatTasks]);

  // A band mirrors the reorder endpoint's own grouping: same priority and same
  // forced flag. Each band's ids, in the order they are drawn, is what POST
  // /api/tasks/reorder is sent, and that list is only the part of the band this
  // screen shows and can move (see `movable` below). A band spans every task
  // the app holds, across both tabs and the settled rows alike, and no single
  // list can name all of it.
  const bandOf = (x: Task): string => `${x.priority}:${x.forced ? 1 : 0}`;

  // Which rows the wait queue can be told to move. A finished or failed task is
  // not in the wait queue, so naming one in a reorder refuses the whole request
  // (App.ReorderBand, app_queue.go: "task %s is not in the wait queue"), and
  // this list shows both. Every band built below therefore carries only the
  // rows the server will accept, which the endpoint reads as "these tasks, in
  // this order, in the slots they already hold".
  const movable = (x: Task): boolean => x.status !== 'done' && x.status !== 'error';

  const bandOrder = useMemo(() => {
    const m = new Map<string, string[]>();
    for (const x of flatTasks) {
      if (!movable(x)) continue;
      const key = bandOf(x);
      const arr = m.get(key);
      if (arr) arr.push(x.id);
      else m.set(key, [x.id]);
    }
    return m;
    // movable and bandOf are pure module-level-style helpers over their own
    // argument, so flatTasks is genuinely the only input this varies with.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [flatTasks]);

  // A package's own band, or null when its links disagree: a mixed package has
  // no one band to move it into, so a drop against it is refused rather than
  // guessing which of its tasks the drag should follow. Judged over the movable
  // links only, so a folder holding one finished file beside three queued ones
  // can still be dragged.
  function packageBand(name: string): string | null {
    const items = (view.find(([n]) => n === name)?.[1] ?? []).filter(movable);
    if (items.length === 0) return null;
    const first = bandOf(items[0]);
    return items.every((x) => bandOf(x) === first) ? first : null;
  }

  function unitBand(u: RowDragKey): string | null {
    if (u.kind === 'task') {
      const t = taskById.get(u.id);
      return t && movable(t) ? bandOf(t) : null;
    }
    return packageBand(u.name);
  }

  function unitIds(u: RowDragKey): string[] {
    if (u.kind === 'task') {
      const t = taskById.get(u.id);
      return t && movable(t) ? [u.id] : [];
    }
    return (view.find(([n]) => n === u.name)?.[1] ?? []).filter(movable).map((x) => x.id);
  }

  // Every drawn row with the movable ids it stands for, which is what
  // selectedBlock walks to work out what a move carries. A folder contributes
  // its own movable links, so one holding a finished file still travels whole.
  const blockRows = useMemo<BlockRow[]>(
    () =>
      rows.map((r) =>
        r.kind === 'package'
          ? { unit: { kind: 'package', name: r.name }, ids: r.items.filter(movable).map((x) => x.id) }
          : { unit: { kind: 'task', id: r.task.id }, ids: [r.task.id] },
      ),
    // movable is a pure helper over its own argument, so rows is the only input.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [rows],
  );

  /** Every id a unit stands for, movable or not: what the marking covers, as
   *  against unitIds, which is what the queue can be told to move. A folder
   *  header paints as selected only when all of its links are, finished ones
   *  included, so the press has to ask the same question the paint does. */
  function unitAllIds(u: RowDragKey): string[] {
    if (u.kind === 'task') return [u.id];
    return (view.find(([n]) => n === u.name)?.[1] ?? []).map((x) => x.id);
  }

  /** selectUnit, addressed by drag unit rather than by kind and key. */
  function selectFromUnit(u: RowDragKey, mods: SelectMods): void {
    if (u.kind === 'task') selectUnit('task', u.id, [u.id], mods);
    else selectUnit('package', u.name, unitAllIds(u), mods);
  }

  /**
   * What a move started on `pressed` carries: the whole marking, not the row
   * the hand grabbed. With no selection model, or with a marking the pressed
   * row has fallen out of between the press and the first move (a poll can
   * remove a task mid-gesture), it falls back to the one row, since a move that
   * carries nothing is the gesture doing nothing.
   */
  function blockFor(pressed: RowDragKey): RowDragKey[] {
    const marked = selection?.ids;
    if (!marked || marked.size === 0) return [pressed];
    const block = selectedBlock(blockRows, marked);
    return block.length > 0 ? block : [pressed];
  }

  /**
   * The drop: `block` lands against `target`'s given half, whatever bands and
   * folders that crosses.
   *
   * A marking may hold rows from two priority bands and from three folders at
   * once, so the splice below is band-blind: it takes the target band's order,
   * drops whatever the block holds out of it and puts the block back at the
   * anchor. A row already in that band is filtered out and re-inserted, a row
   * from elsewhere is inserted.
   *
   * The list is ordered by priority before anything else (the server's bands),
   * so rows of two priorities cannot come to rest next to each other and keep
   * them: either the drop is refused or the priority changes. Refusing would
   * mean a marking made with one Shift-click across a band boundary can never
   * be moved, with nothing on screen to explain why, so the rows take the
   * priority they were dropped on and the toast says so.
   *
   * Loose links join the folder they were dropped into, as in JDownloader. A
   * link travelling as part of its own folder does not, that folder moving
   * whole with its links.
   */
  function dropBlock(block: readonly RowDragKey[], target: RowDragKey, after: boolean): Promise<boolean> | null {
    const band = unitBand(target);
    if (!band) return null;
    const movedIds = block.flatMap(unitIds);
    const targetIds = unitIds(target);
    if (movedIds.length === 0 || targetIds.length === 0) return null;
    // Dropped on itself, or on a part of itself. Not a failure, just not a move.
    const moving = new Set(movedIds);
    if (targetIds.some((id) => moving.has(id))) return null;

    const before = bandOrder.get(band) ?? [];
    // A Set and not `movedIds.includes`: a marking can be thousands of rows on a
    // list of thousands, and that pair walked one against the other.
    const order = before.filter((id) => !moving.has(id));
    // Anchored on the target's own edge, its first id when the block lands
    // before it and its last when after, so a folder of several ids keeps its
    // internal order and lands as one contiguous run where a single link would.
    const anchor = after ? targetIds[targetIds.length - 1] : targetIds[0];
    const at = order.indexOf(anchor);
    if (at < 0) return null;
    order.splice(after ? at + 1 : at, 0, ...movedIds);

    const dst = taskById.get(targetIds[0]);
    if (!dst) return null;
    // The rows that have to change hands before the order above means anything.
    const reband = movedIds.filter((id) => {
      const x = taskById.get(id);
      return !!x && bandOf(x) !== band;
    });
    const home = target.kind === 'package' ? target.name : (taskById.get(target.id)?.package ?? '');
    const travellingWithTheirFolder = new Set(
      block.flatMap((u) => (u.kind === 'package' ? unitIds(u) : [])),
    );
    const rehome = movedIds.filter(
      (id) => !travellingWithTheirFolder.has(id) && (taskById.get(id)?.package ?? '') !== home,
    );

    // A drop that lands where it started is not a change and writes nothing.
    // The refusals above are about the drop being impossible; this one is about
    // it being pointless, and it needs its own test because the splice can put
    // every id back where it was: a row dropped on the upper half of the row
    // below it, or the lower half of the one above, gives the list it started
    // from, which is the shortest movement the gesture can make. Compared by
    // result rather than by index, so it catches a whole marking landing on its
    // own footprint too. Only when nothing else was going to happen: a re-band
    // or a change of folder is a real move even at the same index.
    if (
      reband.length === 0 &&
      rehome.length === 0 &&
      order.length === before.length &&
      order.every((id, i) => id === before[i])
    ) {
      return null;
    }

    // The move goes first and the reorder follows it: a row has to be in the
    // band and in the folder before its position among their rows means
    // anything. A refusal has to say something as well as slide the rows back
    // (see holdOrder): reorderTasks throws with the server's own sentence
    // (api.ts, ok()), and swallowing that made a rejected move
    // indistinguishable from a move the app never noticed, which is precisely
    // how it was reported ("funktioniert überhaupt nicht").
    return (async () => {
      try {
        if (rehome.length > 0) await setPackage(rehome, home, base);
        if (reband.length > 0) {
          const wrongPriority = reband.filter((id) => taskById.get(id)?.priority !== dst.priority);
          const wrongForced = reband.filter((id) => !!taskById.get(id)?.forced !== !!dst.forced);
          if (wrongPriority.length > 0) await setTaskPriority(wrongPriority, dst.priority, base);
          if (wrongForced.length > 0) await setTaskForced(wrongForced, !!dst.forced, base);
        }
        await reorderTasks(order, base);
        if (reband.length > 0) toast(t('list.dropChangedPriority', { n: reband.length }), 'info');
        return true;
      } catch (err) {
        toast(t('list.failed', { error: err instanceof Error ? err.message : String(err) }), 'fail');
        return false;
      }
    })();
  }

  // What each press means, decided here together, because deciding these one at
  // a time is how a gesture ends up with three ideas of what "marked" means:
  //
  //  1. Shift extends the range from the anchor and Ctrl adds or removes one
  //     unit, and neither ever starts a move. That departs from Swing, whose
  //     modifier map reads Shift as "move": a Shift-press on a row that happens
  //     to be marked would otherwise mean "extend" or "carry" depending on
  //     something the person cannot see. Held down through a drag both keep
  //     their meaning.
  //
  //  2. A marking that spans two priority bands still moves, and everything in
  //     it takes the priority of the row it was dropped on. See dropBlock.
  //
  //  3. A marked folder takes its links: pressing a folder header marks every
  //     link in it, and selectedBlock emits the folder rather than its links
  //     one by one, so it lands as one run with its order intact.
  //
  //  4. What drops the marking: a plain press on an unmarked row, a plain press
  //     on a marked row once it is released without travelling, a sweep, and
  //     Escape during a sweep. What does not: a poll or websocket tick, folding
  //     a folder, a right-click, a refused move, a press on the empty space
  //     below the rows, and the press on a marked row, which leaves the marking
  //     alone until it knows whether a move is coming.
  //
  // That deferred collapse is Swing's too: BasicTableUI holds the selection
  // change back to mouseReleased whenever the press could have been a drag,
  // which is what makes "click once to mark, then click and hold to move" work.
  // Doing it at the press destroys the marking the second press picks up.
  function pressRow(e: PointerEvent<HTMLElement>, unit: RowDragKey): void {
    // Left button only, and one pointer at a time. A right-click belongs to the
    // context menu, which reads the row it landed on for itself.
    if (e.button !== 0 || !e.isPrimary) return;
    // Everything that is its own control keeps its own gesture: an action badge,
    // the Enabled switch, the folder twisty.
    if (e.target instanceof Element && e.target.closest(CONTROL)) return;
    abortGesture();
    if (e.pointerType === 'touch') {
      holdRow(e, unit);
      return;
    }

    const mods: SelectMods = { ctrlKey: e.ctrlKey, metaKey: e.metaKey, shiftKey: e.shiftKey };
    const before: ReadonlySet<string> = new Set(selection?.ids ?? []);
    const ids = unitAllIds(unit);
    const marked = !!selection && ids.length > 0 && ids.every((id) => before.has(id));
    const mode: 'select' | 'move' =
      mods.ctrlKey || mods.metaKey || mods.shiftKey || !marked ? 'select' : 'move';

    // The cursor follows the pointer, so a later Tab into the list resumes from
    // the row the mouse last touched.
    keys.setCurrent(rowKey(unit));
    // A sweep marks its first row straight away: the press is already the first
    // step of the range, and a row lighting up under the finger is how the
    // gesture says it has begun. A move marks nothing yet, which is (4) above.
    if (mode === 'select') selectFromUnit(unit, mods);

    gesture.current = {
      mode,
      unit,
      fromIndex: selectableOrder.findIndex((u) =>
        unit.kind === 'task' ? u.kind === 'task' && u.key === unit.id : u.kind === 'package' && u.key === unit.name,
      ),
      pointerId: e.pointerId,
      fromX: e.clientX,
      fromY: e.clientY,
      atX: e.clientX,
      atY: e.clientY,
      mods,
      before,
      live: false,
      block: [],
      sweptTo: null,
      over: null,
      touch: false,
      hold: 0,
      travelled: false,
      refused: null,
      originY: 0,
      scale: 0,
    };
  }

  /**
   * A finger on a row scrolls the list, unless it holds still for HOLD_MS,
   * which arms a move of the row, or of the marking the row is in. A hold let
   * go where it armed opens the row's menu instead (see land), since a long
   * press is how a touch screen asks for one, and so does a hold on rows that
   * cannot move (see armHold).
   */
  function holdRow(e: PointerEvent<HTMLElement>, unit: RowDragKey): void {
    const g: Gesture = {
      mode: 'move',
      unit,
      fromIndex: -1,
      pointerId: e.pointerId,
      fromX: e.clientX,
      fromY: e.clientY,
      atX: e.clientX,
      atY: e.clientY,
      mods: PLAIN,
      before: new Set(selection?.ids ?? []),
      live: false,
      block: [],
      sweptTo: null,
      over: null,
      touch: true,
      hold: 0,
      travelled: false,
      refused: null,
      originY: 0,
      scale: 0,
    };
    // Through a ref, since the selection may change before the timer fires.
    g.hold = window.setTimeout(() => armRef.current(g), HOLD_MS);
    gesture.current = g;
    heldTouch.current = false;
  }

  function armHold(g: Gesture): void {
    g.hold = 0;
    const strip = stripRef.current;
    if (gesture.current !== g || !strip) return;
    const ids = unitAllIds(g.unit);
    const marked = !!selection && ids.length > 0 && ids.every((id) => selection.ids.has(id));
    // An unmarked row becomes the marking and travels alone: the selection it
    // is given here has not rendered yet, so blockFor cannot see it.
    if (!marked) selectFromUnit(g.unit, PLAIN);
    keys.setCurrent(rowKey(g.unit));
    heldTouch.current = true;
    const carried = marked ? blockFor(g.unit) : [g.unit];
    // Nobody has tried to drag yet: the hold may only be asking for the row's
    // menu. So a move the queue would refuse leaves a hold that opens the menu
    // on release, and the refusal waits for the finger to travel.
    g.refused = moveRefusal(!dndEnabled, carried, unitIds);
    if (g.refused) return;
    beginGesture(g, strip, carried);
    applyGesture(g);
  }
  const armRef = useRef(armHold);
  armRef.current = armHold;

  /**
   * A refused move says why instead of doing nothing, since rows that simply
   * do not follow the pointer look like a broken list. The banner above a
   * sorted table says the view is sorted, not that the order cannot change,
   * and nothing on a finished or failed row says it has left the wait queue.
   */
  function sayRefused(why: MoveRefusal): void {
    toast(t(why === 'sorted' ? 'list.dragNeedsQueueOrder' : 'list.dragNotInQueue'), 'info');
  }

  /** The press has travelled far enough, or held long enough, to mean
   *  something. `carried` overrides what a move carries. Returns false when
   *  the gesture is refused outright, in which case it is already over. */
  function beginGesture(g: Gesture, strip: HTMLElement, carried?: RowDragKey[]): boolean {
    if (g.mode === 'move') {
      const block = carried ?? blockFor(g.unit);
      const refused = moveRefusal(!dndEnabled, block, unitIds);
      if (refused) {
        sayRefused(refused);
        gesture.current = null;
        return false;
      }
      // A landing still in flight ends here, so the rows stand in their slots.
      if (settle) endSettle();
      // Taken before any preview has run for this move, which is the one point
      // at which the rendered order still matches bandOrder.
      snapshotSlots();
      g.block = block;
      g.originY = stripY(g.fromY);
      setRowDrag(block);
    }
    g.live = true;
    // From here the strip owns the pointer: every move and the release arrive
    // here whatever is painted underneath, which is what a list whose rows
    // slide out from under a still pointer needs.
    try {
      strip.setPointerCapture(g.pointerId);
    } catch {
      // The pointer is already gone (a window switch mid-press). The gesture
      // still works for as long as events keep arriving; the release ends it.
    }
    edgeScroll.current.box = scrollerOf(strip);
    if (!edgeScroll.current.frame) edgeScroll.current.frame = requestAnimationFrame(stepEdgeScroll);
    return true;
  }

  function applyGesture(g: Gesture): void {
    if (g.mode === 'select') {
      sweepTo(g.atX, g.atY);
      return;
    }
    aimBlock(g.atY);
    placeCarried(g);
  }

  /**
   * Draws the carried rows under the pointer as one block (carriedOffsets),
   * straight onto their `translate` so the block keeps up with the pointer
   * without a render. Only rows that already wear the lift class, so their
   * scale grows on the lift's transition instead of jumping.
   */
  function placeCarried(g: Gesture): void {
    const strip = stripRef.current;
    if (!strip || !g.live || g.block.length === 0) return;
    const at = carriedOffsets(
      rowSlotsRef.current,
      rowOffsets,
      movingRows,
      rowKey(g.unit),
      stripY(g.atY) - g.originY,
      { top: 0, bottom: strip.offsetHeight },
      rowKey,
    );
    for (const node of drawnRows(strip)) {
      const dy = at.get(node.dataset.rowKey ?? '');
      if (dy === undefined || !node.classList.contains(LIFT)) continue;
      if (!g.scale) g.scale = liftScale(node);
      node.style.scale = String(g.scale);
      node.style.translate = `0px ${dy}px`;
    }
  }

  /** Where each drawn row is painted, a slide in flight included. */
  function paintedRows(strip: HTMLElement): Map<string, number> {
    const out = new Map<string, number>();
    for (const node of drawnRows(strip)) out.set(node.dataset.rowKey ?? '', node.offsetTop + translateOf(node).y);
    return out;
  }

  /** Cuts a landing short: every row stands in its slot at once. */
  function endSettle(): void {
    const strip = stripRef.current;
    if (strip) {
      for (const node of drawnRows(strip)) {
        node.style.translate = '';
        node.style.scale = '';
      }
    }
    setSettle(null);
  }

  /**
   * Which row a point is on, live. elementFromPoint and not the frozen
   * snapshot, and the two are not interchangeable: nothing slides during a
   * sweep, so the element under the pointer is the row the eye sees, and
   * reading it live keeps the sweep true while the list scrolls under it. A
   * move is the opposite case and uses the snapshot (see aimBlock).
   *
   * Off the rows, past either end of the list or beside it in the margin, it
   * answers the nearest row by Y, so sweeping downward past the last row keeps
   * selecting to the end instead of stopping at whatever the pointer crossed
   * last.
   */
  function unitUnder(x: number, y: number): RowDragKey | null {
    const strip = stripRef.current;
    if (!strip) return null;
    const hit = document.elementFromPoint(x, y);
    const row = hit instanceof Element ? hit.closest<HTMLElement>('[data-row-key]') : null;
    if (row && strip.contains(row)) return unitOfRow(row);
    let best: HTMLElement | null = null;
    let bestDist = Infinity;
    strip.querySelectorAll<HTMLElement>('[data-task-id],[data-package-row]').forEach((el) => {
      const r = el.getBoundingClientRect();
      const d = y < r.top ? r.top - y : y > r.bottom ? y - r.bottom : 0;
      if (d < bestDist) {
        bestDist = d;
        best = el;
      }
    });
    return best ? unitOfRow(best) : null;
  }

  /**
   * The sweep: mark everything between where the press landed and where the
   * pointer is now.
   *
   * A range from the anchor, not a trail of everywhere the pointer has been, so
   * sweeping down to row nine and back up to row three marks three to five and
   * can be undone by moving the mouse back. It is the range a Shift-click makes
   * as well, off the same anchor and through the same rangeIds.
   *
   * Only when the row under the pointer has changed: a selection write
   * re-renders every drawn row, which on a long list costs hundreds of
   * milliseconds (see listRows.ts).
   *
   * Never against the direction the hand went. Both pages that host this list
   * grow a toolbar row the moment anything is marked, so the press that marks
   * the first row pushes the whole table down by more than one row height: the
   * pointer has not moved but the row beneath it has, and sweeping five pixels
   * downward would mark the folder above. Measured from the pressed row and not
   * from the range anchor, because a Shift-press lands somewhere the anchor is
   * not and clamping against the anchor would collapse the range as soon as the
   * hand moved back toward it.
   */
  function sweepTo(x: number, y: number): void {
    const g = gesture.current;
    if (!g || !selection) return;
    const unit = unitUnder(x, y);
    if (!unit) return;
    const under = selectableOrder.findIndex((u) =>
      unit.kind === 'task' ? u.kind === 'task' && u.key === unit.id : u.kind === 'package' && u.key === unit.name,
    );
    if (under < 0) return;
    const anchor = selectAnchor.current ?? under;
    const pressed = g.fromIndex < 0 ? under : g.fromIndex;
    const travel = y - g.fromY;
    const to = travel > 0 ? Math.max(under, pressed) : travel < 0 ? Math.min(under, pressed) : pressed;
    if (to === g.sweptTo) return;
    g.sweptTo = to;
    const range = rangeIds(anchor, to);
    // Ctrl held at the press means "and these as well", so the sweep adds to
    // what was marked before rather than replacing it.
    selection.set(g.mods.ctrlKey || g.mods.metaKey ? new Set([...g.before, ...range]) : range);
  }

  /**
   * The move's hit test, run against the frozen snapshot and the pointer's own
   * Y rather than against the element the browser delivered the event to. Once
   * the preview starts sliding rows, that element is itself a consequence of
   * the last answer this gave, which under a stationary pointer on the boundary
   * between two rows is a closed loop, with rows swapping back and forth
   * instead of settling. Against a snapshot the preview never touches, "which
   * row, which half" is a pure function of the pointer's position.
   */
  function aimBlock(clientY: number): void {
    const g = gesture.current;
    if (!g || g.block.length === 0) return;
    const aim = aimAt(rowSlotsRef.current, stripY(clientY), g.block, {
      // A link row stands for the folder it is in, which makes the whole of an
      // open folder a landing place for another folder rather than only its
      // header (see aimAt). It is also how aimAt knows that a link of a folder
      // that is itself travelling is in flight too.
      packageOf: (id) => taskById.get(id)?.package ?? '',
      // A row in another band is a legal target, since dropBlock re-bands what
      // lands there. What is skipped is a unit in no band at all: a finished or
      // failed download the queue cannot move, and a folder whose links do not
      // agree on one band.
      canTarget: (unit) => unitBand(unit) !== null,
    });
    if (!aim) return;
    g.over = aim;
    setDragOver((prev) => (prev && prev.after === aim.after && sameUnit(prev.target, aim.target) ? prev : aim));
  }

  // applyGesture through a ref, because the frame loop below re-schedules
  // itself and would otherwise keep answering out of the render it started in:
  // a websocket tick during a long drag rebuilds selectableOrder and the view,
  // and a scroll step reading the old copy would sweep against a list that no
  // longer exists. Every other path into applyGesture comes from an event
  // handler, which React rebuilds per render.
  const applyRef = useRef(applyGesture);
  applyRef.current = applyGesture;

  /** The list crawls under the pointer while the pointer sits near an edge. */
  function stepEdgeScroll(): void {
    edgeScroll.current.frame = 0;
    const g = gesture.current;
    if (!g || !g.live) return;
    const box = edgeScroll.current.box;
    const r = box?.getBoundingClientRect();
    const above = g.atY - (r ? r.top : 0);
    const below = (r ? r.bottom : window.innerHeight) - g.atY;
    let dy = 0;
    if (above < EDGE_BAND_PX) dy = -Math.ceil(((EDGE_BAND_PX - Math.max(above, 0)) / EDGE_BAND_PX) * EDGE_SPEED_PX);
    else if (below < EDGE_BAND_PX) dy = Math.ceil(((EDGE_BAND_PX - Math.max(below, 0)) / EDGE_BAND_PX) * EDGE_SPEED_PX);
    if (dy !== 0) {
      if (box) box.scrollTop += dy;
      else window.scrollBy(0, dy);
      // The content moved under a pointer that did not, so the answer has to be
      // taken again, or the list scrolls past the place it is pointing at and
      // the preview stands still through the whole scroll.
      applyRef.current(g);
    }
    edgeScroll.current.frame = requestAnimationFrame(stepEdgeScroll);
  }

  function stopEdgeScroll(): void {
    if (edgeScroll.current.frame) cancelAnimationFrame(edgeScroll.current.frame);
    edgeScroll.current = { frame: 0, box: null };
  }

  function moveGesture(e: PointerEvent<HTMLElement>): void {
    const g = gesture.current;
    if (!g || e.pointerId !== g.pointerId) return;
    g.atX = e.clientX;
    g.atY = e.clientY;
    if (g.touch) {
      const far = Math.abs(e.clientX - g.fromX) > HOLD_SLOP_PX || Math.abs(e.clientY - g.fromY) > HOLD_SLOP_PX;
      // A finger that moves before its hold arms is scrolling, and the browser
      // has it. One that moves after a refused hold armed is trying to drag.
      if (!g.live) {
        if (!far) return;
        if (g.refused) sayRefused(g.refused);
        abortGesture();
        return;
      }
      if (far) g.travelled = true;
    } else if (!g.live) {
      if (!pastThreshold(e.clientX - g.fromX, e.clientY - g.fromY)) return;
      if (!beginGesture(g, e.currentTarget)) return;
    }
    applyGesture(g);
  }

  /** The release. `commit` is false for a gesture the browser or Escape took
   *  away rather than one somebody let go of. */
  function finishGesture(commit: boolean): void {
    const g = gesture.current;
    gesture.current = null;
    stopEdgeScroll();
    const strip = stripRef.current;
    if (g && strip?.hasPointerCapture(g.pointerId)) strip.releasePointerCapture(g.pointerId);
    if (g?.hold) window.clearTimeout(g.hold);
    if (g?.live && g.mode === 'move') {
      land(g, commit);
      return;
    }
    setRowDrag(null);
    setDragOver(null);
    if (!g) return;
    if (!g.live) {
      // A press that never travelled. On a marked row that is the deferred
      // collapse, see (4) above; on an unmarked one the press already did the
      // marking and there is nothing left to do. A finger let go or scrolling
      // before its hold armed was a tap or a scroll, neither of which marks.
      // One let go after a refused hold armed asked for the row's menu.
      if (g.refused) {
        if (commit) openMenu(g);
      } else if (g.mode === 'move' && !g.touch) {
        selectFromUnit(g.unit, PLAIN);
      }
      return;
    }
    // Escape and a cancelled pointer put a sweep back where it started.
    if (!commit) selection?.set(new Set(g.before));
  }

  /**
   * The end of a move. A drop that changes the order draws the promised order
   * at once (holdOrder), and every row slides from where it was painted into
   * its new slot, the carried ones from under the pointer. Anything else slides
   * every row back home and writes nothing: Escape, a cancelled pointer, a drop
   * that changes nothing, and a finger let go where its hold armed, which opens
   * the row's menu.
   */
  function land(g: Gesture, commit: boolean): void {
    const strip = stripRef.current;
    const carried = new Set(movingRows);
    const still = g.touch && !g.travelled;
    const drop = commit && !still;
    const promised = drop && liveView !== view ? liveView.flatMap(([, items]) => items.map((x) => x.id)) : null;
    const write = drop && g.over ? dropBlock(g.block, g.over.target, g.over.after) : null;
    if (write && promised && strip) {
      flipFrom.current = paintedRows(strip);
      holdOrder(promised, write);
    }
    setRowDrag(null);
    setDragOver(null);
    setSettle(carried);
    if (still && commit) openMenu(g);
  }

  /**
   * Draws `order` until the server's own order agrees (see the effect below),
   * the write fails, or HOLD_ORDER_MS after it succeeds.
   */
  function holdOrder(order: string[], write: Promise<boolean>): void {
    const token = ++pendingToken.current;
    setPending({ order, token });
    void write.then((applied) => {
      if (applied) window.setTimeout(() => releaseRef.current(token), HOLD_ORDER_MS);
      else releaseRef.current(token);
    });
  }

  /** Lets go of a promised order the server's has not matched, and every row
   *  slides from the promise to where the server put it. */
  function releaseOrder(token: number): void {
    if (pendingRef.current?.token !== token) return;
    const strip = stripRef.current;
    if (strip && !gesture.current?.live) {
      flipFrom.current = paintedRows(strip);
      // A landing still in flight keeps its carried rows above the others.
      setSettle((prev) => new Set(prev ?? []));
    }
    setPending(null);
  }
  const releaseRef = useRef(releaseOrder);
  releaseRef.current = releaseOrder;

  // The promise is kept only until the server's order says the same, or until
  // the list holds other tasks than the ones it was made of.
  useEffect(() => {
    if (!pending) return;
    const ids = sorted.flatMap(([, items]) => items.map((x) => x.id));
    if (view === sorted || sameOrder(ids, pending.order)) setPending(null);
  }, [pending, sorted, view]);

  /** A long press let go where it armed asks for the row's menu, which the
   *  hold kept the browser from opening. */
  function openMenu(g: Gesture): void {
    const strip = stripRef.current;
    const key = rowKey(g.unit);
    const node = strip ? drawnRows(strip).find((n) => n.dataset.rowKey === key) : undefined;
    node?.dispatchEvent(
      new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: g.atX, clientY: g.atY }),
    );
  }

  // The carried rows follow the pointer through every render the move causes.
  useLayoutEffect(() => {
    const g = gesture.current;
    if (g?.live && g.mode === 'move') placeCarried(g);
  });

  // The landing. A new order was drawn in this commit when flipFrom is set, so
  // every row starts from where it was painted before; then each slides into
  // its slot, the carried rows from under the pointer, and once the slide has
  // played the offsets and the classes go. On a put-back the others are
  // already sliding, since this commit took their preview offsets away.
  useLayoutEffect(() => {
    const strip = stripRef.current;
    if (!settle || !strip) return;
    const from = flipFrom.current;
    flipFrom.current = null;
    const nodes = drawnRows(strip);
    if (from) {
      const moved: HTMLElement[] = [];
      for (const node of nodes) {
        const was = from.get(node.dataset.rowKey ?? '');
        if (was === undefined) continue;
        node.style.transition = 'none';
        node.style.translate = `0px ${was - node.offsetTop}px`;
        moved.push(node);
      }
      // Reading layout commits the jump back, so the slide starts from there.
      void strip.offsetWidth;
      for (const node of moved) node.style.transition = '';
    }
    for (const node of nodes) {
      const carried = settle.has(node.dataset.rowKey ?? '');
      if (carried) node.style.scale = '';
      if (carried || from) node.style.translate = '0px 0px';
    }
    const timer = window.setTimeout(
      () => {
        for (const node of drawnRows(strip)) node.style.translate = '';
        setSettle(null);
      },
      nodes.length > 0 ? settleMs(nodes[0]) : 0,
    );
    return () => window.clearTimeout(timer);
  }, [settle]);

  function abortGesture(): void {
    if (gesture.current) finishGesture(false);
  }

  // Escape lets go of a gesture in flight, which a person holding a mouse
  // button down has no other way to do.
  //
  // Through a ref, because the listener is installed once and the function it
  // calls is rebuilt on every render along with everything it reads. The same
  // ref tears a gesture down if the list unmounts mid-press.
  const abortRef = useRef(abortGesture);
  abortRef.current = abortGesture;
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === 'Escape') abortRef.current();
    };
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('keydown', onKey);
      abortRef.current();
    };
  }, []);

  // Once a hold has armed, the finger drags instead of scrolling, and the
  // browser's long-press menu stays shut while the hold runs or drags; see
  // openMenu for where it goes instead. A refused hold keeps the finger too,
  // so its first move says why rather than scrolling. Lifting the finger sends
  // no mouse events after it either: they would land on whatever row is under
  // the finger by then, and their mousedown closes the menu a still hold
  // opened. Bound natively, since React's touch listeners are passive and
  // cannot cancel.
  useEffect(() => {
    const strip = stripRef.current;
    if (!strip) return;
    function onTouchMove(e: TouchEvent) {
      const g = gesture.current;
      if (g?.touch && (g.live || g.refused)) e.preventDefault();
    }
    function onTouchEnd(e: TouchEvent) {
      if (!heldTouch.current) return;
      heldTouch.current = false;
      e.preventDefault();
    }
    function onMenu(e: Event) {
      const g = gesture.current;
      if (!g?.touch || !(g.live || g.hold || g.refused)) return;
      e.preventDefault();
      e.stopPropagation();
    }
    strip.addEventListener('touchmove', onTouchMove, { passive: false });
    strip.addEventListener('touchend', onTouchEnd, { passive: false });
    strip.addEventListener('contextmenu', onMenu);
    return () => {
      strip.removeEventListener('touchmove', onTouchMove);
      strip.removeEventListener('touchend', onTouchEnd);
      strip.removeEventListener('contextmenu', onMenu);
    };
  }, []);

  // The arrangement the drag in flight is promising: the same groups the table
  // is showing, in the order they would be in if the pointer were released now.
  // It is a description and not what gets rendered, and previewOffsets below
  // turns it into one translate per row. Rendering it would reorder the real
  // rows, leaving a FLIP measurement as the only way to animate them, which
  // reads a mid-animation box as a row's resting place and makes the folders
  // snap instead of step aside.
  //
  // Built the way the real list is, one flat run of tasks handed to
  // groupByPackage, so the preview cannot promise an arrangement the committed
  // order would not produce. Re-sorting each group's tasks in place moves links
  // and can never move a folder, since a package's ids stay contiguous.
  //
  // One splice, and it is the drop's own (rowDrag.ts's previewOrder): the block
  // is lifted out where it is and put back against the target's edge. A
  // band-shaped arrangement gives up when the target is in another band, so a
  // drag across two priorities shows nothing while the pointer is down.
  //
  // The rows the queue cannot move go to the end of that run, where both hosts
  // draw them (Downloads sorts finished and failed rows last, the collector
  // holds none). Left in place, a finished file would keep its folder where it
  // was, and the preview would show another order than the one the server
  // then sends.
  const liveView = useMemo(() => {
    if (!rowDrag || !dragOver) return view;
    const drawn = view.flatMap(([, items]) => items);
    const flat = [...drawn.filter(movable), ...drawn.filter((x) => !movable(x))];
    // The movable ids of each unit, which is what the drop moves too: a
    // finished link inside a folder is not in the wait queue, so it is not part
    // of the block that travels and the preview must not pretend it is.
    const next = previewOrder(
      flat.map((x) => x.id),
      rowDrag.flatMap(unitIds),
      unitIds(dragOver.target),
      dragOver.after,
    );
    if (!next) return view;
    const byId = new Map(flat.map((x) => [x.id, x] as const));
    const shuffled: Task[] = [];
    for (const id of next) {
      const t = byId.get(id);
      if (t) shuffled.push(t);
    }
    // One task per id, or the two lists describe different things: a mismatch
    // would drop a row out of the preview, which React renders as a row that
    // vanished mid-drag.
    if (shuffled.length !== flat.length) return view;
    return groupByPackage(shuffled);
    // unitIds and unitBand read view and taskById, and taskById is derived from
    // view; listing them again would only re-run this on renders that cannot
    // change its result.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view, rowDrag, dragOver]);

  /**
   * How far every row and every folder header has to slide, right now, to show
   * the arrangement liveView above describes: row key -> pixels, and empty when
   * there is nothing to preview.
   *
   * This is the whole animation, and it is the mobile list's answer: DragList
   * keeps its rows where they are and gives the ones the drag has passed a
   * translate offset. Rows here are not one uniform height, so the offsets come
   * from the measured boxes rather than from a single row height.
   *
   * `wanted` is the previewed arrangement flattened to the rows on screen: a
   * folded folder contributes its header and none of its links, and a row the
   * window never drew contributes nothing, having no box to move. The stacking
   * is rowDrag.ts's stackOffsets, where it runs on plain numbers.
   *
   * Bails out whole rather than in part. A poll that adds or removes a task
   * mid-drag leaves the snapshot describing a list that no longer exists, and
   * half-correct offsets would leave rows lying on top of each other, while the
   * drop still commits against dragOver, which never depended on this.
   */
  function previewOffsets(): Map<string, number> {
    if (!rowDrag || !dragOver) return NO_OFFSETS;
    const slots = rowSlotsRef.current;
    const drawn = new Set(slots.map((s) => rowKey(s.unit)));
    const wanted: string[] = [];
    for (const [name, items] of liveView) {
      const header = rowKey({ kind: 'package', name });
      if (drawn.has(header)) wanted.push(header);
      if (!collapsed.has(name)) {
        for (const x of items) {
          const key = rowKey({ kind: 'task', id: x.id });
          if (drawn.has(key)) wanted.push(key);
        }
      }
    }
    return stackOffsets(slots, wanted, rowKey) ?? NO_OFFSETS;
  }

  const rowOffsets = previewOffsets();

  // The rows a move is carrying, as the keys the two row components ask about.
  // A folder's own links are in it as well as its header: they travel with it,
  // so they lift with it, even though nobody named them one by one.
  const movingRows = new Set<string>();
  if (rowDrag) {
    for (const u of rowDrag) {
      movingRows.add(rowKey(u));
      if (u.kind === 'package') for (const id of unitAllIds(u)) movingRows.add(rowKey({ kind: 'task', id }));
    }
  }

  const dnd: RowDnD = {
    // Every drawn row takes SHIFT for the whole move, including the ones that
    // are not in the way yet: the transition has to be on a row before its
    // offset changes, or its first step aside is a jump. It reads the motion
    // level's duration and curve, none at "off".
    look: (unit) => {
      const key = rowKey(unit);
      if (rowDrag) return movingRows.has(key) ? LIFT : SHIFT;
      if (settle) return settle.has(key) ? SETTLE : SHIFT;
      return '';
    },
    press: pressRow,
    // The carried rows are left out: placeCarried draws them under the pointer.
    //
    // No will-change: it would buy layers for several hundred rows that never
    // move, and a translate transition is composited without being told.
    slide: (unit) => {
      const key = rowKey(unit);
      const dy = movingRows.has(key) ? undefined : rowOffsets.get(key);
      return dy === undefined ? NO_SLIDE : { translate: `0px ${dy}px` };
    },
  };

  const template = gridTemplate(layout.visible, layout.widthOf);

  function persist(next: Partial<ColumnLayout>): void {
    setStored({ ...toStored(layout), ...next });
  }

  function toggleColumn(id: ColumnId): void {
    const def = COLUMN_BY_ID.get(id);
    if (!def?.hideable) return;
    const hidden = new Set(layout.hidden);
    if (hidden.has(id)) hidden.delete(id);
    else {
      // Hiding the last one leaves a table with nothing in it, and the menu that
      // would put a column back is anchored to the header that no longer exists.
      if (layout.visible.length <= 1) return;
      hidden.add(id);
    }
    persist({ hidden: [...hidden] });
  }

  function onResize(id: ColumnId, phase: 'start' | 'move' | 'end', e: PointerEvent<HTMLElement>): void {
    if (phase === 'start') {
      e.preventDefault();
      e.currentTarget.setPointerCapture(e.pointerId);
      const w = layout.widthOf(id);
      drag.current = { id, startX: e.clientX, startWidth: w, width: w };
      return;
    }
    const d = drag.current;
    if (!d || d.id !== id) return;
    const rtl = document.documentElement.dir === 'rtl' ? -1 : 1;
    const min = COLUMN_BY_ID.get(id)?.minWidth ?? 40;
    d.width = Math.max(min, Math.round(d.startWidth + (e.clientX - d.startX) * rtl));
    if (phase === 'move') {
      // Painted straight onto the table, off React's render path: re-rendering
      // several hundred rows per pointermove is what makes a column drag
      // stutter, and every row wants the same widths anyway.
      tableRef.current?.style.setProperty(
        '--kl-cols',
        gridTemplate(layout.visible, (x) => (x === d.id ? d.width : layout.widthOf(x))),
      );
      return;
    }
    drag.current = null;
    persist({ widths: { ...layout.widths, [id]: d.width } });
  }

  function resetWidth(id: ColumnId): void {
    const widths = { ...layout.widths };
    delete widths[id];
    persist({ widths });
  }

  // Which slice of `rows` is worth drawing, and how much empty space stands in
  // for the rest. Whole list, no spacers, for anything short enough not to need
  // it (see VIRTUALIZE_ABOVE).
  const win = useRowWindow(rows, stripRef);

  // The list from the keyboard; see listKeyboard.ts for why a windowed list
  // needs a file of its own for it. selectUnit goes in whole rather than being
  // reimplemented: Shift with an arrow has to extend from the same anchor a
  // Shift-click uses, or the two gestures disagree about what is selected.
  const keys = useListKeyboard({
    rows,
    win,
    stripRef,
    selectUnit,
    collapsed,
    collapse,
    expand,
    openProperties: () => {
      setPropertiesAutoFocus(true);
      setPropertiesOpen(true);
    },
    enabled: rows.length > 0,
  });

  // Scroll a named row into view, exactly once per request.
  //
  // `win` is a fresh object on every scroll (useRowWindow sets viewport state
  // per animation frame), so an effect depending on it would scroll, move the
  // viewport, make a new `win` and scroll again, which never ends on a list
  // with a running download. It is read through a ref and is not a dependency,
  // and a second ref holds the last revealKey handled.
  //
  // That second ref is set only once the row has been found. A revealKey can
  // arrive one commit before the task does, and marking it handled on the way
  // past would swallow the request for good.
  const winRef = useRef(win);
  winRef.current = win;
  const revealed = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!revealKey || revealed.current === revealKey) return;
    const strip = stripRef.current;
    if (!strip) return;
    const key = revealKey.split('#')[0];
    const index = rows.findIndex((r) => r.key === key);
    if (index < 0) return;
    revealed.current = revealKey;

    // Walked rather than matched with a selector: a package name is whatever
    // somebody typed, and quoting it into an attribute selector is a class of
    // bug nobody needs. At most a windowful of nodes is drawn.
    const find = () =>
      [...strip.querySelectorAll<HTMLElement>('[data-row-key]')].find(
        (el) => el.getAttribute('data-row-key') === key,
      ) ?? null;

    const here = find();
    if (here) {
      here.scrollIntoView({ block: 'center', inline: 'nearest' });
      return;
    }

    // The row is outside the drawn window, so there is no element to scroll to
    // and the two spacers carry no data attributes. A 1px marker at the row's
    // own offset stands in for it, and the scroll it causes moves the window
    // over the real row.
    const mark = document.createElement('div');
    mark.style.position = 'absolute';
    mark.style.top = `${winRef.current.topOf(index)}px`;
    mark.style.height = '1px';
    mark.style.width = '1px';
    strip.appendChild(mark);
    mark.scrollIntoView({ block: 'center', inline: 'nearest' });
    mark.remove();

    // Then again, precisely. The first pass lands on ROW_ESTIMATE for every row
    // nobody has drawn, since measureRows only measures what is in the window,
    // and on a five-thousand-row list that is hundreds of pixels out. Two
    // frames: one for the render the scroll caused, one for the measuring pass.
    let second = 0;
    const first = requestAnimationFrame(() => {
      second = requestAnimationFrame(() => find()?.scrollIntoView({ block: 'center', inline: 'nearest' }));
    });
    return () => {
      cancelAnimationFrame(first);
      cancelAnimationFrame(second);
    };
  }, [revealKey, rows]);

  return (
    // Two surfaces side by side and never one inside the other: the panel is a
    // card of its own under the list, because a card inside a card is the one
    // elevation rule this language does not bend.
    //
    // flex-1 on both, not h-full: harmless where a caller renders this in
    // normal document flow, since a flex item with no flex container ancestor
    // falls back to its natural content size, and meaningful where a caller
    // wraps it in its own flex column. A percentage height does not reliably
    // propagate through a block box whose height came from flex-grow plus
    // overflow-auto, while flex-grow has none of that ambiguity, so this is the
    // one place in this component that composes with a caller's height.
    <div className="flex flex-1 flex-col gap-6">
      {/* No overflow-hidden on this outer box: SectionTitle's pill sits half
          over the card's top edge by design, and overflow-hidden on the
          element hosting its `position: relative` clips that half off. This
          card does have flush-edged content below the title, since the table
          runs edge-to-edge under overflow-x-auto, so the clip still exists,
          scoped to a wrapper that starts below the title. rounded-b and not
          rounded-t to match: the badge's negative offset only reaches into the
          pt-4 above it, and a table scrolled all the way down still needs the
          rounded bottom an unclipped square row could break.

          A hand-rolled card rather than <Card>, so the palette position is set
          on the card and everything it holds follows it. `hue` is optional, and
          a card without one must not take the class: `.glim-hue` with no
          --item-hue resolves --accent to nothing and the badge disappears. */}
      <div
        className={`glim-card ${hue !== undefined ? 'glim-hue ' : ''}flex-1`}
        style={hue !== undefined ? (hueVars(hue) as CSSProperties) : undefined}
      >
        <div className="flex items-center gap-2 px-4 pt-4">
          {/* The bubble on this badge holds the one explanation the table needs
              and nothing else could carry: right-click the header for the
              column menu, click a label to sort. One bubble and not two, since
              the keys are listed under Settings, Shortcuts, and two bubbles
              side by side make the reader decide which one holds their answer
              before they can read either. */}
          {/* Sorting is a view of the queue and not the queue, and saying so
              where the order is visibly different is the whole of it: a list
              that shows one order while running another reads as a bug. */}
          <SectionTitle
            hint={t('columns.headerHint')}
            second={sort ? { label: t('list.sortedView'), hint: t('list.sortedViewTip') } : undefined}
          >
            {title}
          </SectionTitle>
        </div>
        <div className="overflow-hidden rounded-b-[var(--radius-card)]">

          {/* min-w-min, not min-w-max: max-content pins the table at the sum of
              its columns, overriding the flexible name track and opening the
              list scrolled off its right edge in any narrower window. With
              min-content the name column gives way to its own minimum first,
              and only then does the table scroll. */}
          <div className="overflow-x-auto">
            <div ref={tableRef} className="min-w-min" style={{ ['--kl-cols' as string]: template } as CSSProperties}>
              <Header
                layout={layout}
                profile={profile}
                sort={sort}
                onSort={(id) => setSort(nextSort(storedSort, id))}
                onReorder={(id, target, after) =>
                  persist({ order: moveColumn(layout.order.map((c) => c.id), id, target, after) })
                }
                onResize={onResize}
                onResizeReset={resetWidth}
                onMenu={setMenuAt}
              />

              {/* `rows` is built from `view` and not from liveView: the rows
                  stay in the order the server last gave for the whole of a
                  drag, and each one is slid to where the drag would put it
                  (previewOffsets above, applied by dnd.slide). Reordering them
                  here leaves the move with nothing to animate but an
                  after-the-fact measurement, and walks the rainbow hues over a
                  moving list, so colours shuffle under the pointer as a side
                  effect of a reorder nobody has committed. The `index` each row
                  carries counts the resting order.

                  The two boxes around the slice are the rest of the list, as
                  height and nothing else (see useRowWindow). They carry no data
                  attributes: the drag snapshot, the right-click and the
                  selection all look rows up by those, and a spacer is not a row
                  any of them may find.

                  role="tree" and not "treegrid": nothing here navigates to a
                  cell, left and right close and open, so cell roles would
                  describe an interaction this list does not have, and a
                  treegrid would force roles onto the header row, onto the tail
                  spacer and onto the cell holding the row's actions.

                  `relative` is for the two probes, the keyboard's and the event
                  list's. A jump to a row the window has not drawn has nothing
                  to focus, so a one-pixel box is placed at that row's offset and
                  scrolled into view, and the commit after the scroll finds the
                  real row (listKeyboard.ts, and the revealKey effect above).
                  Neither probe carries data attributes, for the reason the
                  spacers do not: measureRows averages every [data-row-key] it
                  finds into the height estimate, and a 1px row in that average
                  drags the whole list's scrollbar toward zero.

                  overflow-x-clip keeps the lift's scale from growing a
                  horizontal scrollbar on a table exactly as wide as its card,
                  and the callout rule keeps iOS from answering a long press
                  on a row's link or icon. */}
              <div
                ref={stripRef}
                className="relative overflow-x-clip [-webkit-touch-callout:none]"
                role="tree"
                aria-multiselectable="true"
                aria-label={title}
                tabIndex={keys.stripTabIndex}
                onFocus={keys.onStripFocus}
                // The whole gesture hangs here and not on the rows: a press
                // bubbles up from whichever row it landed on, and from the
                // moment it becomes a gesture this element holds the pointer
                // capture, so every move and the release arrive here whatever
                // is painted underneath. Under a native HTML5 drag the browser
                // hit-tests a transformed element where it is painted and only
                // reconsiders when the pointer moves, so a release with no last
                // twitch arrives on an element that was never sent a dragover
                // and Chromium answers with dragend and no drop at all.
                onPointerMove={moveGesture}
                onPointerUp={(e) => {
                  if (gesture.current?.pointerId === e.pointerId) finishGesture(true);
                }}
                // The browser taking the pointer away (a touch turning into a
                // scroll, a system gesture, a window switch) is not a drop.
                onPointerCancel={(e) => {
                  if (gesture.current?.pointerId === e.pointerId) abortGesture();
                }}
                // Only the strip's own capture: a finger's row loses its
                // implicit capture to the strip when a hold arms, and that
                // event bubbles here too.
                onLostPointerCapture={(e) => {
                  if (e.target === e.currentTarget && gesture.current?.pointerId === e.pointerId) abortGesture();
                }}
                // No native drag may start inside this list any more. Nothing
                // here sets `draggable`, but an <img> is draggable by default and
                // the hoster icon in every row is one - a press that lands on it
                // and moves would hand the pointer to the browser mid-sweep.
                onDragStart={(e) => e.preventDefault()}
              >
                {keys.probeTop !== null && (
                  <div
                    ref={keys.probeRef}
                    aria-hidden
                    style={{ position: 'absolute', top: keys.probeTop, height: 1, width: 1 }}
                  />
                )}
                {win.padTop > 0 && <div aria-hidden style={{ height: win.padTop }} />}
                {rows.slice(win.start, win.end).map((row) =>
                  row.kind === 'package' ? (
                    <PackageRow
                      key={row.key}
                      name={row.name}
                      items={row.items}
                      base={base}
                      ctx={ctx}
                      columns={layout.visible}
                      selection={selection}
                      collapsed={row.collapsed}
                      divider={row.divider}
                      onToggleCollapsed={() => toggle(row.name)}
                      dnd={dnd}
                      level={row.level}
                      posinset={row.posinset}
                      setsize={row.setsize}
                      current={keys.currentKey === row.key}
                      onKeyDown={(e) => keys.onRowKeyDown(e, row.key)}
                      onOpenProperties={() => {
                        setPropertiesAutoFocus(false);
                        setPropertiesOpen(true);
                      }}
                    />
                  ) : (
                    <TaskRow
                      key={row.key}
                      task={row.task}
                      index={row.index}
                      base={base}
                      ctx={ctx}
                      columns={layout.visible}
                      selection={selection}
                      dnd={dnd}
                      level={row.level}
                      posinset={row.posinset}
                      setsize={row.setsize}
                      current={keys.currentKey === row.key}
                      onKeyDown={(e) => keys.onRowKeyDown(e, row.key)}
                      onOpenProperties={() => {
                        setPropertiesAutoFocus(false);
                        setPropertiesOpen(true);
                      }}
                    />
                  ),
                )}
                {win.padBottom > 0 && <div aria-hidden style={{ height: win.padBottom }} />}
              </div>

              {/* The empty space under the rows earns its keep twice: a table
                  that ends flush against the edge of its card reads as cut off,
                  and a right-click needs somewhere to land that is not a row.
                  That is where the list's own menu lives, the same place a
                  desktop list keeps it. */}
              <div className="h-10" />
            </div>
          </div>
        </div>

        {menuAt && (
          <ColumnMenu
            at={menuAt}
            columns={layout.order}
            hidden={layout.hidden}
            onToggle={toggleColumn}
            onReset={() => {
              setStored(null);
              setMenuAt(null);
            }}
            onClose={() => setMenuAt(null)}
          />
        )}
      </div>

      {/* Keyed on the selection, so picking different rows re-reads the boxes
          instead of carrying one selection's half-typed comment onto another.
          Nothing to edit means no panel: an empty properties panel is a card of
          disabled controls, which reads as broken. propertiesOpen gates it on
          top of that, so a double-click is what opens it. */}
      {propertiesOpen && chosenIds && chosen.length > 0 && (
        <TaskProperties
          key={[...chosenIds].join(',')}
          ids={[...chosenIds]}
          tasks={chosen}
          base={base}
          autoFocus={propertiesAutoFocus}
        />
      )}
      {/* The read-only detail, below the properties card and below the strip.
          Below is load-bearing: useRowWindow measures stripRef after every
          commit, so a card above it that grows when an error string arrives, or
          a <video> that reserves space when it loads metadata, repaints the
          whole windowed slice each time it does.

          One row only. A folder header double-click selects a whole package,
          which TaskProperties is built for and this is not: there is no single
          answer for "the error", "the next attempt" or "the file" across eleven
          links. chosenIds is the whole selection and chosen only the rows this
          view can see, so a quick filter hiding the selected row must not put a
          one-row panel up for a different row.

          No key, unlike TaskProperties above: that one snapshots its boxes at
          mount, this one re-renders live off the task object the websocket
          replaces on every tick, and a key derived from anything that moves
          would tear the player down once a second. */}
      {propertiesOpen && chosenIds?.size === 1 && chosen.length === 1 && (
        <TaskDetailPanel task={chosen[0]} base={base} hue={hue === undefined ? undefined : hue + 1} />
      )}
    </div>
  );
}

// groupByPackage groups tasks by their package, preserving insertion order.
export function groupByPackage(list: Task[]): [string, Task[]][] {
  const m = new Map<string, Task[]>();
  for (const t of list) {
    const arr = m.get(t.package || '');
    if (arr) arr.push(t);
    else m.set(t.package || '', [t]);
  }
  return [...m.entries()];
}
