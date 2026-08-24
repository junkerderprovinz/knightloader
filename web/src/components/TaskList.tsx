import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type DragEvent, type PointerEvent } from 'react';
import { createPortal } from 'react-dom';
import { priorityChoices, type PriorityChoice, type Task, type TaskOptionsPatch } from '../lib/api';
import { hueVars, rainbowAt } from '../lib/appearance';
import { useRainbow } from '../lib/useRainbow';
import { pause, resume, remove, startTasks, restartTasks, recheckTasks, setTaskOptions } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { useUIState } from '../lib/uistate';
import { Button, Card, Field, FieldGroup, IconBadge, InfoBubble, SectionTitle, TextArea, TextInput } from './ui';
import { Tabs } from './Tabs';
import { TaskOptionsDialog } from './ListToolbar';
import { ColumnMenu } from './ColumnMenu';
import {
  COLUMN_BY_ID,
  Checkbox,
  FOLDER_GLYPH,
  TREE_INDENT,
  applySort,
  gridTemplate,
  moveColumn,
  nextSort,
  resolveLayout,
  toStored,
  type CellContext,
  type ColumnDef,
  type ColumnId,
  type ColumnLayout,
  type ListProfile,
  type ResolvedLayout,
  type SortState,
} from './columns';
import {
  IconPause,
  IconPlay,
  IconTrash,
  IconRetry,
  IconFolder,
  IconSearch,
  IconArrowUp,
  IconArrowDown,
} from '../lib/icons';

export interface Selection {
  ids: Set<string>;
  toggle: (id: string) => void;
}

// A stable empty default: useUIState leaves the fallback out of its dependencies
// on purpose, and handing it a fresh [] on every render would make every
// subscriber think the value changed.
const NO_COLLAPSED: string[] = [];

// Every row is the same grid, and the track list reaches it through one custom
// property set on the table. That is what lets a column drag repaint by touching
// a single element instead of re-rendering several hundred rows per pointer move.
const ROW_GRID: CSSProperties = { gridTemplateColumns: 'var(--kl-cols)' };

/**
 * useCollapsedPackages is the folded set, and the only thing that knows where it
 * is kept.
 *
 * The list card holds it and so does the page, because the right-click menu
 * folds packages too and the menu belongs to the page. Both read the same field
 * of the same store, which notifies every subscriber on write — so the twisty
 * and the menu entry can never disagree about what is open.
 *
 * Folded packages are keyed by name, which is the only identity the wire model
 * carries: core.Task has a package name and no package id. SetPackage rewrites
 * the name, so a rename or a Packagizer re-package makes a folded package come
 * back expanded. That is the mild failure of the two — the harmful one would be
 * pruning names that are not on screen, which would unfold every package the
 * search is currently hiding — but it needs a stable package id on the task to
 * fix properly, and that is the model owner's lane, not this file's.
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
 * The tree control, as a filled triangle rather than a chevron.
 *
 * Swing's JTree draws exactly this, which is what a package row looks like in
 * JDownloader, and it is the one mark on the row that says "there is something
 * inside this". It points along the reading direction when shut and downward
 * when open, so in an Arabic or Hebrew interface it points the way that
 * interface reads.
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
}: {
  task: Task;
  base: string;
  ctx: CellContext;
  columns: ColumnDef[];
  selection?: Selection;
  /** Position in the rendered list — the rainbow palette position. */
  index: number;
}) {
  const { t } = useT();
  const [options, setOptions] = useState(false);
  const collected = task.status === 'collected';
  const settled = task.status === 'done' || task.status === 'error';

  // In rainbow mode the row owns a colour, and everything inside it that paints
  // activity — the progress fill above all — reads it through --accent without
  // knowing the mode exists. A running row counts as active, so the reactive
  // reading still shows colour where work is actually happening.
  //
  // The colour comes from the row's position, not from a hash of its id. A hash
  // is stable when rows above finish, which sounds better until three rows land
  // in the same bucket and two neighbours share a colour — which is the one
  // thing the mode exists to prevent. By position, eight adjacent rows always
  // differ.
  return (
    <div
      // What a right-click landed on. Without it the menu can only ever act on
      // whatever happened to be selected already, which is how the wrong
      // download gets deleted.
      data-task-id={task.id}
      style={{ ...hueVars(rainbowAt(index)), ...ROW_GRID } as CSSProperties}
      className={`glim-hue glim-tint ${task.status === 'running' ? 'glim-active' : ''} group relative grid
        items-center px-3 py-2 transition-colors hover:bg-carbon-hover/50`}
    >
      <div className="flex items-center justify-center">
        {selection && (
          <Checkbox
            checked={selection.ids.has(task.id)}
            onChange={() => selection.toggle(task.id)}
            label={t('select.row')}
          />
        )}
      </div>

      {columns.map((col) => {
        const node = col.render(task, ctx);
        return (
          <div
            key={col.id}
            dir={col.ltr ? 'ltr' : undefined}
            // A column that renders plain text gets that text as its native
            // tooltip, so a name too long for its width is still readable
            // without widening the column first.
            title={typeof node === 'string' ? node : undefined}
            // The name cell is the tree column, so a link inside a package is
            // indented under it — the second half of what makes the header above
            // read as a container rather than as another row in bold. Written as
            // its own padding pair rather than as `px-2 ps-6`, because two
            // utilities setting the same edge leave the result to stylesheet
            // order.
            //
            // TREE_INDENT, not a guess: it is the exact width of the package
            // row's leading furniture (see columns.tsx), so a link's name starts
            // where its package's name starts. At `ps-9` it started 24px BEFORE
            // it — measured on the live instance — which reads as the link being
            // the outer level and the package the inner one, i.e. the tree
            // upside down.
            style={col.id === 'name' ? { paddingInlineStart: `${TREE_INDENT}px` } : undefined}
            className={`min-w-0 truncate text-[12.5px] text-carbon-textSub ${
              col.id === 'name' ? 'pe-2' : 'px-2'
            } ${col.align === 'end' ? 'text-end' : 'text-start'} ${col.numeric ? 'glim-num' : ''}`}
          >
            {node}
          </div>
        );
      })}

      {/* The primary action stays visible; the rest appears on hover or focus,
          so a long list reads as content instead of a wall of buttons.
          IconBadge, not a plain ghost icon (jdp, on the same pattern in
          Rules.tsx: "die icons ... sind nicht im Glimstone. das sollen
          farbige quadratischen badges mit icon sein") - this is the
          highest-traffic row in the app, so it gets the fix first. */}
      <div className="flex items-center justify-end gap-1">
        {collected && (
          <IconBadge icon={<IconPlay />} title={t('task.start')} onClick={() => startTasks([task.id], base)} />
        )}
        {task.status === 'running' && (
          <IconBadge icon={<IconPause />} title={t('task.pause')} onClick={() => pause(task.id, base)} />
        )}
        {task.status === 'paused' && (
          <IconBadge icon={<IconPlay />} title={t('task.resume')} onClick={() => resume(task.id, base)} />
        )}
        <div className="flex items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
          {collected && (
            <IconBadge
              icon={<IconSearch />}
              title={t('task.recheck')}
              onClick={() => recheckTasks([task.id], base)}
            />
          )}
          <IconBadge icon={<IconFolder />} title={t('task.folder')} onClick={() => setOptions(true)} />
          {settled && (
            <IconBadge
              icon={<IconRetry />}
              title={t('task.restart')}
              onClick={() => restartTasks([task.id], base)}
            />
          )}
          <IconBadge kind="danger" icon={<IconTrash />} title={t('task.remove')} onClick={() => remove(task.id, base)} />
        </div>
      </div>

      {/* Into <body>, because the table is a horizontal scroll container now and
          a dialog that belongs to a row must not be laid out inside one. */}
      {options &&
        createPortal(
          <TaskOptionsDialog tasks={[task]} base={base} onClose={() => setOptions(false)} />,
          document.body,
        )}
    </div>
  );
}

/**
 * The package header's name cell — the folder, the tree control, the name and
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
}: {
  name: string;
  items: Task[];
  collapsed: boolean;
  onToggle: () => void;
}) {
  const { t } = useT();
  const done = items.filter((x) => x.status === 'done').length;
  const online = items.filter((x) => x.online === 'online').length;
  const label = t(collapsed ? 'task.expand' : 'task.collapse');

  const count = `${items.length} ${items.length === 1 ? t('task.file') : t('task.files')}${
    done > 0 ? ` · ${done} ${t('overview.done').toLowerCase()}` : ''
  }`;

  return (
    // @container, because what follows the name is whole or gone — never
    // shredded. `truncate` is the right tool for a value that still means
    // something cut short, and the wrong one for a two-word label: at a narrow
    // name column "2 Dateien" rendered as "2 D…", which is not information, it
    // is damage. Sized against this row rather than the viewport, since the name
    // column is dragged and hidden independently of the window.
    <div className="@container flex min-w-0 items-center gap-1.5">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={!collapsed}
        aria-label={label}
        title={label}
        className="grid h-6 w-6 shrink-0 place-items-center rounded-[var(--radius-control)] text-carbon-textSub
          transition-colors hover:bg-carbon-surface3 hover:text-carbon-text"
      >
        <Twisty open={!collapsed} />
      </button>
      {/* Furniture, never the accent: every package has one, and a column of
          gold folders would spend the one colour that means "something is
          happening here" on the most ordinary fact on the page. */}
      <IconFolder
        width={FOLDER_GLYPH}
        height={FOLDER_GLYPH}
        className="shrink-0 text-carbon-textMuted"
      />
      {/* The name wins the room. Everything after it is shrinkable and the name
          is not, below its own floor: with the counts pinned instead, a package
          called "Season One" in a 200px column rendered as "S · 3 files", which
          is the one word on the row nobody can do without. */}
      <span
        title={`${name || t('task.ungrouped')} — ${count}`}
        className="min-w-[5rem] flex-1 truncate text-[13.5px] font-semibold text-carbon-text"
      >
        {name || t('task.ungrouped')}
      </span>
      {/* Both counts hang on the name's title as well, so a column too narrow to
          show them has not hidden anything that cannot be got at. */}
      <span className="glim-num hidden shrink-0 whitespace-nowrap text-[11px] text-carbon-textSub @[13rem]:inline">
        {count}
      </span>
      {online > 0 && (
        <span className="glim-num hidden shrink-0 whitespace-nowrap text-[11px] text-carbon-textMuted @[17rem]:inline">
          {t('task.onlineRatio', { online, total: items.length })}
        </span>
      )}
    </div>
  );
}

// Anything that is itself a control keeps its own click. Everything else on a
// package header folds it, which is what people try first and what JDownloader
// does.
const CONTROL = 'button, a, input, select, textarea, [role="switch"], [role="checkbox"], .glim-info';

// PackageGroup is a plain block inside the list card — not a nested card. Its
// totals are over every link in the package, folded or not: a collapsed package
// that stops counting turns the header into a number that changes when you click
// a chevron.
function PackageGroup({
  name,
  items,
  base,
  ctx,
  columns,
  selection,
  collapsed,
  onToggleCollapsed,
  indexOffset,
}: {
  name: string;
  items: Task[];
  base: string;
  ctx: CellContext;
  columns: ColumnDef[];
  selection?: Selection;
  collapsed: boolean;
  onToggleCollapsed: () => void;
  indexOffset: number;
}) {
  const { t } = useT();
  const allSelected = selection && items.every((x) => selection.ids.has(x.id));

  return (
    <section>
      <div
        // The name is the only identity a package has on the wire, and it is
        // legitimately empty for the ungrouped one — so the attribute is present
        // and empty rather than absent, and the page tells the two apart.
        data-package-row={name}
        style={ROW_GRID}
        onClick={(e) => {
          if (e.target instanceof Element && e.target.closest(CONTROL)) return;
          onToggleCollapsed();
        }}
        // A colour step, not a rule: the header sits on the quiet surface and
        // the links inside it sit on the card, which is the whole of the weight
        // difference between a container and its contents.
        className="grid cursor-pointer select-none items-center bg-carbon-surface2/80 px-3 py-2.5
          transition-colors hover:bg-carbon-surface2"
      >
        <div className="flex items-center justify-center">
          {selection && (
            <Checkbox
              checked={!!allSelected}
              label={t('select.all')}
              onChange={() => {
                const target = !allSelected;
                items.forEach((x) => {
                  if (selection.ids.has(x.id) !== target) selection.toggle(x.id);
                });
              }}
            />
          )}
        </div>

        {columns.map((col) => (
          <div
            key={col.id}
            className={`min-w-0 truncate px-2 text-[12px] text-carbon-textSub ${
              col.align === 'end' ? 'text-end' : 'text-start'
            } ${col.numeric ? 'glim-num' : ''}`}
          >
            {col.id === 'name' ? (
              <PackageName name={name} items={items} collapsed={collapsed} onToggle={onToggleCollapsed} />
            ) : (
              col.aggregate?.(items, ctx)
            )}
          </div>
        ))}

        {/* The actions gutter, empty in a header row. One cell per track: a
            spare one wraps the grid onto a second line. */}
        <span />
      </div>

      {!collapsed && (
        <div className="flex flex-col">
          {items.map((x, i) => (
            <TaskRow
              key={x.id}
              task={x}
              index={indexOffset + i}
              base={base}
              ctx={ctx}
              columns={columns}
              selection={selection}
            />
          ))}
        </div>
      )}
    </section>
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
  sort,
  onSort,
  onReorder,
  onResize,
  onResizeReset,
  onMenu,
}: {
  layout: ResolvedLayout;
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
      className="grid items-center border-b border-carbon-border/60 px-3 py-1 select-none"
    >
      <div className="flex items-center justify-center">
        <InfoBubble tip={t('columns.headerHint')} />
      </div>

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
              title={sortable ? t('columns.sortHint') : undefined}
              className={`flex min-w-0 flex-1 items-center gap-1 px-2 py-1.5 text-[11px] font-semibold uppercase
                tracking-wide transition-colors ${col.align === 'end' ? 'justify-end' : 'justify-start'} ${
                  sorted ? 'text-carbon-text' : 'text-carbon-textMuted hover:text-carbon-textSub'
                }`}
            >
              <span className="truncate">{t(col.labelKey)}</span>
              {sorted === 'asc' && <IconArrowUp width={11} height={11} className="shrink-0" />}
              {sorted === 'desc' && <IconArrowDown width={11} height={11} className="shrink-0" />}
            </button>

            {/* Double-click gives a column its built-in width back, which is the
                only way out of a column dragged down to its minimum. */}
            <span
              role="separator"
              aria-orientation="vertical"
              aria-label={t('columns.resizeHint')}
              title={t('columns.resizeHint')}
              onPointerDown={(e) => onResize(col.id, 'start', e)}
              onPointerMove={(e) => onResize(col.id, 'move', e)}
              onPointerUp={(e) => onResize(col.id, 'end', e)}
              // A drag the browser takes away (a context menu, a window switch)
              // must still settle the width, or the table keeps a hand-painted
              // track list that the next render silently undoes.
              onPointerCancel={(e) => onResize(col.id, 'end', e)}
              onDoubleClick={() => onResizeReset(col.id)}
              className="absolute inset-y-0 end-0 z-10 w-2 cursor-col-resize touch-none"
            />
          </div>
        );
      })}

      <span />
    </div>
  );
}

// --- The properties panel --------------------------------------------------

/** Which box was edited. Nothing else is sent - see TaskProperties. */
type PropField = 'name' | 'dir' | 'comment' | 'priority' | 'autoExtract';

/**
 * The priorities, from the server, low to high so the strip reads as a scale
 * rather than as a list somebody happened to order that way.
 *
 * This used to be five hardcoded steps with a key set of their own, while the
 * right-click menu offered the server's seven. One app, two answers to "how
 * many priorities are there", and the panel's five could not even express the
 * outer two the queue sorts by. The server's list is the list; priorityChoices()
 * fetches it once per session and both callers read the same copy.
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
 * share - or null when they do not, which is a third answer and not an empty
 * one.
 */
function agree<T>(tasks: Task[], pick: (t: Task) => T): T | null {
  if (tasks.length === 0) return null;
  const first = pick(tasks[0]);
  return tasks.every((x) => pick(x) === first) ? first : null;
}

/**
 * TaskProperties edits what is selected - one row or forty, the same panel and
 * the same request.
 *
 * The rule everything here follows: a field is sent ONLY if it was edited. Not
 * "if it differs from what was loaded", and above all not "if it is non-empty".
 * A selection whose rows disagree opens with an empty box, and an empty box that
 * gets sent writes nothing over forty comments, forty folders and forty
 * passwords in one click - a loss nobody would connect to the field they never
 * touched. So the boxes carry a placeholder rather than a value, and `touched`
 * is set by the change handler rather than derived by comparison, which is what
 * keeps "they disagree" and "I cleared this on purpose" apart. Both look like an
 * empty string on the wire; only one of them is in the request at all.
 *
 * `ids` is the whole selection and `tasks` is the part of it this list can see.
 * They are not the same thing - a quick filter can hide a selected row - and the
 * split is deliberate: what is WRITTEN is the whole selection, what is SHOWN is
 * read off the rows that are on screen. A hidden row can only ever make the
 * panel show a value as agreed when it is not, and since nothing is written
 * unless it was edited, that costs a placeholder and never a value.
 */
export function TaskProperties({ ids, tasks, base }: { ids: string[]; tasks: Task[]; base: string }) {
  const { t } = useT();
  const { toast } = useToast();
  const priorities = usePriorities();

  // Read once, at mount. The panel is remounted whenever the selection changes
  // (see the key in TaskListCard), so this is the only moment these values are
  // the ones the user is looking at - reading them again on every render would
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
  const [error, setError] = useState('');

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
    setError('');
    const r = await setTaskOptions(ids, opts, base);
    setBusy(false);
    if (!r.ok) {
      // These routes refuse with a sentence, and the sentence is the whole point
      // of refusing: a rename that could not happen has a reason, and hiding it
      // behind "save failed" leaves the row promising a name the folder does not
      // have.
      setError((await r.text()).trim() || t('list.optionsFailed'));
      return;
    }
    setTouched(new Set());
    toast(t('settings.saved'), 'ok');
  }

  return (
    // The right-click belongs to the browser in here. The page above this puts a
    // context menu on the whole list area and calls preventDefault on every
    // reading of it, which inside a text box means no paste entry.
    <section aria-label={t('props.title')} onContextMenu={(e) => e.stopPropagation()}>
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
            name is forty downloads pointed at one destination, and the server
            refuses it for the same reason. */}
        {ids.length === 1 && (
          <Field label={t('props.name')} hint={t('props.nameHint')}>
            <TextInput
              value={name}
              spellCheck={false}
              onChange={(e) => edit('name', setName)(e.target.value)}
            />
          </Field>
        )}

        <Field
          label={t('task.folder')}
          hint={hint(t('settings.downloadDirHint'), start.dir === null)}
        >
          <TextInput
            dir="ltr"
            value={dir}
            spellCheck={false}
            placeholder={placeholder(start.dir === null)}
            onChange={(e) => edit('dir', setDir)(e.target.value)}
          />
        </Field>

        <Field label={t('props.comment')} hint={hint(t('props.commentHint'), start.comment === null)}>
          <TextArea
            rows={2}
            value={comment}
            placeholder={placeholder(start.comment === null)}
            onChange={(e) => edit('comment', setComment)(e.target.value)}
          />
        </Field>

        {/* A set of controls, so FieldGroup and not Field: a <label> hands its
            click to the first thing inside it, which here would pick a priority
            every time somebody read the caption. */}
        <FieldGroup
          label={t('props.priority')}
          hint={hint(t('props.priorityHint'), start.priority === null)}
        >
          <Tabs
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

        <div className="flex items-center gap-3">
          <Button disabled={touched.size === 0 || busy} onClick={() => void apply()}>
            {t('settings.save')}
          </Button>
          {error && <span className="text-sm text-statusFail">{error}</span>}
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
}) {
  const { t } = useT();
  // One subscription for the whole table rather than one per row: the palette
  // changes for every row at once anyway.
  useRainbow();

  const [stored, setStored] = useUIState<ColumnLayout | null>(`list.columns.${profile}`, null);
  const [storedSort, setSort] = useUIState<SortState | null>(`list.sort.${profile}`, null);
  const { collapsed, toggle } = useCollapsedPackages(profile);
  const [menuAt, setMenuAt] = useState<{ x: number; y: number } | null>(null);

  const tableRef = useRef<HTMLDivElement>(null);
  const drag = useRef<{ id: ColumnId; startX: number; startWidth: number; width: number } | null>(null);

  const layout = useMemo(() => resolveLayout(profile, stored), [profile, stored]);
  const ctx = useMemo<CellContext>(() => ({ t, base }), [t, base]);

  // A sort on a column that is currently hidden is ignored rather than cleared,
  // so showing the column again brings the order back with it. Applying it while
  // its column is invisible would be a list in an order with nothing on screen
  // to explain it.
  const sort = storedSort && !layout.hidden.has(storedSort.id) ? storedSort : null;
  const view = useMemo(() => applySort(groups, sort), [groups, sort]);

  // The rows the properties panel shows values from. The ids it WRITES come
  // straight off the selection, which is a larger set whenever a quick filter is
  // hiding one of them - see TaskProperties for why the two are allowed to
  // differ.
  const chosenIds = selection?.ids;
  const chosen = useMemo(
    () => (chosenIds ? view.flatMap(([, items]) => items).filter((x) => chosenIds.has(x.id)) : []),
    [view, chosenIds],
  );

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
      // several hundred rows per pointermove is what makes a column drag stutter,
      // and every row wants the same widths anyway.
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

  let index = 0;

  return (
    // Two surfaces side by side and never one inside the other: the panel is a
    // card of its own under the list, because a card inside a card is the one
    // elevation rule this language does not bend.
    //
    // flex-1 on both, not h-full: harmless where a caller (Downloads.tsx)
    // renders this in normal document flow - a flex item with no flex
    // container ancestor of its own just falls back to its natural content
    // size, so nothing changes there. Meaningful where a caller wraps this
    // in its own flex column (Collector.tsx's own overflow-y-auto wrapper,
    // jdp 2026-08-24: "Das hauptlinkfenster soll immer ... bis ganz nach
    // unten im fenster gehen. egal wie viele links drinn sind") - flex-1
    // rather than a percentage height on purpose: h-full measured correctly
    // on paper here but did not actually resolve in the browser, a known
    // quirk where a percentage height does not reliably propagate through a
    // block box whose own height came from flex-grow plus overflow-auto
    // (confirmed live rather than assumed). flex-grow has none of that
    // ambiguity, so this is the one place in this component that composes
    // with whatever a caller does about height.
    <div className="flex flex-1 flex-col gap-4">
      <div className="glim-card flex-1 overflow-hidden">
        {/* Sorting is a view of the queue and not the queue. Saying so where the
            order is visibly different is the whole of it — a list that quietly
            shows one order while running another is read as a bug in the queue. */}
        {sort && (
          <div className="flex items-center gap-1 px-4 py-2 text-[11px] text-carbon-textMuted">
            <span>{t('list.sortedView')}</span>
            <InfoBubble tip={t('list.sortedViewTip')} />
            <span className="flex-1" />
            <Button kind="ghost" className="px-2 py-1 text-[11px]" onClick={() => setSort(null)}>
              {t('list.queueOrder')}
            </Button>
          </div>
        )}

        {/* min-w-min, not min-w-max: max-content pins the table at the sum of its
            own columns, which overrides the flexible name track entirely and makes
            the list open scrolled off its right edge in any window narrower than
            that sum. With min-content the name column gives way down to its own
            minimum first, and only then does the table start scrolling — which is
            the point at which scrolling is actually the right answer. */}
        <div className="overflow-x-auto">
          <div ref={tableRef} className="min-w-min" style={{ ['--kl-cols' as string]: template } as CSSProperties}>
            <Header
              layout={layout}
              sort={sort}
              onSort={(id) => setSort(nextSort(storedSort, id))}
              onReorder={(id, target, after) =>
                persist({ order: moveColumn(layout.order.map((c) => c.id), id, target, after) })
              }
              onResize={onResize}
              onResizeReset={resetWidth}
              onMenu={setMenuAt}
            />

            <div className="divide-y divide-carbon-border/60">
              {view.map(([name, items]) => {
                const folded = collapsed.has(name);
                const offset = index;
                if (!folded) index += items.length;
                return (
                  <PackageGroup
                    key={name || '__none'}
                    name={name}
                    items={items}
                    base={base}
                    ctx={ctx}
                    columns={layout.visible}
                    selection={selection}
                    collapsed={folded}
                    onToggleCollapsed={() => toggle(name)}
                    indexOffset={offset}
                  />
                );
              })}
            </div>

            {/* The empty space under the rows, and it earns its keep twice: a
                table that ends flush against the edge of its card reads as cut
                off, and a right-click needs somewhere to land that is not a row.
                That is where the list's own menu lives — select all, fold the lot,
                clean up — the same place a desktop list keeps it. */}
            <div className="h-10" />
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
          disabled controls, which is a page that reads as broken. */}
      {chosenIds && chosen.length > 0 && (
        <TaskProperties key={[...chosenIds].join(',')} ids={[...chosenIds]} tasks={chosen} base={base} />
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
