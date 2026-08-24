import { useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { type Instance, fetchInstances } from '../lib/api';
import { useTasks } from '../lib/useTasks';
import { useResource } from '../lib/useResource';
import { fmtBytes, fmtSpeed, pct } from '../lib/format';
import { useT } from '../lib/i18n';
import { Card, PageHeader, SectionTitle, EmptyState } from '../components/ui';
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
      // Not what the link filter is holding: those are counted nowhere, because
      // they are in no list and nothing is going to happen to them.
      else if (x.status === 'collected' && !x.skipped) collected++;
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
      {/* Subtitle removed (jdp, 2026-08-24: "text entfernen: Alles auf
          einen Blick.") - the title alone already says what this page is. */}
      <PageHeader title={t('overview.title')} />

      {/* The one hero of the whole app: this page owns the big figure and the
          curve; every other page opens quietly. */}
      <div className="glim-card grid grid-cols-1 items-center gap-4 overflow-hidden p-5 sm:grid-cols-[auto_minmax(0,1fr)] sm:gap-8">
        <div>
          <div className="glim-eyebrow">{t('overview.totalSpeed')}</div>
          <div className="glim-num mt-1 text-[38px] font-semibold leading-none tracking-tight text-carbon-text">
            {fmtSpeed(counts.speed) || '0 B/s'}
          </div>
          <div className="mt-4">
            <Counters counts={counts} />
          </div>
        </div>
        <SpeedGraph value={counts.speed} height={96} />
      </div>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
        {/* Card, not a bare div (jdp, 2026-08-24, same bug found and fixed on
            Accounts.tsx: SectionTitle's badge is `absolute -top-[11px]` and
            requires its ancestor to be `.glim-card` - a plain flex wrapper
            with no visible box left the badge anchored to nothing, floating
            above the real (nested) card instead of notching over it). */}
        <Card className="flex flex-col gap-3">
          <SectionTitle>{t('overview.recent')}</SectionTitle>
          {recent.length === 0 ? (
            <EmptyState nested icon={<IconDownloads width={26} height={26} />} title={t('overview.noDownloads')} />
          ) : (
            <div className="glim-well divide-y divide-carbon-border/60 p-0">
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
                  <span className="glim-num text-xs text-carbon-textSub">{fmtBytes(x.size)}</span>
                  <StatusPill status={x.status} />
                </div>
              ))}
            </div>
          )}
        </Card>

        {/* Instances here are a quiet summary, not a stack of raised cards —
            the full dashboard lives on the Instances page. Card, not a bare
            div, for the same SectionTitle-anchor reason as the recent-
            downloads card above. */}
        <Card className="flex flex-col gap-3">
          <SectionTitle>{t('overview.instances')}</SectionTitle>
          <div className="glim-well divide-y divide-carbon-border/60 p-0">
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
        </Card>
      </div>
    </div>
  );
}
