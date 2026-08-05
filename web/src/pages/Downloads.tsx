import { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { type Instance, type Task, apiBase, pause, resume, remove, restartTasks, fetchInstances } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { fmtSpeed } from '../lib/format';
import { PageHeader, Button } from '../components/ui';
import { SpeedGraph } from '../components/SpeedGraph';
import { PackageGroup, groupByPackage } from '../components/TaskList';
import { IconSearch } from '../lib/icons';

type Filter = 'all' | 'active' | 'done' | 'error';
const FILTERS: { key: Filter; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'active', label: 'Active' },
  { key: 'done', label: 'Done' },
  { key: 'error', label: 'Errors' },
];

function matchesFilter(t: Task, f: Filter): boolean {
  if (f === 'all') return true;
  if (f === 'active') return t.status === 'running' || t.status === 'queued' || t.status === 'extracting' || t.status === 'paused';
  if (f === 'done') return t.status === 'done';
  return t.status === 'error';
}

export function Downloads() {
  const [params] = useSearchParams();
  const [instances, setInstances] = useState<Instance[]>([]);
  const [instance, setInstance] = useState(params.get('instance') ?? '');
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState<Filter>('all');
  const base = apiBase(instance);
  const tasks = useTasks(instance);

  useEffect(() => {
    fetchInstances().then(setInstances);
  }, []);

  const list = useMemo(
    () =>
      Object.values(tasks)
        .filter((t) => t.status !== 'collected')
        .sort((a, b) => (a.createdAt < b.createdAt ? -1 : 1)),
    [tasks],
  );

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return list.filter((t) => matchesFilter(t, filter) && (!q || (t.name || t.url).toLowerCase().includes(q)));
  }, [list, filter, query]);
  const groups = useMemo(() => groupByPackage(filtered), [filtered]);

  const stats = useMemo(() => {
    let running = 0,
      queued = 0,
      done = 0,
      error = 0,
      speed = 0;
    for (const t of list) {
      if (t.status === 'running' || t.status === 'extracting') running++;
      else if (t.status === 'queued') queued++;
      else if (t.status === 'done') done++;
      else if (t.status === 'error') error++;
      if (t.status === 'running') speed += t.speed;
    }
    return { running, queued, done, error };
  }, [list]);
  const speed = useMemo(() => list.reduce((s, t) => s + (t.status === 'running' ? t.speed : 0), 0), [list]);

  const pauseAll = () => list.filter((t) => t.status === 'running').forEach((t) => pause(t.id, base));
  const resumeAll = () => list.filter((t) => t.status === 'paused').forEach((t) => resume(t.id, base));
  const clearDone = () =>
    list.filter((t) => t.status === 'done' || t.status === 'error').forEach((t) => remove(t.id, base));
  const retryFailed = () => restartTasks([], base);

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Downloads"
        subtitle="Active and finished transfers."
        right={
          instances.length > 0 && (
            <select
              value={instance}
              onChange={(e) => setInstance(e.target.value)}
              className="rounded-lg bg-carbon-surface2 px-3 py-2 text-sm text-carbon-text outline-none"
              aria-label="Instance"
            >
              <option value="">This instance</option>
              {instances.map((i) => (
                <option key={i.name} value={i.name}>
                  {i.name}
                </option>
              ))}
            </select>
          )
        }
      />

      <div className="kl-card overflow-hidden">
        <div className="flex items-end justify-between px-5 pt-4">
          <div>
            <div className="text-carbon-textMuted text-xs">Total speed</div>
            <div className="text-2xl font-bold tabular-nums text-carbon-text">{fmtSpeed(speed) || '—'}</div>
          </div>
          <div className="flex items-center gap-1 pb-1">
            <Button kind="ghost" onClick={pauseAll} disabled={stats.running === 0}>
              Pause all
            </Button>
            <Button kind="ghost" onClick={resumeAll} disabled={stats.queued + stats.running === 0}>
              Resume all
            </Button>
            <Button kind="ghost" onClick={retryFailed} disabled={stats.error === 0}>
              Retry failed
            </Button>
            <Button kind="ghost" onClick={clearDone} disabled={stats.done + stats.error === 0}>
              Clear finished
            </Button>
          </div>
        </div>
        <SpeedGraph value={speed} />
        <div className="flex flex-wrap gap-x-6 gap-y-1 px-5 pb-4 text-sm">
          <Counter label="Active" value={stats.running} tone="text-statusInfo" />
          <Counter label="Queued" value={stats.queued} tone="text-statusNeutral" />
          <Counter label="Done" value={stats.done} tone="text-statusOk" />
          {stats.error > 0 && <Counter label="Errors" value={stats.error} tone="text-statusFail" />}
        </div>
      </div>

      {list.length > 0 && (
        <div className="flex flex-wrap items-center gap-3">
          <div className="relative flex-1 min-w-[12rem] max-w-xs">
            <IconSearch className="absolute left-2.5 top-1/2 -translate-y-1/2 text-carbon-textMuted" width={16} height={16} />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Filter by name…"
              className="w-full rounded-lg bg-carbon-surface2 pl-8 pr-3 py-2 text-sm text-carbon-text placeholder:text-carbon-textMuted outline-none focus:ring-2 focus:ring-[var(--status-info-solid)]"
            />
          </div>
          <div className="flex items-center gap-1 rounded-lg bg-carbon-surface2 p-1">
            {FILTERS.map((f) => (
              <button
                key={f.key}
                onClick={() => setFilter(f.key)}
                className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                  filter === f.key ? 'bg-accent text-accentContrast' : 'text-carbon-textSub hover:text-carbon-text'
                }`}
              >
                {f.label}
              </button>
            ))}
          </div>
        </div>
      )}

      {list.length === 0 ? (
        <Empty />
      ) : filtered.length === 0 ? (
        <div className="kl-card p-8 text-center text-carbon-textMuted">Nothing matches this filter.</div>
      ) : (
        groups.map(([name, items]) => <PackageGroup key={name || '__none'} name={name} items={items} base={base} />)
      )}
    </div>
  );
}

function Counter({ label, value, tone }: { label: string; value: number; tone: string }) {
  return (
    <div className="flex items-baseline gap-1.5">
      <span className={`font-semibold tabular-nums ${tone}`}>{value}</span>
      <span className="text-carbon-textMuted text-xs">{label}</span>
    </div>
  );
}

function Empty() {
  return (
    <div className="kl-card p-10 text-center text-carbon-textMuted">
      Nothing downloading yet. Add links in the <span className="text-carbon-textSub">Collector</span> and start them.
    </div>
  );
}
