import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  type Instance,
  pause,
  resume,
  restartTasks,
  fetchInstances,
  setPriority,
  moveTasks,
} from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { useReportListView } from '../lib/listview';
import { useT } from '../lib/i18n';
import { useInstanceScope } from '../lib/instance';
import { PageHeader, EmptyState, IconBadge, InfoBubble } from '../components/ui';
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
  useCleanup,
  useRemoval,
  type ListContext,
  type MenuTarget,
  type QuickFilterId,
} from '../components/ListToolbar';
import { EMPTY_SEARCH, matchesSearch, SearchField, type SearchQuery } from '../components/SearchField';
import { ArchiveJobs, useArchiveMenu, useExtractJobs } from '../components/Archives';
import { useFileMenu } from '../components/FileActions';
import { useScriptMenu } from '../components/ScriptActions';
import { ContextMenu, anchorBelow, anchorFromEvent, useContextMenu } from '../components/ContextMenu';
import { useToast } from '../lib/toast';
import { usePublishCommandPageContext } from '../lib/commands/pageContext';
import {
  IconSearch,
  IconDownloads,
  IconArrowUp,
  IconArrowDown,
  IconTop,
  IconBottom,
  IconCheck,
  IconClose,
  IconPause,
  IconPlay,
  IconRetry,
  IconTrash,
  IconTrashFiles,
} from '../lib/icons';

export function Downloads() {
  const { t } = useT();
  const [instances, setInstances] = useState<Instance[]>([]);
  // Not page state: the shell bar's transport controls have to act on the same
  // instance this list is showing, and they cannot read a useState from in here.
  // See lib/instance.tsx.
  const { instance, base, select } = useInstanceScope();
  const [search, setSearch] = useState<SearchQuery>(EMPTY_SEARCH);
  const [filters, setFilters] = useState<Set<QuickFilterId>>(() => new Set());
  // The search field and its quick filters used to sit in a permanent row of
  // their own (jdp: "was jetzt neben dem Suchfeld steht soll weg") - now they
  // live behind the square badge on the stats line and only take up room
  // while somebody is actually narrowing the list.
  const [searchOpen, setSearchOpen] = useState(false);
  // The popover's own anchor, so a click anywhere else closes it - the
  // collector's own search badge already works exactly this way, and jdp asked
  // for the two to be identical (2026-09-06: "die suche soll exakt wie im
  // sammlertb nach unten aufploppen").
  const searchRef = useRef<HTMLDivElement>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const { toast } = useToast();
  const menu = useContextMenu();
  // A second anchor of its own: the clean-up menu opens under a badge, while
  // `menu` above is the row/selection context menu opened at a pointer.
  const cleanupMenu = useContextMenu();
  // What the pointer landed on. A link, a package header and the empty space
  // below the rows each offer a different menu.
  const [target, setTarget] = useState<MenuTarget>({ kind: 'selection' });
  // The same folded set the list card reads, because folding is also a menu
  // entry and the menu belongs to the page.
  const folds = useCollapsedPackages('downloads');
  const tasks = useTasks(instance);
  // Unpacking is its own stream, not a field on the task: an archive has its own
  // progress, its own failure and its own stop, and folding those onto the row
  // that fetched it is what made an extraction invisible in the first place.
  const jobs = useExtractJobs(instance);

  useEffect(() => {
    fetchInstances().then(setInstances);
  }, []);

  // Everything this instance holds, collector included: a removal has to be able
  // to weigh bytes that the download list itself never shows.
  const all = useMemo(() => Object.values(tasks), [tasks]);

  // position over createdAt between two tasks that are BOTH still in the
  // wait queue: position is what drag-to-reorder and the menu's own
  // top/up/down/bottom moves actually write (App.ReorderBand/renumberBand),
  // and staying on createdAt here meant a reorder kept saving and
  // broadcasting a real change that this list re-sorted right back out of
  // view on every render. A settled task's position is frozen at whatever
  // it happened to be the moment it left the queue and compares to
  // nothing - createdAt is kept for any comparison touching one, unchanged
  // from before.
  const list = useMemo(
    () =>
      all
        .filter((x) => x.status !== 'collected')
        .sort((a, b) => {
          // Queue rank first, and that ordering is the whole fix (jdp,
          // 2026-09-06: "in der downloadliste kann ich ordner nach wie vor
          // nicht per drag and drop verschieben").
          //
          // What stood here compared two DIFFERENT keys depending on the pair:
          // position when both rows were still in the queue, createdAt as soon
          // as either one was not. That is not a total order - a settled row
          // could sort before a queued row that sorted before another settled
          // row that sorted before the first - and a comparator that
          // contradicts itself lets a sort produce any arrangement it likes.
          //
          // Measured live on the preview instance, dragging one folder above
          // another: the server accepted the reorder and renumbered exactly as
          // asked (the moved folder's links really did take the lower
          // positions), and the list did not move. One link in the folder had
          // failed, and that settled row - compared by createdAt against
          // everything - kept sorting near the top; groupByPackage re-merges a
          // package at its FIRST row, so the whole folder stayed anchored where
          // its dead link sat. The drag looked ignored, which is exactly the
          // shape "does not work" takes.
          //
          // So: everything still in the queue first, in the order the queue
          // holds it, then everything settled, oldest first. Both halves are
          // ordered by one key each, so the result is the same every time, and
          // a folder's place in the list is decided by the links a reorder can
          // actually move.
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

  // Selections follow the list: anything that leaves it stops being selected.
  useEffect(() => {
    setSelected((prev) => {
      const live = new Set(list.map((x) => x.id));
      const next = new Set([...prev].filter((id) => live.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [list]);

  // Closes the search popover on an outside click or Escape, the same handler
  // the collector's own popover uses.
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
  const removal = useRemoval({ all, selected, base, onDone: clearSelection });
  // The clean-up flow's own instance for this page's command surface (a
  // third caller of useCleanup, the same as ListActionBar and ListMenu below
  // already are — see that hook's own doc comment). Loaded proactively, the
  // same reason ListMenu loads its own copy on mount rather than waiting for
  // a click: a command visible in a palette that has to wait on a request
  // before it can say whether "clear finished" applies is a command that
  // answers late.
  const cleanup = useCleanup(all);
  useEffect(() => {
    void cleanup.load().catch(() => {
      /* the badge row above the list reports this on click; a command does not nag twice */
    });
  }, [cleanup.load]);

  // "Select all" means the rows on screen, never the whole queue: with a
  // filter on, the two are different sets and only one of them is the one
  // somebody is looking at.
  const allChosen = filtered.length > 0 && filtered.every((x) => selected.has(x.id));

  async function openCleanup(el: HTMLButtonElement | null): Promise<void> {
    try {
      await cleanup.load();
      cleanupMenu.openAt(anchorBelow(el));
    } catch {
      toast(t('list.optionsFailed'), 'fail');
    }
  }

  // The shell's overview strip offers Total / Visible / Selected, and "visible"
  // is the one it cannot work out for itself: the search text and the quick
  // filters are page state. Told which rows, it sums them from its own stream —
  // see lib/listview.ts.
  useReportListView(filtered, selected);
  // The command surface's own bridge (lib/commands/pageContext.ts): the exact
  // setSelected/removal/cleanup this page already holds, so
  // lib/commands/downloads.ts's selectAll/removeSelected/clearFinished call
  // the identical functions the toolbar's own buttons call, never a second
  // copy of what those verbs mean here.
  usePublishCommandPageContext(
    useMemo(
      () => ({ setSelection: setSelected, removal, cleanup, toggleSearch: () => setSearchOpen((v) => !v) }),
      [removal, cleanup],
    ),
  );
  const chosen = useMemo(() => all.filter((x) => selected.has(x.id)), [all, selected]);
  const archiveGroups = useArchiveMenu({ chosen, base, jobs });
  // Reveal-in-folder and open-natively only ever mean this instance's own
  // filesystem, never a federated peer's - see FileActions.tsx.
  const fileGroups = useFileMenu({ chosen, base, local: instance === '' });
  // Wave 11B: a saved script becomes a manual command on this table's own
  // context menu - the census's "DOWNLOAD_TABLE_CONTEXT_MENU_BUTTON" half of
  // the row. See ScriptActions.tsx.
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

  const narrowed = filters.size > 0 || search.text.trim() !== '';
  // The same chips the collector shows, over this list's own eight states.
  // Shared logic rather than a second copy, so the two rows cannot drift.
  const offeredFilters = useMemo(() => offeredQuickFilters(DOWNLOAD_FILTERS, list, filters), [list, filters]);

  function toggleFilter(id: QuickFilterId): void {
    const next = new Set(filters);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setFilters(next);
  }

  const pauseAll = () => list.filter((x) => x.status === 'running').forEach((x) => pause(x.id, base));
  const resumeAll = () => list.filter((x) => x.status === 'paused').forEach((x) => resume(x.id, base));
  const retryFailed = () => restartTasks([], base);

  /**
   * Right-click opens the menu for what it landed on, and takes the selection
   * with it — acting on something the user cannot see highlighted is how the
   * wrong download gets deleted.
   *
   * A link becomes the selection when it was not one already. A package header
   * takes the whole package, unless the package is already inside a bigger
   * selection, in which case that selection is what the user can see and what
   * the menu keeps acting on. Empty space acts on the list itself.
   */
  function onContextMenu(e: React.MouseEvent): void {
    // Something closer to the pointer has already claimed this right-click — the
    // column header opens its own menu on it. Read off the native event, not the
    // synthetic one: React captures `defaultPrevented` when it builds the
    // synthetic event, so it is still false here however many handlers below
    // have called preventDefault.
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

  // What the selection-half of the action row needs twice each.
  const selectedIds = chosen.map((x) => x.id);
  const selectedOnDisk = chosen.some((x) => x.loaded > 0);

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={t('downloads.title')} />

      {/* ONE row for everything this page can do, right-hugging, directly above
          the list - the collector's own row, down to the order of its parts
          (jdp, 2026-09-06: "all diese sachen sollen als buttons in einer zeile
          stehen"). It used to be three stacked rows: a speed-and-counters line,
          a badge row, and a selection strip that appeared underneath and pushed
          the list down every time somebody clicked a link.
          The counters line is gone entirely (jdp, same round, screenshot of it:
          "Was man im screenshot sieht bitte alles entfernen") - the speed is in
          the head card's own curve and the four states are the filter chips
          right here, each with its own count.
          "Mehr" and the shortcut bubble are not carried over, for the same
          reason the collector dropped them: the menu behind "Mehr" is the one a
          right-click on the selection already opens. */}
      {list.length > 0 && (
        <div className="flex shrink-0 flex-wrap items-center gap-2" role="group" aria-label={t('list.actions')}>
          <span className="flex-1" />

          {offeredFilters.length > 0 && (
            <Tabs
              select="many"
              size="sm"
              label={t('filter.label')}
              active={filters}
              onSelect={(id) => toggleFilter(id as QuickFilterId)}
              items={offeredFilters.map(({ f, n }) => ({ id: f.id, label: t(f.label), badge: n }))}
              after={
                filters.size > 0 && (
                  <IconBadge
                    hue={0}
                    icon={<IconClose width={16} height={16} />}
                    title={t('filter.clear')}
                    aria-label={t('filter.clear')}
                    onClick={() => setFilters(new Set())}
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
              <span className="glim-num text-sm text-carbon-textSub">
                {selected.size} {t('select.count')}
              </span>
              <IconBadge
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
              hue={0}
              active={searchOpen}
              icon={<IconSearch width={16} height={16} />}
              title={t('search.placeholder')}
              aria-label={t('search.placeholder')}
              aria-expanded={searchOpen}
              onClick={() => setSearchOpen((v) => !v)}
            />
            {/* The panel can close with a filter still active - this is the one
                sign of that once it does, so "why is my list short" has an
                answer without reopening the panel to find it. */}
            {narrowed && !searchOpen && (
              <span
                aria-hidden
                className="pointer-events-none absolute -right-1 -top-1 h-2 w-2 rounded-[var(--radius-pill)] bg-accent"
              />
            )}
            {searchOpen && (
              <div
                className="absolute end-0 top-full z-20 mt-2 w-96 rounded-[var(--radius-control)]
                  bg-carbon-surface p-2 shadow-[var(--elevation)]"
              >
                <SearchField value={search} onChange={setSearch} className="w-full" />
              </div>
            )}
          </div>

          {selected.size > 0 ? (
            <>
              <PackageActions tasks={list} selected={selected} base={base} />
              {/* Queue order only means something while something is waiting,
                  so these ride with the selection rather than sitting on the
                  page all the time. */}
              <IconBadge
                icon={<IconArrowUp width={16} height={16} />}
                hue={0}
                title={t('task.priorityUp')}
                aria-label={t('task.priorityUp')}
                onClick={() => setPriority(ids(), 1, base)}
              />
              <IconBadge
                icon={<IconArrowDown width={16} height={16} />}
                hue={1}
                title={t('task.priorityDown')}
                aria-label={t('task.priorityDown')}
                onClick={() => setPriority(ids(), -1, base)}
              />
              <IconBadge
                icon={<IconTop width={16} height={16} />}
                hue={2}
                title={t('task.moveTop')}
                aria-label={t('task.moveTop')}
                onClick={() => moveTasks(ids(), 'top', base)}
              />
              <IconBadge
                icon={<IconBottom width={16} height={16} />}
                hue={3}
                title={t('task.moveBottom')}
                aria-label={t('task.moveBottom')}
                onClick={() => moveTasks(ids(), 'bottom', base)}
              />
              <IconBadge
                hue={3}
                icon={<IconRetry width={16} height={16} />}
                title={t('task.restart')}
                aria-label={t('task.restart')}
                onClick={() => restartTasks(ids(), base)}
              />
              <IconBadge
                hue={4}
                icon={<IconTrash width={16} height={16} />}
                title={t('task.remove')}
                aria-label={t('task.remove')}
                onClick={() => void removal.removeNow(selectedIds)}
              />
              {selectedOnDisk && (
                <IconBadge
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
              {/* Each bulk verb appears only when it can do something, so the
                  row stays short instead of showing three dead badges. */}
              {counts.running > 0 && (
                <IconBadge
                  hue={2}
                  icon={<IconPause width={16} height={16} />}
                  title={t('downloads.pauseAll')}
                  aria-label={t('downloads.pauseAll')}
                  onClick={pauseAll}
                />
              )}
              {list.some((x) => x.status === 'paused') && (
                <IconBadge
                  hue={3}
                  icon={<IconPlay width={16} height={16} />}
                  title={t('downloads.resumeAll')}
                  aria-label={t('downloads.resumeAll')}
                  onClick={resumeAll}
                />
              )}
              {counts.error > 0 && (
                <IconBadge
                  hue={4}
                  icon={<IconRetry width={16} height={16} />}
                  title={t('downloads.retryFailed')}
                  aria-label={t('downloads.retryFailed')}
                  onClick={retryFailed}
                />
              )}
              <IconBadge
                hue={1}
                icon={<IconCheck width={16} height={16} />}
                title={allChosen ? t('select.none') : t('select.all')}
                aria-label={allChosen ? t('select.none') : t('select.all')}
                disabled={filtered.length === 0}
                onClick={() => setSelected(allChosen ? new Set() : new Set(filtered.map((x) => x.id)))}
              />
              <IconBadge
                hue={2}
                icon={<IconTrashFiles width={16} height={16} />}
                title={t('cleanup.menu')}
                aria-label={t('cleanup.menu')}
                disabled={instance !== ''}
                onClick={(e) => void openCleanup(e.currentTarget)}
              />
              {instance !== '' && <InfoBubble tip={t('cleanup.localOnly')} />}
            </>
          )}
        </div>
      )}

      <div onContextMenu={onContextMenu}>
        {list.length === 0 ? (
          <EmptyState
            icon={<IconDownloads width={28} height={28} />}
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
            title={t('downloads.listTitle')}
            // No hint bubble on the badge (jdp, 2026-09-06: "die i infobubble
            // im kartentitel entfernen. auch in der linklisten card"). It used
            // to explain what this list is, which was worth saying once and is
            // not worth a permanent (i) on the title of the page's main table:
            // by the time somebody has links in here they know what the list
            // is, and the bubble was in the way of the thing it described.
            hue={0}
          />
        )}
      </div>

      {/* Under the rows, because an extraction is what happens after one of them
          finished, and only while there is one to look at. */}
      <ArchiveJobs jobs={jobs} base={base} />

      {/* The clean-up badge's own menu, anchored under it. */}
      {cleanupMenu.anchor && cleanup.classes && (
        <ContextMenu
          anchor={cleanupMenu.anchor}
          label={t('cleanup.menuLabel')}
          onClose={cleanupMenu.close}
          groups={[{ id: 'cleanup', items: cleanupItems(cleanup.classes, t, (cls) => void cleanup.preview(cls)) }]}
        />
      )}

      {/* `all`, not `list`: a removal has to weigh bytes that belong to rows this
          page never shows, and a clean-up class picks its own. */}
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
      {/* This page's own useCleanup() instance (above) — raised by
          lib/commands/downloads.ts's "clear finished" command as well as by
          ListActionBar/ListMenu's own "Clean up" entries, each with its own
          copy of this same hook. */}
      {cleanup.dialog}
    </div>
  );
}
