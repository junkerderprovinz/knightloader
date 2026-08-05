import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import {
  type Instance,
  type Task,
  apiBase,
  pause,
  resume,
  remove,
  restartTasks,
  fetchInstances,
  setPriority,
  moveTasks,
} from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { fmtSpeed } from '../lib/format';
import { useT, type TranslationKey } from '../lib/i18n';
import { PageHeader, Button, EmptyState } from '../components/ui';
import { Counters } from '../components/Counters';
import { TaskListCard, groupByPackage, type Selection } from '../components/TaskList';
import { IconSearch, IconDownloads, IconArrowUp, IconArrowDown, IconTop, IconBottom } from '../lib/icons';

type Filter = 'all' | 'active' | 'done' | 'error';
const FILTERS: { key: Filter; label: TranslationKey }[] = [
  { key: 'all', label: 'downloads.filterAll' },
  { key: 'active', label: 'downloads.filterActive' },
  { key: 'done', label: 'downloads.filterDone' },
  { key: 'error', label: 'downloads.filterErrors' },
];

function matchesFilter(t: Task, f: Filter): boolean {
  if (f === 'all') return true;
  if (f === 'active')
    return t.status === 'running' || t.status === 'queued' || t.status === 'extracting' || t.status === 'paused';
  if (f === 'done') return t.status === 'done';
  return t.status === 'error';
}

export function Downloads() {
  const { t } = useT();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const [instances, setInstances] = useState<Instance[]>([]);
  const [instance, setInstance] = useState(params.get('instance') ?? '');
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState<Filter>('all');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const base = apiBase(instance);
  const tasks = useTasks(instance);

  useEffect(() => {
    fetchInstances().then(setInstances);
  }, []);

  const list = useMemo(
    () =>
      Object.values(tasks)
        .filter((x) => x.status !== 'collected')
        .sort((a, b) => (a.createdAt < b.createdAt ? -1 : 1)),
    [tasks],
  );

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return list.filter((x) => matchesFilter(x, filter) && (!q || (x.name || x.url).toLowerCase().includes(q)));
  }, [list, filter, query]);
  const groups = useMemo(() => groupByPackage(filtered), [filtered]);

  // Selections follow the list: anything that leaves it stops being selected.
  useEffect(() => {
    setSelected((prev) => {
      const live = new Set(list.map((x) => x.id));
      const next = new Set([...prev].filter((id) => live.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [list]);

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
  const clearDone = () =>
    list.filter((x) => x.status === 'done' || x.status === 'error').forEach((x) => remove(x.id, base));
  const retryFailed = () => restartTasks([], base);

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

      {/* No hero here — the list is this page's weight. The speed and counters
          ride as one quiet uncarded line so Overview keeps the big figure. */}
      {list.length > 0 && (
        <div className="flex flex-wrap items-baseline gap-x-6 gap-y-2">
          <span className="keep-num text-[20px] font-semibold leading-none text-carbon-text">
            {fmtSpeed(counts.speed) || '0 B/s'}
          </span>
          <Counters counts={counts} />
        </div>
      )}

      {list.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative min-w-[12rem] flex-1 max-w-xs">
            <IconSearch
              className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-carbon-textMuted"
              width={15}
              height={15}
            />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t('downloads.filterPlaceholder')}
              className="w-full rounded-[var(--radius-control)] bg-carbon-surface2 py-2 pl-8 pr-3 text-sm text-carbon-text placeholder:text-carbon-textMuted outline-none transition-shadow focus:shadow-[0_0_0_2px_var(--focus-ring)]"
            />
          </div>
          <div className="keep-well flex items-center gap-0.5 p-1">
            {FILTERS.map((f) => (
              <button
                key={f.key}
                onClick={() => setFilter(f.key)}
                className={`rounded-[7px] px-2.5 py-1 text-xs font-medium transition-colors ${
                  filter === f.key
                    ? 'bg-carbon-surface text-carbon-text'
                    : 'text-carbon-textMuted hover:text-carbon-text'
                }`}
              >
                {t(f.label)}
              </button>
            ))}
          </div>
          <span className="flex-1" />
          {/* Bulk actions appear only when they can do something, so the strip
              stays short instead of showing four greyed-out verbs. */}
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
            {counts.done + counts.error > 0 && (
              <Button kind="ghost" className="px-2.5 text-xs" onClick={clearDone}>
                {t('downloads.clearFinished')}
              </Button>
            )}
          </div>
        </div>
      )}

      {/* Queue order only means something while something is waiting, so these
          controls appear with a selection rather than sitting there greyed out. */}
      {selected.size > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <span className="keep-num text-sm text-carbon-textSub">
            {selected.size} {t('select.count')}
          </span>
          <Button kind="ghost" className="px-2.5 text-xs" onClick={() => setSelected(new Set())}>
            {t('select.none')}
          </Button>
          <span className="flex-1" />
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
            {t('downloads.retryFailed')}
          </Button>
          <Button
            kind="danger"
            className="px-2.5 text-xs"
            onClick={() => {
              ids().forEach((id) => remove(id, base));
              setSelected(new Set());
            }}
          >
            {t('collector.remove')}
          </Button>
        </div>
      )}

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
  );
}
