import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { type Instance, pause, resume, restartTasks, fetchInstances } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { useReportListView } from '../lib/listview';
import { useT } from '../lib/i18n';
import { useInstanceScope } from '../lib/instance';
import { PageHeader, EmptyState, IconBadge } from '../components/ui';
import { Tabs } from '../components/Tabs';
import {
  TaskListCard,
  groupByPackage,
  useCollapsedPackages,
  type Selection,
} from '../components/TaskList';
import { PackageActions } from '../components/PackageActions';
import {
  DOWNLOAD_FILTERS,
  ListMenu,
  matchesQuickFilters,
  offeredQuickFilters,
  targetPackage,
  targetTaskId,
  cleanupItems,
  queueMenuGroup,
  useCleanup,
  useQueueVerbs,
  useRemoval,
  type ListContext,
  type MenuTarget,
  type QuickFilterId,
} from '../components/ListToolbar';
import { matchesSearch, SearchField } from '../components/SearchField';
import { claimReveal, useRevealRequest } from '../lib/reveal';
import { selectionReach, useDrawnRows } from '../lib/selectionReach';
import { SelectionReach } from '../components/SelectionReach';
import { SavedViewChips } from '../components/SavedViewChips';
import { useListNarrowing } from '../lib/listNarrowing';
import { ErrorCauses } from '../components/ErrorCauses';
import { ArchiveJobs, useArchiveMenu, useExtractJobs } from '../components/Archives';
import { useFileMenu } from '../components/FileActions';
import { useScriptMenu } from '../components/ScriptActions';
import { ContextMenu, anchorBelow, anchorFromEvent, useContextMenu } from '../components/ContextMenu';
import { useToast } from '../lib/toast';
import { usePublishCommandPageContext } from '../lib/commands/pageContext';
import {
  IconSearch,
  IconDownloads,
  IconCheck,
  IconClose,
  IconPause,
  IconPlay,
  IconPriority,
  IconRetry,
  IconTrash,
  IconTrashFiles,
} from '../lib/icons';

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
  // The search lives behind the badge on the stats line.
  const [searchOpen, setSearchOpen] = useState(false);
  // The popover's anchor, so an outside click closes it, as in the collector.
  const searchRef = useRef<HTMLDivElement>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  // The row TaskListCard should scroll to, with the request's nonce so a
  // second jump to the same row is a new value.
  const [revealRow, setRevealRow] = useState<string | undefined>(undefined);
  const { toast } = useToast();
  const menu = useContextMenu();
  // The clean-up menu opens under a badge; `menu` above opens at the pointer.
  const cleanupMenu = useContextMenu();
  // The queue-order badge's own anchor, apart from the right-click menu.
  const orderMenu = useContextMenu();
  // The priorities the server implements and where the stop mark sits, fetched
  // on mount so the menu opens complete.
  const queueVerbs = useQueueVerbs(base);
  // A link, a package header and empty space each offer their own menu.
  const [target, setTarget] = useState<MenuTarget>({ kind: 'selection' });
  // The same folded set the list card reads; folding is also a menu entry.
  const folds = useCollapsedPackages('downloads');
  const tasks = useTasks(instance);
  // Extraction has its own progress, failure and stop, so it is its own stream.
  const jobs = useExtractJobs(instance);

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

  const filtered = useMemo(
    () => list.filter((x) => matchesQuickFilters(x, filters) && matchesSearch(x, search)),
    [list, filters, search],
  );
  const groups = useMemo(() => groupByPackage(filtered), [filtered]);

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
    // '' is the ungrouped bucket's real name. A folded package draws no rows.
    folds.expand([task.package || '']);
    setSelected(new Set([reveal.id]));
    // Selecting the row marks it; .glim-row-selected paints the wash.
    setRevealRow(`task:${reveal.id}#${reveal.nonce}`);
  }, [reveal, tasks, filters, search, folds, narrowing]);

  // Closes the search popover on an outside click or Escape, as in the collector.
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

  const clearSelection = useCallback(() => setSelected(new Set()), []);

  // The rows actually drawn, narrower than `filtered` by every folded package
  // (lib/selectionReach.ts).
  const drawn = useDrawnRows(groups, folds.collapsed);
  const reach = useMemo(() => selectionReach(selected, drawn), [selected, drawn]);
  // Drops hidden rows from the selection and can offer the whole selection back.
  const reduceToShown = useCallback(() => {
    const before = new Set(selected);
    setSelected(new Set(reach.shown));
    toast(t('select.reduced').replace('{n}', String(reach.hidden.length)), 'info', 'action-done', {
      label: t('remove.undo'),
      run: () => setSelected(before),
    });
  }, [selected, reach, toast, t]);

  const removal = useRemoval({ all, selected, base, drawn, onDone: clearSelection });
  // The command surface's clean-up instance, loaded at once so "clear finished"
  // knows whether it applies.
  const cleanup = useCleanup(all);
  useEffect(() => {
    void cleanup.load().catch(() => {
      /* The badge row reports this on click; a command does not report twice. */
    });
  }, [cleanup.load]);

  // "Select all" means the rows on screen, not the whole queue.
  const allChosen = filtered.length > 0 && filtered.every((x) => selected.has(x.id));

  async function openCleanup(el: HTMLButtonElement | null): Promise<void> {
    try {
      await cleanup.load();
      cleanupMenu.openAt(anchorBelow(el));
    } catch {
      toast(t('list.optionsFailed'), 'fail');
    }
  }

  // The shell's overview strip cannot know which rows are visible, so it is told
  // (lib/listview.ts).
  useReportListView(filtered, selected);
  // The commands in lib/commands/downloads.ts call the same functions as the
  // toolbar (lib/commands/pageContext.ts).
  usePublishCommandPageContext(
    useMemo(
      () => ({ setSelection: setSelected, removal, cleanup, toggleSearch: () => setSearchOpen((v) => !v) }),
      [removal, cleanup],
    ),
  );
  const chosen = useMemo(() => all.filter((x) => selected.has(x.id)), [all, selected]);
  const archiveGroups = useArchiveMenu({ chosen, base, jobs });
  // Reveal and open natively only on this instance's own filesystem.
  const fileGroups = useFileMenu({ chosen, base, local: instance === '' });
  // Saved scripts become manual commands on this menu (ScriptActions.tsx).
  const scriptGroups = useScriptMenu({ chosen, base });

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

  const pauseAll = () => list.filter((x) => x.status === 'running').forEach((x) => pause(x.id, base));
  const resumeAll = () => list.filter((x) => x.status === 'paused').forEach((x) => resume(x.id, base));
  const retryFailed = () => restartTasks([], base);

  /**
   * onContextMenu opens the menu for what the pointer landed on and takes the
   * selection with it. A link becomes the selection unless it is in one; a
   * package header takes its package unless a larger selection holds it; empty
   * space acts on the list.
   */
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
    // Clean-up always runs here, never on the peer whose list is being shown.
    local: instance === '',
  };

  const selectedIds = chosen.map((x) => x.id);
  const selectedOnDisk = chosen.some((x) => x.loaded > 0);

  // The queue-order badge's menu, built by the right-click menu's own function
  // (queueMenuGroup), so the badge shows only when the group has entries. The
  // server sets a priority in every state, so finished and failed rows keep
  // the seven rungs.
  const queueGroup = queueMenuGroup({
    chosen,
    ids: selectedIds,
    base,
    t,
    fail: (e) => toast(e instanceof Error ? e.message : String(e), 'fail'),
    queue: queueVerbs,
  });

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={t('downloads.title')} />

      {/* One right-aligned row for every action, directly above the list, in
          the collector's order. */}
      {list.length > 0 && (
        <div className="flex shrink-0 flex-wrap items-center gap-2" role="group" aria-label={t('list.actions')}>
          {/* Left of the spacer, which nothing else here uses. */}
          <SavedViewChips
            profile="downloads"
            allowed={DOWNLOAD_FILTERS}
            narrowing={narrowing.narrowing}
            onApply={narrowing.apply}
          />
          <span className="flex-1" />

          {offeredFilters.length > 0 && (
            <Tabs
              select="many"
              size="sm"
              label={t('filter.label')}
              active={filters}
              onSelect={(id) => narrowing.toggleFilter(id as QuickFilterId)}
              items={offeredFilters.map(({ f, n }) => ({ id: f.id, label: t(f.label), badge: n }))}
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
              {t('search.shown', { n: filtered.length, total: list.length })}
            </span>
          )}

          {selected.size > 0 && (
            <>
              <SelectionReach
                mode="select"
                total={selected.size}
                hidden={reach.hidden.length}
                onReduce={reduceToShown}
              />
              <IconBadge
                labelled
                hue={1}
                icon={<IconClose width={16} height={16} />}
                title={t('select.none')}
                aria-label={t('select.none')}
                onClick={clearSelection}
              />
            </>
          )}

          <div ref={searchRef} className="relative">
            <IconBadge
              labelled
              hue={0}
              active={searchOpen}
              icon={<IconSearch width={16} height={16} />}
              // The badge's own name rather than the field's placeholder, which
              // Beschriftung would print; web/check-placeholder-as-label.mjs
              // keeps it so.
              title={t('search.toggle')}
              aria-label={t('search.toggle')}
              aria-expanded={searchOpen}
              onClick={() => setSearchOpen((v) => !v)}
            />
            {/* Shows that a filter is still active once the panel is closed. */}
            {narrowed && !searchOpen && (
              <span
                aria-hidden
                className="pointer-events-none absolute -end-1 -top-1 h-2 w-2 rounded-[var(--radius-pill)] bg-accent"
              />
            )}
            {searchOpen && (
              <div
                className="absolute end-0 top-full z-20 mt-2 w-96 rounded-[var(--radius-control)]
                  bg-carbon-surface p-2 shadow-[var(--elevation)]"
              >
                <SearchField value={search} onChange={narrowing.setSearch} className="w-full" />
              </div>
            )}
          </div>

          {/* Clears search and filters at once; "Show everything" in the chip
              strip still clears only the quick filters. */}
          {narrowed && (
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

          {selected.size > 0 ? (
            <>
              <PackageActions tasks={list} selected={selected} base={base} />
              {/* One badge for queue order, opening the same group as the
                  right-click menu: moves by step and the seven priorities by
                  name. It shows whenever the group has entries. */}
              {queueGroup.items.length > 0 && (
                <IconBadge
                  labelled
                  icon={<IconPriority width={16} height={16} />}
                  // Not 0: with rainbow on and search open, two adjacent badges
                  // of one hue would read as one control.
                  hue={2}
                  title={t('queue.order')}
                  aria-label={t('queue.order')}
                  aria-haspopup="menu"
                  aria-expanded={!!orderMenu.anchor}
                  onClick={(e) => orderMenu.openAt(anchorBelow(e.currentTarget))}
                />
              )}
              <IconBadge
                labelled
                hue={3}
                icon={<IconRetry width={16} height={16} />}
                title={t('task.restart')}
                aria-label={t('task.restart')}
                onClick={() => restartTasks(ids(), base)}
              />
              <IconBadge
                labelled
                hue={4}
                icon={<IconTrash width={16} height={16} />}
                title={t('task.remove')}
                aria-label={t('task.remove')}
                onClick={() => void removal.removeNow(selectedIds)}
              />
              {selectedOnDisk && (
                <IconBadge
                  labelled
                  hue={5}
                  icon={<IconTrashFiles width={16} height={16} />}
                  title={t('task.removeWithFiles')}
                  aria-label={t('task.removeWithFiles')}
                  onClick={() => removal.askWithFiles(selectedIds)}
                />
              )}
            </>
          ) : (
            <>
              {/* Each bulk verb appears only when it can do something. */}
              {counts.running > 0 && (
                <IconBadge
                  labelled
                  hue={2}
                  icon={<IconPause width={16} height={16} />}
                  title={t('downloads.pauseAll')}
                  aria-label={t('downloads.pauseAll')}
                  onClick={pauseAll}
                />
              )}
              {list.some((x) => x.status === 'paused') && (
                <IconBadge
                  labelled
                  hue={3}
                  icon={<IconPlay width={16} height={16} />}
                  title={t('downloads.resumeAll')}
                  aria-label={t('downloads.resumeAll')}
                  onClick={resumeAll}
                />
              )}
              {counts.error > 0 && (
                <IconBadge
                  labelled
                  hue={4}
                  icon={<IconRetry width={16} height={16} />}
                  title={t('downloads.retryFailed')}
                  aria-label={t('downloads.retryFailed')}
                  onClick={retryFailed}
                />
              )}
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
                icon={<IconTrashFiles width={16} height={16} />}
                title={t('cleanup.menu')}
                aria-label={t('cleanup.menu')}
                disabled={instance !== ''}
                hint={instance !== '' ? t('cleanup.localOnly') : undefined}
                onClick={(e) => void openCleanup(e.currentTarget)}
              />
            </>
          )}
        </div>
      )}

      {/* Above the rows while the failures have several causes; draws nothing
          below two groups. */}
      <ErrorCauses tasks={list} base={base} />

      <div onContextMenu={onContextMenu}>
        {list.length === 0 ? (
          <EmptyState
            icon={<IconDownloads width={26} height={26} />}
            title={t('empty.downloadsTitle')}
            hint={t('empty.downloadsHint')}
          />
        ) : filtered.length === 0 ? (
          <EmptyState icon={<IconSearch width={26} height={26} />} title={t('downloads.noMatch')} />
        ) : (
          <TaskListCard
            groups={groups}
            base={base}
            selection={selection}
            revealKey={revealRow}
            // The same removal path as the selection bar and the context menu.
            onRemovePackage={removal.askWithFiles}
            title={t('downloads.listTitle')}
            hue={0}
          />
        )}
      </div>

      {/* Under the rows, while an extraction is running. */}
      <ArchiveJobs jobs={jobs} base={base} />

      {/* The queue-order menu under its badge, the same group as the
          right-click menu's queue section. */}
      {orderMenu.anchor && queueGroup.items.length > 0 && (
        <ContextMenu
          anchor={orderMenu.anchor}
          label={t('queue.order')}
          onClose={orderMenu.close}
          groups={[queueGroup]}
        />
      )}

      {cleanupMenu.anchor && cleanup.classes && (
        <ContextMenu
          anchor={cleanupMenu.anchor}
          label={t('cleanup.menuLabel')}
          onClose={cleanupMenu.close}
          groups={[{ id: 'cleanup', items: cleanupItems(cleanup.classes, t, (cls) => void cleanup.preview(cls)) }]}
        />
      )}

      {/* `all`, since a removal weighs bytes of rows this page never shows. */}
      <ListMenu
        anchor={menu.anchor}
        onClose={menu.close}
        all={all}
        selected={selected}
        base={base}
        removal={removal}
        target={target}
        list={listContext}
        extraGroups={[...archiveGroups, ...fileGroups, ...scriptGroups]}
      />
      {removal.dialog}
      {/* The dialog of this page's useCleanup(), also raised by the "clear
          finished" command. */}
      {cleanup.dialog}
    </div>
  );
}
