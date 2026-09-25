// Extraction jobs: the stream that feeds the status column, and the context
// menu group that starts and stops them.
import { useEffect, useState } from 'react';
import {
  type ExtractJob,
  type Task,
  abortExtraction,
  apiBase,
  connectWS,
  fetchExtractJobs,
  startExtraction,
} from '../lib/api';
import { useT } from '../lib/i18n';
import { useToast } from '../lib/toast';
import { type MenuGroup } from './ContextMenu';
import { IconArchive, IconPlay, IconStop } from '../lib/icons';

const live = (j: ExtractJob) => j.status === 'queued' || j.status === 'running';

const partsOf = (j: ExtractJob): string[] => j.parts ?? [j.taskId];

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
        // The server greets every connection with a snapshot, a reconnect
        // included. Loading the jobs again then catches up on one that ended
        // while the socket was down, which would otherwise read "Unpacking 45%"
        // on its rows until the page is reloaded.
        if (type === 'snapshot') {
          void load();
          return;
        }
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
 * extractionsByTask maps every file of an archive to the latest unpacking of
 * it, which is what that file's row reports. The jobs arrive oldest first, so a
 * retry replaces the failure before it. A cancelled job hands the rows back to
 * their own status, as the server hands the task back to done.
 */
export function extractionsByTask(jobs: ExtractJob[]): Map<string, ExtractJob> {
  const out = new Map<string, ExtractJob>();
  for (const j of jobs) {
    for (const id of partsOf(j)) {
      if (j.status === 'cancelled') out.delete(id);
      else out.set(id, j);
    }
  }
  return out;
}

/**
 * useArchiveMenu is the archive group for the list's context menu. "Unpack
 * now" is offered on any finished download, since only the server can tell an
 * archive by its magic bytes. "Stop unpacking" is offered on every part of a
 * set being unpacked, not only on the one the job started on.
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

  const ids = new Set(chosen.map((x) => x.id));
  const running = jobs.filter((j) => live(j) && partsOf(j).some((id) => ids.has(id)));
  // The other parts of a set being unpacked read as done, and the server would
  // refuse them with "not finished" while it works on their first volume.
  const busy = new Set(running.flatMap(partsOf));
  const finished = chosen.filter((x) => x.status === 'done' && !busy.has(x.id));
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
