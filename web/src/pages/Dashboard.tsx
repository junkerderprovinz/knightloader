import { useMemo, type CSSProperties } from 'react';
import { useNavigate } from 'react-router-dom';
import { type Instance, type Settings, fetchInstances, fetchSettings } from '../lib/api';
import { hueVars, rainbowAt } from '../lib/appearance';
import { useRainbow } from '../lib/useRainbow';
import { useTasks } from '../lib/useTasks';
import { useResource } from '../lib/useResource';
import { fmtBytes, fmtSpeed, pct } from '../lib/format';
import { useT } from '../lib/i18n';
import { Card, PageHeader, SectionTitle, EmptyState } from '../components/ui';
import { SpeedGraph } from '../components/SpeedGraph';
import { VolumeCard } from '../components/VolumeCard';
import { Counters } from '../components/Counters';
import { DiskSpaceTile } from '../components/DiskSpaceTile';
import { ProgressBar } from '../components/ProgressBar';
import { StatusPill } from '../components/StatusPill';
import { InstanceRow } from '../components/InstanceCard';
import { IconDownloads } from '../lib/icons';

export function Dashboard() {
  const { t } = useT();
  useRainbow();
  const tasks = useTasks('');
  const { data: instances } = useResource<Instance[]>(fetchInstances);
  // Carries the configured instance name and the speed limit.
  const { data: settings } = useResource<Settings>(fetchSettings);
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
      // Links the filter holds back and rows a hoster preset set aside are in
      // no list and not counted.
      else if (x.status === 'collected' && !x.skipped && !x.variantOff) collected++;
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
    <div className="flex flex-col gap-10">
      <PageHeader title={t('overview.title')} />

      {/* A hand-rolled card, so the hue class and properties are set here
          together, or `.glim-hue` resolves --accent to nothing. */}
      <div
        className="glim-card glim-hue grid grid-cols-1 items-center gap-4 overflow-hidden p-5 sm:grid-cols-[auto_minmax(0,1fr)] sm:gap-8"
        style={hueVars(rainbowAt(0)) as CSSProperties}
      >
        <div>
          <div className="glim-eyebrow">{t('overview.totalSpeed')}</div>
          {/* The figure keeps its own order in a right-to-left page, where a
              number followed by a Latin unit would otherwise read unit first;
              the inner span isolates it and the line still starts at the start
              edge. */}
          <div className="glim-num mt-1 text-[38px] font-semibold leading-none tracking-tight text-carbon-text">
            <span dir="ltr">{fmtSpeed(counts.speed) || '0 B/s'}</span>
          </div>
          <div className="mt-4">
            <Counters counts={counts} />
          </div>
        </div>
        {/* The limit feeds the 1337 easter egg (docs/easter-eggs.md). */}
        <SpeedGraph value={counts.speed} height={96} limit={settings?.speedLimit ?? 0} />
      </div>

      <div className="grid grid-cols-1 gap-10 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
        {/* SectionTitle's badge is positioned against a `.glim-card`. */}
        <Card hue={1} className="flex flex-col gap-3">
          <SectionTitle>{t('overview.recent')}</SectionTitle>
          {recent.length === 0 ? (
            <EmptyState nested icon={<IconDownloads width={26} height={26} />} title={t('overview.noDownloads')} />
          ) : (
            <div className="glim-well divide-y divide-carbon-border/60 p-0">
              {recent.map((x) => (
                <div key={x.id} className="flex items-center gap-4 px-5 py-3">
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm text-carbon-text">{x.name || x.url}</div>
                    <div className="mt-1.5 max-w-xs">
                      <ProgressBar
                        percent={pct(x.loaded, x.size, x.status === 'done')}
                        active={x.status !== 'error'}
                        indeterminate={x.status === 'running' && x.size <= 0}
                        moving={x.status === 'running' || x.status === 'extracting'}
                        tone={x.status === 'done' ? 'ok' : 'accent'}
                      />
                    </div>
                  </div>
                  <span className="glim-num text-xs text-carbon-textSub" dir="ltr">
                    {fmtBytes(x.size)}
                  </span>
                  <StatusPill status={x.status} />
                </div>
              ))}
            </div>
          )}
        </Card>

        <Card hue={2} className="flex flex-col gap-3">
          <SectionTitle>{t('overview.instances')}</SectionTitle>
          <div className="glim-well divide-y divide-carbon-border/60 p-0">
            <InstanceRow name={settings?.instanceName || t('instances.thisInstance')} base="/api" />
            {(instances ?? []).map((i) => (
              <InstanceRow
                key={i.name}
                name={i.displayName ?? i.name}
                base={`/api/instances/${encodeURIComponent(i.name)}`}
                onOpen={() => navigate(`/downloads?instance=${encodeURIComponent(i.name)}`)}
              />
            ))}
          </div>
        </Card>

        <div className="lg:col-span-2">
          <VolumeCard hue={3} />
        </div>
      </div>

      {/* Full width, since it grows by one row per target folder. */}
      <DiskSpaceTile settings={settings} />
    </div>
  );
}
