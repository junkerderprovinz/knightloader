import { useEffect, useState } from 'react';
import { isLocalBase, taskFileHead, type Task, type TaskFileHead } from '../../lib/api';
import { useT } from '../../lib/i18n';
import { reachable as taskFileReachable } from '../FileActions';
import { FailureCard } from './FailureCard';
import { LinkCard } from './LinkCard';
import { PlayerCard } from './PlayerCard';
import { RulesCard } from './RulesCard';
import { TimesCard } from './TimesCard';

/**
 * Everything the app knows about ONE link, read-only, under the list.
 *
 * It opens on the same double-click that already opens the properties card and
 * sits below it, because the two answer opposite questions: the properties
 * card is what can still be changed about a selection, and this is what is
 * already true about one row. Neither replaces the other, and neither replaces
 * the right-click options dialog.
 *
 * ONE ROW ONLY, and the caller enforces it. Double-clicking a package header
 * selects every link in the package, which the properties card is built for
 * and this is not: there is no honest single answer for "the error", "the next
 * attempt" or "the file" across eleven links, and a panel that showed the
 * first one's would be quietly wrong rather than visibly empty.
 *
 * FIVE CARDS AND NOT ONE, because a Card carries at most one SectionTitle and
 * because the settings pages already establish one card per file for exactly
 * this shape. Three of them draw nothing when they have nothing: a link in the
 * collector has no failure and no file, and five cards of blank rows read as a
 * page that broke.
 *
 * NO KEY ON THIS COMPONENT, deliberately, and the opposite of the properties
 * card's own keying. That one snapshots its boxes at mount and so has to be
 * torn down when the selection changes; this one has to re-render live off the
 * task object the WebSocket replaces on every tick, so that the retry count,
 * the error sentence and the timestamps are current. A key derived from
 * anything that moves would tear the <video> down once a second.
 *
 * IT MUST BE RENDERED BELOW THE TABLE. useRowWindow's layout effect measures
 * the row strip after every commit and recomputes the visible slice whenever
 * it has moved by a pixel; anything above the strip that grows when an error
 * string arrives, or reserves space when a video loads its metadata, repaints
 * the whole windowed list each time it does. Below it, none of that happens.
 * It must never go inside a row: measureRows caches heights per row key, and a
 * row whose height depends on a media element that loads asynchronously
 * poisons that cache and all the scroll arithmetic built on it.
 */
export function TaskDetailPanel({ task, base, hue }: { task: Task; base: string; hue?: number }) {
  const { t } = useT();
  const head = useTaskFileHead(task, base);
  // The cards run on from whatever position the list card above them holds, so
  // that the rainbow reads as one sequence down the page rather than restarting
  // under the table. Undefined stays undefined: `.glim-hue` without the
  // variables resolves the accent to nothing.
  const at = (n: number) => (hue === undefined ? undefined : hue + n);

  return (
    // The right-click belongs to the browser in here. The page above this puts
    // a context menu on the whole list area and calls preventDefault on every
    // reading of it, so without this line right-clicking the address to copy
    // the link opens the download list's own menu instead. This panel is
    // nothing but text people will want to right-click, which makes it the one
    // place that line must not be forgotten.
    //
    // A <section> with a name is already role="region". Focus is deliberately
    // NOT moved here when it opens: the panel is opened by a double-click on a
    // row, and pulling focus down would scroll the page off the row somebody
    // just clicked, which on a windowed list repaints the slice as well. It
    // comes after the table in the document, so Tab reaches it in order.
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
    </section>
  );
}

/**
 * What GET /api/tasks/{id}/file would answer, asked once with HEAD.
 *
 * It lives in the shell rather than in the player because two cards want the
 * same answer: the link card prints the bytes on disk and the player gates on
 * whether there is a file at all. Two probes would be two requests for one
 * fact, each costing the server a pair of symlink resolutions and a stat
 * inside SafeTaskFile.
 *
 * TRAP, and it is the one that turns a panel into a load test: useTasks
 * replaces the whole task object on every 'task' broadcast, and a running
 * download broadcasts constantly. An effect that depended on `task` would
 * therefore fire several times a second for as long as the panel is open. The
 * dependencies are the id, the base, and one boolean that flips at most once
 * over a task's life, and nothing else.
 *
 * Not asked at all unless it is worth asking. A peer instance answers this
 * route through the federation proxy, which reads up to 32 MB of the reply
 * into memory before handing it back, so probing one is expensive and its
 * answer is a lie either way; and a link with no resolved name has nothing on
 * disk to ask about.
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
        // A probe that could not be made says nothing, which is exactly the
        // same state as one that has not been made yet: no size on the link
        // card, no player offered. Guessing "yes" here would put a play
        // button in front of a file nobody has confirmed is there.
        if (live) setHead(null);
      },
    );
    return () => {
      live = false;
    };
  }, [task.id, base, worth]);

  return head;
}
