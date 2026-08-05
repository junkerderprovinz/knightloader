import { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { type Instance, apiBase, pause, resume, remove, fetchInstances } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { fmtSpeed } from '../lib/format';
import { PageHeader, Button } from '../components/ui';
import { SpeedGraph } from '../components/SpeedGraph';
import { PackageGroup, groupByPackage } from '../components/TaskList';

export function Downloads() {
  const [params] = useSearchParams();
  const [instances, setInstances] = useState<Instance[]>([]);
  const [instance, setInstance] = useState(params.get('instance') ?? '');
  const base = apiBase(instance);
  const tasks = useTasks(instance);

  useEffect(() => {
    fetchInstances().then(setInstances);
  }, []);

  // Downloads shows everything that has left the collector.
  const list = useMemo(
    () =>
      Object.values(tasks)
        .filter((t) => t.status !== 'collected')
        .sort((a, b) => (a.createdAt < b.createdAt ? -1 : 1)),
    [tasks],
  );
  const groups = useMemo(() => groupByPackage(list), [list]);

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
    return { running, queued, done, error, speed };
  }, [list]);

  const pauseAll = () => list.filter((t) => t.status === 'running').forEach((t) => pause(t.id, base));
  const resumeAll = () => list.filter((t) => t.status === 'paused').forEach((t) => resume(t.id, base));
  const clearDone = () =>
    list.filter((t) => t.status === 'done' || t.status === 'error').forEach((t) => remove(t.id, base));

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

      {/* Live speed + counters */}
      <div className="kl-card overflow-hidden">
        <div className="flex items-end justify-between px-5 pt-4">
          <div>
            <div className="text-carbon-textMuted text-xs">Total speed</div>
            <div className="text-2xl font-bold tabular-nums text-carbon-text">{fmtSpeed(stats.speed) || '—'}</div>
          </div>
          <div className="flex items-center gap-1 pb-1">
            <Button kind="ghost" onClick={pauseAll} disabled={stats.running === 0}>
              Pause all
            </Button>
            <Button kind="ghost" onClick={resumeAll} disabled={stats.queued + stats.running === 0}>
              Resume all
            </Button>
            <Button kind="ghost" onClick={clearDone} disabled={stats.done + stats.error === 0}>
              Clear finished
            </Button>
          </div>
        </div>
        <SpeedGraph value={stats.speed} />
        <div className="flex flex-wrap gap-x-6 gap-y-1 px-5 pb-4 text-sm">
          <Counter label="Active" value={stats.running} tone="text-statusInfo" />
          <Counter label="Queued" value={stats.queued} tone="text-statusNeutral" />
          <Counter label="Done" value={stats.done} tone="text-statusOk" />
          {stats.error > 0 && <Counter label="Errors" value={stats.error} tone="text-statusFail" />}
        </div>
      </div>

      {list.length === 0 ? (
        <Empty />
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
