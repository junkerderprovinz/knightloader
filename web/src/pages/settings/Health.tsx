import { ErrorCard, LoadingCard } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { useHealthReport } from '../../lib/useHealthReport';
import { OverallCard } from './health/Overall';
import { PartsCard } from './health/Parts';
import { ScrapeCard } from './health/Scrape';
import { TasksCard } from './health/Tasks';

/**
 * Health shows the state of this instance: every part that can fail on its
 * own, the queue by why tasks wait or failed, and the metrics switch. It reads
 * one polled report and is not part of the settings draft, since nothing here
 * is saved.
 */
export function Health() {
  const { t } = useT();
  const { report, failed } = useHealthReport();

  // A failed poll after the first report leaves that report standing.
  if (failed && !report) {
    // No retry button: the hook polls every ten seconds anyway.
    return <ErrorCard message={t('settings.health.loadFailed')} />;
  }
  if (!report) return <LoadingCard label={t('common.loading')} />;

  return (
    <div className="flex flex-col gap-10">
      <OverallCard hue={0} report={report} />
      <PartsCard hue={1} report={report} />
      <TasksCard hue={2} report={report} />
      <ScrapeCard hue={3} />
    </div>
  );
}
