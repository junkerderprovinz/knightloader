import { ErrorCard, LoadingCard } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { useHealthReport } from '../../lib/useHealthReport';
import { PartsCard } from './health/Parts';
import { ScrapeCard } from './health/Scrape';
import { SelfTestPulseCard } from './health/SelfTestPulse';

/**
 * Health shows the state of this instance: the self-test as a pulse, every
 * part that can fail on its own with why it is not fine, and the metrics
 * switch. It reads one polled report and is not part of the
 * settings draft, since nothing here is saved.
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
    <div data-new="state-card-gone" className="flex flex-col gap-10">
      <SelfTestPulseCard hue={0} />
      <PartsCard hue={1} report={report} />
      <ScrapeCard hue={2} />
    </div>
  );
}
