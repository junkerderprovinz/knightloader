import { useT, type TranslationKey } from '../../lib/i18n';
import { reasonKey } from '../columns';
import { Card, LabelBadge, SectionTitle } from '../ui';
import { Fact } from './Fact';
import type { Task } from '../../lib/api';

/**
 * Why this link is not moving: the typed cause, the backend's own sentence,
 * what is holding a queued task back, what a running one is busy with, the
 * link filter's verdict, and whether anything will be tried again at all.
 *
 * THE WHOLE CARD DISAPPEARS when there is none of that. A link sitting in the
 * collector has never failed and is not waiting on anything, and a card headed
 * "What went wrong" with six blank rows under it reads as a panel that broke
 * rather than as a download that is fine.
 *
 * THIS IS NOT A HISTORY, and the card says so in the (i) on the message row.
 * RestartTasksIn clears Error and resets Reason on every restart and on every
 * requeue, so exactly one failure is ever recorded and it is the current one.
 * Calling this "Last error" would promise a list the app has never kept.
 */

/**
 * The word for each reason a queued task has not started.
 *
 * Built from the union rather than from a second lookup table: Task['waiting']
 * is closed in lib/api.ts, every member has a `task.waiting.<member>` key, and
 * a template literal type makes that a compile-time fact. A renamed or deleted
 * key stops this compiling; a hand-written copy of columns.tsx's own map would
 * simply drift, which is exactly how the disk reason spent a wave being
 * reported as "all slots busy".
 *
 * A value from a newer server that this union has never heard of survives it:
 * t() finds nothing under the key and hands back undefined, Fact sees an empty
 * value and draws no row. A raw enum on screen would be worse than silence.
 */
const waitingKey = (w: NonNullable<Task['waiting']>): TranslationKey => `task.waiting.${w}`;

export function FailureCard({ task, hue }: { task: Task; hue?: number }) {
  const { t } = useT();

  // reasonKey is a plain Record, so the index type says TranslationKey while
  // an unrecognised reason really does come back undefined at runtime. The
  // annotation is what keeps the ternary below honest about that: the server's
  // reason is an open string on purpose, and a build that has not heard of a
  // newer one shows no cause rather than "reason: hoster_soft_limit".
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

      {/* A quiet badge, not a red one. The row's own status pill upstairs
          already carries the colour for this task, and a second red thing
          repeating it in a card headed "What went wrong" says the same thing
          twice. This one names the cause; it does not announce it. */}
      <Fact label={t('detail.cause')}>{cause ? <LabelBadge label={cause} /> : null}</Fact>

      <Fact label={t('detail.message')} hint={t('detail.messageHint')} value={task.error} ltr copy />
      <Fact label={t('detail.heldBack')} value={held} />
      <Fact label={t('detail.doing')} value={task.note} />
      <Fact label={t('detail.filtered')} value={filtered} />

      {/* A fact, never a Toggle and never a checkbox: nobody sets this, the
          dispatcher does, and a switch here would offer to change something
          this panel cannot change. It is also deliberately NOT the same thing
          as "the attempts ran out" - core.Task only sets it for a captcha, a
          full disk or a never rule, all of which raising the retry count does
          nothing about. And it is not persisted, so it silently becomes false
          after a restart while the captcha or the full disk is still true.
          Both of those live in the (i), because they are the kind of thing
          somebody otherwise discovers the hard way. */}
      {task.gaveUp && (
        <div>
          <LabelBadge label={t('detail.gaveUp')} tone="fail" tip={t('detail.gaveUpHint')} />
        </div>
      )}
    </Card>
  );
}
