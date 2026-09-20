import { useT } from '../../lib/i18n';
import { Card, SectionTitle } from '../ui';
import { Fact } from './Fact';
import type { Task } from '../../lib/api';

// A copy of columns.tsx's module-private fmtDateFull. The year test drops Go's
// zero time, which the wire carries for an event that has not happened yet.
function fmtDateFull(iso: string | undefined): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() <= 1) return '';
  return new Intl.DateTimeFormat(document.documentElement.lang || undefined, {
    dateStyle: 'full',
    timeStyle: 'medium',
  }).format(d);
}

/**
 * TimesCard shows a task's recorded timestamps and retry counters. There is no
 * start time because the server stores none; the card's (i) says so.
 */
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

      {/* Always drawn, since zero is an answer; the Go fields are omitempty.
          No "2 of 5": the count resets on done and on reclaim, the mirror
          policy decrements it, and the ceiling comes from server-side rules. */}
      <Fact label={t('detail.retries')} hint={t('detail.retriesHint')} value={String(task.retries ?? 0)} />
      <Fact
        label={t('detail.stallRestarts')}
        hint={t('detail.stallRestartsHint')}
        value={String(task.stallRestarts ?? 0)}
      />
    </Card>
  );
}
