// Unpacking, on screen: the jobs that are running or lately finished, and the
// two verbs that act on them.
//
// An extraction used to be a word the download wore for a while. That hid two
// things people need: how far it has got - a forty-gigabyte set takes longer to
// unpack than it took to fetch - and the fact that it can fail on its own, after
// the download succeeded. Here it is an object with its own progress, its own
// reason for stopping, and its own stop button.
//
// The menu entries are a GROUP handed to the existing context menu, never a
// second menu system: the shell already knows where to sit, when to close and
// how to be walked with the keyboard, and a second one would learn all three
// again and get one of them wrong.
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

/** live is a job that is still going to do something. */
const live = (j: ExtractJob) => j.status === 'queued' || j.status === 'running';

/**
 * useExtractJobs streams the unpacking jobs for an instance.
 *
 * The same shape as useTasks and for the same reason: this instance is pushed
 * over the WebSocket, a named peer is polled. A job carries no total - nothing
 * knows how many bytes an archive will become until it has become them - so what
 * arrives is a counter and not a percentage, and the bar below says so by being
 * indeterminate rather than by inventing a denominator.
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
 * useArchiveMenu is the archive group for the list's context menu.
 *
 * "Unpack now" is offered on any finished download rather than only on ones the
 * app recognises as archives, and the refusal comes back as a sentence naming
 * the file. Hiding it would mean deciding here what counts as an archive - a
 * second copy of a judgement the server makes from the magic bytes, which will
 * disagree with it the first time somebody renames a .rar to .bin.
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

  // Start and stop as a transport pair, the same two glyphs the list's own
  // menu uses one group above for a download: unpacking is a job that runs and
  // can be called off, and it is the same act to a reader either way.
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
      danger: true,
      onSelect: () => {
        for (const j of running) {
          void abortExtraction(j.id, base).catch((e: unknown) =>
            toast(String(e instanceof Error ? e.message : e), 'fail', 'extraction-failed'),
          );
        }
      },
    });
  }

  // One word in the menu with the verbs behind it, the way the queue and the
  // clean-up entries already do it. Two more entries in a menu that has a dozen
  // is where the ones that act on the selection start getting lost.
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
 * ArchiveJobs shows the unpacking that is happening now, and the ones that
 * stopped without finishing.
 *
 * A job that finished cleanly is deliberately not listed: it has nothing left to
 * say, the files are in the folder, and a card that grows a row for every archive
 * ever opened is a log nobody asked for. What stays is what somebody still has to
 * do something about.
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
              <span className="min-w-0 flex-1 truncate text-[13px] text-carbon-text" dir="ltr">
                {j.name}
              </span>
              {/* The file open right now, which at depth is one found inside the
                  output rather than the one the job is named after. */}
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
              {/* The one failure with an obvious next step, so it is said as the
                  step and not as the error: type a password in and press start. */}
              {j.password && <span className="text-statusFail">{t('archive.needsPassword')}</span>}
              {j.error && !j.password && <span className="text-statusFail">{j.error}</span>}
            </div>
          </div>
        ))}
      </div>
    </Card>
  );
}
