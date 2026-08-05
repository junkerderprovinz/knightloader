import type { TaskStatus } from '../lib/api';

type Tone = 'ok' | 'fail' | 'warn' | 'info' | 'neutral';

const statusTone: Record<TaskStatus, { tone: Tone; label: string }> = {
  queued: { tone: 'neutral', label: 'Queued' },
  running: { tone: 'info', label: 'Running' },
  paused: { tone: 'warn', label: 'Paused' },
  extracting: { tone: 'info', label: 'Extracting' },
  done: { tone: 'ok', label: 'Done' },
  error: { tone: 'fail', label: 'Error' },
};

const toneClass: Record<Tone, string> = {
  ok: 'text-statusOk bg-statusOkBg',
  fail: 'text-statusFail bg-statusFailBg',
  warn: 'text-statusWarn bg-statusWarnBg',
  info: 'text-statusInfo bg-statusInfoBg',
  neutral: 'text-statusNeutral bg-statusNeutralBg',
};

export function StatusPill({ status }: { status: TaskStatus }) {
  const s = statusTone[status] ?? statusTone.queued;
  return (
    <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-medium ${toneClass[s.tone]}`}>
      {s.label}
    </span>
  );
}

// ResolverBadge names which backend handles a task (direct/torbox/ytdlp/jd).
const resolverLabel: Record<string, string> = {
  direct: 'Direct',
  torbox: 'TorBox',
  ytdlp: 'yt-dlp',
  jd: 'JDownloader',
};

export function ResolverBadge({ resolver }: { resolver: string }) {
  return (
    <span className="inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-carbon-textSub bg-carbon-surface2">
      {resolverLabel[resolver] ?? resolver}
    </span>
  );
}
