import { useState } from 'react';
import { Dropdown, type DropdownOption } from '../../../components/Dropdown';
import { Field } from '../../../components/ui';
import { resumeSchedule, suspendSchedule, type ScheduleSuspension } from '../../../lib/api';
import { useT } from '../../../lib/i18n';
import { useToast } from '../../../lib/toast';

// Setting every schedule aside for a while, as the schedule status card and the
// shell bar's quick settings both offer it. The rows stay as they are; the
// server ends the suspension by itself when its time is up.

type Choice = 'off' | 'hour' | 'threeHours' | 'midnight' | 'open' | 'current';

/**
 * spanOf is what a choice asks the server for. The lengths are counted on the
 * server's clock; midnight is the reader's own, sent as an instant, since
 * that is the midnight they mean.
 */
function spanOf(choice: Choice): { minutes: number } | { until: string } | null {
  switch (choice) {
    case 'hour':
      return { minutes: 60 };
    case 'threeHours':
      return { minutes: 180 };
    case 'midnight': {
      const d = new Date();
      d.setHours(24, 0, 0, 0);
      return { until: d.toISOString() };
    }
    default:
      return null;
  }
}

/** fmtUntil names the end by its clock time, with the weekday when it is not today. */
export function fmtUntil(until: Date): string {
  const locale = document.documentElement.lang || undefined;
  const today = until.toDateString() === new Date().toDateString();
  return new Intl.DateTimeFormat(locale, {
    weekday: today ? undefined : 'short',
    hour: '2-digit',
    minute: '2-digit',
  }).format(until);
}

/**
 * ScheduleSuspendField is one dropdown: off, or suspended for one of four
 * spans. While a suspension with an end runs, its first entry names that end,
 * because "for an hour" stops being true a minute later. `onState` receives the
 * server's answer after every change. `blocked` says why there is nothing to
 * suspend, and disables the dropdown unless a suspension still runs.
 */
export function ScheduleSuspendField({
  state,
  onState,
  blocked,
}: {
  state: ScheduleSuspension;
  onState: (s: ScheduleSuspension) => void;
  blocked?: string;
}) {
  const { t } = useT();
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  const [shake, setShake] = useState(0);
  const idle = Boolean(blocked) && !state.suspended;

  const until = state.suspended && state.suspendedUntil ? new Date(state.suspendedUntil) : null;
  const value: Choice = !state.suspended ? 'off' : until ? 'current' : 'open';
  const options: DropdownOption<Choice>[] = [
    ...(until ? [{ value: 'current' as const, label: t('settings.schedule.suspend.until', { when: fmtUntil(until) }) }] : []),
    { value: 'off', label: t('settings.schedule.suspend.off') },
    { value: 'hour', label: t('settings.schedule.suspend.hour') },
    { value: 'threeHours', label: t('settings.schedule.suspend.threeHours') },
    { value: 'midnight', label: t('settings.schedule.suspend.midnight') },
    { value: 'open', label: t('settings.schedule.suspend.open') },
  ];

  async function choose(choice: Choice) {
    if (choice === value || choice === 'current') return;
    setBusy(true);
    try {
      onState(choice === 'off' ? await resumeSchedule() : await suspendSchedule(spanOf(choice)));
    } catch (e) {
      toast(t('list.failed', { error: e instanceof Error ? e.message : String(e) }), 'fail');
      setShake((n) => n + 1);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Field
      label={t('settings.schedule.suspend')}
      hint={idle ? `${blocked} ${t('settings.schedule.suspendHint')}` : t('settings.schedule.suspendHint')}
    >
      <Dropdown
        value={value}
        options={options}
        onChange={(c) => void choose(c)}
        label={t('settings.schedule.suspend')}
        disabled={busy || idle}
        shake={shake}
      />
    </Field>
  );
}
