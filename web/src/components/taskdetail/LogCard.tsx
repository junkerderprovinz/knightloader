import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { fetchTaskLog, isLocalBase, type LogLine, type Task } from '../../lib/api';
import { useT } from '../../lib/i18n';
import { Button, Card, EmptyState, SectionTitle } from '../ui';

/**
 * LogCard shows the instance's log lines about one download. It is usually
 * empty, since few log call sites name their download, and the card says so.
 *
 * A peer's download gets no fetch: /api/diagnostics is not forwarded by the
 * relay or the federation proxy, because a log line can carry a feed URL with
 * an indexer key.
 */
export function LogCard({ task, base, hue }: { task: Task; base: string; hue?: number }) {
  const { t } = useT();
  const navigate = useNavigate();
  const local = isLocalBase(base);
  const [lines, setLines] = useState<LogLine[] | null>(null);

  // Keyed on the id, not the task: useTasks replaces the object on every broadcast.
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
        // Unreachable is not the same as empty.
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
        <span className="text-sm text-carbon-textMuted">{t('common.loading')}</span>
      ) : lines.length === 0 ? (
        <EmptyState nested title={t('detail.log.empty')} hint={t('detail.log.emptyHint')} />
      ) : (
        // Log lines mix paths, hosts and stack traces, none of which reads mirrored.
        <pre
          dir="ltr"
          className="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-[var(--radius-control)]
            bg-carbon-surface2 p-4 font-mono text-[11px] leading-relaxed text-carbon-textSub"
        >
          {lines.map((l) => l.line).join('\n')}
        </pre>
      )}

      {local && (
        <Button kind="secondary" className="self-start" onClick={() => navigate('/settings/diagnostics')}>
          {t('detail.log.openDiagnostics')}
        </Button>
      )}
    </Card>
  );
}
