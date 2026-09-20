import { useT, type TranslationKey } from '../../lib/i18n';
import { reasonKey } from '../columns';
import { Card, LabelBadge, SectionTitle } from '../ui';
import { Fact } from './Fact';
import type { Task } from '../../lib/api';

// Derived from the union so a renamed key stops compiling. A value from a newer
// server finds no key, and Fact then draws no row rather than a raw enum.
const waitingKey = (w: NonNullable<Task['waiting']>): TranslationKey => `task.waiting.${w}`;

/**
 * FailureCard explains why a link is not moving and disappears when there is
 * nothing to explain. It shows the current failure only: a restart or requeue
 * clears the error, so no history exists to show.
 */
export function FailureCard({ task, hue }: { task: Task; hue?: number }) {
  const { t } = useT();

  // reasonKey's index type claims every reason has a key, but a reason from a
  // newer server comes back undefined at runtime.
  const causeKey: TranslationKey | undefined = task.reason ? reasonKey[task.reason] : undefined;
  const cause = causeKey ? t(causeKey) : '';
  const held = task.waiting ? t(waitingKey(task.waiting)) : '';
  // skipReason without `skipped` is a stale sentence from a park that has
  // since been lifted; the flag is what makes it true right now.
  const filtered = task.skipped ? (task.skipReason ?? '') : '';

  if (!cause && !task.error && !held && !task.note && !filtered && !task.gaveUp) return null;

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle>{t('detail.failure')}</SectionTitle>

      {/* Neutral, because the row's status pill already carries the colour. */}
      <Fact label={t('detail.cause')}>{cause ? <LabelBadge label={cause} /> : null}</Fact>

      <Fact label={t('detail.message')} hint={t('detail.messageHint')} value={task.error} ltr copy />
      <Fact label={t('detail.heldBack')} value={held} />
      <Fact label={t('detail.doing')} value={task.note} />
      <Fact label={t('detail.filtered')} value={filtered} />

      {/* A badge, not a switch: the dispatcher sets this, not the user. It
          covers a captcha, a full disk or a never rule rather than exhausted
          attempts, and it is not persisted; the (i) says both. */}
      {task.gaveUp && (
        <div>
          <LabelBadge label={t('detail.gaveUp')} tone="fail" tip={t('detail.gaveUpHint')} />
        </div>
      )}
    </Card>
  );
}
