import type { ComponentType, SVGProps } from 'react';
import type { ExtractJob, Task, TaskStatus } from '../lib/api';
import { fmtPct } from '../lib/format';
import { useT, type TranslationKey } from '../lib/i18n';
import { resolverLabel } from '../lib/resolverLabels';
import {
  IconArchive,
  IconCheck,
  IconClock,
  IconCollector,
  IconDownloads,
  IconKey,
  IconPause,
  IconWarning,
} from '../lib/icons';

// Paused shares the neutral tone; the glyph and label tell it apart.
type Tone = 'ok' | 'fail' | 'info' | 'neutral';

type Glyph = ComponentType<SVGProps<SVGSVGElement>>;

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
const statusGlyph: Record<TaskStatus, Glyph> = {
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
//
// shrink-0 so a long neighbour, such as a waiting reason, truncates first. A
// label that can outgrow the status column's floor on its own passes `fits`
// and truncates too, rather than being cut off by the cell.
function Pill({ tone, glyph: Glyph, label, fits }: { tone: Tone; glyph: Glyph; label: string; fits?: boolean }) {
  return (
    <span
      className={`inline-flex min-w-0 items-center gap-1.5 text-[11px] font-medium ${fits ? '' : 'shrink-0'} ${toneText[tone]}`}
    >
      <Glyph width={13} height={13} className="shrink-0" />
      <span className="truncate">{label}</span>
    </span>
  );
}

export function StatusPill({ status }: { status: TaskStatus }) {
  const { t } = useT();
  const s = statusTone[status] ?? statusTone.queued;
  return <Pill tone={s.tone} glyph={statusGlyph[status] ?? IconClock} label={t(s.key)} />;
}

/**
 * Where the unpacking of a finished file's archive stands, as its row names it
 * in place of "Done". A failure for want of a password is its own state
 * because it is the one with an obvious remedy.
 */
export type UnpackState = 'queued' | 'running' | 'done' | 'password' | 'error';

/** unpackState reads a job's open status; a cancelled or unknown one is null. */
export function unpackState(j: ExtractJob): UnpackState | null {
  switch (j.status) {
    case 'queued':
      return 'queued';
    case 'running':
      return 'running';
    case 'done':
      return 'done';
    case 'error':
      return j.password ? 'password' : 'error';
  }
  return null;
}

const unpackLook: Record<UnpackState, { tone: Tone; key: TranslationKey; glyph: Glyph }> = {
  queued: { tone: 'neutral', key: 'archive.queued', glyph: IconArchive },
  running: { tone: 'info', key: 'status.extracting', glyph: IconArchive },
  done: { tone: 'ok', key: 'archive.unpacked', glyph: IconCheck },
  password: { tone: 'fail', key: 'archive.needsPassword', glyph: IconKey },
  error: { tone: 'fail', key: 'archive.failed', glyph: IconWarning },
};

/**
 * unpackLabel is the word an unpacking shows, with the share of the archive
 * written so far while it runs. Without `percent` it says only that it runs,
 * which is all a single compressed stream lets anybody know.
 */
export function unpackLabel(
  state: UnpackState,
  percent: number | undefined,
  t: (key: TranslationKey, vars?: Record<string, string | number>) => string,
): string {
  if (state === 'running' && percent !== undefined) return t('archive.unpackingAt', { percent: fmtPct(percent) });
  return t(unpackLook[state].key);
}

/** UnpackPill is StatusPill for an unpacking. */
export function UnpackPill({ state, percent }: { state: UnpackState; percent?: number }) {
  const { t } = useT();
  const look = unpackLook[state];
  return <Pill tone={look.tone} glyph={look.glyph} label={unpackLabel(state, percent, t)} fits />;
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
