import { Card, SectionTitle } from '../../../components/ui';
import { useT } from '../../../lib/i18n';
import { breakdownLabel } from '../../../lib/useHealthReport';
import type { HealthReport } from '../../../lib/api';

/**
 * The download list as it stands, split by why things are not moving.
 *
 * THREE FIGURES AND TWO BREAKDOWNS, and the breakdowns are the point. "16
 * waiting" is a number somebody already has on the Downloads page; "12 of them
 * waiting for disk space" is the answer to why, and it is the one thing no
 * other surface in the app puts in one place.
 *
 * THE LABELS COME FROM THE CATALOGUE THE TASK LIST ALREADY USES. The server
 * sends core.Waiting and core.Reason ids, which every row in the download list
 * is already labelled from (task.waiting.*, task.reason.*, present in all 42
 * locales) - so this card needed no new per-reason strings at all, and a
 * reason renamed in one place is renamed in both. Looked up through
 * breakdownLabel rather than switched on, so an id a newer server has learnt
 * shows as itself rather than as a blank.
 *
 * ONE SectionTitle, and the two sub-headings below are deliberately not
 * SectionTitles: a second one in the same Card lands on top of the first (the
 * badge is absolutely positioned against the card, see ui.tsx) and paints it
 * out entirely.
 *
 * NO DISK CARD HERE. Room on the target folders already has a finished card on
 * the Downloads page, and two cards answering "how much room is left" is two
 * answers that can disagree - so this points at the one that exists.
 */
export function TasksCard({ hue, report }: { hue: number; report: HealthReport }) {
  const { t } = useT();
  const tasks = report.tasks;

  return (
    <Card hue={hue} className="flex flex-col gap-5">
      <SectionTitle hint={t('settings.health.tasksHint')}>{t('settings.health.tasks')}</SectionTitle>

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

      <span className="text-[11px] text-carbon-textMuted">{t('settings.health.diskLink')}</span>
    </Card>
  );
}

/**
 * One of the three headline figures.
 *
 * Its own component rather than Reading next door only because these three are
 * drawn larger - they are the numbers somebody glances at. `data-glim-label` is
 * carried for the same reason Reading carries it: these three captions are in
 * the settings search index, and a result that resolves to a caption with no
 * property to find lands on the card instead of on the row.
 */
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
 * One breakdown map as a list, largest first.
 *
 * Sorted here rather than trusted from the wire: the server writes it as a JSON
 * object and object key order is not something to build a reading order on. Ties
 * fall back to the id so that two renders of an unchanged queue do not shuffle.
 *
 * An empty map gets a sentence rather than an empty box, because "nothing is
 * being held back" is a real answer and a blank space is not.
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
