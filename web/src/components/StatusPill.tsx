import type { TaskStatus } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';

// Four state hues only: gold = running, green = settled, red = fault,
// neutral = waiting. Paused deliberately shares the neutral tone; the label and
// the resume control carry the distinction.
type Tone = 'ok' | 'fail' | 'info' | 'neutral';

const statusTone: Record<TaskStatus, { tone: Tone; key: TranslationKey }> = {
  collected: { tone: 'neutral', key: 'status.collected' },
  queued: { tone: 'neutral', key: 'status.queued' },
  running: { tone: 'info', key: 'status.running' },
  paused: { tone: 'neutral', key: 'status.paused' },
  extracting: { tone: 'info', key: 'status.extracting' },
  done: { tone: 'ok', key: 'status.done' },
  error: { tone: 'fail', key: 'status.error' },
};

const toneText: Record<Tone, string> = {
  ok: 'text-statusOk',
  fail: 'text-statusFail',
  info: 'text-statusInfo',
  neutral: 'text-statusNeutral',
};
const toneDot: Record<Tone, string> = {
  ok: 'bg-statusOkSolid',
  fail: 'bg-statusFailSolid',
  info: 'bg-statusInfoSolid',
  neutral: 'bg-statusNeutralSolid',
};

// A dot plus a word: state reads at a glance and never relies on colour alone.
// The dot never pulses here: one pulsing element per screen is plenty, and a
// list of blinking rows is the loudest thing an idle-heavy page can do. Rows
// convey liveness through the moving progress fill instead.
export function StatusPill({ status }: { status: TaskStatus }) {
  const { t } = useT();
  const s = statusTone[status] ?? statusTone.queued;
  return (
    <span className={`inline-flex items-center gap-1.5 text-[11px] font-medium ${toneText[s.tone]}`}>
      <span className={`h-1.5 w-1.5 rounded-[var(--radius-pill)] ${toneDot[s.tone]}`} />
      {t(s.key)}
    </span>
  );
}

const resolverLabel: Record<string, string> = {
  direct: 'Direct',
  torbox: 'TorBox',
  alldebrid: 'AllDebrid',
  realdebrid: 'Real-Debrid',
  ytdlp: 'yt-dlp',
  jd: 'JDownloader',
  http: 'HTTP',
};

// Which backend carries a task — quiet by design, it is metadata, not status.
export function ResolverBadge({ resolver }: { resolver: string }) {
  return <span className="text-[11px] text-carbon-textMuted">{resolverLabel[resolver] ?? resolver}</span>;
}
