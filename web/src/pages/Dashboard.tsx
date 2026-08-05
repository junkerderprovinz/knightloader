import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { type Instance, fetchInstances } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { fmtBytes, fmtSpeed, pct } from '../lib/format';
import { PageHeader, Card } from '../components/ui';
import { StatCard } from '../components/StatCard';
import { SpeedGraph } from '../components/SpeedGraph';
import { ProgressBar } from '../components/ProgressBar';
import { StatusPill, ResolverBadge } from '../components/StatusPill';
import { InstanceCard } from '../components/InstanceCard';

export function Dashboard() {
  const tasks = useTasks('');
  const [instances, setInstances] = useState<Instance[]>([]);
  const navigate = useNavigate();

  useEffect(() => {
    fetchInstances().then(setInstances);
  }, []);

  const list = useMemo(() => Object.values(tasks), [tasks]);
  const s = useMemo(() => {
    let running = 0,
      queued = 0,
      done = 0,
      error = 0,
      collected = 0,
      speed = 0;
    for (const t of list) {
      if (t.status === 'running' || t.status === 'extracting') running++;
      else if (t.status === 'queued') queued++;
      else if (t.status === 'done') done++;
      else if (t.status === 'error') error++;
      else if (t.status === 'collected') collected++;
      if (t.status === 'running') speed += t.speed;
    }
    return { running, queued, done, error, collected, speed };
  }, [list]);

  const recent = useMemo(
    () =>
      list
        .filter((t) => t.status !== 'collected')
        .sort((a, b) => (a.createdAt > b.createdAt ? -1 : 1))
        .slice(0, 6),
    [list],
  );

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Overview" subtitle="Everything at a glance." />

      <Card className="p-0 overflow-hidden">
        <div className="px-5 pt-4">
          <div className="text-carbon-textMuted text-xs">Total download speed</div>
          <div className="text-3xl font-bold tabular-nums text-carbon-text">{fmtSpeed(s.speed) || '0 B/s'}</div>
        </div>
        <SpeedGraph value={s.speed} height={72} />
      </Card>

      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-4">
        <StatCard label="Active" value={s.running} tone="text-statusInfo" />
        <StatCard label="Queued" value={s.queued} tone="text-statusNeutral" />
        <StatCard label="In collector" value={s.collected} tone="text-carbon-text" />
        <StatCard label="Done" value={s.done} tone="text-statusOk" />
        <StatCard label="Errors" value={s.error} tone={s.error ? 'text-statusFail' : 'text-carbon-text'} />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <div className="lg:col-span-2 flex flex-col gap-3">
          <h2 className="text-base font-semibold text-carbon-text">Recent</h2>
          {recent.length === 0 ? (
            <Card className="text-carbon-textMuted text-sm">No downloads yet.</Card>
          ) : (
            <Card className="flex flex-col p-0 overflow-hidden">
              {recent.map((t) => (
                <div
                  key={t.id}
                  className="flex items-center gap-3 px-5 py-2.5 border-t border-carbon-border/50 first:border-t-0"
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-carbon-text text-sm">{t.name || t.url}</span>
                      <ResolverBadge resolver={t.resolver} />
                    </div>
                    <div className="mt-1 max-w-sm">
                      <ProgressBar
                        percent={pct(t.loaded, t.size, t.status === 'done')}
                        active={t.status !== 'error'}
                        indeterminate={t.status === 'queued'}
                      />
                    </div>
                  </div>
                  <span className="text-carbon-textSub text-xs tabular-nums">{fmtBytes(t.size)}</span>
                  <StatusPill status={t.status} />
                </div>
              ))}
            </Card>
          )}
        </div>

        <div className="flex flex-col gap-3">
          <h2 className="text-base font-semibold text-carbon-text">Instances</h2>
          <InstanceCard name="This instance" url={location.host} base="/api" />
          {instances.map((i) => (
            <InstanceCard
              key={i.name}
              name={i.name}
              url={i.url}
              base={`/api/instances/${encodeURIComponent(i.name)}`}
              onOpen={() => navigate(`/downloads?instance=${encodeURIComponent(i.name)}`)}
            />
          ))}
        </div>
      </div>
    </div>
  );
}
