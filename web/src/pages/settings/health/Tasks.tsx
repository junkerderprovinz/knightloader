import { Card, SectionTitle } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { breakdownLabel } from '../../../lib/useHealthReport';
import type { HealthReport } from '../../../lib/api';

/**
 * TasksCard counts the download list and breaks the waiting and failed tasks
 * down by reason, labelled with the task list's own task.waiting.* and
 * task.reason.* keys. The sub-headings are not SectionTitles because a second
 * one in the same Card paints over the first.
 */
export function TasksCard({ hue, report }: { hue: number; report: HealthReport }) {
  const { t } = useT();
  const tasks = report.tasks;

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle
        hint={
          <span className="flex flex-col gap-1.5">
            <span>{t('settings.health.tasksHint')}</span>
            <span>{t('settings.health.diskWhere')}</span>
          </span>
        }
      >
        {t('settings.health.tasks')}
      </SectionTitle>

      <div className="grid grid-cols-3 gap-4">
        <Count label={t('settings.health.running')} n={tasks.running} />
        <Count label={t('settings.health.waiting')} n={tasks.waiting} />
        <Count label={t('settings.health.failed')} n={tasks.failed} />
      </div>

      <Breakdown
        heading={t('settings.health.waitingWhy')}
        empty={t('settings.health.nothingWaiting')}
        counts={tasks.waitingBy}
        label={(id) => breakdownLabel(t, 'task.waiting.', id)}
      />
      <Breakdown
        heading={t('settings.health.failedWhy')}
        empty={t('settings.health.nothingFailed')}
        counts={tasks.failedBy}
        label={(id) => breakdownLabel(t, 'task.reason.', id)}
      />
    </Card>
  );
}

/** Count is a larger Reading; data-glim-label lets the settings search land on it. */
function Count({ label, n }: { label: string; n: number }) {
  return (
    <div className="flex flex-col gap-1">
      <span data-glim-label={label} className="text-[11px] text-carbon-textMuted">
        {label}
      </span>
      <span className="glim-num text-lg text-carbon-text" dir="ltr">
        {n}
      </span>
    </div>
  );
}

/**
 * Breakdown lists one map largest first. Ties sort by id so an unchanged queue
 * keeps its order, since JSON object key order means nothing.
 */
function Breakdown({
  heading,
  empty,
  counts,
  label,
}: {
  heading: string;
  empty: string;
  counts: Record<string, number>;
  label: (id: string) => string;
}) {
  const rows = Object.entries(counts ?? {}).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
  return (
    <div className="flex flex-col gap-2">
      <span className="text-xs font-medium text-carbon-textSub">{heading}</span>
      {rows.length === 0 ? (
        <span className="text-[11px] text-carbon-textMuted">{empty}</span>
      ) : (
        <ul className="flex flex-col gap-1">
          {rows.map(([id, n]) => (
            <li key={id} className="flex items-baseline justify-between gap-4 text-xs text-carbon-text">
              <span>{label(id)}</span>
              <span className="glim-num text-carbon-textSub" dir="ltr">
                {n}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
