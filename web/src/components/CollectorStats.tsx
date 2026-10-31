// The collector's totals for what is staged: packages, links, bytes, hosts and
// check results, scoped like the shell strip to total, visible or selected.
// Counters.tsx answers the different question of what the queue owes. Figures
// stay at body size, below the paste box that heads the page.
import { useMemo, useState, type ReactNode } from 'react';
import type { Task } from '../lib/api';
import { useT } from '../lib/i18n';
import { fmtTotal } from '../lib/format';
import { hostOf } from './columns';
import { Tabs } from './Tabs';
import { Card, SectionTitle } from './ui';

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

// The label goes under its figure when the column is too narrow for both, and
// breaks inside a word only when even that does not fit, so it never runs
// into the next column.
function Item({ label, value, tone = 'text-carbon-text' }: { label: string; value: string | number; tone?: string }) {
  return (
    <div className="flex min-w-0 flex-wrap items-baseline gap-x-1.5 gap-y-0.5">
      <span className={`glim-num whitespace-nowrap text-sm font-semibold leading-none ${tone}`}>{value}</span>
      <span className="min-w-0 text-meta text-carbon-textMuted [overflow-wrap:anywhere]">{label}</span>
    </div>
  );
}

/**
 * CollectorStats is the card of figures beside the collector's paste box. The
 * page passes the three arrays it already computed rather than this reading
 * them back from lib/listview.ts a render later. `children` is what narrows the
 * list or picks from it, under the figures.
 */
export function CollectorStats({
  all,
  visible,
  selected,
  children,
}: {
  all: Task[];
  visible: Task[];
  selected: Task[];
  children?: ReactNode;
}) {
  const { t } = useT();
  const [scope, setScope] = useState<StatsScope>('total');

  const scoped = scope === 'total' ? all : scope === 'visible' ? visible : selected;
  const f = useMemo(() => weigh(scoped), [scoped]);

  return (
    // h-full on the Card too: the row's items-stretch only reaches the wrapper.
    <div role="group" aria-label={t('collector.stats.label')} className="h-full">
      {/* Fitted to its figures beside the other cards, and full width once
          the row stacks. */}
      <Card hue={1} className="flex h-full min-w-[13rem] flex-col gap-3 lg:w-fit">
        <SectionTitle>{t('collector.stats.label')}</SectionTitle>
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
        {/* What is staged in the first column and what the checks found in the
            second, so the card is no taller than the form beside it. Stacked
            under the form it has the width for four columns. */}
        <div className="grid grid-flow-col grid-rows-4 auto-cols-fr gap-x-6 gap-y-2 sm:max-lg:grid-rows-2">
          <Item label={t('collector.stats.packages')} value={f.packages} />
          <Item label={t('collector.stats.links')} value={f.links} />
          <Item label={t('collector.stats.totalSize')} value={fmtTotal(f.bytes)} />
          <Item label={t('collector.stats.hosts')} value={f.hosts} />
          <Item label={t('filter.online')} value={f.online} />
          <Item label={t('filter.offline')} value={f.offline} tone={f.offline > 0 ? 'text-statusFail' : 'text-carbon-textMuted'} />
          <Item label={t('filter.uncheckable')} value={f.uncheckable} tone="text-carbon-textMuted" />
          <Item label={t('filter.unchecked')} value={f.unchecked} tone="text-carbon-textMuted" />
        </div>
        {children}
      </Card>
    </div>
  );
}
