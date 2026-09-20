// The collector's totals for what is staged: packages, links, bytes, hosts and
// check results, scoped like the shell strip to total, visible or selected.
// Counters.tsx answers the different question of what the queue owes. Figures
// stay at body size, below the paste box that heads the page.
import { useCallback, useMemo, useState } from 'react';
import type { Task } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { fmtBytes } from '../lib/format';
import { hostOf } from './columns';
import { Tabs } from './Tabs';
import { Card, SectionTitle } from './ui';

// English fallbacks for keys not yet in the catalogues, as in CollectorFacets.tsx.
const PENDING = {
  'collector.stats.label': 'Collector totals',
  'collector.stats.packages': 'Packages',
  'collector.stats.links': 'Links',
  'collector.stats.totalSize': 'Total size',
  'collector.stats.hosts': 'Hosts',
} as const;

type PendingKey = keyof typeof PENDING;

function useCx() {
  const { t } = useT();
  return useCallback(
    (key: PendingKey, vars?: Record<string, string | number>) => {
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      let s: string = translated ?? PENDING[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [t],
  );
}

export type StatsScope = 'total' | 'visible' | 'selected';

interface Figures {
  packages: number;
  links: number;
  bytes: number;
  hosts: number;
  online: number;
  offline: number;
  uncheckable: number;
  unchecked: number;
}

/**
 * weigh adds up one set of staged links. Unlike Counters.tsx it counts every
 * row with a known size, since nothing here is disabled or running yet. The
 * four check states mirror Task.online and the collector's filter chips.
 */
function weigh(rows: Task[]): Figures {
  const packages = new Set<string>();
  const hosts = new Set<string>();
  const f: Figures = { packages: 0, links: rows.length, bytes: 0, hosts: 0, online: 0, offline: 0, uncheckable: 0, unchecked: 0 };
  for (const x of rows) {
    packages.add(x.package || '');
    const h = hostOf(x);
    if (h) hosts.add(h);
    if (x.size > 0) f.bytes += x.size;
    if (x.online === 'online') f.online++;
    else if (x.online === 'offline') f.offline++;
    else if (x.online === 'uncheckable') f.uncheckable++;
    else f.unchecked++;
  }
  f.packages = packages.size;
  f.hosts = hosts.size;
  return f;
}

function Item({ label, value, tone = 'text-carbon-text' }: { label: string; value: string | number; tone?: string }) {
  return (
    <div className="flex items-baseline gap-1.5">
      <span className={`glim-num text-sm font-semibold leading-none ${tone}`}>{value}</span>
      <span className="text-[11px] text-carbon-textMuted">{label}</span>
    </div>
  );
}

/**
 * CollectorStats is the card of figures beside the collector's paste box. The
 * page passes the three arrays it already computed rather than this reading
 * them back from lib/listview.ts a render later.
 */
export function CollectorStats({ all, visible, selected }: { all: Task[]; visible: Task[]; selected: Task[] }) {
  const { t } = useT();
  const cx = useCx();
  const [scope, setScope] = useState<StatsScope>('total');

  const scoped = scope === 'total' ? all : scope === 'visible' ? visible : selected;
  const f = useMemo(() => weigh(scoped), [scoped]);

  return (
    // h-full on the Card too: the row's items-stretch only reaches the wrapper.
    <div role="group" aria-label={cx('collector.stats.label')} className="h-full">
      <Card hue={1} className="flex h-full w-fit min-w-[13rem] flex-col gap-3">
        <SectionTitle>{cx('collector.stats.label')}</SectionTitle>
        <Tabs
          select="one"
          size="sm"
          label={t('strip.scope')}
          active={scope}
          onSelect={(id) => setScope(id as StatsScope)}
          items={[
            { id: 'total', label: t('strip.total'), badge: all.length },
            { id: 'visible', label: t('strip.visible'), badge: visible.length },
            { id: 'selected', label: t('strip.selected'), badge: selected.length },
          ]}
        />
        <div className="flex flex-col gap-2">
          <Item label={cx('collector.stats.packages')} value={f.packages} />
          <Item label={cx('collector.stats.links')} value={f.links} />
          <Item label={cx('collector.stats.totalSize')} value={f.bytes > 0 ? fmtBytes(f.bytes) : '0 B'} />
          <Item label={cx('collector.stats.hosts')} value={f.hosts} />
          <Item label={t('filter.online')} value={f.online} />
          <Item label={t('filter.offline')} value={f.offline} tone={f.offline > 0 ? 'text-statusFail' : 'text-carbon-textMuted'} />
          <Item label={t('filter.uncheckable')} value={f.uncheckable} tone="text-carbon-textMuted" />
          <Item label={t('filter.unchecked')} value={f.unchecked} tone="text-carbon-textMuted" />
        </div>
      </Card>
    </div>
  );
}
