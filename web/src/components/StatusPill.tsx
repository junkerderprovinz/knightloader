import type { ComponentType, SVGProps } from 'react';
import type { Task, TaskStatus } from '../lib/api';
import { useT, type TranslationKey } from '../lib/i18n';
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

// Paused shares the neutral tone; the glyph and label tell it apart.
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

// Seven states share four tones, so the glyph is what tells them apart.
const statusGlyph: Record<TaskStatus, ComponentType<SVGProps<SVGSVGElement>>> = {
  collected: IconCollector,
  queued: IconClock,
  running: IconDownloads,
  paused: IconPause,
  extracting: IconArchive,
  done: IconCheck,
  error: IconWarning,
};

// A glyph plus a word, so state never relies on colour alone. It does not
// pulse; the progress fill carries liveness.
export function StatusPill({ status }: { status: TaskStatus }) {
  const { t } = useT();
  const s = statusTone[status] ?? statusTone.queued;
  const Glyph = statusGlyph[status] ?? IconClock;
  return (
    // shrink-0 so a long neighbour, such as a waiting reason, truncates first.
    <span className={`inline-flex min-w-0 shrink-0 items-center gap-1.5 text-[11px] font-medium ${toneText[s.tone]}`}>
      <Glyph width={13} height={13} className="shrink-0" />
      <span className="truncate">{t(s.key)}</span>
    </span>
  );
}

/**
 * ResolverBadge names the backend carrying a task and, for a hoster, whether it
 * runs free or premium. It is metadata, so it stays in muted ink.
 */
export function ResolverBadge({ resolver, mode }: { resolver: string; mode?: Task['mode'] }) {
  const { t } = useT();
  return (
    <span className="text-[11px] text-carbon-textMuted">
      {resolverLabel(resolver, t)}
      {mode ? ` · ${t(mode === 'premium' ? 'task.mode.premium' : 'task.mode.free')}` : ''}
    </span>
  );
}
