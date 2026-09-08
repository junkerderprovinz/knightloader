import { useMemo, useState } from 'react';
import { fetchVolumeStats, type VolumeBucket, type VolumeStats } from '../lib/api';
import { useT } from '../lib/i18n';
import { IconDownloads } from '../lib/icons';
import { resolverLabel } from '../lib/resolverLabels';
import { useResource } from '../lib/useResource';
import { Card, EmptyState, ErrorCard, LoadingCard, SectionTitle } from './ui';
import { Tabs } from './Tabs';
import { VolumeGraph, type VolumeSeries } from './VolumeGraph';
import { VolumeUsageRow } from './VolumeMeter';

type Range = 'days' | 'months';
type Split = 'total' | 'host' | 'backend';

// How many hosters or backends get a band of their own before the rest are
// folded together. Five is what a legend can be read at a glance; an instance
// that has met two hundred hosts would otherwise draw two hundred bands, of
// which a hundred and ninety are a hairline each.
const TOP_N = 5;

/**
 * The bucket key is rendered from its PARTS and never parsed.
 *
 * The server buckets by its own local calendar and enforces the cap on that
 * same calendar, so its day is the only day the chart and the counter can both
 * mean. new Date('2026-03-14') is UTC midnight in every browser, which prints
 * as the 13th anywhere west of Greenwich - so a curve built that way draws
 * every bar one day early while the cap goes on being charged against the
 * server's day. Date.UTC from the digits, formatted in UTC, keeps the label on
 * the day the server meant.
 *
 * The locale comes off <html lang>, which the language picker stamps at boot
 * and on every change - the same trick lib/format.ts uses to follow the user's
 * choice without pulling the dictionary loader into a formatter.
 */
function bucketLabel(key: string, fmt: Intl.DateTimeFormat): string {
  const parts = key.split('-');
  const year = Number(parts[0]);
  const month = Number(parts[1]);
  // A month bucket has no day in it; the first of the month stands in, and only
  // the month and year are ever printed from it.
  const day = parts.length > 2 ? Number(parts[2]) : 1;
  if (!Number.isFinite(year) || !Number.isFinite(month)) return key;
  return fmt.format(new Date(Date.UTC(year, month - 1, day)));
}

/**
 * buildSeries turns the buckets into the stack.
 *
 * The fold-together band is a REMAINDER (bucket total minus the named bands)
 * and not the sum of the tail. That is deliberate and it is what keeps the
 * three views honest: the server records a host and a backend per row, but not
 * every row has either, so summing the tail would draw a "By hoster" stack
 * shorter than the "Everything" stack beside it and leave somebody comparing
 * two heights that disagree by the amount nobody attributed. As a remainder,
 * all three views are the same height for the same day, always.
 */
function buildSeries(
  buckets: VolumeBucket[],
  split: Split,
  totalLabel: string,
  otherLabel: string,
): VolumeSeries[] {
  if (split === 'total') {
    return [{ id: 'total', label: totalLabel, values: buckets.map((b) => b.bytes) }];
  }

  // Annotated, and the ?? is load-bearing: Go marshals an EMPTY map as JSON
  // null, not as {}, so a gap-filled bucket - or a run of downloads the server
  // recorded no host for - arrives here with nothing to index into.
  const share = (b: VolumeBucket): Record<string, number> =>
    (split === 'host' ? b.byHost : b.byResolver) ?? {};

  const totals = new Map<string, number>();
  for (const b of buckets) {
    for (const [key, bytes] of Object.entries(share(b))) totals.set(key, (totals.get(key) ?? 0) + bytes);
  }

  // Ranked over the WHOLE window, not per bucket: a band that changes which
  // hoster it means from one day to the next is a legend that lies.
  const named = [...totals.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, TOP_N)
    .map(([key]) => key);

  // A hostname is a hostname in every language; a backend id is a product name
  // this app already has a word for, and it has to be the SAME word the task
  // row's own badge uses (lib/resolverLabels.ts).
  const series: VolumeSeries[] = named.map((key) => ({
    id: key,
    label: split === 'backend' ? resolverLabel(key) : key,
    values: buckets.map((b) => share(b)[key] ?? 0),
  }));

  const rest = buckets.map((b) => {
    const claimed = named.reduce((sum, key) => sum + (share(b)[key] ?? 0), 0);
    return Math.max(0, b.bytes - claimed);
  });
  if (rest.some((v) => v > 0)) series.push({ id: '__other', label: otherLabel, values: rest });

  return series;
}

/**
 * VolumeCard is the reading: what this instance has finished downloading, by
 * the day and by the month.
 *
 * A quiet card in the lower grid and never a second hero. The Overview page
 * already owns one, the live speed curve at the top, and GlimStone allows one
 * per page - this is a record of what has happened, which is a thing you go and
 * look at rather than a thing you watch.
 *
 * Every caveat about what these figures cannot say lives in the title's own
 * bubble, computed from the answer rather than written out flat: what the
 * history is missing, how many downloads finished without a size, and whose
 * calendar the buckets belong to are all facts about THIS instance's data.
 * Nothing loose under the plot.
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

  // Held rather than derived inline, so the two memos below key off one stable
  // array instead of a fresh [] on every render while the fetch is still out.
  const buckets = useMemo(() => (range === 'days' ? data?.days : data?.months) ?? [], [data, range]);
  const labels = useMemo(() => buckets.map((b) => bucketLabel(b.key, fmt)), [buckets, fmt]);
  const series = useMemo(
    () => buildSeries(buckets, split, t('volume.split.total'), t('volume.other')),
    [buckets, split, t],
  );

  const moved = buckets.reduce((sum, b) => sum + b.bytes, 0);
  const unsized = buckets.reduce((sum, b) => sum + b.unsized, 0);

  // The one place the hint is assembled, so the sentences that only sometimes
  // apply cannot end up as a paragraph that is half wrong. Same shape as
  // StatusStrip's own computed tip.
  //
  // Nothing at all until the fetch has answered, rather than the base sentence
  // with a hole where the zone goes: it ends "the calendar is the server's, in
  // {zone}", and "in ." is worse than a bubble that arrives a moment later.
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

      {/* Two exclusive, closely-related sets, which is what the well variant is
          for. w-fit on each track, so the shared surface stops at the last
          segment instead of running the width of the card. */}
      <div className="flex flex-wrap items-center gap-3">
        <Tabs
          variant="well"
          size="sm"
          className="w-fit"
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
          className="w-fit"
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

      {/* The allowance beside the record it is charged against. It is the same
          figure the shell bar carries, and the shell bar is hidden on this page
          (app/Layout.tsx renders it only for the downloads section), so without
          this the number would be invisible exactly where somebody is reading
          about their volume. */}
      <VolumeUsageRow />

      {loading && <LoadingCard nested label={t('common.loading')} />}
      {failed && <ErrorCard nested message={t('common.loadFailed')} retry={reload} retryLabel={t('common.retry')} />}
      {data &&
        (moved === 0 ? (
          // An empty window is not a broken chart, and it is not "nothing was
          // downloaded, ever" either: the history has a cap and a delete button
          // of its own, which the title's bubble says.
          <EmptyState nested icon={<IconDownloads width={26} height={26} />} title={t('volume.empty')} />
        ) : (
          <VolumeGraph labels={labels} series={series} label={t('volume.title')} />
        ))}
    </Card>
  );
}
