import type { ComponentType, SVGProps } from 'react';
import type { Task, TaskStatus } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
// The map this file used to keep to itself. It moved out the moment a second
// reader appeared (the volume curve's legend): two copies of "which word does
// this id get" is how one screen ends up calling the same backend two things.
import { resolverLabel } from '../lib/resolverLabels';
import {
  IconArchive,
  IconCheck,
  IconClock,
  IconCollector,
  IconDownloads,
  IconPause,
  IconWarning,
} from '../lib/icons';

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

/**
 * One glyph per state, the way JDownloader's own status column reads (jdp,
 * 2026-09-06: "für den jeweiligen zustand: läuft, download, entpacken, etc soll
 * es ein entsprechenden glyph anzeigen").
 *
 * A glyph, not the dot this used to draw: a coloured dot distinguishes four
 * tones, and there are seven states - queued and paused shared one tone, and so
 * did running and extracting, so half the column was telling two states apart
 * by their word alone. The glyph says which state; the tone still says how it
 * feels.
 */
const statusGlyph: Record<TaskStatus, ComponentType<SVGProps<SVGSVGElement>>> = {
  collected: IconCollector,
  queued: IconClock,
  running: IconDownloads,
  paused: IconPause,
  extracting: IconArchive,
  done: IconCheck,
  error: IconWarning,
};

// A glyph plus a word: state reads at a glance and never relies on colour
// alone. Nothing pulses here: one pulsing element per screen is plenty, and a
// list of blinking rows is the loudest thing an idle-heavy page can do. Rows
// convey liveness through the moving progress fill instead.
export function StatusPill({ status }: { status: TaskStatus }) {
  const { t } = useT();
  const s = statusTone[status] ?? statusTone.queued;
  const Glyph = statusGlyph[status] ?? IconClock;
  return (
    // shrink-0, because the pill is the ANSWER and whatever stands beside it is
    // the footnote. Left shrinkable it lost the argument to a longer neighbour -
    // measured on the preview instance, "Wartet" next to a waiting reason came
    // out as "W…", so the one word the column exists for was the one word gone.
    // min-w-0 stays for the truncate inside, which now only ever bites on a
    // genuinely narrow column rather than on a crowded one.
    <span className={`inline-flex min-w-0 shrink-0 items-center gap-1.5 text-[11px] font-medium ${toneText[s.tone]}`}>
      <Glyph width={13} height={13} className="shrink-0" />
      <span className="truncate">{t(s.key)}</span>
    </span>
  );
}

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
      {resolverLabel(resolver)}
      {mode ? ` · ${t(mode === 'premium' ? 'task.mode.premium' : 'task.mode.free')}` : ''}
    </span>
  );
}
