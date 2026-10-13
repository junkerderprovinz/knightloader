// Failures grouped by cause, each group with its own retry, since dead links,
// a spent hoster allowance and a full disk need different remedies. The server
// half is app.RestartTasksIn.
import { useMemo } from 'react';
import { restartTasks, type Reason, type Task } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
import { Button } from './ui';
import { ContextMenu, anchorBelow, useContextMenu } from './ContextMenu';
import { reasonKey } from './columns';
import { IconExpand, IconRefresh } from '../lib/icons';

export interface Cause {
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
 * useErrorCauses is the list's failure groups once there are at least two;
 * with one, a chip would duplicate the "retry failed" button. It counts the
 * whole list, like that button, because the retry acts on the whole list too.
 */
export function useErrorCauses(tasks: Task[]): Cause[] {
  const { t } = useT();
  return useMemo(() => {
    const causes = causesOf(tasks, t);
    return causes.length < 2 ? [] : causes;
  }, [tasks, t]);
}

/**
 * ErrorCauses draws a retry chip per cause in the list's action row, or, when
 * the row has no room for them, one chip that opens them as a menu. `glyph`
 * leaves that chip its glyphs, for a row shorter still.
 */
export function ErrorCauses({
  causes,
  base,
  folded,
  glyph = false,
}: {
  causes: Cause[];
  base: string;
  folded: boolean;
  glyph?: boolean;
}) {
  const { t } = useT();
  const menu = useContextMenu();
  if (causes.length === 0) return null;
  const retry = (c: Cause) => void restartTasks([], base, c.reasons);

  if (folded)
    return (
      <>
        <Button
          kind="secondary"
          className="shrink-0 gap-1.5 px-2.5 text-xs"
          icon={<IconRefresh width={14} height={14} />}
          title={t('downloads.retryByCause')}
          aria-label={glyph ? t('downloads.retryByCause') : undefined}
          aria-haspopup="menu"
          aria-expanded={!!menu.anchor}
          onClick={(e) => menu.openAt(anchorBelow(e.currentTarget))}
        >
          {!glyph && t('downloads.byCause')}
          <IconExpand width={12} height={12} />
        </Button>
        {menu.anchor && (
          <ContextMenu
            anchor={menu.anchor}
            label={t('downloads.retryByCause')}
            onClose={menu.close}
            groups={[
              {
                id: 'causes',
                items: causes.map((c) => ({
                  id: c.key || 'other',
                  label: c.label,
                  detail: String(c.count),
                  icon: <IconRefresh width={14} height={14} />,
                  onSelect: () => retry(c),
                })),
              },
            ]}
          />
        )}
      </>
    );

  return (
    <div className="flex shrink-0 items-center gap-2" role="group" aria-label={t('downloads.retryFailed')}>
      {causes.map((c) => (
        <Button
          key={c.key}
          // No hue: one verb over several groups should read as one control.
          kind="secondary"
          className="gap-1.5 px-2.5 text-xs"
          icon={<IconRefresh width={14} height={14} />}
          title={t('downloads.retryCause', { n: c.count, reason: c.label })}
          onClick={() => retry(c)}
        >
          {c.label}
          <span className="glim-num text-carbon-textMuted">{c.count}</span>
        </Button>
      ))}
    </div>
  );
}
