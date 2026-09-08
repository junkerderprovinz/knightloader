import { ErrorCard, LoadingCard } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { useHealthReport } from '../../lib/useHealthReport';
import { OverallCard } from './health/Overall';
import { PartsCard } from './health/Parts';
import { ScrapeCard } from './health/Scrape';
import { TasksCard } from './health/Tasks';

/**
 * The state of this instance right now: every part with a state of its own, the
 * queue by why it is waiting and why it failed, and one switch.
 *
 * IT READS AND DOES NOT WRITE. Nothing on the first three cards starts, stops
 * or changes anything, and leaving the page open changes nothing either - it is
 * one GET every ten seconds against a route whose expensive half the server
 * shares for half a minute. The one control is on the last card, and it is a
 * door rather than a preference.
 *
 * IT IS NOT PART OF THE SETTINGS DRAFT (context.tsx's useDraft), the same
 * arrangement the Diagnostics page uses and for the same reason: there is
 * nothing here to save. The metrics switch goes through the module registry
 * rather than the draft, which is what keeps it and the Modules page from
 * disagreeing - see Scrape.tsx.
 *
 * WHY IT SITS BESIDE DIAGNOSE IN THE RAIL. The two are the operator's pair and
 * they are read in that order: this page answers "is it working right now", the
 * next one hands over a bundle for a bug report about why it was not.
 *
 * ONE CARD PER FILE, which is what makes "at most one SectionTitle per Card" a
 * structural fact here rather than a rule somebody has to remember. The `hue`
 * numbers follow the ORDER THE PAGE DRAWS THEM: the palette position belongs to
 * the card sequence, and a badge sequence that jumps reads as a bug.
 *
 * A NOTE FOR THE RELEASE, because it will otherwise be reported as a missing
 * tab: anybody who has ever dragged a settings tab has a stored tab order, and
 * orderPages (pages/Settings.tsx) puts every page that order does not name
 * AFTER the ones it does. So a long-standing install finds this tab at the
 * BOTTOM of the rail while a fresh one finds it beside Diagnose. Nothing on the
 * server can fix that without overwriting somebody's own arrangement.
 */
export function Health() {
  const { t } = useT();
  const { report, failed } = useHealthReport();

  // failed is only ever true while NOTHING has arrived. Once a report is on
  // screen a dropped poll leaves it standing rather than replacing a working
  // page with an error - see useHealthReport, which owns that rule for the same
  // reason the disk readout does.
  if (failed && !report) {
    // No retry button: the hook is already polling every ten seconds, so the
    // page mends itself the moment the server answers. A button that promised
    // to do what is already happening would be a control that does nothing.
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
