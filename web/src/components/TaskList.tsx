import { useMemo, useRef, useState, type CSSProperties, type DragEvent, type PointerEvent } from 'react';
import { createPortal } from 'react-dom';
import { type Task } from '../lib/api';
import { hueVars, rainbowAt } from '../lib/appearance';
import { useRainbow } from '../lib/useRainbow';
import {
  pause,
  resume,
  remove,
  startTasks,
  restartTasks,
  recheckTasks,
  setTaskOptions,
} from '../lib/api';
import { useT } from '../lib/i18n';
import { useUIState } from '../lib/uistate';
import { Button, Field, InfoBubble, Modal, TextInput } from './ui';
import { ResolverBadge } from './StatusPill';
import { ColumnMenu } from './ColumnMenu';
import {
  COLUMN_BY_ID,
  Checkbox,
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

function Chevron({ open }: { open: boolean }) {
  return (
    <svg viewBox="0 0 16 16" width={13} height={13} aria-hidden focusable="false">
      <path
        d="M6 3l5 5-5 5"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
        style={{ transformOrigin: '8px 8px', transform: open ? 'rotate(90deg)' : 'none' }}
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
            className={`min-w-0 truncate px-2 text-[12.5px] text-carbon-textSub ${
              col.align === 'end' ? 'text-end' : 'text-start'
            } ${col.numeric ? 'glim-num' : ''}`}
          >
            {node}
          </div>
        );
      })}

      {/* The primary action stays visible; the rest appears on hover or focus,
          so a long list reads as content instead of a wall of buttons. */}
      <div className="flex items-center justify-end gap-0.5">
        {collected && (
          <Button kind="ghost" icon={<IconPlay />} title={t('task.start')} onClick={() => startTasks([task.id], base)} />
        )}
        {task.status === 'running' && (
          <Button kind="ghost" icon={<IconPause />} title={t('task.pause')} onClick={() => pause(task.id, base)} />
        )}
        {task.status === 'paused' && (
          <Button kind="ghost" icon={<IconPlay />} title={t('task.resume')} onClick={() => resume(task.id, base)} />
        )}
        <div className="flex items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
          {collected && (
            <Button
              kind="ghost"
              icon={<IconSearch />}
              title={t('task.recheck')}
              onClick={() => recheckTasks([task.id], base)}
            />
          )}
          <Button kind="ghost" icon={<IconFolder />} title={t('task.folder')} onClick={() => setOptions(true)} />
          {settled && (
            <Button
              kind="ghost"
              icon={<IconRetry />}
              title={t('task.restart')}
              onClick={() => restartTasks([task.id], base)}
            />
          )}
          <Button kind="danger" icon={<IconTrash />} title={t('task.remove')} onClick={() => remove(task.id, base)} />
        </div>
      </div>

      {/* Into <body>, because the table is a horizontal scroll container now and
          a dialog that belongs to a row must not be laid out inside one. */}
      {options &&
        createPortal(
          <TaskOptionsDialog task={task} base={base} onClose={() => setOptions(false)} />,
          document.body,
        )}
    </div>
  );
}

// TaskOptionsDialog edits the per-task overrides: where this file goes and the
// password its archive needs. Both are left alone unless actually changed.
function TaskOptionsDialog({ task, base, onClose }: { task: Task; base: string; onClose: () => void }) {
  const { t } = useT();
  const [dir, setDir] = useState(task.dir ?? '');
  const [password, setPassword] = useState(task.password ?? '');
  const [error, setError] = useState('');

  async function apply() {
    const r = await setTaskOptions([task.id], { dir, password }, base);
    if (!r.ok) {
      setError(await r.text());
      return;
    }
    onClose();
  }

  return (
    <Modal
      title={task.name || task.url}
      onClose={onClose}
      footer={
        <>
          <Button onClick={apply}>{t('settings.save')}</Button>
          {error && <span className="text-statusFail text-sm">{error}</span>}
        </>
      }
    >
      <Field label={t('task.folder')} hint={t('settings.downloadDirHint')}>
        <TextInput dir="ltr" value={dir} spellCheck={false} onChange={(e) => setDir(e.target.value)} />
      </Field>
      <Field label={t('task.password')}>
        <TextInput value={password} onChange={(e) => setPassword(e.target.value)} />
      </Field>
    </Modal>
  );
}

/**
 * The package header's name cell — the tree control, and the one cell no column
 * renderer can produce because the folded set is list state.
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
  const resolvers = new Set(items.map((x) => x.resolver));
  const uniform = resolvers.size === 1 ? items[0].resolver : null;

  return (
    <div className="flex min-w-0 items-center gap-2">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={!collapsed}
        title={t(collapsed ? 'task.expand' : 'task.collapse')}
        className="grid h-5 w-5 shrink-0 place-items-center rounded-[var(--radius-control)] text-carbon-textMuted
          transition-colors hover:bg-carbon-hover hover:text-carbon-text"
      >
        <Chevron open={!collapsed} />
      </button>
      <span className="truncate text-[13px] font-semibold text-carbon-text">{name || t('task.ungrouped')}</span>
      <span className="glim-num shrink-0 text-[11px] text-carbon-textMuted">
        {items.length} {items.length === 1 ? t('task.file') : t('task.files')}
        {done > 0 && ` · ${done} ${t('overview.done').toLowerCase()}`}
      </span>
      {online > 0 && (
        <span className="glim-num shrink-0 text-[11px] text-carbon-textMuted">
          {t('task.onlineRatio', { online, total: items.length })}
        </span>
      )}
      {uniform && <ResolverBadge resolver={uniform} />}
    </div>
  );
}

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
      <div style={ROW_GRID} className="grid items-center px-3 py-2">
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
            className={`min-w-0 truncate px-2 text-[12px] text-carbon-textMuted ${
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
  // Folded packages are keyed by name, which is the only identity the wire model
  // carries: core.Task has a package name and no package id. SetPackage rewrites
  // the name, so a rename or a Packagizer re-package makes a folded package come
  // back expanded. That is the mild failure of the two — the harmful one would be
  // pruning names that are not on screen, which would unfold every package the
  // search is currently hiding — but it needs a stable package id on the task to
  // fix properly, and that is the model owner's lane, not this file's.
  const [collapsed, setCollapsed] = useUIState<string[]>(`list.collapsed.${profile}`, NO_COLLAPSED);
  const [menuAt, setMenuAt] = useState<{ x: number; y: number } | null>(null);

  const tableRef = useRef<HTMLDivElement>(null);
  const drag = useRef<{ id: ColumnId; startX: number; startWidth: number; width: number } | null>(null);

  const layout = useMemo(() => resolveLayout(profile, stored), [profile, stored]);
  const ctx = useMemo<CellContext>(() => ({ t, base }), [t, base]);
  const collapsedSet = useMemo(() => new Set(collapsed), [collapsed]);

  // A sort on a column that is currently hidden is ignored rather than cleared,
  // so showing the column again brings the order back with it. Applying it while
  // its column is invisible would be a list in an order with nothing on screen
  // to explain it.
  const sort = storedSort && !layout.hidden.has(storedSort.id) ? storedSort : null;
  const view = useMemo(() => applySort(groups, sort), [groups, sort]);

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

  function toggleCollapsed(name: string): void {
    const next = new Set(collapsedSet);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    setCollapsed([...next]);
  }

  let index = 0;

  return (
    <div className="glim-card overflow-hidden">
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
              const folded = collapsedSet.has(name);
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
                  onToggleCollapsed={() => toggleCollapsed(name)}
                  indexOffset={offset}
                />
              );
            })}
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
