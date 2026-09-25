import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { recheckTasks, startTasks, type Task } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { useReportListView } from '../lib/listview';
import { useToast } from '../lib/toast';
import { useT } from '../lib/i18n';
import { PageHeader, IconBadge } from '../components/ui';
import { Tabs } from '../components/Tabs';
import {
  TaskListCard,
  groupByPackage,
  useCollapsedPackages,
  type Selection,
} from '../components/TaskList';
import { usePackageMenu } from '../components/PackageActions';
import { AddLinksForm } from '../components/AddLinksForm';
import { FileDrop, type FileDropHandle } from '../components/FileDrop';
import { FilteredLinks } from '../components/FilteredLinks';
import { SkippedLinks } from '../components/SkippedLinks';
import {
  COLLECTOR_FILTERS,
  ListMenu,
  SelectionMore,
  cleanupItems,
  matchesQuickFilters,
  offeredQuickFilters,
  targetPackage,
  targetTaskId,
  useCleanup,
  useQueueVerbs,
  useRemoval,
  useRename,
  type ListContext,
  type MenuTarget,
  type QuickFilterId,
} from '../components/ListToolbar';
import { matchesSearch, SearchField } from '../components/SearchField';
import { SavedViewChips } from '../components/SavedViewChips';
import { useListNarrowing, type Narrowing } from '../lib/listNarrowing';
import { anchorBelow, anchorFromEvent, useContextMenu, ContextMenu } from '../components/ContextMenu';
import { CollectorFacetSidebar, matchesFacets } from '../components/CollectorFacets';
import { CollectorStats } from '../components/CollectorStats';
import { useScriptMenu } from '../components/ScriptActions';
import { usePublishCommandPageContext } from '../lib/commands/pageContext';
import { selectionReach, useDrawnRows } from '../lib/selectionReach';
import { SelectionReach } from '../components/SelectionReach';
import {
  IconCheck,
  IconClock,
  IconClose,
  IconPlay,
  IconRetry,
  IconSearch,
  IconTrash,
  IconWarning,
} from '../lib/icons';

/** COLLECTOR_FILTERS without the two drawn as square badges in the action row. */
const COLLECTOR_BADGE_FILTERS: QuickFilterId[] = COLLECTOR_FILTERS.filter(
  (id) => id !== 'uncheckable' && id !== 'unchecked',
);

export function Collector() {
  const { t } = useT();
  const tasks = useTasks('');
  const { toast } = useToast();
  // Kept here, since the container drop zone gives dropped links the same
  // package name as pasted ones.
  const [pkg, setPkg] = useState('');
  // Lets AddLinksForm's buttons and paste box hand files to FileDrop.
  const fileDrop = useRef<FileDropHandle>(null);
  // When a link from a container file last landed, in epoch milliseconds; it
  // ends FileDrop's "handed to JDownloader" bar. Derived from the task list this
  // page already holds rather than a second subscription.
  const lastContainerAt = useMemo(() => {
    let newest = 0;
    for (const x of Object.values(tasks)) {
      if (x.origin !== 'container') continue;
      const at = Date.parse(x.createdAt);
      if (Number.isFinite(at) && at > newest) newest = at;
    }
    return newest;
  }, [tasks]);
  // The search text, quick filters and facet ticks are stored in the
  // interface-state document, so they survive leaving the page
  // (lib/listNarrowing.ts). The full COLLECTOR_FILTERS, since the two badge
  // filters toggle the same set.
  const narrowing = useListNarrowing('collector', COLLECTOR_FILTERS);
  const { search, filters } = narrowing;
  // The search opens as a popover under its badge.
  const [searchOpen, setSearchOpen] = useState(false);
  const searchRef = useRef<HTMLDivElement>(null);
  // The list's own scroll box, reset when a saved view cuts the list short.
  const listScroll = useRef<HTMLDivElement>(null);
  // Host, file type and package (components/CollectorFacets.tsx).
  const facets = narrowing.facets;
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const menu = useContextMenu();
  // The clean-up badge's own dropdown, apart from the context menu.
  const cleanupMenu = useContextMenu();
  // The priorities and the stop mark, for the right-click menu and the rows.
  const queueVerbs = useQueueVerbs('/api');
  const [target, setTarget] = useState<MenuTarget>({ kind: 'selection' });
  const folds = useCollapsedPackages('collector');

  // Everything this instance holds, since a removal weighs bytes of rows this
  // page never shows.
  const all = useMemo(() => Object.values(tasks), [tasks]);
  // Sorted by position, which drag-to-reorder writes; applySort does nothing in
  // the default queue order. A row its hoster's preset set aside waits out of
  // view and comes back the moment the preset lists its kind again.
  const collected = useMemo(
    () =>
      all
        .filter((x) => x.status === 'collected' && !x.skipped && !x.variantOff)
        .sort((a, b) => a.position - b.position),
    [all],
  );
  // Links the filter held stay recorded and restorable, apart from the list.
  const held = useMemo(
    () => all.filter((x) => x.skipped).sort((a, b) => (a.createdAt < b.createdAt ? -1 : 1)),
    [all],
  );
  const filtered = useMemo(
    () => collected.filter((x) => matchesQuickFilters(x, filters) && matchesSearch(x, search) && matchesFacets(x, facets)),
    [collected, filters, search, facets],
  );
  const groups = useMemo(() => groupByPackage(filtered), [filtered]);

  // "Uncheckable" and "unchecked" are square badges toggling the same filter set.
  const uncheckableCount = useMemo(() => collected.filter((x) => x.online === 'uncheckable').length, [collected]);
  const uncheckedCount = useMemo(() => collected.filter((x) => !x.online).length, [collected]);
  // The other chips share the badge row, counted by ListToolbar's own logic.
  const offeredFilters = useMemo(
    () => offeredQuickFilters(COLLECTOR_BADGE_FILTERS, collected, filters),
    [collected, filters],
  );
  // Compared by count, since the sidebar facets narrow the list too.
  const narrowed = filtered.length !== collected.length;
  // Whether anything is set, even a filter that hides nothing; the dot and the
  // reset badge read this.
  const anyNarrowing = narrowing.active;

  /**
   * applyView applies a saved view and puts the list's own scroll box back to
   * the top, which SavedViewChips cannot reach.
   */
  const applyView = useCallback(
    (next: Narrowing) => {
      narrowing.apply(next);
      listScroll.current?.scrollTo({ top: 0 });
    },
    [narrowing.apply],
  );

  // Closes the search popover on an outside click or Escape.
  useEffect(() => {
    if (!searchOpen) return;
    const onClick = (e: MouseEvent) => {
      if (searchRef.current && !searchRef.current.contains(e.target as Node)) setSearchOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setSearchOpen(false);
    document.addEventListener('mousedown', onClick);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onClick);
      document.removeEventListener('keydown', onKey);
    };
  }, [searchOpen]);

  // Drop selections that have left the collector.
  useEffect(() => {
    setSelected((prev) => {
      const live = new Set(collected.map((x) => x.id));
      const next = new Set([...prev].filter((id) => live.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [collected]);

  const clearSelection = useCallback(() => setSelected(new Set()), []);

  // The rows actually drawn (lib/selectionReach.ts), built from `groups`, since
  // skipped and held rows are never offered here.
  const drawn = useDrawnRows(groups, folds.collapsed);
  const reach = useMemo(() => selectionReach(selected, drawn), [selected, drawn]);
  const reduceToShown = useCallback(() => {
    const before = new Set(selected);
    setSelected(new Set(reach.shown));
    toast(t('select.reduced', { n: reach.hidden.length }), 'info', 'action-done', {
      label: t('remove.undo'),
      run: () => setSelected(before),
    });
  }, [selected, reach, toast, t]);

  const removal = useRemoval({ all, selected, base: '/api', drawn, onDone: clearSelection });
  // Every staged link of a package, rows the filters and facets hide included.
  const members = useCallback((pkg: string) => collected.filter((x) => (x.package || '') === pkg), [collected]);
  const rename = useRename({ all, base: '/api', members, command: 'collector.rename' });
  // The clean-up instance behind the badge row, loaded at once so "clear
  // finished" knows whether it applies. Downloads.tsx draws the same row; the
  // two must hold the same controls.
  const cleanup = useCleanup(all);
  useEffect(() => {
    void cleanup.load().catch(() => {
      /* The badge's menu reports this when opened; a command does not report twice. */
    });
  }, [cleanup.load]);

  // The shell's strip is told which rows survived the search (lib/listview.ts).
  useReportListView(filtered, selected);
  // The commands in lib/commands/collector.ts call the same functions as the
  // toolbar (lib/commands/pageContext.ts).
  usePublishCommandPageContext(
    useMemo(
      () => ({
        setSelection: setSelected,
        removal,
        cleanup,
        openFilePicker: () => fileDrop.current?.openPicker(),
        rename: rename.fromKeyboard,
      }),
      [removal, cleanup, rename.fromKeyboard],
    ),
  );
  // Staged rows only, the scope of every figure on this page.
  const selectedTasks = useMemo(() => collected.filter((x) => selected.has(x.id)), [collected, selected]);
  // Scripts can act on staged links before they start (ScriptActions.tsx).
  const scriptGroups = useScriptMenu({ chosen: selectedTasks, base: '/api' });

  const selection: Selection = {
    ids: selected,
    toggle: (id) =>
      setSelected((s) => {
        const n = new Set(s);
        if (n.has(id)) n.delete(id);
        else n.add(id);
        return n;
      }),
    set: setSelected,
  };

  // AddLinksForm's onStaged: the form owns the request, this page the report.
  // submittedCount is the number of URL-shaped lines the box held.
  function handleStaged(created: Task[], submittedCount: number) {
    if (!created.length) {
      toast(t('collector.toastNone'), 'fail');
      return;
    }
    // A held link comes back in `created` but was not staged.
    const heldNow = created.filter((x) => x.skipped).length;
    const staged = created.length - heldNow;
    const skipped = Math.max(0, submittedCount - created.length);
    if (heldNow) {
      toast(
        staged
          ? t('collector.filtered.toastHeldBack', { n: staged, held: heldNow })
          : t('collector.filtered.toastAllHeldBack', { held: heldNow }),
        staged ? 'ok' : 'info',
      );
      return;
    }
    toast(
      skipped
        ? t('collector.toastSkipped', { n: staged, skipped })
        : t('collector.toastStaged', { n: staged }),
      'ok',
    );
  }

  /**
   * runStart reports what the start did, from the route's answer: a schedule
   * holding the queue, a filter holding the links, disabled links or nothing
   * matching each get their own sentence.
   */
  const runStart = async (ids: string[]) => {
    try {
      const r = await startTasks(ids);
      if (r.blocked) return toast(t('collector.toastStartBlocked'), 'fail');
      if (r.started === 0 && r.skipped > 0) return toast(t('collector.toastStartHeld', { n: r.skipped }), 'fail');
      // A disabled link is not started, and the toast says so.
      if (r.started === 0 && (r.disabled ?? 0) > 0)
        return toast(t('collector.toastStartDisabled', { n: r.disabled ?? 0 }), 'fail');
      if (r.started === 0) return;
      if ((r.disabled ?? 0) > 0)
        return toast(t('collector.toastStartedSomeDisabled', { n: r.started, disabled: r.disabled ?? 0 }), 'info');
      toast(t('collector.toastStarted', { n: r.started }), 'info');
    } catch {
      toast(t('list.optionsFailed'), 'fail');
    }
  };
  const startSelected = () => {
    if (!selected.size) return;
    void runStart([...selected]);
  };
  const startAll = () => void runStart([]);

  async function openCleanup(el: HTMLButtonElement | null): Promise<void> {
    try {
      await cleanup.load();
      cleanupMenu.openAt(anchorBelow(el));
    } catch {
      toast(t('list.optionsFailed'), 'fail');
    }
  }

  // A link, a package header or empty space, as in the download list.
  function onContextMenu(e: React.MouseEvent): void {
    // The column header may have claimed this right-click. Read off the native
    // event, since React fixes the synthetic defaultPrevented when it builds it.
    if (e.nativeEvent.defaultPrevented) return;
    const id = targetTaskId(e);
    const pkg = id === null ? targetPackage(e) : null;
    if (id) {
      if (!selected.has(id)) setSelected(new Set([id]));
      setTarget({ kind: 'selection' });
    } else if (pkg !== null) {
      const ids = filtered.filter((x) => (x.package || '') === pkg).map((x) => x.id);
      if (!(ids.length > 0 && ids.every((x) => selected.has(x)))) setSelected(new Set(ids));
      setTarget({ kind: 'package', name: pkg });
    } else {
      setTarget({ kind: 'list' });
    }
    e.preventDefault();
    menu.openAt(anchorFromEvent(e));
  }

  const listContext: ListContext = {
    packages: groups.map(([name]) => name),
    collapsed: folds.collapsed,
    onCollapse: folds.collapse,
    onExpand: folds.expand,
    onSelectAll: () => setSelected(new Set(filtered.map((x) => x.id))),
    onSelectNone: clearSelection,
    // The collector is always this instance's own.
    local: true,
    members,
  };

  const allChosen = filtered.length > 0 && filtered.every((x) => selected.has(x.id));
  const selectedIds = selectedTasks.map((x) => x.id);
  const packageMenu = usePackageMenu({
    tasks: collected,
    selected,
    base: '/api',
    onDone: () => toast(t('task.applied'), 'ok'),
  });

  return (
    // flex-1 rather than h-full, since app/Layout.tsx's wrapper is a flex column.
    <div className="flex min-h-0 flex-1 flex-col gap-6">
      <div className="shrink-0">
        <PageHeader title={t('collector.title')} />
      </div>

      {/* Three columns of equal height through the row's default stretch;
          AddLinksForm takes the free width. */}
      <div className="flex min-w-0 shrink-0 flex-col gap-4 lg:flex-row">
        <div className="min-w-0 flex-1">
          <AddLinksForm
            pkg={pkg}
            onPkgChange={setPkg}
            onStaged={handleStaged}
            onChooseFile={() => fileDrop.current?.openPicker()}
            onFilesDropped={(files) => fileDrop.current?.handleFiles(files)}
            // Mounted once, here: the folder-icon badge above opens the picker
            // through this ref, wherever the progress itself renders.
            footer={<FileDrop ref={fileDrop} pkg={pkg} landedAt={lastContainerAt} />}
          />
        </div>
        {collected.length > 0 && <CollectorStats all={collected} visible={filtered} selected={selectedTasks} />}
        {collected.length > 0 && (
          <CollectorFacetSidebar tasks={collected} selection={facets} onChange={narrowing.setFacets} />
        )}
      </div>

      {/* The list cluster keeps a tighter gap than the page: held and skipped
          links, the action row and the list. SkippedLinks floats as its own
          fixed card wherever it is mounted. */}
      <div className="flex min-h-0 flex-1 flex-col gap-3">
        {/* Direct children, so a null one adds no gap. */}
        <FilteredLinks held={held} />
        <SkippedLinks />

        {/* One row for saved views, filters, search, the selection's actions
            and the page actions, wrapping when narrow. Downloads.tsx draws the
            same row, and the two must hold the same control in every slot. The
            search opens as a popover under its badge, so nothing else moves. */}
        <div
          className="flex flex-wrap shrink-0 items-center justify-end gap-2"
          role="group"
          aria-label={t('list.actions')}
        >
          {/* Left of the spacer, which nothing else here uses. */}
          <SavedViewChips
            profile="collector"
            allowed={COLLECTOR_FILTERS}
            narrowing={narrowing.narrowing}
            onApply={applyView}
          />
          <span className="flex-1" />

          {/* Filters, visible whatever the selection, left of the search badge
              so a changing number of chips does not move it. Like the other
              chips they show only when they can match something or are on. */}
          {(uncheckableCount > 0 || filters.has('uncheckable')) && (
            <IconBadge
              labelled
              hue={0}
              active={filters.has('uncheckable')}
              icon={<IconWarning width={16} height={16} />}
              title={t('filter.uncheckable')}
              aria-label={t('filter.uncheckable')}
              onClick={() => narrowing.toggleFilter('uncheckable')}
            />
          )}
          {(uncheckedCount > 0 || filters.has('unchecked')) && (
            <IconBadge
              labelled
              hue={1}
              active={filters.has('unchecked')}
              icon={<IconClock width={16} height={16} />}
              title={t('filter.unchecked')}
              aria-label={t('filter.unchecked')}
              onClick={() => narrowing.toggleFilter('unchecked')}
            />
          )}

          {offeredFilters.length > 0 && (
            <Tabs
              select="many"
              size="sm"
              label={t('filter.label')}
              active={filters}
              onSelect={(id) => narrowing.toggleFilter(id as QuickFilterId)}
              items={offeredFilters.map(({ f, n }) => ({ id: f.id, label: t(f.label), badge: n }))}
              // The same badge Downloads.tsx has in this slot.
              after={
                filters.size > 0 && (
                  <IconBadge
                    labelled
                    hue={0}
                    icon={<IconClose width={16} height={16} />}
                    title={t('filter.clear')}
                    aria-label={t('filter.clear')}
                    onClick={narrowing.clearFilters}
                  />
                )
              }
            />
          )}
          {narrowed && (
            <span className="glim-num text-xs text-carbon-textMuted">
              {t('search.shown', { n: filtered.length, total: collected.length })}
            </span>
          )}

          <div ref={searchRef} className="relative">
            {/* A glyph whatever the label setting, as on Downloads.tsx. */}
            <IconBadge
              hue={0}
              // Lit while its popover is open, as on Downloads.tsx.
              active={searchOpen}
              icon={<IconSearch width={16} height={16} />}
              // The key the download list's badge uses.
              title={t('search.toggle')}
              aria-label={t('search.toggle')}
              aria-expanded={searchOpen}
              onClick={() => setSearchOpen((v) => !v)}
            />
            {/* Shows that something still narrows the list while the panel is
                closed, such as after a restart. */}
            {anyNarrowing && !searchOpen && (
              <span
                aria-hidden
                className="pointer-events-none absolute -end-1 -top-1 h-2 w-2 rounded-[var(--radius-pill)] bg-accent"
              />
            )}
            {searchOpen && (
              <div
                // end-0 keeps it on the badge's trailing edge at any width.
                className="absolute end-0 top-full z-20 mt-2 w-96 rounded-[var(--radius-control)]
                  bg-carbon-surface p-2 shadow-[var(--elevation)]"
              >
                <SearchField value={search} onChange={narrowing.setSearch} className="w-full" />
              </div>
            )}
          </div>

          {/* Clears search, quick filters and facet ticks at once; it also works
              while the collector is empty and the sidebar is not drawn. */}
          {anyNarrowing && (
            <>
              <IconBadge
                labelled
                hue={5}
                icon={<IconClose width={16} height={16} />}
                title={t('views.clearAll')}
                aria-label={t('views.clearAll')}
                hint={t('views.clearAllHint')}
                onClick={narrowing.clearAll}
              />
            </>
          )}

          {/* The verbs in one piece after the count they act on, as on
              Downloads.tsx. */}
          <div className="flex flex-wrap items-center justify-end gap-2">
            {selected.size > 0 ? (
              <>
                {/* The count and its ×, as on Downloads.tsx. */}
                <span className="flex items-center gap-1.5">
                  <SelectionReach
                    mode="select"
                    total={selected.size}
                    hidden={reach.hidden.length}
                    onReduce={reduceToShown}
                  />
                  <IconBadge
                    hue={1}
                    icon={<IconClose width={16} height={16} />}
                    title={t('select.none')}
                    aria-label={t('select.none')}
                    onClick={clearSelection}
                  />
                </span>
                {/* Secondary: the page's one accent button is "Add to collector". */}
                <IconBadge
                  labelled
                  hue={2}
                  icon={<IconPlay width={16} height={16} />}
                  title={t('collector.startSelected')}
                  aria-label={t('collector.startSelected')}
                  onClick={startSelected}
                />
                <IconBadge
                  labelled
                  hue={4}
                  icon={<IconTrash width={16} height={16} />}
                  title={t('task.remove')}
                  aria-label={t('task.remove')}
                  onClick={() => void removal.removeNow(selectedIds)}
                />
                <SelectionMore hue={5} chosen={selectedTasks} removal={removal} groups={[packageMenu.group]} />
              </>
            ) : (
              <>
                <IconBadge
                  labelled
                  hue={1}
                  icon={<IconCheck width={16} height={16} />}
                  title={allChosen ? t('select.none') : t('select.all')}
                  aria-label={allChosen ? t('select.none') : t('select.all')}
                  disabled={filtered.length === 0}
                  onClick={() => setSelected(allChosen ? new Set() : new Set(filtered.map((x) => x.id)))}
                />
                <IconBadge
                  labelled
                  hue={2}
                  icon={<IconTrash width={16} height={16} />}
                  title={t('cleanup.menu')}
                  aria-label={t('cleanup.menu')}
                  onClick={(e) => void openCleanup(e.currentTarget)}
                />
                <IconBadge
                  labelled
                  hue={3}
                  icon={<IconRetry width={16} height={16} />}
                  title={t('collector.checkAll')}
                  aria-label={t('collector.checkAll')}
                  disabled={collected.length === 0}
                  onClick={() => {
                    // An empty id list means every staged link on this route,
                    // unlike the bulk routes, which refuse it.
                    recheckTasks([]);
                    toast(t('task.recheck'), 'info');
                  }}
                />
                <IconBadge
                  labelled
                  hue={4}
                  icon={<IconPlay width={16} height={16} />}
                  title={t('collector.startAll')}
                  aria-label={t('collector.startAll')}
                  disabled={collected.length === 0}
                  onClick={startAll}
                />
              </>
            )}
          </div>
        </div>

        {/* The one scrolling region: everything above keeps its height and the
            list takes the rest down to the window's edge, never less than a
            few rows. A window too short for that scrolls the frame instead,
            as on Downloads.tsx. On a phone the page scrolls as a whole and the
            list runs at full length (app/Layout.tsx). */}
        <div className="flex min-h-48 flex-1 flex-col" onContextMenu={onContextMenu}>
        {collected.length === 0 ? (
          <div className="glim-card flex flex-1 items-center justify-center p-12 text-center text-sm text-carbon-textMuted">
            {t('collector.empty')}
          </div>
        ) : filtered.length === 0 ? (
          <div className="glim-card flex flex-1 items-center justify-center p-12 text-center text-sm text-carbon-textMuted">
            {t('downloads.noMatch')}
          </div>
        ) : (
          // A flex column, since h-full on TaskListCard does not resolve through
          // a flex-grown overflow box. pt-3 keeps the card's title badge inside
          // this box's clip edge.
          <div ref={listScroll} className="flex min-h-0 flex-1 flex-col pt-3 md:overflow-y-auto">
            <TaskListCard
              groups={groups}
              base="/api"
              selection={selection}
              profile="collector"
              // As in Downloads.tsx: one removal question for the whole page.
              onRemovePackage={removal.askWithFiles}
              stopMark={queueVerbs.stopMark}
              title={t('collector.listTitle')}
              hue={3}
            />
          </div>
        )}
        </div>
      </div>

      {cleanupMenu.anchor && cleanup.classes && (
        <ContextMenu
          anchor={cleanupMenu.anchor}
          label={t('cleanup.menuLabel')}
          onClose={cleanupMenu.close}
          groups={[{ id: 'cleanup', items: cleanupItems(cleanup.classes, t, (cls) => void cleanup.preview(cls)) }]}
        />
      )}

      <ListMenu
        anchor={menu.anchor}
        onClose={menu.close}
        all={all}
        selected={selected}
        base="/api"
        removal={removal}
        queue={queueVerbs}
        target={target}
        list={listContext}
        rename={rename}
        extraGroups={scriptGroups}
      />
      {removal.dialog}
      {rename.dialog}
      {packageMenu.dialog}
      {cleanup.dialog}
    </div>
  );
}
