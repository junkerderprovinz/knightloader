import { PageHeader } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { IdleActionCard } from './automation/IdleAction';
import { MediaHooksCard } from './automation/MediaHooks';
import { EventTargetsCard } from './EventTargets';
import { ScheduleCards } from './Schedule';
import { ScriptsCard } from './Scripts';

/**
 * Automation is what the instance does with nobody at the screen: the
 * timetable, the action once the queue runs dry, the calls after a package
 * lands, the messages sent out on events, and the scripts. The timetable and
 * the scripts save through their own routes and keep their own loading state,
 * so one of them failing to load leaves the others in place.
 */
export function Automation() {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={t('settings.nav.automation')} />
      <ScheduleCards hue={0} />
      <IdleActionCard hue={2} />
      <MediaHooksCard hue={3} />
      <EventTargetsCard hue={4} />
      <ScriptsCard hue={5} />
    </div>
  );
}
