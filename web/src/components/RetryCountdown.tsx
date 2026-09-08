// What a failed row is waiting for, and the one control that brings it forward.
//
// Half of this already existed and none of it was legible: the two numbers have
// been on the wire for a long time (core.Task.Retries, core.Task.NextTry), the
// row carries a retry glyph, and the exact moment is behind a hover as a
// formatted date. So a person looking at a red row could see THAT something
// would happen again and never when, never how many attempts were left, and
// never whether "again" was coming at all. What they did instead was press
// Restart, on the reasonable assumption that nothing else was going to.
//
// Its own file rather than more of columns.tsx, which is already long past the
// length at which a cell renderer can be found in it, and because the status
// cell, the row tooltip, the row's hover strip and the context menu all have to
// agree about the same four states. They agree by all calling retryStateOf.
//
// THE COUNTER IS NOT RESET BY ANY OF THIS. app.RestartTasksIn clears the
// status, the error and the progress and never touches Retries or NextTry, so a
// restart continues the backoff ladder from the attempts already spent rather
// than handing the link a fresh budget. That was already true and nothing said
// so, which is why somebody watching a ten minute wait presses Restart, sees the
// wait start over, and concludes the button made it worse. The fix is not a new
// route: it is a control named after what the existing one does, sitting where
// the wait is visible.
import { restartTasks, type Task } from '../lib/api';
import { fmtCountdown, goTimeMs, useCountdown } from '../lib/countdown';
import { useT, type TranslationKey } from '../lib/i18n';
import { IconBolt } from '../lib/icons';
import { useToast } from '../lib/toast';
import { IconBadge } from './ui';

/**
 * How long a deadline that has just gone by still reads as "due now" rather
 * than as no deadline at all.
 *
 * Those seconds are a real state and not a rounding error: the server's timer
 * fires at the deadline, the task is requeued, and the new status reaches this
 * tab over the socket a moment afterwards. Without the window the row would
 * either blink its countdown out before anything about it had changed, or sit
 * on a frozen zero.
 *
 * It is also the whole defence against a deadline that outlived the process
 * counting to it. This instance clears NextTry for a failed task at boot
 * (App.reviveOnBoot) precisely so it never sends one - but the downloads list
 * routinely shows a PEER instance's tasks, and a peer older than that fix still
 * hands out a deadline from before its own last restart with nothing behind it.
 * Past the grace such a row falls through to the exhausted reading instead of
 * promising a retry that is never coming.
 */
const GRACE_MS = 60_000;

/**
 * The four things a failed row can be waiting for, and null for a row that is
 * not failed or has nothing to say.
 *
 * Four and not one, because they are mended by four different things and the
 * app has been showing all of them as the same red row. "Waiting" needs
 * nothing. "Due" needs a moment. "Exhausted" is the only one a bigger
 * "Automatic retries" setting actually fixes. "Gave up" is not fixed by that
 * number at all and never will be, which is what core.Task.GaveUp exists to
 * say.
 */
export type RetryState =
  | { kind: 'waiting'; n: number; max?: number; secs: number }
  | { kind: 'due'; n: number; max?: number }
  | { kind: 'exhausted'; n: number; max?: number }
  | { kind: 'gaveUp' }
  | null;

/**
 * The deadline a row is actually counting to, or null.
 *
 * Gated on the status and not only on the field, because nextTry is never
 * CLEARED when a task is requeued: neither app.RestartTasksIn nor the
 * dispatcher touches it, so a queued, running or paused row can arrive over the
 * socket still carrying the deadline of its last failure. A reader that checked
 * only `!!task.nextTry` would paint a retry countdown over a download that is
 * moving bytes. Both readers that existed before this one check the status
 * first for exactly that reason.
 */
function deadlineOf(task: Task): number | null {
  if (task.status !== 'error') return null;
  return goTimeMs(task.nextTry);
}

/**
 * retryStateOf is the one place a failed row's retry is read, so the status
 * cell, the row tooltip, the hover strip and the context menu cannot end up
 * disagreeing about what the same row says. Pure, and handed its own `nowMs`,
 * so the component that ticks owns the clock and this owns the vocabulary.
 *
 * The order is the argument.
 *
 * gaveUp wins before any deadline is looked at: a never rule, an unanswered
 * captcha and a full disk are decisions, and nothing about the next ten minutes
 * changes one of them.
 *
 * Then the deadline, which is the only authoritative account of when the retry
 * happens. The wait is not always the ladder - a backend can send a delay of
 * its own, and a reconnect brings a pending retry forward without rewriting
 * anything - so nothing here is derived from `retries` and the retry settings.
 * That is also why the wording is "next in" and not "starts in".
 *
 * Then, with no live deadline left, attempts already spent make it an exhausted
 * row rather than a plain failure, because that is the distinction somebody
 * acts on.
 */
export function retryStateOf(task: Task, nowMs: number): RetryState {
  if (task.status !== 'error') return null;
  if (task.gaveUp) return { kind: 'gaveUp' };
  // Retries counts RETRIES, not attempts: the dispatcher increments it on the
  // failure that schedules the next run, so with the default of three the
  // values a row passes through are 1, 2, 3 and the fourth failure settles.
  // Rendered as "attempt {n} of {max}" a row would claim "attempt 3 of 3" with
  // a fourth run still coming; shifted by one to fix that it would say "of 4"
  // against a settings field somebody typed 3 into. Both read as bugs, so the
  // strings use the setting's own word and its own number.
  const n = task.retries ?? 0;
  // Zero is "nobody has said", not "no attempts allowed" (core.Task.MaxTries):
  // nothing has failed yet, the failure settled on a branch that decides
  // instead of counting, or the instance is older than the field. All three
  // drop the denominator, because inventing one is worse than showing none -
  // and it cannot be recovered here either, since the ceiling is a host rule
  // merged over a per-reason table merged over the global count, and the list
  // is often showing a peer instance's rows and this box's settings.
  const max = task.maxTries && task.maxTries > 0 ? task.maxTries : undefined;
  const deadline = deadlineOf(task);
  if (deadline !== null) {
    const secs = Math.round((deadline - nowMs) / 1000);
    if (secs > 0) return { kind: 'waiting', n, max, secs };
    if (nowMs - deadline <= GRACE_MS) return { kind: 'due', n, max };
    // Older than the grace: read as no deadline at all, and the row falls
    // through to whatever it would have said carrying none. See GRACE_MS.
  }
  if (n > 0) return { kind: 'exhausted', n, max };
  return null;
}

/**
 * retryPending is "this row is waiting for a retry that has not happened yet",
 * the single predicate both skip controls are offered on.
 *
 * Deliberately the same reading the countdown itself uses, so the control and
 * the number appear and vanish together: a button offering to skip a wait that
 * is not on screen is a button for a wait that does not exist.
 *
 * Only the waiting state qualifies. In `due` there is nothing left to skip, and
 * in `exhausted` or `gaveUp` pressing it would be the plain Restart that
 * already sits beside it.
 */
export function retryPending(task: Task): boolean {
  return retryStateOf(task, Date.now())?.kind === 'waiting';
}

// Every string this feature can say, in one lookup per state, so the two forms
// below cannot drift into saying different things about the same row. The NoMax
// twin of each is not a fallback for a missing translation: it is the sentence
// for a row whose ceiling the server genuinely never resolved.
function fullKey(state: NonNullable<RetryState>): TranslationKey {
  switch (state.kind) {
    case 'waiting':
      return state.max ? 'task.retry.pending' : 'task.retry.pendingNoMax';
    case 'due':
      return state.max ? 'task.retry.due' : 'task.retry.dueNoMax';
    case 'exhausted':
      return state.max ? 'task.retry.exhausted' : 'task.retry.exhaustedNoMax';
    case 'gaveUp':
      return 'task.retry.gaveUp';
  }
}

/**
 * RetryNote is the sentence, and the only thing in this feature that ticks.
 *
 * THE INTERVAL LIVES HERE AND NOWHERE ABOVE HERE. This span is a leaf: a tick
 * repaints it and stops. Moved up into the list card, into a page, or into a
 * shared clock the card reads, the same tick would drag the card's own
 * post-commit measuring pass along with it and re-measure every rendered row
 * once a second on a list that is expensive to build once. See useCountdown.
 *
 * The compact form is for the status cell, which is 148px by default and 90px
 * at its floor. The full sentence belongs in the row tooltip, which is a leaf
 * that only exists while it is hovered, so a value ticking inside it costs
 * nothing. Neither of them goes on the name cell's error line: prose that
 * cannot shrink beside a sentence that can is a fight the sentence loses, and
 * that line already lost it once - measured at 1440, the German note wanted
 * 142px of a 116px line and squeezed the reason the download failed down to
 * nothing.
 */
export function RetryNote({ task, form }: { task: Task; form: 'compact' | 'full' }) {
  const { t } = useT();
  const deadline = deadlineOf(task);
  const secs = useCountdown(deadline, GRACE_MS);
  // One tick decides both the words and the number. Rebuilding the moment the
  // countdown is describing, rather than asking the clock a second time, is
  // what stops the sentence saying "next in" while the state underneath it has
  // already moved on to "due now".
  const nowMs = deadline === null || secs === null ? Date.now() : deadline - secs * 1000;
  const state = retryStateOf(task, nowMs);
  if (!state) return null;

  const countdown = state.kind === 'waiting' ? fmtCountdown(state.secs) : '';
  const vars =
    state.kind === 'gaveUp' ? undefined : { n: state.n, max: state.max ?? 0, countdown };
  const full = t(fullKey(state), vars);

  if (form === 'full') {
    // dir="auto" against the tooltip field's own dir="ltr", which is there for
    // the absolute date underneath: this line is translated prose and has to
    // read the way its own language reads.
    return (
      <span dir="auto" className="glim-num block">
        {full}
      </span>
    );
  }

  // "2/3 · 4:12" while there is something to count, and the whole short
  // sentence otherwise - "no retries left" and "will not be tried again" are
  // already short, and abbreviating a verdict is how a cell stops meaning
  // anything. Same size, colour, truncation and native title as the two notes
  // this cell already carries beside the pill, because a third grey line that
  // behaved differently would read as a different kind of fact.
  const compact =
    state.kind === 'waiting'
      ? t(state.max ? 'task.retry.compact' : 'task.retry.compactNoMax', vars)
      : full;
  return (
    <span className="glim-num min-w-0 truncate text-[11px] text-carbon-textMuted" title={full}>
      {compact}
    </span>
  );
}

/**
 * RetrySkipBadge is the wait's own exit, on the row that is waiting.
 *
 * It is restartTasks, unchanged, which is the identical call the server's own
 * timer would have made when the deadline arrived - so this does what the timer
 * was going to do, no more and no less. It does NOT hand the link a fresh
 * budget: the attempts already spent stay spent and the next failure carries on
 * up the ladder. That is the whole reason it is a separate control from the
 * Restart badge one place to its right rather than a rename of it, and the
 * reason it is only offered while something is actually being waited for.
 *
 * The name and the bubble deliberately differ here, where every other badge in
 * the row strip passes one string to both. The name has to be a verb short
 * enough to be a name; the thing a person needs before pressing a button on a
 * red row is that the counter keeps running, and a bubble is where this app
 * puts an explanation.
 */
export function RetrySkipBadge({
  task,
  base,
  focusable,
}: {
  task: Task;
  base: string;
  /** Whether the row this badge sits in owns the list's tab stop. The row strip
   *  is inside a roving-tabindex tree (components/listKeyboard.ts): without
   *  this, Tab walks the skip badge of every drawn row and the one-stop list is
   *  decorative. Not optional, so a second call site cannot forget it. */
  focusable: boolean;
}) {
  const { t } = useT();
  const { toast } = useToast();
  if (!retryPending(task)) return null;
  return (
    <IconBadge
      tabIndex={focusable ? 0 : -1}
      // The slot's own hue, not the row's: every row's skip badge is the same
      // colour and it is the one the folder badge used to hold, immediately
      // before Restart's. No `labelled` - the row strip deliberately opts out
      // of the Beschriftung setting, because a badge that can grow into a
      // labelled button inside a table row starts setting the width of a
      // column.
      hue={2}
      icon={<IconBolt width={16} height={16} />}
      title={t('task.retry.skipDetail')}
      aria-label={t('task.retry.skip')}
      onClick={() => {
        void restartTasks([task.id], base).then(
          (r) => {
            if (!r.ok) toast(t('task.retry.skipFailed'), 'fail');
          },
          // A request that never arrived, as opposed to one the server
          // refused. Both leave the row waiting exactly as it was, so both say
          // the same thing.
          () => toast(t('task.retry.skipFailed'), 'fail'),
        );
      }}
    />
  );
}
