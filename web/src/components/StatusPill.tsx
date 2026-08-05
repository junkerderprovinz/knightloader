import type { TaskStatus } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';

type Tone = 'ok' | 'fail' | 'warn' | 'info' | 'neutral';

const statusTone: Record<TaskStatus, { tone: Tone; key: TranslationKey }> = {
  collected: { tone: 'neutral', key: 'status.collected' },
  queued: { tone: 'neutral', key: 'status.queued' },
  running: { tone: 'info', key: 'status.running' },
  paused: { tone: 'warn', key: 'status.paused' },
  extracting: { tone: 'info', key: 'status.extracting' },
  done: { tone: 'ok', key: 'status.done' },
  error: { tone: 'fail', key: 'status.error' },
};

const toneClass: Record<Tone, string> = {
  ok: 'text-statusOk bg-statusOkBg',
  fail: 'text-statusFail bg-statusFailBg',
  warn: 'text-statusWarn bg-statusWarnBg',
  info: 'text-statusInfo bg-statusInfoBg',
  neutral: 'text-statusNeutral bg-statusNeutralBg',
};

export function StatusPill({ status }: { status: TaskStatus }) {
  const { t } = useT();
  const s = statusTone[status] ?? statusTone.queued;
  return (
    <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-medium ${toneClass[s.tone]}`}>
      {t(s.key)}
    </span>
  );
}

// ResolverBadge names which backend handles a task.
const resolverLabel: Record<string, string> = {
  direct: 'Direct',
  torbox: 'TorBox',
  alldebrid: 'AllDebrid',
  realdebrid: 'Real-Debrid',
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
