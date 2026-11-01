// The download list's totals: how much of what it holds is in already, how
// many links it holds and how they stand, beside the collector's own totals
// card.
import { useMemo, type ReactNode } from 'react';
import type { Task } from '../lib/api';
import { fmtEta, fmtSpeed, fmtTotal } from '../lib/format';
import { useT } from '../lib/i18n';
import { useInstanceScope } from '../lib/instance';
import { uploadOf } from '../lib/speedHistory';
import { useMeterAverage } from './SpeedGraph';
import { Card, SectionTitle } from './ui';

/**
 * DownloadStats is the card of totals beside the speed graph. `children` is
 * what narrows the list or picks from it, under the totals.
 */
export function DownloadStats({ all, hue, children }: { all: Task[]; hue: number; children?: ReactNode }) {
  const { t } = useT();
  const { instance } = useInstanceScope();
  const sums = useMemo(() => {
    let loaded = 0;
    let total = 0;
    let done = 0;
    let failed = 0;
    // What the running downloads still need, over what they move now.
    let runLoaded = 0;
    let runSize = 0;
    let speed = 0;
    for (const x of all) {
      if (x.size > 0) total += x.size;
      loaded += x.status === 'done' && x.size > 0 ? x.size : Math.max(x.loaded, 0);
      if (x.status === 'done') done++;
      else if (x.status === 'error') failed++;
      else if (x.status === 'running') {
        speed += x.speed;
        if (x.size > 0) {
          runSize += x.size;
          runLoaded += Math.max(x.loaded, 0);
        }
      }
    }
    return { loaded, total, done, failed, open: all.length - done - failed, left: fmtEta(runLoaded, runSize, speed), speed };
  }, [all]);
  const average = useMeterAverage(instance, sums.speed, uploadOf(all));

  const first = [
    t('downloads.stats.loaded', { loaded: fmtTotal(sums.loaded), total: fmtTotal(sums.total) }),
    t('downloads.stats.links', { n: all.length }),
    sums.left && t('downloads.stats.left', { time: sums.left }),
  ];
  const second = [
    t('downloads.stats.done', { n: sums.done }),
    t('downloads.stats.open', { n: sums.open }),
    sums.failed > 0 && t('downloads.stats.failed', { n: sums.failed }),
    average > 0 && t('downloads.stats.average', { speed: fmtSpeed(average) }),
  ];
  const line = (parts: (string | false | 0)[]) => parts.filter(Boolean).join(' · ');

  return (
    <div role="group" aria-label={t('downloads.stats.label')} data-new="totals" className="flex flex-1 flex-col">
      <Card hue={hue} className="flex flex-1 flex-col gap-3">
        <SectionTitle>{t('downloads.stats.label')}</SectionTitle>
        <div className="flex flex-col gap-1 text-sm">
          <p className="glim-num text-carbon-text">{line(first)}</p>
          <p className="glim-num text-carbon-textSub">{line(second)}</p>
        </div>
        {children}
      </Card>
    </div>
  );
}
