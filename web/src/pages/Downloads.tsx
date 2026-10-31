import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { type Instance, type Task, ok, pause, resume, restartTasks, fetchInstances } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { useReportListView } from '../lib/listview';
import { useT } from '../lib/i18n';
import { useInstanceScope } from '../lib/instance';
import { splitByCard, useListCards, type ListCard } from '../lib/listCards';
import { useUIState } from '../lib/uistate';
import { PageHeader, EmptyState, IconBadge } from '../components/ui';
import {
  TaskListCard,
  groupByPackage,
  sortMenuGroups,
  useCollapsedPackages,
  useListSort,
  type ColumnFilters,
  type Selection,
} from '../components/TaskList';
import {
  DOWNLOAD_FILTERS,
  ListMenu,
  SeedingBadges,
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
import { ListSearch, matchesSearch } from '../components/SearchField';
import { DownloadStats } from '../components/DownloadStats';
import { ListNarrowing } from '../components/ListNarrowing';
import { StatusFilter } from '../components/StatusFilter';
import type { SearchHandle } from '../components/SearchToggle';
import { claimReveal, useRevealRequest } from '../lib/reveal';
import { selectionReach, useDrawnRows } from '../lib/selectionReach';
import { SelectionReach } from '../components/SelectionReach';
import { SavedViewChips } from '../components/SavedViewChips';
import { useListNarrowing } from '../lib/listNarrowing';
import { causeItems, useErrorCauses } from '../components/ErrorCauses';
import { extractionsByTask, useArchiveMenu, useExtractJobs } from '../components/Archives';
import { useFileMenu } from '../components/FileActions';
import { useScriptMenu } from '../components/ScriptActions';
import { anchorFromEvent, useContextMenu, type MenuGroup } from '../components/ContextMenu';
import { useToast } from '../lib/toast';
import { taskRefusal } from '../lib/taskRefusal';
import { usePublishCommandPageContext } from '../lib/commands/pageContext';
import { IconClearSelection, IconDownloadArrow, IconPause, IconPlay, IconRetry, IconSearch, IconUndo } from '../lib/icons';

// The two cards below the download list, in the order they are drawn. Each
// sorts and folds on its own and shares the download list's columns.
const SETTLED = [
  { card: 'finished', title: 'downloads.finishedTitle', hue: 1 },
  { card: 'torrents', title: 'downloads.torrentsTitle', hue: 2 },
] as const;

type Narrowed = Record<ListCard, [string, Task[]][]>;

// A stable empty grouping for a folded card, which draws no rows.
const NONE: [string, Task[]][] = [];

const flat = (groups: [string, Task[]][]): Task[] => groups.flatMap(([, items]) => items);

export function Downloads() {
  const { t } = useT();
  const [instances, setInstances] = useState<Instance[]>([]);
  // Shared scope, so the shell bar's transport controls act on the instance
  // this list shows (lib/instance.tsx).
  const { instance, base, select } = useInstanceScope();
  // The search text and quick filters are stored in the interface-state
  // document, so they survive leaving the page (lib/listNarrowing.ts). The page
  // is their one writer and passes the pieces down.
  const narrowing = useListNarrowing('downloads', DOWNLOAD_FILTERS);
  const { search, filters } = narrowing;
  const searchHandle = useRef<SearchHandle>(null);
  // The lists' area, where typing opens the search.
  const listArea = useRef<HTMLDivElement>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  // The row a card should scroll to, with the request's nonce so a second jump
  // to the same row is a new value, and the card that holds it.
  const [revealRow, setRevealRow] = useState<{ key: string; card: ListCard } | undefined>(undefined);
  const { toast } = useToast();
  const menu = useContextMenu();
  // The clean-up menu opens under a badge; `menu` above opens at the pointer.
  // The priorities the server implements and where the stop mark sits, for the
  // right-click menu, the More menu and the rows.
  const queueVerbs = useQueueVerbs(base);
  // A link, a package header and empty space each offer their own menu.
  const [target, setTarget] = useState<MenuTarget>({ kind: 'selection' });
  const [menuCard, setMenuCard] = useState<ListCard>('downloads');
  const cardsOn = useListCards();
  const [finishedFolded, setFinishedFolded] = useUIState<boolean>('list.folded.finished', false);
  const [torrentsFolded, setTorrentsFolded] = useUIState<boolean>('list.folded.torrents', false);
  const cardFold: Record<'finished' | 'torrents', { folded: boolean; onToggle: () => void }> = {
    finished: { folded: finishedFolded, onToggle: () => setFinishedFolded(!finishedFolded) },
    torrents: { folded: torrentsFolded, onToggle: () => setTorrentsFolded(!torrentsFolded) },
  };
  // ?card=torrents, from the overview's torrent card: the Torrents card is
  // opened and scrolled to once it is drawn, which after a page load is a
  // moment later, when the list arrives. The parameter goes at once, so a
  // reload or a fold later does not do it again.
  const [params, setParams] = useSearchParams();
  const torrentsRef = useRef<HTMLDivElement>(null);
  const toTorrents = useRef(false);
  useEffect(() => {
    if (params.get('card') !== 'torrents') return;
    toTorrents.current = true;
    setTorrentsFolded(false);
    setParams(
      (p) => {
        const next = new URLSearchParams(p);
        next.delete('card');
        return next;
      },
      { replace: true },
    );
  }, [params, setParams, setTorrentsFolded]);
  // The same folded set the list card reads; folding is also a menu entry.
  const folds = useCollapsedPackages('downloads');
  const tasks = useTasks(instance);
  // Extraction has its own progress, failure and stop, so it is its own stream.
  // The status column reads it per row and the right-click menu stops it.
  const unpacking = useMemo(() => Object.values(tasks).some((x) => x.status === 'extracting'), [tasks]);
  const jobs = useExtractJobs(instance, unpacking);
  const extractions = useMemo(() => extractionsByTask(jobs), [jobs]);

  useEffect(() => {
    fetchInstances().then(setInstances);
  }, []);

  // Everything this instance holds, collector included, since a removal weighs
  // bytes the list never shows.
  const all = useMemo(() => Object.values(tasks), [tasks]);

  const list = useMemo(
    () =>
      all
        .filter((x) => x.status !== 'collected')
        .sort((a, b) => {
          // Everything still in the queue first, by queue position (what a
          // reorder writes), then everything settled, oldest first. One key per
          // half keeps the order total, so a failed link cannot anchor its
          // package near the top and make a drag look ignored.
          const settledA = a.status === 'done' || a.status === 'error' ? 1 : 0;
          const settledB = b.status === 'done' || b.status === 'error' ? 1 : 0;
          if (settledA !== settledB) return settledA - settledB;
          if (settledA === 0) return a.position - b.position;
          return a.createdAt < b.createdAt ? -1 : 1;
        }),
    [all],
  );

  // The scroll the overview asked for, once the list is there. Without a
  // finished torrent there is no Torrents card, and the page stays at the top.
  useEffect(() => {
    if (!toTorrents.current || list.length === 0) return;
    toTorrents.current = false;
    torrentsRef.current?.scrollIntoView({ block: 'start' });
  });

  // Split before the search and the quick filters narrow it, because a
  // package's card follows all of its links. The narrowing then applies to all
  // three cards alike: a search is how somebody finds a file, wherever it is.
  const byCard = useMemo(() => splitByCard(groupByPackage(list), cardsOn), [list, cardsOn]);
  const shown = useMemo<Narrowed>(() => {
    const narrow = (groups: [string, Task[]][]) =>
      groups
        .map(([name, items]): [string, Task[]] => [
          name,
          items.filter((x) => matchesQuickFilters(x, filters) && matchesSearch(x, search)),
        ])
        .filter(([, items]) => items.length > 0);
    return { downloads: narrow(byCard.downloads), finished: narrow(byCard.finished), torrents: narrow(byCard.torrents) };
  }, [byCard, filters, search]);
  const groups = shown.downloads;
  // Every row on the page, in the order the cards draw them.
  const filtered = useMemo(
    () => [...flat(shown.downloads), ...flat(shown.finished), ...flat(shown.torrents)],
    [shown],
  );
  // Which card each row is in, whatever hides it.
  const cardIds = useMemo(() => {
    const ids = (c: ListCard) => new Set(flat(byCard[c]).map((x) => x.id));
    return { downloads: ids('downloads'), finished: ids('finished'), torrents: ids('torrents') };
  }, [byCard]);

  useEffect(() => {
    setSelected((prev) => {
      const live = new Set(list.map((x) => x.id));
      const next = new Set([...prev].filter((id) => live.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [list]);

  // "Show me that row", from the event list behind the sidebar's bell. It
  // depends on `tasks` because arriving from a peer scope refills the list a
  // beat after the press; lib/reveal.ts holds the deadline.
  const reveal = useRevealRequest();
  useEffect(() => {
    if (!reveal) return;
    const task = tasks[reveal.id];
    if (!task || task.status === 'collected') return;
    // Claimed first, so the deadline stops before this effect changes state.
    claimReveal(reveal.nonce);
    // Filters are cleared only when they would hide this row; the category is
    // a setting and survives.
    if (!matchesQuickFilters(task, filters)) narrowing.clearFilters();
    if (!matchesSearch(task, search)) narrowing.setSearch({ text: '', category: search.category });
    // '' is the ungrouped bucket's real name. A folded package draws no rows,
    // and neither does a folded card.
    folds.expand([task.package || '']);
    const card: ListCard = cardIds.finished.has(reveal.id)
      ? 'finished'
      : cardIds.torrents.has(reveal.id)
        ? 'torrents'
        : 'downloads';
    if (card === 'finished' && finishedFolded) setFinishedFolded(false);
    if (card === 'torrents' && torrentsFolded) setTorrentsFolded(false);
    setSelected(new Set([reveal.id]));
    // Selecting the row marks it; .glim-row-selected paints the wash.
    setRevealRow({ key: `task:${reveal.id}#${reveal.nonce}`, card });
  }, [
    reveal,
    tasks,
    filters,
    search,
    folds,
    narrowing,
    cardIds,
    finishedFolded,
    torrentsFolded,
    setFinishedFolded,
    setTorrentsFolded,
  ]);

  const clearSelection = useCallback(() => setSelected(new Set()), []);

  // The rows actually drawn, narrower than `filtered` by every folded package
  // and folded card (lib/selectionReach.ts).
  const drawnDownloads = useDrawnRows(groups, folds.collapsed);
  const drawnFinished = useDrawnRows(finishedFolded ? NONE : shown.finished, folds.collapsed);
  const drawnTorrents = useDrawnRows(torrentsFolded ? NONE : shown.torrents, folds.collapsed);
  const drawn = useMemo(
    () => new Set([...drawnDownloads, ...drawnFinished, ...drawnTorrents]),
    [drawnDownloads, drawnFinished, drawnTorrents],
  );
  const reach = useMemo(() => selectionReach(selected, drawn), [selected, drawn]);
  // Drops hidden rows from the selection and can offer the whole selection back.
  const reduceToShown = useCallback(() => {
    const before = new Set(selected);
    setSelected(new Set(reach.shown));
    toast(t('select.reduced', { n: reach.hidden.length }), 'info', 'action-done', {
      label: t('remove.undo'),
      icon: <IconUndo />,
      run: () => setSelected(before),
    });
  }, [selected, reach, toast, t]);

  const removal = useRemoval({ all, selected, base, drawn, onDone: clearSelection });
  // Every link of a package in this list, rows the filters hide included.
  const members = useCallback((pkg: string) => list.filter((x) => (x.package || '') === pkg), [list]);
  const rename = useRename({ all, base, members, command: 'downloads.rename' });
  // The command surface's clean-up instance, loaded at once so "clear finished"
  // knows whether it applies.
  const cleanup = useCleanup(all);
  useEffect(() => {
    void cleanup.load().catch(() => {
      /* The badge row reports this on click; a command does not report twice. */
    });
  }, [cleanup.load]);

  // The shell's overview strip cannot know which rows are visible, so it is told
  // (lib/listview.ts).
  useReportListView(filtered, selected);
  // The commands in lib/commands/downloads.ts call the same functions as the
  // toolbar (lib/commands/pageContext.ts).
  usePublishCommandPageContext(
    useMemo(
      () => ({
        setSelection: setSelected,
        removal,
        cleanup,
        openSearch: () => searchHandle.current?.open(),
        rename: rename.fromKeyboard,
      }),
      [removal, cleanup, rename.fromKeyboard],
    ),
  );
  const chosen = useMemo(() => all.filter((x) => selected.has(x.id)), [all, selected]);
  // Starting or stopping an unpacking is not forwarded to a peer.
  const archiveGroups = useArchiveMenu({ chosen: instance === '' ? chosen : [], base, jobs });
  // Reveal and open natively only on this instance's own filesystem.
  const fileGroups = useFileMenu({ chosen, base, local: instance === '' });
  // Saved scripts become manual commands on this menu (ScriptActions.tsx).
  const scriptGroups = useScriptMenu({ chosen, base });

  // Each card selects on its own: a click in one keeps only that card's rows,
  // so the menu and the verbs never act on rows of two cards at once unless
  // "select all" asked for every row on the page.
  const selectionIn = (card: ListCard): Selection => {
    const mine = (ids: Iterable<string>) => new Set([...ids].filter((id) => cardIds[card].has(id)));
    return {
      ids: selected,
      toggle: (id) =>
        setSelected((s) => {
          const n = mine(s);
          if (n.has(id)) n.delete(id);
          else n.add(id);
          return n;
        }),
      set: (ids) => setSelected(mine(ids)),
    };
  };
  const ids = () => [...selected];

  const counts = useMemo(() => {
    let running = 0,
      queued = 0,
      done = 0,
      error = 0,
      speed = 0;
    for (const x of list) {
      if (x.status === 'running' || x.status === 'extracting') running++;
      else if (x.status === 'queued') queued++;
      else if (x.status === 'done') done++;
      else if (x.status === 'error') error++;
      if (x.status === 'running') speed += x.speed;
    }
    return { running, queued, done, error, speed };
  }, [list]);

  // From the stored narrowing, so the badge and its dot agree.
  const narrowed = narrowing.active;
  // The collector's chips over this list's states, from shared logic.
  const offeredFilters = useMemo(() => offeredQuickFilters(DOWNLOAD_FILTERS, list, filters), [list, filters]);
  const causes = useErrorCauses(list);
  // Each card sorts on its own, so the Sort button offers the ones on show,
  // and the download list's while nothing is.
  const sortDownloads = useListSort(t('downloads.listTitle'));
  const sortFinished = useListSort(t('downloads.finishedTitle'), 'downloads', 'downloads.finished');
  const sortTorrents = useListSort(t('downloads.torrentsTitle'), 'downloads', 'downloads.torrents');
  const onShow = [
    ...(groups.length > 0 ? [sortDownloads] : []),
    ...(shown.finished.length > 0 ? [sortFinished] : []),
    ...(shown.torrents.length > 0 ? [sortTorrents] : []),
  ];
  const sorts = onShow.length > 0 ? onShow : [sortDownloads];

  const pauseAll = () => list.filter((x) => x.status === 'running').forEach((x) => pause(x.id, base));
  const resumeAll = () => list.filter((x) => x.status === 'paused').forEach((x) => resume(x.id, base));
  const retryFailed = () => restartTasks([], base);

  // Reset failed, and the same per cause once there are two causes or more.
  // The More badge and the right-click on empty space both offer them.
  const failedGroups: MenuGroup[] =
    counts.error > 0
      ? [
          {
            id: 'failed',
            items: [
              {
                id: 'retryFailed',
                label: t('downloads.retryFailed'),
                detail: String(counts.error),
                icon: <IconRetry width={14} height={14} />,
                onSelect: () => void retryFailed(),
              },
              ...(causes.length > 0
                ? [
                    {
                      id: 'byCause',
                      label: t('downloads.retryByCause'),
                      icon: <IconRetry width={14} height={14} />,
                      submenu: [{ id: 'causes', items: causeItems(causes, base) }],
                    },
                  ]
                : []),
            ],
          },
        ]
      : [];

  /**
   * onContextMenu opens the menu for what the pointer landed on and takes the
   * selection with it. A link becomes the selection unless it is in one; a
   * package header takes its package unless a larger selection holds it; empty
   * space acts on the list.
   */
  function onContextMenu(e: React.MouseEvent, card: ListCard): void {
    // The column header may have claimed this right-click. Read off the native
    // event, since React fixes the synthetic defaultPrevented when it builds it.
    if (e.nativeEvent.defaultPrevented) return;
    const id = targetTaskId(e);
    const pkg = id === null ? targetPackage(e) : null;
    setMenuCard(card);
    if (id) {
      if (!selected.has(id)) setSelected(new Set([id]));
      setTarget({ kind: 'selection' });
    } else if (pkg !== null) {
      const ids = flat(shown[card])
        .filter((x) => (x.package || '') === pkg)
        .map((x) => x.id);
      if (!(ids.length > 0 && ids.every((x) => selected.has(x)))) setSelected(new Set(ids));
      setTarget({ kind: 'package', name: pkg });
    } else {
      setTarget({ kind: 'list' });
    }
    e.preventDefault();
    menu.openAt(anchorFromEvent(e));
  }

  // The menu's view of the card it was opened in. A package header there stands
  // for the package's links in that card, which for the loose links is not all
  // of them.
  // What the right-click on empty space offers besides the selection and the
  // folds: the bulk verbs, the failed ones, and the sort of every card on show.
  const bulkGroups: MenuGroup[] = [
    {
      id: 'bulk',
      items: [
        ...(counts.running > 0
          ? [{ id: 'pauseAll', label: t('downloads.pauseAll'), icon: <IconPause width={14} height={14} />, onSelect: pauseAll }]
          : []),
        ...(list.some((x) => x.status === 'paused')
          ? [{ id: 'resumeAll', label: t('downloads.resumeAll'), icon: <IconPlay width={14} height={14} />, onSelect: resumeAll }]
          : []),
      ],
    },
    ...failedGroups,
    ...sortMenuGroups(sorts, t('list.sort')),
  ];

  // The Status head's ▾ narrows every card by the quick filters.
  const columnFilters: ColumnFilters = {
    status: {
      active: filters.size > 0,
      panel: (
        <StatusFilter
          filters={offeredFilters}
          active={filters}
          onToggle={narrowing.toggleFilter}
          onClear={narrowing.clearFilters}
        />
      ),
    },
  };
  const openSearch = () => searchHandle.current?.open();

  const listContext: ListContext = {
    packages: shown[menuCard].map(([name]) => name),
    collapsed: folds.collapsed,
    onCollapse: folds.collapse,
    onExpand: folds.expand,
    onSelectAll: () => setSelected(new Set(flat(shown[menuCard]).map((x) => x.id))),
    onSelectNone: clearSelection,
    // Clean-up always runs here, never on the peer whose list is being shown.
    local: instance === '',
    members: (pkg) => byCard[menuCard].find(([name]) => name === pkg)?.[1] ?? [],
    whole: bulkGroups,
  };

  return (
    // flex-1 rather than h-full, as in Collector.tsx: the page takes the height
    // app/Layout.tsx leaves it and the list is the one part that scrolls.
    <div className="flex min-h-0 flex-1 flex-col gap-6">
      <PageHeader title={t('downloads.title')} />

      {/* The totals, and what narrows or picks from the lists, above them.
          The lists' own heads sort and filter, and typing searches. */}
      {list.length > 0 && (
        <DownloadStats all={list} hue={3}>
          <ListNarrowing
            shown={filtered.length}
            total={list.length}
            narrowed={narrowed}
            onClear={narrowing.clearAll}
            selection={
              selected.size > 0 ? (
                <span className="flex items-center gap-1.5">
                  <SelectionReach mode="select" total={selected.size} hidden={reach.hidden.length} onReduce={reduceToShown} />
                  <IconBadge
                    hue={1}
                    icon={<IconClearSelection width={16} height={16} />}
                    title={t('select.none')}
                    aria-label={t('select.none')}
                    onClick={clearSelection}
                  />
                </span>
              ) : undefined
            }
            views={
              <SavedViewChips
                profile="downloads"
                allowed={DOWNLOAD_FILTERS}
                narrowing={narrowing.narrowing}
                onApply={narrowing.apply}
                glyphs={false}
                folded={false}
              />
            }
          />
        </DownloadStats>
      )}

      {/* The scrolling regions: everything above keeps its height and the
          download list takes the rest, never less than a few rows. The two
          cards below it scroll on their own under a cap while the download
          list has rows, so a long finished list cannot push it off the page.
          A window too short for that scrolls the frame instead. */}
      <div ref={listArea} tabIndex={-1} className="relative flex min-h-48 flex-1 flex-col gap-6 outline-none">
        <ListSearch value={search} onChange={narrowing.setSearch} handle={searchHandle} area={listArea} />
        {list.length === 0 ? (
          <EmptyState
            fill
            icon={<IconDownloadArrow width={26} height={26} />}
            title={t('empty.downloadsTitle')}
            hint={t('empty.downloadsHint')}
          />
        ) : filtered.length === 0 ? (
          <EmptyState fill icon={<IconSearch width={26} height={26} />} title={t('downloads.noMatch')} />
        ) : (
          <>
            {groups.length > 0 && (
              // The collector's scroll box: a flex column, since h-full on
              // TaskListCard does not resolve through a flex-grown overflow
              // box, and pt-3 keeps the card's title badge inside its clip edge.
              <div
                className="flex min-h-48 flex-[1_0_50%] flex-col overflow-y-auto pt-3"
                onContextMenu={(e) => onContextMenu(e, 'downloads')}
              >
                <TaskListCard
                  groups={groups}
                  base={base}
                  selection={selectionIn('downloads')}
                  revealKey={revealRow?.card === 'downloads' ? revealRow.key : undefined}
                  // The same removal path as the selection bar and the context menu.
                  onRemovePackage={removal.ask}
                  rename={rename}
                  extractions={extractions}
                  stopMark={queueVerbs.stopMark}
                  title={narrowedTitle(filters, t, t('downloads.listTitle'))}
                  count={flat(groups).length}
                  hue={0}
                  filters={columnFilters}
                  onSearch={openSearch}
                />
              </div>
            )}
            {/* The badges on these cards' top edge are buttons, a whole
                button height tall, so half of one and its focus ring overhang
                the edge and need more room above it than a title badge. */}
            {SETTLED.map(
              ({ card, title, hue }) =>
                shown[card].length > 0 && (
                  <div
                    key={card}
                    ref={card === 'torrents' ? torrentsRef : undefined}
                    className={`flex min-h-0 flex-initial flex-col overflow-y-auto pt-[calc(var(--btn-h)/2_+_4px)] ${groups.length > 0 ? 'max-h-[35vh]' : ''} ${cardFold[card].folded ? 'shrink-0' : ''}`}
                    onContextMenu={(e) => onContextMenu(e, card)}
                  >
                    <TaskListCard
                      groups={shown[card]}
                      base={base}
                      selection={selectionIn(card)}
                      revealKey={revealRow?.card === card ? revealRow.key : undefined}
                      onRemovePackage={removal.ask}
                      rename={rename}
                      extractions={extractions}
                      stopMark={queueVerbs.stopMark}
                      sortKey={`downloads.${card}`}
                      title={t(title)}
                      count={flat(shown[card]).length}
                      fold={cardFold[card]}
                      edge={card === 'torrents' ? <SeedingBadges tasks={flat(shown.torrents)} base={base} /> : undefined}
                      hue={hue}
                      filters={columnFilters}
                      onSearch={openSearch}
                    />
                  </div>
                ),
            )}
          </>
        )}
      </div>

      {/* `all`, since a removal weighs bytes of rows this page never shows. */}
      <ListMenu
        anchor={menu.anchor}
        onClose={menu.close}
        all={all}
        selected={selected}
        base={base}
        removal={removal}
        queue={queueVerbs}
        target={target}
        list={listContext}
        rename={rename}
        extraGroups={[...archiveGroups, ...fileGroups, ...scriptGroups]}
        settled={menuCard !== 'downloads'}
      />
      {removal.dialog}
      {rename.dialog}
      {/* The dialog of this page's useCleanup(), also raised by the "clear
          finished" command. */}
      {cleanup.dialog}
    </div>
  );
}
