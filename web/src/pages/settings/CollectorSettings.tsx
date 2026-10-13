import { PageHeader } from '../../components/ui';
import { useT } from '../../lib/i18n';
// Each card owns one subject in ./collector; the page passes the hues because
// it decides the order.
import { CollectorCard } from './collector/Collector';
import { CrawlCard } from './collector/Crawl';
import { LinkIntakeCard } from './collector/LinkIntake';
import { MirrorsCard } from './collector/Mirrors';

/**
 * CollectorSettings is everything between a link arriving and it becoming a
 * download: how it gets in, how long its batch waits and what happens to
 * links it already has or knows to be dead, which pages are crawled for more,
 * and what happens to copies of one file. Not
 * Collector, which is already the name of pages/Collector.
 */
export function CollectorSettings() {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={t('settings.nav.collector')} />
      <LinkIntakeCard hue={0} />
      <CollectorCard hue={1} />
      <CrawlCard hue={2} />
      <MirrorsCard hue={3} />
    </div>
  );
}
