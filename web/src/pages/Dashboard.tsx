import { useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { type Instance, fetchInstances } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { useResource } from '../lib/useResource';
import { fmtBytes, fmtSpeed, pct } from '../lib/format';
import { useT } from '../lib/i18n';
import { PageHeader, SectionTitle, EmptyState } from '../components/ui';
import { SpeedGraph } from '../components/SpeedGraph';
import { Counters } from '../components/Counters';
import { ProgressBar } from '../components/ProgressBar';
import { StatusPill } from '../components/StatusPill';
import { InstanceRow } from '../components/InstanceCard';
import { IconDownloads } from '../lib/icons';

export function Dashboard() {
  const { t } = useT();
  const tasks = useTasks('');
  const { data: instances } = useResource<Instance[]>(fetchInstances);
  const navigate = useNavigate();

  const list = useMemo(() => Object.values(tasks), [tasks]);
  const counts = useMemo(() => {
    let running = 0,
      queued = 0,
      done = 0,
      error = 0,
      collected = 0,
      speed = 0;
    for (const x of list) {
      if (x.status === 'running' || x.status === 'extracting') running++;
      else if (x.status === 'queued') queued++;
      else if (x.status === 'done') done++;
      else if (x.status === 'error') error++;
      else if (x.status === 'collected') collected++;
      if (x.status === 'running') speed += x.speed;
    }
    return { running, queued, done, error, collected, speed };
  }, [list]);

  const recent = useMemo(
    () =>
      list
        .filter((x) => x.status !== 'collected')
        .sort((a, b) => (a.createdAt > b.createdAt ? -1 : 1))
        .slice(0, 6),
    [list],
  );

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={t('overview.title')} subtitle={t('overview.subtitle')} />

      {/* The one hero of the whole app: this page owns the big figure and the
          curve; every other page opens quietly. */}
      <div className="kl-card grid grid-cols-1 items-center gap-4 overflow-hidden p-5 sm:grid-cols-[auto_minmax(0,1fr)] sm:gap-8">
        <div>
          <div className="kl-eyebrow">{t('overview.totalSpeed')}</div>
          <div className="kl-num mt-1 text-[38px] font-semibold leading-none tracking-tight text-carbon-text">
            {fmtSpeed(counts.speed) || '0 B/s'}
          </div>
          <div className="mt-4">
            <Counters counts={counts} />
          </div>
        </div>
        <SpeedGraph value={counts.speed} height={96} />
      </div>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
        <div className="flex flex-col gap-3">
          <SectionTitle>{t('overview.recent')}</SectionTitle>
          {recent.length === 0 ? (
            <EmptyState icon={<IconDownloads width={26} height={26} />} title={t('overview.noDownloads')} />
          ) : (
            <div className="kl-card divide-y divide-carbon-border/60 p-0">
              {recent.map((x) => (
                <div key={x.id} className="flex items-center gap-4 px-5 py-3">
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-[13.5px] text-carbon-text">{x.name || x.url}</div>
                    <div className="mt-1.5 max-w-xs">
                      <ProgressBar
                        percent={pct(x.loaded, x.size, x.status === 'done')}
                        active={x.status !== 'error'}
                        indeterminate={x.status === 'queued'}
                        tone={x.status === 'done' ? 'ok' : 'accent'}
                      />
                    </div>
                  </div>
                  <span className="kl-num text-xs text-carbon-textSub">{fmtBytes(x.size)}</span>
                  <StatusPill status={x.status} />
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Instances here are a quiet summary, not a stack of raised cards —
            the full dashboard lives on the Instances page. */}
        <div className="flex flex-col gap-3">
          <SectionTitle>{t('overview.instances')}</SectionTitle>
          <div className="kl-card divide-y divide-carbon-border/60 p-0">
            <InstanceRow name={t('instances.thisInstance')} base="/api" />
            {(instances ?? []).map((i) => (
              <InstanceRow
                key={i.name}
                name={i.name}
                base={`/api/instances/${encodeURIComponent(i.name)}`}
                onOpen={() => navigate(`/downloads?instance=${encodeURIComponent(i.name)}`)}
              />
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
