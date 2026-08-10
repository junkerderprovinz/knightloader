import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
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
import { fmtSpeed } from '../lib/format';
import { useT } from '../lib/i18n';
import { useInstanceScope } from '../lib/instance';
import { PageHeader, Button, EmptyState } from '../components/ui';
import { Counters } from '../components/Counters';
import {
  TaskListCard,
  groupByPackage,
  useCollapsedPackages,
  type Selection,
} from '../components/TaskList';
import { PackageActions } from '../components/PackageActions';
import {
  DOWNLOAD_FILTERS,
  ListActionBar,
  ListMenu,
  ListToolbar,
  SelectionStrip,
  matchesQuickFilters,
  targetPackage,
  targetTaskId,
  useRemoval,
  type ListContext,
  type MenuTarget,
  type QuickFilterId,
} from '../components/ListToolbar';
import { EMPTY_SEARCH, matchesSearch, type SearchQuery } from '../components/SearchField';
import { ArchiveJobs, useArchiveMenu, useExtractJobs } from '../components/Archives';
import { useFileMenu } from '../components/FileActions';
import { anchorFromEvent, useContextMenu } from '../components/ContextMenu';
import { IconSearch, IconDownloads, IconArrowUp, IconArrowDown, IconTop, IconBottom } from '../lib/icons';

export function Downloads() {
  const { t } = useT();
  const navigate = useNavigate();
  const [instances, setInstances] = useState<Instance[]>([]);
  // Not page state: the shell bar's transport controls have to act on the same
  // instance this list is showing, and they cannot read a useState from in here.
  // See lib/instance.tsx.
  const { instance, base, select } = useInstanceScope();
  const [search, setSearch] = useState<SearchQuery>(EMPTY_SEARCH);
  const [filters, setFilters] = useState<Set<QuickFilterId>>(() => new Set());
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const menu = useContextMenu();
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

  const list = useMemo(
    () =>
      all
        .filter((x) => x.status !== 'collected')
        .sort((a, b) => (a.createdAt < b.createdAt ? -1 : 1)),
    [all],
  );

  const filtered = useMemo(
    () => list.filter((x) => matchesQuickFilters(x, filters) && matchesSearch(x, search)),
    [list, filters, search],
  );
  const groups = useMemo(() => groupByPackage(filtered), [filtered]);

  // The shell's overview strip offers Total / Visible / Selected, and "visible"
  // is the one it cannot work out for itself: the search text and the quick
  // filters are page state. Told which rows, it sums them from its own stream —
  // see lib/listview.ts.
  useReportListView(filtered, selected);

  // Selections follow the list: anything that leaves it stops being selected.
  useEffect(() => {
    setSelected((prev) => {
      const live = new Set(list.map((x) => x.id));
      const next = new Set([...prev].filter((id) => live.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [list]);

  const clearSelection = useCallback(() => setSelected(new Set()), []);
  const removal = useRemoval({ all, selected, base, onDone: clearSelection });
  const chosen = useMemo(() => all.filter((x) => selected.has(x.id)), [all, selected]);
  const archiveGroups = useArchiveMenu({ chosen, base, jobs });
  // Reveal-in-folder and open-natively only ever mean this instance's own
  // filesystem, never a federated peer's - see FileActions.tsx.
  const fileGroups = useFileMenu({ chosen, base, local: instance === '' });

  const selection: Selection = {
    ids: selected,
    toggle: (id) =>
      setSelected((s) => {
        const n = new Set(s);
        if (n.has(id)) n.delete(id);
        else n.add(id);
        return n;
      }),
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

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={t('downloads.title')}
        subtitle={t('downloads.subtitle')}
        right={
          instances.length > 0 && (
            <select
              value={instance}
              onChange={(e) => select(e.target.value)}
              className="rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-sm text-carbon-text outline-none"
              aria-label={t('nav.instances')}
            >
              <option value="">{t('downloads.thisInstance')}</option>
              {instances.map((i) => (
                <option key={i.name} value={i.name}>
                  {i.name}
                </option>
              ))}
            </select>
          )
        }
      />

      {/* No hero here — the list is this page's weight. The speed and counters
          ride as one quiet uncarded line so Overview keeps the big figure. */}
      {list.length > 0 && (
        <div className="flex flex-wrap items-baseline gap-x-6 gap-y-2">
          <span className="glim-num text-[20px] font-semibold leading-none text-carbon-text">
            {fmtSpeed(counts.speed) || '0 B/s'}
          </span>
          <Counters counts={counts} />
        </div>
      )}

      {list.length > 0 && (
        <ListToolbar
          search={search}
          onSearch={setSearch}
          filters={DOWNLOAD_FILTERS}
          active={filters}
          onActive={setFilters}
          tasks={list}
          shown={filtered.length}
          right={
            /* Bulk actions appear only when they can do something, so the strip
               stays short instead of showing three greyed-out verbs. */
            <div className="flex items-center gap-0.5">
              {counts.running > 0 && (
                <Button kind="ghost" className="px-2.5 text-xs" onClick={pauseAll}>
                  {t('downloads.pauseAll')}
                </Button>
              )}
              {list.some((x) => x.status === 'paused') && (
                <Button kind="ghost" className="px-2.5 text-xs" onClick={resumeAll}>
                  {t('downloads.resumeAll')}
                </Button>
              )}
              {counts.error > 0 && (
                <Button kind="ghost" className="px-2.5 text-xs" onClick={retryFailed}>
                  {t('downloads.retryFailed')}
                </Button>
              )}
            </div>
          }
        />
      )}

      <SelectionStrip
        all={list}
        selected={selected}
        onSelected={setSelected}
        removal={removal}
        onMore={(at) => {
          setTarget({ kind: 'selection' });
          menu.openAt(at);
        }}
      >
        <PackageActions tasks={list} selected={selected} base={base} />
        {/* Queue order only means something while something is waiting, so these
            sit with the selection rather than on the page all the time. */}
        <Button
          kind="ghost"
          icon={<IconArrowUp width={16} height={16} />}
          title={t('task.priorityUp')}
          onClick={() => setPriority(ids(), 1, base)}
        />
        <Button
          kind="ghost"
          icon={<IconArrowDown width={16} height={16} />}
          title={t('task.priorityDown')}
          onClick={() => setPriority(ids(), -1, base)}
        />
        <Button
          kind="ghost"
          icon={<IconTop width={16} height={16} />}
          title={t('task.moveTop')}
          onClick={() => moveTasks(ids(), 'top', base)}
        />
        <Button
          kind="ghost"
          icon={<IconBottom width={16} height={16} />}
          title={t('task.moveBottom')}
          onClick={() => moveTasks(ids(), 'bottom', base)}
        />
        <Button kind="secondary" className="px-2.5 text-xs" onClick={() => restartTasks(ids(), base)}>
          {t('task.restart')}
        </Button>
      </SelectionStrip>

      <div onContextMenu={onContextMenu}>
        {list.length === 0 ? (
          <EmptyState
            icon={<IconDownloads width={28} height={28} />}
            title={t('empty.downloadsTitle')}
            hint={t('empty.downloadsHint')}
            action={
              <Button kind="secondary" onClick={() => navigate('/collector')}>
                {t('empty.goCollector')}
              </Button>
            }
          />
        ) : filtered.length === 0 ? (
          <EmptyState icon={<IconSearch width={26} height={26} />} title={t('downloads.noMatch')} />
        ) : (
          <TaskListCard groups={groups} base={base} selection={selection} />
        )}
      </div>

      {/* Under the rows, because an extraction is what happens after one of them
          finished, and only while there is one to look at. */}
      <ArchiveJobs jobs={jobs} base={base} />

      {/* Under the list and always there, including when the list is empty —
          which is the one moment somebody is looking for the way to add
          something. */}
      <ListActionBar
        all={all}
        selected={selected}
        onSelected={setSelected}
        visible={filtered}
        local={instance === ''}
      >
        <Button kind="secondary" className="px-2.5 text-xs" onClick={() => navigate('/collector')}>
          {t('empty.goCollector')}
        </Button>
      </ListActionBar>

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
        extraGroups={[...archiveGroups, ...fileGroups]}
      />
      {removal.dialog}
    </div>
  );
}
