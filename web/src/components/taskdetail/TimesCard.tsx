import { useT } from '../../lib/i18n';
import { Card, SectionTitle } from '../ui';
import { Fact } from './Fact';
import type { Task } from '../../lib/api';

/**
 * Every moment this app has recorded about one link, and the two counters that
 * go with them.
 *
 * FIVE TIMESTAMPS, NOT SIX. There is no "started at" anywhere in this app:
 * internal/store's migrations create created_at, finished_at, changed_at and
 * next_try and nothing else, and core.Task declares no StartedAt. So no row is
 * drawn for it and no gap is left where one would go. The reason lives in the
 * card's own (i) rather than in a disabled row, because a greyed-out "Started"
 * is a promise that it will fill in one day, and it will not until somebody
 * writes the field on the server first.
 *
 * The dates are spelled out in full rather than in the columns' short form.
 * The column is short because it has to fit a cell; this card has the room a
 * cell never does, and repeating the short form here would tell a reader
 * nothing the column had not already said.
 */

/**
 * The full-precision date, copied from columns.tsx's own fmtDateFull (which is
 * module-private there, and this wave's export budget went on the connection
 * label the panel cannot recompute). Kept identical on purpose, including the
 * year check.
 *
 * TRAP: Go's zero time arrives on the wire as "0001-01-01T00:00:00Z" and is a
 * VALUE, not an absent field. finishedAt, changedAt, nextTry and stalledSince
 * all carry it while the thing they describe has not happened, so a formatter
 * that only checked for undefined would print "Friday, 1 January 1 AD" on
 * every unfinished row. The year test is what turns it back into nothing, and
 * Fact then drops the whole row.
 */
function fmtDateFull(iso: string | undefined): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() <= 1) return '';
  return new Intl.DateTimeFormat(document.documentElement.lang || undefined, {
    dateStyle: 'full',
    timeStyle: 'medium',
  }).format(d);
}

export function TimesCard({ task, hue }: { task: Task; hue?: number }) {
  const { t } = useT();

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('detail.timesHint')}>{t('detail.times')}</SectionTitle>

      <Fact label={t('columns.added')} value={fmtDateFull(task.createdAt)} ltr />
      <Fact label={t('task.tooltip.changed')} value={fmtDateFull(task.changedAt)} ltr />
      <Fact label={t('columns.finished')} value={fmtDateFull(task.finishedAt)} ltr />
      <Fact label={t('detail.nextTry')} value={fmtDateFull(task.nextTry)} ltr />
      <Fact label={t('detail.stalledSince')} value={fmtDateFull(task.stalledSince)} ltr />

      {/* Both counters are always drawn, zero included, because zero is an
          answer here and a missing row is not. They are also both read through
          a fallback rather than defensively: the Go fields are omitempty, so a
          task that has never been retried genuinely has no `retries` in its
          JSON at all.

          "Retries so far", never "Attempts" and never "2 of 5". The count is
          reset to zero the moment a task reaches done (app_dispatch.go) and
          again when the reclaim sweep finds a finished file (app_boot.go), so
          a finished row reads zero however many goes it took, and the mirror
          policy decrements it outright when a spare copy takes over, so it is
          not even monotonic. And there is no ceiling to show: it comes out of
          settings.RetryFor, which merges the per-host rule, the per-reason rule
          and the global maximum with a longest-pattern-wins host match. A
          client-side re-implementation would be wrong the first time somebody
          adds a host rule. Both facts are in the (i). */}
      <Fact label={t('detail.retries')} hint={t('detail.retriesHint')} value={String(task.retries ?? 0)} />
      <Fact
        label={t('detail.stallRestarts')}
        hint={t('detail.stallRestartsHint')}
        value={String(task.stallRestarts ?? 0)}
      />
    </Card>
  );
}
