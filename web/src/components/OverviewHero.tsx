import { useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import type { Task } from '../lib/api';
import { fmtPct, fmtRate, fmtUptime } from '../lib/format';
import { useT, type TranslationKey } from '../lib/i18n';
import { IconCheck, IconClock, IconDownloads } from '../lib/icons';
import { headState, kindSegments, queueFigures, type KindSegment, type KindTone, type Need } from '../lib/needs';
import { resolverLabel } from '../lib/resolverLabels';
import { breakdownLabel } from '../lib/useHealthReport';
import { useTooltip } from './ui';

type Translate = ReturnType<typeof useT>['t'];

// The ring's geometry, in the units of its 64 by 64 viewBox.
const RADIUS = 27;
const ROUND = 2 * Math.PI * RADIUS;
const GAP = 4;

const STROKE: Record<KindTone, string> = {
  fail: 'stroke-statusFailSolid',
  warn: 'stroke-statusWarnSolid',
  run: 'stroke-accent',
  neutral: 'stroke-carbon-surface3',
  ok: 'stroke-statusOkSolid',
};
const DOT: Record<KindTone, string> = {
  fail: 'bg-statusFailSolid',
  warn: 'bg-statusWarnSolid',
  run: 'bg-accent',
  neutral: 'bg-carbon-surface3',
  ok: 'bg-statusOkSolid',
};
const VERDICT: Record<KindTone, TranslationKey> = {
  fail: 'status.error',
  warn: 'status.error',
  run: 'status.running',
  neutral: 'status.queued',
  ok: 'status.done',
};

/** What a segment says of its kind: the wait that holds it back, or its state. */
function verdictOf(t: Translate, s: KindSegment): string {
  return s.held ? breakdownLabel(t, 'task.waiting.', s.held) : t(VERDICT[s.tone]);
}

/**
 * useWorsened names the segments whose kind just took a turn for the worse,
 * as `id:tone`, so each pulses once. A segment keeps its entry until another
 * one worsens, which lets the pulse play out across the renders in between.
 */
function useWorsened(segments: KindSegment[]): ReadonlySet<string> {
  const before = useRef<Map<string, KindTone> | null>(null);
  const [worsened, setWorsened] = useState<ReadonlySet<string>>(new Set());
  useEffect(() => {
    const was = before.current;
    before.current = new Map(segments.map((s) => [s.id, s.tone]));
    if (!was) return;
    const worse = segments.filter((s) => {
      const old = was.get(s.id);
      return old !== undefined && old !== s.tone && (s.tone === 'fail' || (s.tone === 'warn' && old !== 'fail'));
    });
    if (worse.length > 0) setWorsened(new Set(worse.map((s) => `${s.id}:${s.tone}`)));
  }, [segments]);
  return worsened;
}

/**
 * The ring of the overview's head. At rest it has one segment per kind of
 * download, coloured by the worst state among them, around the number of
 * things that wait for the person. While the queue runs and nothing waits it
 * is one arc of how far the open packages have come.
 */
function StatusRing({
  segments,
  percent,
  centre,
  ink,
}: {
  segments: KindSegment[];
  /** Set while the ring shows progress, where it stands inside a link. */
  percent: number | null;
  centre: ReactNode;
  ink: string;
}) {
  const { t } = useT();
  const worsened = useWorsened(segments);
  const names = segments.map((s) => `${resolverLabel(s.id, t)}: ${verdictOf(t, s)}`);
  const tip = useTooltip<HTMLDivElement>(
    segments.length > 0 ? (
      <span className="flex flex-col gap-1">
        {segments.map((s) => (
          <span key={s.id} className="flex items-center gap-2">
            <span className={`h-2 w-2 shrink-0 rounded-[var(--radius-pill)] ${DOT[s.tone]}`} />
            <span className="font-medium">{resolverLabel(s.id, t)}</span>
            <span>{verdictOf(t, s)}</span>
          </span>
        ))}
      </span>
    ) : undefined,
  );

  // Inside the link the ring is no stop of its own, and its bubble opens
  // under the pointer only.
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  const named = segments.length > 0 && percent === null;

  const each = ROUND / Math.max(1, segments.length);
  const gap = segments.length > 1 ? GAP : 0;
  return (
    <>
      <div
        {...(named ? tip.triggerProps : tipHoverProps)}
        aria-label={named ? names.join(', ') : undefined}
        className="kl-ring relative h-16 w-16 shrink-0 rounded-[var(--radius-pill)]"
      >
        <svg viewBox="0 0 64 64" aria-hidden="true" className="block h-full w-full overflow-visible fill-none stroke-6">
          <g transform="rotate(-90 32 32)">
            {percent !== null || segments.length === 0 ? (
              <circle cx="32" cy="32" r={RADIUS} className="stroke-carbon-surface2" />
            ) : (
              segments.map((s, i) => (
                <circle
                  key={s.id}
                  cx="32"
                  cy="32"
                  r={RADIUS}
                  className={`kl-ring-seg ${STROKE[s.tone]} ${worsened.has(`${s.id}:${s.tone}`) ? 'kl-ring-worse' : ''}`}
                  style={{ '--seg-i': i, '--ring-round': ROUND } as CSSProperties}
                  strokeDasharray={`${(each - gap).toFixed(2)} ${ROUND.toFixed(2)}`}
                  strokeDashoffset={(-(i * each + gap / 2)).toFixed(2)}
                />
              ))
            )}
            {percent !== null && (
              <circle
                cx="32"
                cy="32"
                r={RADIUS}
                className="kl-ring-progress stroke-accent"
                strokeLinecap="round"
                strokeDasharray={ROUND.toFixed(2)}
                strokeDashoffset={(ROUND * (1 - percent / 100)).toFixed(2)}
              />
            )}
          </g>
        </svg>
        <span className={`absolute inset-0 flex items-center justify-center ${ink}`}>{centre}</span>
      </div>
      {tip.node}
    </>
  );
}

/**
 * OverviewHero opens the overview: the ring, one sentence from a short fixed
 * list and one line of live figures.
 */
export function OverviewHero({
  tasks,
  needs,
  done,
}: {
  tasks: Task[];
  needs: Need[];
  /** What was marked as done, so a settled failure does not colour the ring. */
  done: ReadonlySet<string>;
}) {
  const { t } = useT();
  const [segments, figures] = useMemo(() => {
    const now = Date.now();
    return [kindSegments(tasks, done, now), queueFigures(tasks, now)] as const;
  }, [tasks, done]);
  const n = needs.length;
  const state = headState(n, figures);
  const progress = state === 'running' ? figures.percent : null;

  const count = (one: TranslationKey, many: TranslationKey, of: number) => (of === 1 ? t(one) : t(many, { n: of }));
  const title =
    state === 'needs'
      ? count('overview.hero.needsOne', 'overview.hero.needs', n)
      : state === 'running'
        ? count('overview.hero.runningOne', 'overview.hero.running', figures.running)
        : state === 'waiting'
          ? count('overview.hero.waitingOne', 'overview.hero.waiting', figures.waiting)
          : state === 'empty'
            ? t('overview.noDownloads')
            : t('overview.hero.allDone');

  const line: string[] = [
    figures.running > 0 ? fmtRate(figures.speed) : t(figures.halted ? 'task.waiting.halted' : 'tray.idle'),
  ];
  if (figures.eta !== null) line.push(t('overview.hero.timeLeft', { time: fmtUptime(figures.eta) }));
  // The sentence above already counts them in the waiting state.
  if (figures.waiting > 0 && state !== 'waiting') line.push(t('overview.hero.inQueue', { n: figures.waiting }));

  const centre =
    state === 'needs' ? (
      <b className="glim-num text-[22px] font-bold leading-none">{n}</b>
    ) : progress !== null ? (
      <b className="glim-num text-sm font-semibold leading-none">{fmtPct(progress)}</b>
    ) : state === 'done' ? (
      <IconCheck width={24} height={24} />
    ) : state === 'waiting' ? (
      <IconClock width={24} height={24} />
    ) : (
      <IconDownloads width={24} height={24} />
    );
  const ink =
    state === 'needs'
      ? needs.some((x) => x.tone === 'fail')
        ? 'text-statusFail'
        : 'text-statusWarn'
      : state === 'done'
        ? 'text-statusOk'
        : state === 'running'
          ? 'text-carbon-text'
          : 'text-carbon-textMuted';
  const ring = <StatusRing segments={segments} percent={progress} centre={centre} ink={ink} />;

  return (
    <div data-new="overview-head" className="flex flex-wrap items-center gap-x-[22px] gap-y-4 px-1 pt-1.5">
      {progress !== null ? (
        <Link to="/downloads" aria-label={t('nav.downloads')} className="shrink-0 rounded-[var(--radius-pill)]">
          {ring}
        </Link>
      ) : (
        ring
      )}
      <div className="min-w-0 flex-1 basis-48">
        <h2 className="text-[22px] font-semibold leading-tight text-carbon-text">{title}</h2>
        <p className="glim-num mt-1 text-sm text-carbon-textSub">{line.join(' · ')}</p>
      </div>
    </div>
  );
}
