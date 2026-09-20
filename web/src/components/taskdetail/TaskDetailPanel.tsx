import { useEffect, useState } from 'react';
import { isLocalBase, taskFileHead, type Task, type TaskFileHead } from '../../lib/api';
import { useT } from '../../lib/i18n';
import { reachable as taskFileReachable } from '../FileActions';
import { FailureCard } from './FailureCard';
import { LinkCard } from './LinkCard';
import { LogCard } from './LogCard';
import { PlayerCard } from './PlayerCard';
import { RulesCard } from './RulesCard';
import { TimesCard } from './TimesCard';

/**
 * TaskDetailPanel shows everything known about one task, read-only, below the
 * list. The caller passes a single row only, since fields like the error have
 * no single answer across a package.
 *
 * It has no key, so live task updates re-render it in place instead of tearing
 * down a playing <video>. It has to stay below the table: useRowWindow re-slices
 * whenever the row strip moves, and a panel above it would move the strip each
 * time a card grows.
 */
export function TaskDetailPanel({ task, base, hue }: { task: Task; base: string; hue?: number }) {
  const { t } = useT();
  const head = useTaskFileHead(task, base);
  // The hue sequence carries on from the list card above.
  const at = (n: number) => (hue === undefined ? undefined : hue + n);

  return (
    // Stops the list area's context menu so the browser's own menu can copy
    // text. Focus is not moved here on open, which would scroll away from the
    // row that was just double-clicked.
    <section
      aria-label={t('detail.label')}
      onContextMenu={(e) => e.stopPropagation()}
      className="flex flex-col gap-4"
    >
      <LinkCard task={task} base={base} head={head} hue={at(0)} />
      <TimesCard task={task} hue={at(1)} />
      <FailureCard task={task} hue={at(2)} />
      <RulesCard task={task} hue={at(3)} />
      <PlayerCard task={task} base={base} head={head} hue={at(4)} />
      <LogCard task={task} base={base} hue={at(5)} />
    </section>
  );
}

/**
 * useTaskFileHead asks GET /api/tasks/{id}/file once with HEAD, shared by the
 * link and player cards. It skips peers, whose proxy reads up to 32 MB of the
 * reply, and links with no file yet.
 *
 * The effect depends on the id rather than the task, because useTasks replaces
 * the task object on every broadcast.
 */
function useTaskFileHead(task: Task, base: string): TaskFileHead | null {
  const [head, setHead] = useState<TaskFileHead | null>(null);
  const worth = isLocalBase(base) && taskFileReachable(task);

  useEffect(() => {
    if (!worth) {
      setHead(null);
      return;
    }
    let live = true;
    void taskFileHead(task.id, base).then(
      (h) => {
        if (live) setHead(h);
      },
      () => {
        if (live) setHead(null);
      },
    );
    return () => {
      live = false;
    };
  }, [task.id, base, worth]);

  return head;
}
