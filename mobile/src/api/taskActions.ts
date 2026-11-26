import type { TranslationKey } from '../i18n/en';
import type { Task, TaskStatus } from './types';

/**
 * What a link's or a package's More sheet offers, and what each entry sends.
 *
 * The entries and the states that gate them are the web interface's right-click
 * menu (web/src/components/ListToolbar.tsx), so a phone and a browser looking
 * at the same list offer the same verbs. Only types are imported, which lets
 * check-task-actions.mjs run this file as it is.
 */

/** One of the seven priorities GET /api/queue/priorities lists, highest first. */
export interface PriorityChoice {
  id: string;
  value: number;
}

export type ActionId =
  | 'force'
  | 'unforce'
  | 'pause'
  | 'resume'
  | 'retryNow'
  | 'restart'
  | 'recheck'
  | 'moveTop'
  | 'moveBottom'
  | 'priority'
  | 'remove';

/** An entry a selection is offered, and the links it acts on. */
export interface Offer {
  id: ActionId;
  ids: string[];
}

/** A request an entry makes, relative to the instance's base path. Every one is a POST. */
export interface Call {
  path: string;
  body: unknown;
}

export const ACTION_LABELS: Record<ActionId, TranslationKey> = {
  force: 'actions.forceStart',
  unforce: 'actions.unforce',
  pause: 'actions.pause',
  resume: 'actions.resume',
  retryNow: 'actions.retryNow',
  restart: 'actions.restart',
  recheck: 'actions.recheck',
  moveTop: 'actions.moveTop',
  moveBottom: 'actions.moveBottom',
  priority: 'actions.priority',
  remove: 'actions.remove',
};

/** The words for the ids the instance names its priorities by. */
export const PRIORITY_KEYS: Record<string, TranslationKey> = {
  highest: 'priority.highest',
  higher: 'priority.higher',
  high: 'priority.high',
  default: 'priority.default',
  low: 'priority.low',
  lower: 'priority.lower',
  lowest: 'priority.lowest',
};

/** The links still waiting for their turn. A running one has no turn left to
 *  skip, and a failed one needs a reset. */
export const FORCE_STATES: readonly TaskStatus[] = ['collected', 'queued', 'paused'];

/** Every status but done and error, which have no place left in the wait order. */
export const MOVE_STATES: readonly TaskStatus[] = ['collected', 'queued', 'running', 'paused', 'extracting'];

/** Every status: on a finished or failed link the priority counts after a reset. */
export const PRIORITY_STATES: readonly TaskStatus[] = [
  'collected',
  'queued',
  'running',
  'paused',
  'extracting',
  'done',
  'error',
];

/** The year Go's zero time falls in, which the instance sends for "no retry". */
const GO_ZERO_YEAR = 1;

/** retryPending reports a failed link whose next automatic try is still ahead. */
export function retryPending(task: Task, now: number): boolean {
  if (task.status !== 'error' || !task.nextTry) return false;
  const at = new Date(task.nextTry);
  return at.getUTCFullYear() > GO_ZERO_YEAR && at.getTime() > now;
}

/** The priority every link of a selection has, if they agree on one. */
export function sharedPriority(chosen: Task[]): number | undefined {
  const first = chosen[0]?.priority;
  return chosen.every((x) => x.priority === first) ? first : undefined;
}

/**
 * offeredActions lists what a selection is offered, in groups: what runs, where
 * it waits, and removal, which is always last on its own. An entry that applies
 * to none of the links is left out.
 *
 * `settled` is a part below the list, finished packages or torrents, where only
 * a reset and a removal still mean something. `priorities` says whether the
 * instance named its priorities.
 */
export function offeredActions(
  chosen: Task[],
  { settled = false, priorities, now }: { settled?: boolean; priorities: boolean; now: number },
): Offer[][] {
  const all = chosen.map((x) => x.id);
  const some = (p: (x: Task) => boolean) => chosen.some(p);
  const idsOf = (p: (x: Task) => boolean) => chosen.filter(p).map((x) => x.id);
  const restart: Offer[] = some((x) => x.status === 'done' || x.status === 'error') ? [{ id: 'restart', ids: all }] : [];
  const remove: Offer[] = [{ id: 'remove', ids: all }];
  if (settled) return [restart, remove].filter((g) => g.length > 0);

  const run: Offer[] = [];
  if (some((x) => FORCE_STATES.includes(x.status))) run.push({ id: 'force', ids: all });
  if (some((x) => !!x.forced)) run.push({ id: 'unforce', ids: all });
  // Waiting links are paused too, or the slots the running ones free would
  // start them.
  const pausable = idsOf((x) => x.status === 'running' || x.status === 'queued');
  if (pausable.length > 0) run.push({ id: 'pause', ids: pausable });
  const resumable = idsOf((x) => x.status === 'paused');
  if (resumable.length > 0) run.push({ id: 'resume', ids: resumable });
  // Spends the retry already scheduled rather than granting a new one, so it
  // goes to the waiting links alone: a reset would also start finished ones over.
  const waiting = idsOf((x) => retryPending(x, now));
  if (waiting.length > 0) run.push({ id: 'retryNow', ids: waiting });
  run.push(...restart);
  // The instance asks the hosts about collected links only.
  const unchecked = idsOf((x) => x.status === 'collected');
  if (unchecked.length > 0) run.push({ id: 'recheck', ids: unchecked });

  const queue: Offer[] = [];
  if (some((x) => MOVE_STATES.includes(x.status))) queue.push({ id: 'moveTop', ids: all }, { id: 'moveBottom', ids: all });
  if (priorities && some((x) => PRIORITY_STATES.includes(x.status))) queue.push({ id: 'priority', ids: all });

  return [run, queue, remove].filter((g) => g.length > 0);
}

/** actionCall is the request an offer makes. `priority` is the value a priority entry picked. */
export function actionCall(offer: Offer, priority?: number): Call {
  const ids = offer.ids;
  switch (offer.id) {
    case 'force':
      return { path: '/queue/force', body: { ids } };
    case 'unforce':
      return { path: '/tasks/force', body: { ids, forced: false } };
    case 'pause':
      return { path: '/tasks/pause', body: { ids } };
    case 'resume':
      return { path: '/tasks/resume', body: { ids } };
    case 'retryNow':
    case 'restart':
      return { path: '/tasks/restart', body: { ids } };
    case 'recheck':
      return { path: '/tasks/recheck', body: { ids } };
    case 'moveTop':
      return { path: '/queue/move', body: { ids, where: 'top' } };
    case 'moveBottom':
      return { path: '/queue/move', body: { ids, where: 'bottom' } };
    case 'priority':
      return { path: '/queue/priority', body: { ids, priority } };
    // The rows only. Erasing what was downloaded is refused over the relay, and
    // a removal that keeps the files is one the instance can still undo.
    case 'remove':
      return { path: '/tasks/delete', body: { ids, files: false } };
  }
}

/** Resets every failed link of the instance: no ids means all of them. */
export const retryFailedCall: Call = { path: '/tasks/restart', body: { ids: [] } };

/** Puts back the rows the removal that handed out `token` took. */
export function undoCall(token: string): Call {
  return { path: '/tasks/undo-delete', body: { token } };
}
