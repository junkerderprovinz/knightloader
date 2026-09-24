import { useMemo, useState } from 'react';
import { fetchVolumeStats, type VolumeBucket, type VolumeStats } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { IconDownloads } from '../lib/icons';
import { resolverLabel } from '../lib/resolverLabels';
import { useResource } from '../lib/useResource';
import { Card, EmptyState, ErrorCard, LoadingCard, SectionTitle } from './ui';
import { Tabs } from './Tabs';
import { VolumeGraph, type VolumeSeries } from './VolumeGraph';
import { VolumeUsageRow } from './VolumeMeter';

type Range = 'days' | 'months';
type Split = 'total' | 'host' | 'backend';

// Hosters or backends beyond the top five share one band, so the legend stays readable.
const TOP_N = 5;

/**
 * bucketLabel formats a bucket key from its digits, in UTC. The server buckets
 * by its own calendar, and parsing "2026-03-14" as a Date would print the 13th
 * anywhere west of Greenwich.
 */
function bucketLabel(key: string, fmt: Intl.DateTimeFormat): string {
  const parts = key.split('-');
  const year = Number(parts[0]);
  const month = Number(parts[1]);
  // A month bucket has no day; only month and year are printed from it.
  const day = parts.length > 2 ? Number(parts[2]) : 1;
  if (!Number.isFinite(year) || !Number.isFinite(month)) return key;
  return fmt.format(new Date(Date.UTC(year, month - 1, day)));
}

/**
 * buildSeries turns the buckets into the stack. The "other" band is the bucket
 * total minus the named bands, not the sum of the tail, because some rows have
 * no host or backend and every view must stack to the same height.
 */
function buildSeries(buckets: VolumeBucket[], split: Split, t: (key: TranslationKey) => string): VolumeSeries[] {
  if (split === 'total') {
    return [{ id: 'total', label: t('volume.split.total'), values: buckets.map((b) => b.bytes) }];
  }

  // Go marshals an empty map as null.
  const share = (b: VolumeBucket): Record<string, number> =>
    (split === 'host' ? b.byHost : b.byResolver) ?? {};

  const totals = new Map<string, number>();
  for (const b of buckets) {
    for (const [key, bytes] of Object.entries(share(b))) totals.set(key, (totals.get(key) ?? 0) + bytes);
  }

  // Ranked over the whole window, so a band means the same hoster every day.
  const named = [...totals.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, TOP_N)
    .map(([key]) => key);

  const series: VolumeSeries[] = named.map((key) => ({
    id: key,
    label: split === 'backend' ? resolverLabel(key, t) : key,
    values: buckets.map((b) => share(b)[key] ?? 0),
  }));

  const rest = buckets.map((b) => {
    const claimed = named.reduce((sum, key) => sum + (share(b)[key] ?? 0), 0);
    return Math.max(0, b.bytes - claimed);
  });
  if (rest.some((v) => v > 0)) series.push({ id: '__other', label: t('volume.other'), values: rest });

  return series;
}

/**
 * VolumeCard charts what the instance has finished downloading, per day or per
 * month. Its caveats go in the title's bubble, built from the data.
 */
export function VolumeCard({ hue }: { hue?: number }) {
  const { t } = useT();
  const { data, failed, loading, reload } = useResource<VolumeStats>(fetchVolumeStats);
  const [range, setRange] = useState<Range>('days');
  const [split, setSplit] = useState<Split>('total');

  const locale = typeof document === 'undefined' ? '' : document.documentElement.lang;
  const fmt = useMemo(
    () =>
      new Intl.DateTimeFormat(
        locale || undefined,
        range === 'days'
          ? { day: '2-digit', month: '2-digit', timeZone: 'UTC' }
          : { month: 'short', year: '2-digit', timeZone: 'UTC' },
      ),
    [locale, range],
  );

  // Memoised so the memos below do not see a fresh [] on every render.
  const buckets = useMemo(() => (range === 'days' ? data?.days : data?.months) ?? [], [data, range]);
  const labels = useMemo(() => buckets.map((b) => bucketLabel(b.key, fmt)), [buckets, fmt]);
  const series = useMemo(
    () => buildSeries(buckets, split, t),
    [buckets, split, t],
  );

  const moved = buckets.reduce((sum, b) => sum + b.bytes, 0);
  const unsized = buckets.reduce((sum, b) => sum + b.unsized, 0);

  // Empty until the fetch answers, since the base sentence needs the time zone.
  const hint = !data
    ? ''
    : [
        t('volume.titleHint', { zone: data.timeZone }),
        unsized > 0 ? t('volume.unsizedHint', { n: unsized }) : '',
        data.trimmed ? t('volume.trimmedHint') : '',
      ]
        .filter(Boolean)
        .join(' ');

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={hint}>{t('volume.title')}</SectionTitle>

      <div className="flex flex-wrap items-center gap-3">
        <Tabs
          variant="well"
          size="sm"
          label={t('volume.rangeLabel')}
          active={range}
          onSelect={(id) => setRange(id as Range)}
          items={[
            { id: 'days', label: t('volume.range.days') },
            { id: 'months', label: t('volume.range.months') },
          ]}
        />
        <Tabs
          variant="well"
          size="sm"
          label={t('volume.splitLabel')}
          active={split}
          onSelect={(id) => setSplit(id as Split)}
          items={[
            { id: 'total', label: t('volume.split.total') },
            { id: 'host', label: t('volume.split.host') },
            { id: 'backend', label: t('volume.split.backend') },
          ]}
        />
      </div>

      {/* The shell bar that carries this figure is hidden on this page. */}
      <VolumeUsageRow />

      {loading && <LoadingCard nested label={t('common.loading')} />}
      {failed && <ErrorCard nested message={t('common.loadFailed')} retry={reload} retryLabel={t('common.retry')} />}
      {data &&
        (moved === 0 ? (
          <EmptyState nested icon={<IconDownloads width={26} height={26} />} title={t('volume.empty')} />
        ) : (
          <VolumeGraph labels={labels} series={series} label={t('volume.title')} />
        ))}
    </Card>
  );
}
