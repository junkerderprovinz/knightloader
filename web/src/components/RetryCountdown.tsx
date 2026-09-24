// What a failed row is waiting for, and the control that retries it now. The
// status cell, row tooltip, hover strip and context menu all read the state
// through retryStateOf. A restart never resets Retries or NextTry
// (app.RestartTasksIn), so the backoff continues from the attempts spent.
import { restartTasks, type Task } from '../lib/api';
import { fmtCountdown, goTimeMs, useCountdown } from '../lib/countdown';
import { useT, type TranslationKey } from '../lib/i18n';
import { IconBolt } from '../lib/icons';
import { useToast } from '../lib/toast';
import { IconBadge, useTooltip } from './ui';

// How long a passed deadline still reads as "due", covering the moment before
// the requeue arrives over the socket. It also retires a stale deadline from a
// peer that restarted without clearing NextTry.
const GRACE_MS = 60_000;

/**
 * RetryState is what a failed row is waiting for, or null. Each state has a
 * different remedy: only "exhausted" is fixed by allowing more retries, and
 * "gaveUp" by none.
 */
export type RetryState =
  | { kind: 'waiting'; n: number; max?: number; secs: number }
  | { kind: 'due'; n: number; max?: number }
  | { kind: 'exhausted'; n: number; max?: number }
  | { kind: 'gaveUp' }
  | null;

// deadlineOf checks the status first, because a requeued task keeps the
// nextTry of its last failure.
function deadlineOf(task: Task): number | null {
  if (task.status !== 'error') return null;
  return goTimeMs(task.nextTry);
}

/**
 * retryStateOf reads a failed row's retry state. It is pure and takes `nowMs`,
 * so the ticking component owns the clock.
 *
 * gaveUp wins first, since those failures are decisions. The deadline comes
 * next and is authoritative, because a backend delay or a reconnect can move
 * it off the backoff ladder. With no live deadline, spent retries mean
 * exhausted.
 */
export function retryStateOf(task: Task, nowMs: number): RetryState {
  if (task.status !== 'error') return null;
  if (task.gaveUp) return { kind: 'gaveUp' };
  // Retries, not attempts: with the default of three a row passes 1, 2, 3 and
  // the fourth failure settles, so the strings use the setting's own word.
  const n = task.retries ?? 0;
  // Zero means unknown (core.Task.MaxTries), so no denominator is shown.
  const max = task.maxTries && task.maxTries > 0 ? task.maxTries : undefined;
  const deadline = deadlineOf(task);
  if (deadline !== null) {
    const secs = Math.round((deadline - nowMs) / 1000);
    if (secs > 0) return { kind: 'waiting', n, max, secs };
    if (nowMs - deadline <= GRACE_MS) return { kind: 'due', n, max };
  }
  if (n > 0) return { kind: 'exhausted', n, max };
  return null;
}

/**
 * retryPending reports a row waiting on a scheduled retry, which is when both
 * skip controls are offered, in step with the countdown.
 */
export function retryPending(task: Task): boolean {
  return retryStateOf(task, Date.now())?.kind === 'waiting';
}

// The NoMax variants are for rows whose ceiling the server never resolved.
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
 * RetryNote renders the retry state and is the only part that ticks. The
 * interval stays in this leaf, since ticking higher up would re-run the list's
 * row measuring every second. The compact form fits the status cell; the full
 * sentence goes in the row tooltip.
 */
export function RetryNote({ task, form }: { task: Task; form: 'compact' | 'full' }) {
  const { t } = useT();
  const deadline = deadlineOf(task);
  const secs = useCountdown(deadline, GRACE_MS);
  // Derived from the same tick, so words and number never disagree.
  const nowMs = deadline === null || secs === null ? Date.now() : deadline - secs * 1000;
  const state = retryStateOf(task, nowMs);
  if (!state) return null;

  const countdown = state.kind === 'waiting' ? fmtCountdown(state.secs) : '';
  const vars =
    state.kind === 'gaveUp' ? undefined : { n: state.n, max: state.max ?? 0, countdown };
  const full = t(fullKey(state), vars);

  if (form === 'full') {
    // Translated prose, overriding the tooltip field's dir="ltr".
    return (
      <span dir="auto" className="glim-num block">
        {full}
      </span>
    );
  }

  // "2/3 · 4:12" while counting down; the verdicts are short enough already.
  // Styled like the cell's other notes.
  const compact =
    state.kind === 'waiting'
      ? t(state.max ? 'task.retry.compact' : 'task.retry.compactNoMax', vars)
      : full;
  return <CompactNote text={compact} full={full} />;
}

/**
 * CompactNote is the short form with the whole sentence in the house bubble,
 * for where the cell truncates it, like columns.tsx's Tip beside it. No role
 * and no tab stop: the row owns the focus model, and the whole sentence is in
 * the row tooltip for a screen reader.
 */
function CompactNote({ text, full }: { text: string; full: string }) {
  const tip = useTooltip<HTMLSpanElement>(full);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <span className="glim-num min-w-0 truncate text-[11px] text-carbon-textMuted" {...tipHoverProps}>
        {text}
      </span>
      {tip.node}
    </>
  );
}

/**
 * RetrySkipBadge retries a waiting row now, with the same restartTasks call the
 * server's timer would make. Spent attempts stay spent. Its bubble explains
 * that, so it differs from the short aria-label.
 */
export function RetrySkipBadge({
  task,
  base,
  focusable,
}: {
  task: Task;
  base: string;
  /** Whether the row owns the list's roving tab stop (listKeyboard.ts). */
  focusable: boolean;
}) {
  const { t } = useT();
  const { toast } = useToast();
  if (!retryPending(task)) return null;
  return (
    <IconBadge
      tabIndex={focusable ? 0 : -1}
      // A fixed slot hue. No `labelled`: a label would widen the table column.
      hue={2}
      icon={<IconBolt width={16} height={16} />}
      title={t('task.retry.skipDetail')}
      aria-label={t('task.retry.skip')}
      onClick={() => {
        void restartTasks([task.id], base).then(
          (r) => {
            if (!r.ok) toast(t('task.retry.skipFailed'), 'fail');
          },
          () => toast(t('task.retry.skipFailed'), 'fail'),
        );
      }}
    />
  );
}
