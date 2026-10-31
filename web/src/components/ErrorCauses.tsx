// Failures grouped by cause, each group with its own reset, since dead links,
// a spent hoster allowance and a full disk need different remedies. The server
// half is app.RestartTasksIn.
import { useMemo } from 'react';
import { restartTasks, type Reason, type Task } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import type { MenuItem } from './ContextMenu';
import { reasonKey } from './columns';
import { IconRetry } from '../lib/icons';

export interface Cause {
  key: string;
  label: string;
  /**
   * More than one for the catch-all entry, which gathers reasons this build
   * has no label for, so its reset restarts exactly the rows it counted.
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
 * useErrorCauses is the list's failure groups once there are at least two;
 * with one, its entry would repeat "Reset failed". It counts the whole list,
 * like that entry, because the reset acts on the whole list too.
 */
export function useErrorCauses(tasks: Task[]): Cause[] {
  const { t } = useT();
  return useMemo(() => {
    const causes = causesOf(tasks, t);
    return causes.length < 2 ? [] : causes;
  }, [tasks, t]);
}

/**
 * causeItems are the failure groups as menu entries, each resetting only the
 * rows it counted.
 */
export function causeItems(causes: Cause[], base: string): MenuItem[] {
  return causes.map((c) => ({
    id: c.key || 'other',
    label: c.label,
    detail: String(c.count),
    icon: <IconRetry width={14} height={14} />,
    onSelect: () => void restartTasks([], base, c.reasons),
  }));
}
