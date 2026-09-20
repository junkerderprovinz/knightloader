// Failures grouped by cause, each group with its own retry, since dead links,
// a spent hoster allowance and a full disk need different remedies. The server
// half is app.RestartTasksIn.
import { useMemo } from 'react';
import { restartTasks, type Reason, type Task } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { Button } from './ui';
import { reasonKey } from './columns';
import { IconRetry } from '../lib/icons';

interface Cause {
  key: string;
  label: string;
  /**
   * More than one for the catch-all chip, which gathers reasons this build has
   * no label for, so its retry restarts exactly the rows it counted.
   */
  reasons: Reason[];
  count: number;
}

// causesOf groups the errored rows, largest group first for triage.
function causesOf(tasks: Task[], t: (k: TranslationKey) => string): Cause[] {
  const known = new Map<string, Cause>();
  const other: Cause = { key: '', label: t('task.reason.unknown'), reasons: [], count: 0 };
  const seenOther = new Set<Reason>();
  for (const task of tasks) {
    if (task.status !== 'error') continue;
    const reason = task.reason ?? '';
    const label = reasonKey[reason];
    if (!label) {
      other.count++;
      if (!seenOther.has(reason)) {
        seenOther.add(reason);
        other.reasons.push(reason);
      }
      continue;
    }
    const found = known.get(reason);
    if (found) found.count++;
    else known.set(reason, { key: reason, label: t(label), reasons: [reason], count: 1 });
  }
  const out = [...known.values()];
  if (other.count > 0) out.push(other);
  return out.sort((a, b) => b.count - a.count);
}

/**
 * ErrorCauses draws the chip row once there are at least two causes; with one,
 * a chip would duplicate the "retry failed" button. It counts the whole list,
 * like that button, because the retry acts on the whole list too.
 */
export function ErrorCauses({ tasks, base }: { tasks: Task[]; base: string }) {
  const { t } = useT();
  const causes = useMemo(() => causesOf(tasks, t), [tasks, t]);
  if (causes.length < 2) return null;
  return (
    <div className="flex flex-wrap items-center gap-2" role="group" aria-label={t('downloads.retryFailed')}>
      {causes.map((c) => (
        <Button
          key={c.key}
          // No hue: one verb over several groups should read as one control.
          kind="secondary"
          className="gap-1.5 px-2.5 text-xs"
          icon={<IconRetry width={14} height={14} />}
          title={t('downloads.retryCause', { n: c.count, reason: c.label })}
          onClick={() => void restartTasks([], base, c.reasons)}
        >
          {c.label}
          <span className="glim-num text-carbon-textMuted">{c.count}</span>
        </Button>
      ))}
    </div>
  );
}
