import type { Task, TaskStatus } from '../lib/api';
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

/**
 * Which backend carries a task, and whether it goes out on an account.
 *
 * Quiet by design, both halves: this is metadata, not status, so it takes the
 * muted ink and no ground of its own. The mode is a second word rather than a
 * colour or a badge for the same reason - "free" is not a warning, it is an
 * answer to a question that previously had none (jdp, 2026-09-02: "Wenn man
 * links runterladen möchte für die kein premium account hinterlegt ist muss das
 * angezeigt werden"). A link with no account behind it looked exactly like one
 * with an account behind it, right up until it was slow or asking for a captcha.
 *
 * Nothing is drawn for a plain file: an ordinary download is neither free nor
 * premium, and a word there would answer a question nobody asked.
 */
export function ResolverBadge({ resolver, mode }: { resolver: string; mode?: Task['mode'] }) {
  const { t } = useT();
  return (
    <span className="text-[11px] text-carbon-textMuted">
      {resolverLabel[resolver] ?? resolver}
      {mode ? ` · ${t(mode === 'premium' ? 'task.mode.premium' : 'task.mode.free')}` : ''}
    </span>
  );
}
