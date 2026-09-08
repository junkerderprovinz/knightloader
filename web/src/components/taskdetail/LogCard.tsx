import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { fetchTaskLog, isLocalBase, type LogLine, type Task } from '../../lib/api';
import { useT } from '../../lib/i18n';
import { Card, EmptyState, SectionTitle } from '../ui';

/**
 * What this instance has said in its own log about THIS download.
 *
 * IT IS USUALLY EMPTY, AND THE CARD SAYS SO OUT LOUD. Seven of the app's log
 * call sites record which download they are about; the other hundred and forty
 * odd do not, so a download can run into real trouble without a single line
 * appearing here. A card that showed an empty list with no explanation is a
 * card people report as broken, and they would be half right: the emptiness is
 * a fact about the logging and not about the download.
 *
 * THE LOG IS NOT FETCHED FOR A PEER'S DOWNLOAD, and that is a decision rather
 * than a gap. The route this reads sits under /api/diagnostics, which neither
 * the relay nor the federation proxy forwards - deliberately, because a log
 * line can carry a feed URL with an indexer's key in its query string, and the
 * two forwarding allowlists were reasoned about the task LIST, which carries
 * nothing of the sort. So a task running on another box gets the sentence
 * saying where its log actually is, rather than a spinner that never resolves
 * or an empty card that reads as "nothing happened".
 *
 * IT ASKS ONCE PER DOWNLOAD AND NOT ON EVERY TICK. useTasks replaces the whole
 * task object on every broadcast and a running download broadcasts constantly,
 * so the effect depends on the id and the base and on nothing that moves - the
 * same trap the panel's own file probe documents one file over.
 */
export function LogCard({ task, base, hue }: { task: Task; base: string; hue?: number }) {
  const { t } = useT();
  const local = isLocalBase(base);
  const [lines, setLines] = useState<LogLine[] | null>(null);

  useEffect(() => {
    if (!local) {
      setLines(null);
      return;
    }
    let live = true;
    void fetchTaskLog(task.id).then(
      (log) => {
        if (live) setLines(log.lines);
      },
      () => {
        // A request that could not be made says nothing, which is the same
        // state as one that has not been made yet. Guessing "no lines" here
        // would put the honest empty sentence in front of somebody whose log
        // was simply not reachable.
        if (live) setLines(null);
      },
    );
    return () => {
      live = false;
    };
  }, [task.id, local]);

  return (
    <Card hue={hue} className="flex flex-col gap-4">
      <SectionTitle hint={t('detail.log.hint')}>{t('detail.log.title')}</SectionTitle>

      {!local ? (
        <span className="text-sm text-carbon-textMuted">{t('detail.log.remote')}</span>
      ) : lines === null ? (
        // Nothing to draw yet, and nothing to apologise for either: the panel
        // opens on a double-click and this answers out of memory, so the state
        // lasts a frame.
        <span className="text-sm text-carbon-textMuted">{t('common.loading')}</span>
      ) : lines.length === 0 ? (
        // Kept as a real card even when it is empty, exactly as the rules card
        // above it is. `nested`, because this sits inside a Card already.
        <EmptyState nested title={t('detail.log.empty')} hint={t('detail.log.emptyHint')} />
      ) : (
        // ltr regardless of interface direction, the convention every other
        // path, URL and code cell in this app uses: log lines mix paths, hosts
        // and stack traces, none of which reads correctly mirrored.
        <pre
          dir="ltr"
          className="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-[var(--radius-control)]
            bg-carbon-surface2 p-4 font-mono text-[11px] leading-relaxed text-carbon-textSub"
        >
          {lines.map((l) => l.line).join('\n')}
        </pre>
      )}

      {local && (
        <Link
          to="/settings/diagnostics"
          className="self-start rounded-[var(--radius-control)] px-2 py-1 text-[11px] text-carbon-textMuted
            transition-colors hover:bg-carbon-hover hover:text-carbon-text"
        >
          {t('detail.log.openDiagnostics')}
        </Link>
      )}
    </Card>
  );
}
