import { fmtDateFull } from '../../lib/format';
import { useT } from '../../lib/i18n';
import { Card, SectionTitle } from '../ui';
import { Fact } from './Fact';
import type { Task } from '../../lib/api';

/**
 * TimesCard shows a task's recorded timestamps and retry counters. There is no
 * start time because the server stores none; the card's (i) says so.
 */
export function TimesCard({ task, hue }: { task: Task; hue?: number }) {
  const { t } = useT();

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('detail.timesHint')}>{t('detail.times')}</SectionTitle>

      <Fact label={t('columns.added')} value={fmtDateFull(task.createdAt)} />
      <Fact label={t('task.tooltip.changed')} value={fmtDateFull(task.changedAt)} />
      <Fact label={t('columns.finished')} value={fmtDateFull(task.finishedAt)} />
      <Fact label={t('detail.nextTry')} value={fmtDateFull(task.nextTry)} />
      <Fact label={t('detail.stalledSince')} value={fmtDateFull(task.stalledSince)} />

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
