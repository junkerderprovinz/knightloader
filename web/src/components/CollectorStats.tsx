// The collector's own totals: what is currently staged, not what the queue
// owes. Wave 4's OverviewStrip (components/Counters.tsx) already answers a
// different question — bytes fetched against bytes owed, scoped to what still
// needs to run — and it already rides in the shell bar on every page
// (app/Layout.tsx's ShellBar mounts QuickSettings.tsx's ShellStrip, which
// renders it); a second "totals" strip asking the same question here would
// just be two headers disagreeing about what a collector even measures. This
// one is the strip the census actually found missing
// (docs/jd-feature-census.md, section 6, "Überblick (Downloadübersicht
// anzeigen)"): packages, links, bytes, hosts, and what a check said about each
// — scoped by the same Total/Visible/Selected switch the shell strip offers,
// because "how much of this did I actually narrow down" is the same question
// on both pages even though the totals underneath it are not.
//
// Every figure stays at Counters.tsx's supporting-detail weight (15px value,
// 11px label), never Downloads.tsx's page-hero 20px: the paste box above this
// strip is the Collector page's one hero (docs/design-language.md rule 2, "one
// hero per page"), and nothing below it may compete with that for weight —
// which is also why OverviewStrip itself, riding in the shell bar on every
// page including this one, keeps its own bytes figure at 15px rather than 20.
import { useCallback, useMemo, useState } from 'react';
import type { Task } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { fmtBytes } from '../lib/format';
import { hostOf } from './columns';
import { Tabs } from './Tabs';

// Same PENDING-table arrangement as CollectorFacets.tsx (see that file's own
// comment for the precedent) — locale files are one writer's lane, 8F, and it
// runs after 8A–8D/8G land.
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
 * weigh adds up one set of staged links.
 *
 * Bytes count every row with a known size, unconditionally — unlike the shell
 * strip's own weigh (Counters.tsx), nothing here is excluded for being
 * disabled or already running: every row on this page is staged and
 * unstarted, so there is no "owed" subset to single out the way the queue-wide
 * strip does, and excluding disabled links here would just make the total
 * quietly disagree with what the list above it is showing.
 *
 * online/offline/uncheckable/unchecked mirror Task.online exactly, the same
 * four states ListToolbar's own COLLECTOR_FILTERS chips already expose right
 * below this strip — not the three JD's census still describes, because this
 * app tells "never checked" and "checked, host would not say" apart on
 * purpose (docs/build-plan.md section 9, package 12).
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
      <span className={`glim-num text-[15px] font-semibold leading-none ${tone}`}>{value}</span>
      <span className="text-[11px] text-carbon-textMuted">{label}</span>
    </div>
  );
}

/**
 * CollectorStats is the row of figures above the collector's list.
 *
 * `all`, `visible` and `selected` are the same three arrays the page already
 * holds for its own list — the collected set, the post-search-and-facets set,
 * and the current selection resolved to tasks — handed straight in rather than
 * read back off lib/listview.ts the way the shell strip does. That store is a
 * round trip this component does not need: the page rendering this strip is
 * the very page that just computed all three, in the same render, and reading
 * them back through a store it also happens to publish to would only add a
 * tick of lag between a keystroke in the search box and this row noticing it.
 */
export function CollectorStats({ all, visible, selected }: { all: Task[]; visible: Task[]; selected: Task[] }) {
  const { t } = useT();
  const cx = useCx();
  const [scope, setScope] = useState<StatsScope>('total');

  const scoped = scope === 'total' ? all : scope === 'visible' ? visible : selected;
  const f = useMemo(() => weigh(scoped), [scoped]);

  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-2" role="group" aria-label={cx('collector.stats.label')}>
      <Item label={cx('collector.stats.packages')} value={f.packages} />
      <Item label={cx('collector.stats.links')} value={f.links} />
      <Item label={cx('collector.stats.totalSize')} value={f.bytes > 0 ? fmtBytes(f.bytes) : '0 B'} />
      <Item label={cx('collector.stats.hosts')} value={f.hosts} />
      <Item label={t('filter.online')} value={f.online} />
      <Item label={t('filter.offline')} value={f.offline} tone={f.offline > 0 ? 'text-statusFail' : 'text-carbon-textMuted'} />
      <Item label={t('filter.uncheckable')} value={f.uncheckable} tone="text-carbon-textMuted" />
      <Item label={t('filter.unchecked')} value={f.unchecked} tone="text-carbon-textMuted" />

      <span className="flex-1" />
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
    </div>
  );
}
