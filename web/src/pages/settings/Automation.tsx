import { PageHeader } from '../../components/ui';
import { useT } from '../../lib/i18n';
import { IdleActionCard } from './automation/IdleAction';
import { MediaHooksCard } from './automation/MediaHooks';
import { QuietModeCard } from './automation/QuietMode';
import { EventProgramsCard } from './EventPrograms';
import { EventTargetsCard } from './EventTargets';
import { ScheduleCards } from './Schedule';
import { ScriptsCard } from './Scripts';

/**
 * Automation is what the instance does with nobody at the screen: the
 * timetable, quiet mode and the streams that switch it on, the action once the
 * queue runs dry, the calls after a package lands, the messages sent out and
 * the programs started on events, and the scripts. The timetable and the scripts save through their own routes and
 * keep their own loading state, so one of them failing to load leaves the
 * others in place.
 */
export function Automation() {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-10">
      <PageHeader title={t('settings.nav.automation')} />
      <ScheduleCards hue={0} />
      <QuietModeCard hue={2} />
      <IdleActionCard hue={3} />
      <MediaHooksCard hue={4} />
      <EventTargetsCard hue={5} />
      <EventProgramsCard hue={6} />
      <ScriptsCard hue={7} />
    </div>
  );
}
