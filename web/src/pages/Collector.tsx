import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { recheckTasks, type Task } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { useStartTasks } from '../lib/useStartTasks';
import { useReportListView } from '../lib/listview';
import { useToast } from '../lib/toast';
import { useT } from '../lib/i18n';
import { EmptyState, PageHeader, IconBadge } from '../components/ui';
import {
  TaskListCard,
  groupByPackage,
  useCollapsedPackages,
  useListSort,
  type Selection,
} from '../components/TaskList';
import { ListBar } from '../components/ListBar';
import { usePackageMenu } from '../components/PackageActions';
import { AddLinksForm, useStagedReport } from '../components/AddLinksForm';
import { FileDrop, newestContainerLink, type FileDropHandle } from '../components/FileDrop';
import { PageAction, PageActions } from '../components/PageActions';
import { FilteredLinks } from '../components/FilteredLinks';
import { SkippedLinks } from '../components/SkippedLinks';
import {
  COLLECTOR_FILTERS,
  ListMenu,
  ListMore,
  SelectionMore,
  matchesQuickFilters,
  narrowedTitle,
  offeredQuickFilters,
  targetPackage,
  targetTaskId,
  useCleanup,
  useQueueVerbs,
  useRemoval,
  useRename,
  type ListContext,
  type MenuTarget,
} from '../components/ListToolbar';
import { matchesSearch } from '../components/SearchField';
import { SavedViewChips } from '../components/SavedViewChips';
import { useListNarrowing, type Narrowing } from '../lib/listNarrowing';
import { useSavedViews } from '../lib/savedViews';
import { useLabelMode } from '../lib/labelModes';
import { useRowFit } from '../lib/rowFit';
import { anchorFromEvent, useContextMenu, type MenuGroup } from '../components/ContextMenu';
import { CollectorFacets, facetActiveCount, matchesFacets } from '../components/CollectorFacets';
import { CollectorStats } from '../components/CollectorStats';
import { useFileMenu } from '../components/FileActions';
import { useScriptMenu } from '../components/ScriptActions';
import { usePublishCommandPageContext } from '../lib/commands/pageContext';
import { selectionReach, useDrawnRows } from '../lib/selectionReach';
import { SelectionReach } from '../components/SelectionReach';
import {
  IconCheck,
  IconClearSelection,
  IconClose,
  IconCollector,
  IconPlay,
  IconSearch,
  IconTrash,
  IconUndo,
} from '../lib/icons';

export function Collector() {
  const { t } = useT();
  const live = useTasks('');
  // While Start all moves every link out, the list stays as it was until the
  // move is done and the page changes, instead of losing its rows one by one.
  const [frozen, setFrozen] = useState<Record<string, Task> | null>(null);
  const tasks = frozen ?? live;
  const { toast } = useToast();
  // Kept here, since the container drop zone gives dropped links the same
  // package name as pasted ones.
  const [pkg, setPkg] = useState('');
  // Lets AddLinksForm's buttons and paste box hand files to FileDrop.
  const fileDrop = useRef<FileDropHandle>(null);
  const lastContainerAt = useMemo(() => newestContainerLink(tasks), [tasks]);
  // The search text, quick filters and facet ticks are stored in the
  // interface-state document, so they survive leaving the page
  // (lib/listNarrowing.ts).
  const narrowing = useListNarrowing('collector', COLLECTOR_FILTERS);
  const { search, filters } = narrowing;
  const rowRef = useRef<HTMLDivElement>(null);
  // The list's own scroll box, reset when a saved view cuts the list short.
  const listScroll = useRef<HTMLDivElement>(null);
  // Host, file type and package (components/CollectorFacets.tsx).
  const facets = narrowing.facets;
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const menu = useContextMenu();
  // The clean-up badge's own dropdown, apart from the context menu.
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
  // Links the filter rejected stay recorded and restorable, apart from the list.
  const held = useMemo(
    () => all.filter((x) => x.skipped).sort((a, b) => (a.createdAt < b.createdAt ? -1 : 1)),
    [all],
  );
  const filtered = useMemo(
    () => collected.filter((x) => matchesQuickFilters(x, filters) && matchesSearch(x, search) && matchesFacets(x, facets)),
    [collected, filters, search, facets],
  );
  const groups = useMemo(() => groupByPackage(filtered), [filtered]);

  // The chips under Filter, counted by ListToolbar's own logic.
  const offeredFilters = useMemo(() => offeredQuickFilters(COLLECTOR_FILTERS, collected, filters), [collected, filters]);
  const sort = useListSort(t('collector.listTitle'), 'collector');
  // Compared by count, since the facets narrow the list too.
  const narrowed = filtered.length !== collected.length;
  // Whether anything is set, even a filter that hides nothing; the dot and the
  // reset badge read this.
  const anyNarrowing = narrowing.active;

  // What the action row holds, so it measures itself again when that changes.
  const buttonLabels = useLabelMode('buttons');
  const tabLabels = useLabelMode('tabs');
  const { views } = useSavedViews('collector', COLLECTOR_FILTERS);
  const rowContent = [
    buttonLabels,
    tabLabels,
    selected.size,
    narrowed ? `${filtered.length}/${collected.length}` : '',
    anyNarrowing,
    views.map((v) => v.name).join('\n'),
  ].join('|');
  // Downloads.tsx's fold order.
  const fold = useRowFit(rowRef, 4, rowContent);
  const glyphs = fold >= 1;
  const hideCount = fold >= 2;
  const compact = fold >= 3;
  const scrolls = fold >= 4;

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
      icon: <IconUndo />,
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
  // The folder a link will land in and the links themselves; nothing is on disk yet.
  const linkGroups = useFileMenu({ chosen: selectedTasks, base: '/api', local: true, files: false });

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

  // The form owns the request, this page the report.
  const handleStaged = useStagedReport();

  const navigate = useNavigate();
  const runStart = useStartTasks();
  const startSelected = () => {
    if (!selected.size) return;
    void runStart([...selected]);
  };
  // Once everything collected is on its way, there is nothing left here to
  // look at. Starting a selection leaves the rest, so that stays on the page.
  const startAll = () => {
    setFrozen(live);
    void runStart([]).then((started) => {
      if (started) navigate('/downloads');
      else setFrozen(null);
    });
  };

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

  // Checking every link is offered by More and by the right-click on empty
  // space. An empty id list means every staged link on this route, unlike the
  // bulk routes, which refuse it.
  const checkGroups: MenuGroup[] = [
    {
      id: 'check',
      items: [
        {
          id: 'checkAll',
          label: t('collector.checkAll'),
          icon: <IconCheck width={14} height={14} />,
          disabled: collected.length === 0,
          onSelect: () => {
            recheckTasks([]);
            toast(t('task.recheck'), 'info');
          },
        },
      ],
    },
  ];

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
    whole: checkGroups,
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
      <PageHeader title={t('nav.collector')} />

      {/* Two cards of equal height through the row's default stretch, as low
          as the form needs; AddLinksForm takes the free width. The cards sit
          deeper than .glim-column-top reaches in index.css, so the row keeps
          the room their badges overhang the top edge by. */}
      <div className="flex min-w-0 shrink-0 flex-col gap-4 pt-3 lg:flex-row">
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
      </div>

      {/* The list cluster keeps a tighter gap than the page: held and skipped
          links, the action row and the list. SkippedLinks floats as its own
          fixed card wherever it is mounted. */}
      <div className="flex min-h-0 flex-1 flex-col gap-3">
        {/* Direct children, so a null one adds no gap. */}
        <FilteredLinks held={held} />
        <SkippedLinks />

        {/* One row for the saved views, the selection's actions and the page
            actions, with Filter, Sort and Search at its end. Downloads.tsx
            draws the same row, and the two must hold the same control in every
            slot and fold the same way when short of room (useRowFit). */}
        <ListBar
          rowRef={rowRef}
          glyphs={glyphs}
          scrolls={scrolls}
          filters={offeredFilters}
          active={filters}
          onToggleFilter={narrowing.toggleFilter}
          onClearFilters={narrowing.clearFilters}
          // Host, file type and package, which need links to be counted from.
          facets={{
            panel:
              collected.length > 0 ? (
                <CollectorFacets tasks={collected} selection={facets} onChange={narrowing.setFacets} />
              ) : null,
            active: facetActiveCount(facets) > 0,
          }}
          sorts={[sort]}
          search={search}
          onSearch={narrowing.setSearch}
        >
          {/* Left of the spacer, which nothing else here uses. */}
          <div className="shrink-0">
            <SavedViewChips
              profile="collector"
              allowed={COLLECTOR_FILTERS}
              narrowing={narrowing.narrowing}
              onApply={applyView}
              glyphs={glyphs}
              folded={compact}
            />
          </div>
          <span data-spacer className="flex-1" />

          {/* The verbs in one piece after the count they act on, as on
              Downloads.tsx. */}
          <div className="flex shrink-0 items-center gap-2">
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
                    icon={<IconClearSelection width={16} height={16} />}
                    title={t('select.none')}
                    aria-label={t('select.none')}
                    onClick={clearSelection}
                  />
                </span>
                {/* Secondary: the page's one accent is the floating Start all. */}
                <IconBadge
                  labelled={!glyphs}
                  hue={2}
                  icon={<IconPlay width={16} height={16} />}
                  title={t('collector.startSelected')}
                  aria-label={t('collector.startSelected')}
                  onClick={startSelected}
                />
                <IconBadge
                  labelled={!glyphs}
                  hue={4}
                  icon={<IconTrash width={16} height={16} />}
                  title={t('task.remove')}
                  aria-label={t('task.remove')}
                  onClick={() => void removal.removeNow(selectedIds)}
                />
                <SelectionMore
                  hue={5}
                  labelled={!glyphs}
                  chosen={selectedTasks}
                  removal={removal}
                  groups={[packageMenu.group]}
                />
              </>
            ) : (
              <>
                {/* Start all floats at the foot; the rest acts on the whole
                    list and is pressed rarely. */}
                <ListMore
                  hue={5}
                  labelled={!glyphs}
                  allChosen={allChosen}
                  shown={filtered.length}
                  onSelect={(all) => setSelected(all ? new Set(filtered.map((x) => x.id)) : new Set())}
                  own={checkGroups}
                  cleanup={cleanup}
                />
              </>
            )}
          </div>

          {/* The overview strip counts the visible rows as well. */}
          {narrowed && !hideCount && (
            <span className="glim-num shrink-0 whitespace-nowrap text-meta text-carbon-textMuted">
              {t('search.shown', { n: filtered.length, total: collected.length })}
            </span>
          )}
          {/* Clears search, quick filters and facet ticks at once; it also works
              while the collector is empty and the Filter window has no facets
              to show. */}
          {anyNarrowing && (
            <IconBadge
              labelled={!glyphs}
              hue={5}
              icon={<IconClose width={16} height={16} />}
              title={t('views.clearAll')}
              aria-label={t('views.clearAll')}
              hint={t('views.clearAllHint')}
              onClick={narrowing.clearAll}
            />
          )}
        </ListBar>

        {/* The one scrolling region: everything above keeps its height and the
            list takes the rest down to the window's edge, never less than a
            few rows. A window too short for that scrolls the frame instead,
            as on Downloads.tsx. On a phone the page scrolls as a whole and the
            list runs at full length (app/Layout.tsx). */}
        <div className="flex min-h-48 flex-1 flex-col" onContextMenu={onContextMenu}>
        {collected.length === 0 ? (
          <EmptyState
            fill
            icon={<IconCollector width={26} height={26} />}
            title={t('empty.collectorTitle')}
            hint={t('empty.collectorHint')}
          />
        ) : filtered.length === 0 ? (
          <EmptyState fill icon={<IconSearch width={26} height={26} />} title={t('downloads.noMatch')} />
        ) : (
          // A flex column, since h-full on TaskListCard does not resolve through
          // a flex-grown overflow box. pt-3 keeps the card's title badge inside
          // this box's clip edge. The room for Start all is under the card, so
          // a short list ends above the button and a long one scrolls under it.
          <div ref={listScroll} className="glim-actions-below flex min-h-0 flex-1 flex-col pt-3 md:overflow-y-auto">
            <TaskListCard
              groups={groups}
              base="/api"
              selection={selection}
              profile="collector"
              // As in Downloads.tsx: one removal question for the whole page.
              onRemovePackage={removal.ask}
              rename={rename}
              stopMark={queueVerbs.stopMark}
              title={narrowedTitle(filters, t, t('collector.listTitle'))}
              hue={3}
            />
          </div>
        )}
        </div>
      </div>

      {/* Starting is what the collected links wait for, so it is the page's
          floating action; adding stays with the box it sends, in the first
          card. */}
      <PageActions>
        <PageAction
          primary
          icon={<IconPlay />}
          label={t('collector.startAll')}
          disabled={collected.length === 0 || frozen !== null}
          onClick={startAll}
        />
      </PageActions>

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
        extraGroups={[...linkGroups, ...scriptGroups]}
      />
      {removal.dialog}
      {rename.dialog}
      {packageMenu.dialog}
      {cleanup.dialog}
    </div>
  );
}
