// The download list's totals: how much of what it holds is in already, and how
// many links it holds, beside the collector's own totals card.
import { useMemo, type ReactNode } from 'react';
import type { Task } from '../lib/api';
import { fmtTotal } from '../lib/format';
import { useT } from '../lib/i18n';
import { Card, SectionTitle } from './ui';

/**
 * DownloadStats is the card of totals at the top of Downloads. `children` is
 * what narrows the list or picks from it, under the totals.
 */
export function DownloadStats({ all, hue, children }: { all: Task[]; hue: number; children?: ReactNode }) {
  const { t } = useT();
  const { loaded, total } = useMemo(() => {
    let loaded = 0;
    let total = 0;
    for (const x of all) {
      if (x.size > 0) total += x.size;
      loaded += x.status === 'done' && x.size > 0 ? x.size : Math.max(x.loaded, 0);
    }
    return { loaded, total };
  }, [all]);

  return (
    <div role="group" aria-label={t('downloads.stats.label')}>
      <Card hue={hue} className="flex flex-col gap-3">
        <SectionTitle>{t('downloads.stats.label')}</SectionTitle>
        <p className="flex flex-wrap items-baseline gap-x-3 gap-y-1 text-sm text-carbon-text">
          <span className="glim-num font-semibold">
            {t('downloads.stats.loaded', { loaded: fmtTotal(loaded), total: fmtTotal(total) })}
          </span>
          <span className="glim-num text-carbon-textSub">{t('downloads.stats.links', { n: all.length })}</span>
        </p>
        {children}
      </Card>
    </div>
  );
}
