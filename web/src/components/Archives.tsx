// Extraction jobs on screen, with their own progress, failure and stop button,
// plus the context menu group that starts and stops them.
import { useCallback, useEffect, useState } from 'react';
import {
  type ExtractJob,
  type Task,
  abortExtraction,
  apiBase,
  connectWS,
  fetchExtractJobs,
  startExtraction,
} from '../lib/api';
import { fmtBytes } from '../lib/format';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { Button, Card, SectionTitle } from './ui';
import { ProgressBar } from './ProgressBar';
import { type MenuGroup } from './ContextMenu';
import { IconArchive, IconClose, IconPlay, IconStop } from '../lib/icons';

const live = (j: ExtractJob) => j.status === 'queued' || j.status === 'running';

/**
 * useExtractJobs streams an instance's extraction jobs, pushed over the
 * WebSocket locally and polled from a peer, like useTasks.
 */
export function useExtractJobs(instance: string): ExtractJob[] {
  const [jobs, setJobs] = useState<ExtractJob[]>([]);
  useEffect(() => {
    const base = apiBase(instance);
    setJobs([]);
    const load = () => fetchExtractJobs(base).then(setJobs).catch(() => setJobs([]));
    void load();
    if (instance) {
      const iv = setInterval(() => void load(), 2000);
      return () => clearInterval(iv);
    }
    return connectWS(
      (type, data) => {
        if (type !== 'extract') return;
        const j = data as ExtractJob;
        setJobs((prev) => {
          const i = prev.findIndex((x) => x.id === j.id);
          if (i < 0) return [...prev, j];
          const next = prev.slice();
          next[i] = j;
          return next;
        });
      },
      ['extract'],
    );
  }, [instance]);
  return jobs;
}

/**
 * useArchiveMenu is the archive group for the list's context menu. "Unpack
 * now" is offered on any finished download, since only the server can tell an
 * archive by its magic bytes.
 */
export function useArchiveMenu({
  chosen,
  base,
  jobs,
}: {
  chosen: Task[];
  base: string;
  jobs: ExtractJob[];
}): MenuGroup[] {
  const { t } = useT();
  const { toast } = useToast();

  const finished = chosen.filter((x) => x.status === 'done');
  const running = jobs.filter((j) => live(j) && chosen.some((x) => x.id === j.taskId));
  if (finished.length === 0 && running.length === 0) return [];

  const items = [];
  if (finished.length > 0) {
    items.push({
      id: 'unpack',
      label: t('archive.unpackNow'),
      detail: finished.length > 1 ? String(finished.length) : undefined,
      icon: <IconPlay />,
      onSelect: () => {
        void startExtraction(
          finished.map((x) => x.id),
          base,
        ).catch((e: unknown) => toast(String(e instanceof Error ? e.message : e), 'fail', 'extraction-failed'));
      },
    });
  }
  if (running.length > 0) {
    items.push({
      id: 'abort',
      label: t('archive.stop'),
      icon: <IconStop />,
      onSelect: () => {
        for (const j of running) {
          void abortExtraction(j.id, base).catch((e: unknown) =>
            toast(String(e instanceof Error ? e.message : e), 'fail', 'extraction-failed'),
          );
        }
      },
    });
  }

  // A submenu, like the queue and clean-up entries, to keep the top level short.
  return [
    {
      id: 'archive',
      items: [
        {
          id: 'archive',
          label: t('archive.menu'),
          icon: <IconArchive width={14} height={14} />,
          submenu: [{ id: 'verbs', items }],
        },
      ],
    },
  ];
}

/**
 * ArchiveJobs lists running and failed extractions. A job that finished
 * cleanly needs nothing more and is left out.
 */
export function ArchiveJobs({ jobs, base }: { jobs: ExtractJob[]; base: string }) {
  const { t } = useT();
  const { toast } = useToast();
  const stop = useCallback(
    (id: string) => {
      void abortExtraction(id, base).catch((e: unknown) =>
        toast(String(e instanceof Error ? e.message : e), 'fail', 'extraction-failed'),
      );
    },
    [base, toast],
  );

  const shown = jobs.filter((j) => live(j) || j.status === 'error');
  if (shown.length === 0) return null;

  return (
    <Card className="flex flex-col gap-4">
      <SectionTitle>{t('archive.title')}</SectionTitle>
      <div className="flex flex-col gap-4">
        {shown.map((j) => (
          <div key={j.id} className="flex flex-col gap-1.5">
            <div className="flex items-baseline gap-3">
              <span className="min-w-0 flex-1 truncate text-[12px] text-carbon-text" dir="ltr">
                {j.name}
              </span>
              {/* The archive open now, which can be one nested in the output. */}
              {j.archive && j.archive !== j.name && (
                <span className="glim-num shrink-0 text-[11px] text-carbon-textMuted" dir="ltr">
                  {j.archive}
                </span>
              )}
              <span className="glim-num shrink-0 text-[11px] text-carbon-textMuted">
                {t('archive.progress', { files: j.files, bytes: fmtBytes(j.bytes) })}
              </span>
              <Button
                kind="ghost"
                className="px-1.5"
                title={t('archive.stop')}
                icon={<IconClose width={14} height={14} />}
                onClick={() => stop(j.id)}
              />
            </div>
            {/* Indeterminate: nobody knows an archive's unpacked size in advance. */}
            <ProgressBar percent={0} active={j.status === 'running'} indeterminate />
            <div className="flex items-baseline gap-2 text-[11px]">
              <span className="text-carbon-textMuted">
                {t(j.status === 'queued' ? 'archive.queued' : j.status === 'error' ? 'archive.failed' : 'archive.running')}
              </span>
              {j.volumes > 1 && (
                <span className="glim-num text-carbon-textMuted">
                  {t('archive.volumes', { volumes: j.volumes })}
                </span>
              )}
              {/* Phrased as the next step rather than as the error. */}
              {j.password && <span className="text-statusFail">{t('archive.needsPassword')}</span>}
              {j.error && !j.password && <span className="text-statusFail">{j.error}</span>}
            </div>
          </div>
        ))}
      </div>
    </Card>
  );
}
