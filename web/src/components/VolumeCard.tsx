import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import { fetchVolumeCurve, type VolumeBucket, type VolumeCurve } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { IconDownload } from '../lib/icons';
import { resolverLabel } from '../lib/resolverLabels';
import { useResource } from '../lib/useResource';
import { useShake } from '../lib/useShake';
import { useUIState } from '../lib/uistate';
import {
  DEFAULT_SPAN,
  SPAN_DAILY_UP_TO,
  SPAN_MAX,
  SPAN_STEPS,
  isStep,
  readCount,
  readSpan,
  storedSpan,
  type SpanUnit,
} from '../lib/volumeSpan';
import { Dropdown } from './Dropdown';
import { Card, EmptyState, ErrorCard, InfoBubble, LoadingCard, SectionTitle, TextInput, useFocusWheel } from './ui';
import { Tabs } from './Tabs';
import { VolumeGraph, type VolumeSeries } from './VolumeGraph';
import { VolumeUsageRow } from './VolumeMeter';

type Split = 'total' | 'host' | 'backend';

const SPLITS: Split[] = ['total', 'host', 'backend'];

const SPLIT_LABEL: Record<Split, TranslationKey> = {
  total: 'volume.split.total',
  host: 'volume.split.host',
  backend: 'volume.split.backend',
};

const STEP_LABEL: Record<(typeof SPAN_STEPS)[number], TranslationKey> = {
  '7d': 'volume.range.7d',
  '30d': 'volume.range.30d',
  '90d': 'volume.range.90d',
  '12m': 'volume.range.12m',
  all: 'volume.range.all',
};

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
 * VolumeCard charts what the instance has finished downloading over a span the
 * viewer picks: one of the steps, or any count of days or months typed under
 * them. Its caveats go in the title's bubble, built from the data.
 */
export function VolumeCard({ hue }: { hue?: number }) {
  const { t } = useT();
  const [stored, setStored] = useUIState<string>('overview.volumeSpan', DEFAULT_SPAN);
  const [storedSplit, setSplit] = useUIState<Split>('overview.volumeSplit', 'total');
  const span = storedSpan(stored);
  const split: Split = SPLITS.includes(storedSplit) ? storedSplit : 'total';

  const load = useCallback(() => fetchVolumeCurve(span), [span]);
  const { data, failed, loading, reload } = useResource<VolumeCurve>(load, span);

  // The free value under the steps, as GlimStone lays one out. `draft` is the
  // text while it is typed; otherwise the field shows the span in force where
  // that is not a step.
  const custom = isStep(span) ? null : readSpan(span);
  const [draft, setDraft] = useState<string | null>(null);
  const [unit, setUnit] = useState<SpanUnit>(custom?.unit ?? 'd');
  const [refused, setRefused] = useState(0);
  const shake = useShake<HTMLDivElement>(refused);
  const field = useRef<HTMLInputElement>(null);
  const text = draft ?? (custom ? String(custom.n) : '');
  const count = readCount(text, unit);

  // The remembered span arrives after first paint and brings its unit along.
  const customUnit = custom?.unit;
  useEffect(() => {
    if (customUnit) setUnit(customUnit);
  }, [customUnit]);

  // Where emptying the field goes back to.
  const lastStep = useRef(DEFAULT_SPAN);
  useEffect(() => {
    if (isStep(span)) lastStep.current = span;
  }, [span]);

  function type(next: string) {
    setDraft(next);
    if (next.trim() === '') {
      setStored(lastStep.current);
      return;
    }
    const n = readCount(next, unit);
    if (n !== null) setStored(`${n}${unit}`);
  }

  // Leaving shows the count as stored. A count that is one of the steps hands
  // the choice back to it, since the field then shows nothing.
  function leave() {
    if (draft === null) return;
    if (draft.trim() !== '' && readCount(draft, unit) === null) {
      setRefused((r) => r + 1);
      return;
    }
    setDraft(null);
  }

  function step(up: boolean, far: boolean) {
    const by = (far ? 10 : 1) * (up ? 1 : -1);
    const next = Math.min(SPAN_MAX[unit], Math.max(1, (count ?? 0) + by));
    setDraft(String(next));
    setStored(`${next}${unit}`);
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter') leave();
    if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return;
    e.preventDefault();
    step(e.key === 'ArrowUp', e.shiftKey);
  }

  useFocusWheel(field, (up) => step(up, false));

  // A count too large for the other unit comes down to that unit's ceiling.
  function pickUnit(next: SpanUnit) {
    setUnit(next);
    const n = readCount(text, next) ?? (count === null ? null : Math.min(count, SPAN_MAX[next]));
    if (n === null) return;
    setDraft(null);
    setStored(`${n}${next}`);
  }

  const locale = typeof document === 'undefined' ? '' : document.documentElement.lang;
  const byDay = data?.unit !== 'month';
  const fmt = useMemo(
    () =>
      new Intl.DateTimeFormat(
        locale || undefined,
        byDay
          ? { day: '2-digit', month: '2-digit', timeZone: 'UTC' }
          : { month: 'short', year: '2-digit', timeZone: 'UTC' },
      ),
    [locale, byDay],
  );

  // Memoised so the memos below do not see a fresh [] on every render.
  const buckets = useMemo(() => data?.buckets ?? [], [data]);
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

      {/* The shell bar that carries this figure is hidden on this page. */}
      <VolumeUsageRow />

      {loading && <LoadingCard nested label={t('common.loading')} />}
      {failed && <ErrorCard nested message={t('common.loadFailed')} retry={reload} retryLabel={t('common.retry')} />}
      {data &&
        !failed &&
        (moved === 0 ? (
          <EmptyState nested icon={<IconDownload width={26} height={26} />} title={t('volume.empty')} />
        ) : (
          <VolumeGraph labels={labels} series={series} label={t('volume.title')} />
        ))}

      {/* Under the figures they choose. The free value stands at the end of
          the row under the steps. */}
      <div className="flex flex-col gap-3">
        <div className="flex flex-col items-end gap-2">
          <div className="self-stretch">
            <Tabs
              variant="well"
              size="sm"
              label={t('volume.rangeLabel')}
              // A valid count is the span in force, so no step shows as chosen beside it.
              active={count !== null ? null : isStep(span) ? span : null}
              onSelect={(id) => {
                setDraft(null);
                setStored(id);
              }}
              items={SPAN_STEPS.map((id) => ({ id, label: t(STEP_LABEL[id]) }))}
            />
          </div>
          <div className="flex items-center gap-2">
            <InfoBubble tip={t('volume.customHint', { days: SPAN_MAX.d, months: SPAN_MAX.m, daily: SPAN_DAILY_UP_TO })} />
            <div ref={shake} className="w-20">
              <TextInput
                ref={field}
                inputMode="numeric"
                autoComplete="off"
                value={text}
                placeholder={t('volume.custom')}
                aria-label={t('volume.customLabel')}
                aria-invalid={text.trim() !== '' && count === null}
                onChange={(e) => type(e.target.value)}
                onBlur={leave}
                onKeyDown={onKeyDown}
                className={`glim-num border ${count !== null ? 'border-accent' : 'border-transparent'}`}
              />
            </div>
            <Dropdown<SpanUnit>
              width="widest"
              label={t('volume.unit')}
              value={unit}
              onChange={pickUnit}
              options={[
                { value: 'd', label: t('volume.unit.days') },
                { value: 'm', label: t('volume.unit.months') },
              ]}
            />
          </div>
        </div>
        <Tabs
          variant="well"
          size="sm"
          label={t('volume.splitLabel')}
          active={split}
          onSelect={(id) => setSplit(id as Split)}
          items={SPLITS.map((id) => ({ id, label: t(SPLIT_LABEL[id]) }))}
        />
      </div>
    </Card>
  );
}
