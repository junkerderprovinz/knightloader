import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import {
  type Instance,
  apiBase,
  pause,
  resume,
  restartTasks,
  fetchInstances,
  setPriority,
  moveTasks,
} from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { fmtSpeed } from '../lib/format';
import { useT } from '../lib/i18n';
import { PageHeader, Button, EmptyState } from '../components/ui';
import { Counters } from '../components/Counters';
import { TaskListCard, groupByPackage, type Selection } from '../components/TaskList';
import { PackageActions } from '../components/PackageActions';
import { QueueBar } from '../components/QueueBar';
import {
  DOWNLOAD_FILTERS,
  ListActionBar,
  ListMenu,
  ListToolbar,
  SelectionStrip,
  matchesQuickFilters,
  targetTaskId,
  useRemoval,
  type QuickFilterId,
} from '../components/ListToolbar';
import { EMPTY_SEARCH, matchesSearch, type SearchQuery } from '../components/SearchField';
import { anchorFromEvent, useContextMenu } from '../components/ContextMenu';
import { IconSearch, IconDownloads, IconArrowUp, IconArrowDown, IconTop, IconBottom } from '../lib/icons';

export function Downloads() {
  const { t } = useT();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const [instances, setInstances] = useState<Instance[]>([]);
  const [instance, setInstance] = useState(params.get('instance') ?? '');
  const [search, setSearch] = useState<SearchQuery>(EMPTY_SEARCH);
  const [filters, setFilters] = useState<Set<QuickFilterId>>(() => new Set());
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const menu = useContextMenu();
  const base = apiBase(instance);
  const tasks = useTasks(instance);

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
   * Right-click opens the menu for the row it landed on, and selects that row
   * first when it was not already selected — acting on something the user cannot
   * see highlighted is how the wrong download gets deleted.
   *
   * With no row under the pointer and nothing selected there is nothing to offer,
   * so the browser's own menu is left alone rather than replaced by an empty one.
   */
  function onContextMenu(e: React.MouseEvent): void {
    // Something closer to the pointer has already claimed this right-click — the
    // column header opens its own menu on it. Read off the native event, not the
    // synthetic one: React captures `defaultPrevented` when it builds the
    // synthetic event, so it is still false here however many handlers below
    // have called preventDefault.
    if (e.nativeEvent.defaultPrevented) return;
    const id = targetTaskId(e);
    if (!id && selected.size === 0) return;
    if (id && !selected.has(id)) setSelected(new Set([id]));
    e.preventDefault();
    menu.openAt(anchorFromEvent(e));
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={t('downloads.title')}
        subtitle={t('downloads.subtitle')}
        right={
          instances.length > 0 && (
            <select
              value={instance}
              onChange={(e) => setInstance(e.target.value)}
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

      <QueueBar base={base} />

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
        onMore={menu.openAt}
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

      <ListMenu
        anchor={menu.anchor}
        onClose={menu.close}
        all={list}
        selected={selected}
        base={base}
        removal={removal}
      />
      {removal.dialog}
    </div>
  );
}
