// Failures grouped by what actually went wrong, each group its own retry.
//
// After a night the download list is not "forty errors", it is twenty-one dead
// links, twelve downloads that ran into a hoster's daily allowance and seven
// that stopped when the disk filled up. Those are three different situations
// with three different remedies, and the single "retry failed" button treats
// them as one: it asks the host to confirm twenty-one times that a file is still
// gone, spends an allowance that is already spent, and buries the seven somebody
// could have fixed by freeing space under the noise of the other thirty-three.
//
// The app has been recording the cause on every failure all along (core.Reason,
// and the label already appears on the row itself). All that was missing was a
// way to aim at one - see app.RestartTasksIn for the server half.
import { useMemo } from 'react';
import { restartTasks, type Reason, type Task } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { Button } from './ui';
import { reasonKey } from './columns';
import { IconRetry } from '../lib/icons';

/**
 * One chip: a name, how many rows are behind it, and the reason values the
 * retry has to send.
 *
 * `reasons` is a list rather than a single value because of the last group. The
 * server's taxonomy grows, so a running instance can settle a task with a cause
 * this build has never heard of, and there is no honest word to print for it -
 * exactly the case reasonKey deliberately leaves unlabelled. Those rows join the
 * unclassified ones under one chip, and the chip carries every value it stands
 * for so that pressing it restarts the rows it counted and no others.
 */
interface Cause {
  key: string;
  label: string;
  reasons: Reason[];
  count: number;
}

/**
 * causesOf groups the errored rows, largest group first.
 *
 * Largest first because the point of the row is triage: the biggest pile is the
 * one worth a decision, and ordering by the taxonomy's own order instead would
 * put whichever cause happens to be declared first in front of it.
 */
function causesOf(tasks: Task[], t: (k: TranslationKey) => string): Cause[] {
  const known = new Map<string, Cause>();
  // Built once and reused, so the "everything else" chip exists only if
  // something landed in it.
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
 * ErrorCauses is the chip row, and it draws nothing until it has something to
 * say.
 *
 * Below two groups there is nothing to choose between: one cause means the chip
 * and the plain "retry failed" button beside it would do the identical thing
 * under two different names, which is worse than one button.
 *
 * It counts from the whole download list rather than from the filtered view, on
 * purpose and like the "retry failed" button it sits next to: this is a
 * statement about the list, not about the rows that survived a search, and a
 * chip that said "3" while restarting twelve would be the more surprising of the
 * two possible mismatches.
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
          // Quiet, and no hue. The badge row above this one gives every entry
          // its own palette colour because those entries are unrelated verbs;
          // these are one verb over several groups, so they read as one control
          // - and a row of filled accent buttons would be the loudest thing on
          // the page for a state nobody chose.
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
