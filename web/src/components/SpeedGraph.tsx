import { useCallback, useMemo } from 'react';
import { fmtSpeed } from '../lib/format';
import { useT, type TranslationKey } from '../lib/i18n';
import { isLeet } from '../lib/leet';
import { useUIState } from '../lib/uistate';
import { useSpeedWindow, type SpeedScale } from '../lib/speedHistory';
import { InfoBubble } from './ui';
import { Tabs } from './Tabs';

// The speed curves. The sample window comes from lib/speedHistory.ts, shared by
// both components and seeded from GET /api/stats/speed; this file only draws.

// The server's whole coarse ring: one point per ten seconds for an hour.
const HOUR_POINTS = 360;

// English fallbacks for keys not yet in en.ts; the catalogue is asked first.
const PENDING = {
  'overview.speedWindow': 'Speed window',
  'overview.speedWindow.minute': 'Last minute',
  'overview.speedWindow.hour': 'Last hour',
  'overview.speedGraphHint':
    'The instance records this curve itself: one sample a second for the last two minutes, one every ten seconds for the last hour. That is why a reload draws it already filled instead of starting flat. The record is held in memory only, so restarting KnightLoader empties it and the curve starts as a flat line again and fills up as it runs.',
} as const;

type PendingKey = keyof typeof PENDING;

function useCx() {
  const { t } = useT();
  return useCallback(
    (key: PendingKey) => {
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      return translated ?? PENDING[key];
    },
    [t],
  );
}

// The vertical scale rises at once and relaxes 8% per sample, never below a
// floor, so a spike stays in the box, a blip near idle does not rescale the
// curve, and a small transfer looks small.
const FLOOR = 64 * 1024;

function ceilingStep(previous: number, value: number): number {
  const target = Math.max(value, FLOOR);
  return target > previous ? target : Math.max(target, previous * 0.92);
}

/**
 * ceilingFor folds ceilingStep over the window, so the axis depends only on
 * the samples drawn and not on how often the component re-renders.
 */
function ceilingFor(samples: readonly number[]): number {
  let c = FLOOR;
  for (const v of samples) c = ceilingStep(c, v);
  return c;
}

/** smoothPath draws the samples as one curve, scaled into a box. Needs at least two. */
function smoothPath(samples: number[], w: number, h: number, pad: number, max: number) {
  const y = (v: number) => h - pad - (v / max) * (h - pad * 2);
  const pts = samples.map((v, i) => [i * (w / (samples.length - 1)), y(v)] as const);
  let d = `M${pts[0][0]},${pts[0][1].toFixed(1)}`;
  for (let i = 0; i < pts.length - 1; i++) {
    const [x0, y0] = pts[i];
    const [x1, y1] = pts[i + 1];
    const cx = (x0 + x1) / 2;
    d += ` C${cx.toFixed(1)},${y0.toFixed(1)} ${cx.toFixed(1)},${y1.toFixed(1)} ${x1.toFixed(1)},${y1.toFixed(1)}`;
  }
  return { d, last: pts[pts.length - 1] };
}

/**
 * spanLabel labels the left end of the time axis by how much the window holds,
 * so a ring that has recorded forty seconds says "-40s". The units are
 * fmtEta's, untranslated symbols.
 */
function spanLabel(seconds: number): string {
  if (seconds < 120) return `-${seconds}s`;
  if (seconds < 3600) return `-${Math.round(seconds / 60)}m`;
  return `-${Math.round(seconds / 360) / 10}h`;
}

/**
 * SpeedGraph draws this instance's download speed over the last minute or hour
 * as the Overview page's hero. Idle shows a flat hairline. It always uses scope
 * '', since the Overview page reads only the local instance.
 */
export function SpeedGraph({
  value,
  height = 96,
  points = 60,
  limit = 0,
}: {
  value: number;
  height?: number;
  points?: number;
  /** The speed limit in bytes per second, or 0; the page already has settings. */
  limit?: number;
}) {
  const cx = useCx();

  // The stored window loads after first paint; both scales are already in the
  // store, so the switch costs no request.
  const [stored, setStored] = useUIState<SpeedScale>('overview.speedWindow', 'minute');
  const scale: SpeedScale = stored === 'hour' ? 'hour' : 'minute';

  const { samples, seconds } = useSpeedWindow('', value, scale === 'hour' ? HOUR_POINTS : points, scale);

  const W = 600;
  const H = height;
  const pad = 6;
  const ceiling = useMemo(() => ceilingFor(samples), [samples]);

  // smoothPath needs at least two samples, which a cold boot may not have yet.
  const drawable = samples.length >= 2;
  // Idle means nothing in the whole window, not a current speed of zero.
  const peak = drawable ? Math.max(...samples) : 0;
  const idle = peak === 0;

  return (
    /* kl-storm-curve is the 1337 easter egg (docs/easter-eggs.md): at a limit of
       exactly 1337 KiB/s this plot runs on the hidden motion level. It only
       sets two custom properties, so motion "off" and reduced motion still
       win, and the reader's data-motion setting is left alone. */
    <div className={`relative ${isLeet(limit) ? 'kl-storm-curve' : ''}`}>
      {/* The ceiling above the plot, in every state, with the window chooser
          on the same line (GlimStone 1.6.0). */}
      <div className="flex items-center justify-between gap-3">
        <Tabs
          label={cx('overview.speedWindow')}
          size="sm"
          select="one"
          active={scale}
          onSelect={(id) => setStored(id === 'hour' ? 'hour' : 'minute')}
          items={[
            { id: 'minute', label: cx('overview.speedWindow.minute') },
            { id: 'hour', label: cx('overview.speedWindow.hour') },
          ]}
        />
        <span className="flex items-center gap-1.5">
          <InfoBubble tip={cx('overview.speedGraphHint')} />
          <span className="glim-num text-[11px] leading-none text-carbon-textMuted">{fmtSpeed(ceiling)}</span>
        </span>
      </div>
      {/* overflow-visible: the newest sample sits on the right edge, and the
          svg would clip the halo it throws. */}
      <svg
        viewBox={`0 0 ${W} ${H}`}
        preserveAspectRatio="none"
        className="block w-full overflow-visible"
        style={{ height }}
        aria-hidden
      >
        {!drawable || idle ? (
          <line
            x1="0"
            y1={H - pad}
            x2={W}
            y2={H - pad}
            stroke="var(--carbon-border)"
            strokeWidth="1"
            vectorEffect="non-scaling-stroke"
          />
        ) : (
          <Curve samples={samples} w={W} h={H} pad={pad} ceiling={ceiling} />
        )}
      </svg>
      {/* Both ends of the time axis, oldest on the left. */}
      <div className="flex justify-between">
        <span className="glim-num text-[11px] leading-none text-carbon-textMuted">{spanLabel(seconds)}</span>
        <span className="glim-num text-[11px] leading-none text-carbon-textMuted">0s</span>
      </div>
    </div>
  );
}

// Curve draws the fill, the line and the live tip; the caller guarantees at
// least two samples.
function Curve({
  samples,
  w,
  h,
  pad,
  ceiling,
}: {
  samples: number[];
  w: number;
  h: number;
  pad: number;
  ceiling: number;
}) {
  const { d, last } = smoothPath(samples, w, h, pad, ceiling);
  return (
    <>
      <defs>
        <linearGradient id="glim-speed-fill" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="var(--accent)" stopOpacity="0.26" />
          <stop offset="100%" stopColor="var(--accent)" stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={`${d} L${w},${h} L0,${h} Z`} fill="url(#glim-speed-fill)" />
      {/* --accent-ink for strokes, since the plain accent is too pale on the
          light theme; the gradient wash keeps --accent. */}
      <path
        d={d}
        fill="none"
        stroke="var(--accent-ink)"
        strokeWidth="1.75"
        strokeLinecap="round"
        vectorEffect="non-scaling-stroke"
      />
      {/* A halo leaving the live dot on the same --motion-pulse-dur, drawn
          first so it expands from under the dot. Decoration only: index.css
          removes it at motion "off" and under reduced motion, but keeps the dot. */}
      <circle
        cx={last[0]}
        cy={last[1]}
        r="3"
        fill="none"
        stroke="var(--accent-ink)"
        strokeWidth="1.25"
        vectorEffect="non-scaling-stroke"
        className="kl-tip-halo"
      />
      <circle cx={last[0]} cy={last[1]} r="3" fill="var(--accent-ink)" className="glim-live" />
    </>
  );
}

/**
 * SpeedMeter is the shell-bar reading: the current figure with the last
 * half-minute drawn behind it. `instance` is the shell's scope, which can be a
 * peer; only scope '' is seeded, since /api/stats/speed is not forwarded, so a
 * peer's curve starts empty.
 */
export function SpeedMeter({
  value,
  points = 30,
  instance = '',
}: {
  value: number;
  points?: number;
  instance?: string;
}) {
  const { samples, seconds } = useSpeedWindow(instance, value, points, 'minute');

  // The viewBox is a coordinate system: with preserveAspectRatio="none" the svg
  // stretches to whatever box the card gives it.
  const W = 148;
  const H = 40;
  const ceiling = useMemo(() => ceilingFor(samples), [samples]);
  const drawable = samples.length >= 2;
  const peak = drawable ? Math.max(...samples) : 0;
  const idle = peak === 0;
  // Computed here because the live dot needs the curve's last point.
  const curve = drawable && !idle ? smoothPath(samples, W, H, 2, ceiling) : null;

  return (
    // No h-full, for the reason ShellStrip gives.
    <span className="flex flex-1 items-stretch gap-2 px-1.5">
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="glim-num self-end text-[11px] leading-none text-carbon-textMuted">
          {fmtSpeed(ceiling)}
        </span>
        {/* h-0 with flex-auto: without a height the svg's aspect ratio would
            set the card's height from its width. flex-1 does not work here,
            because a 0% basis in an indefinite column falls back to content
            size. check-stretched-svg-height.mjs guards this. */}
        <svg
          viewBox={`0 0 ${W} ${H}`}
          preserveAspectRatio="none"
          className="h-0 min-h-[26px] w-full flex-auto"
          aria-hidden
          focusable="false"
        >
          {!curve ? (
            <line
              x1="0"
              y1={H - 2}
              x2={W}
              y2={H - 2}
              stroke="var(--carbon-border)"
              strokeWidth="1"
              vectorEffect="non-scaling-stroke"
            />
          ) : (
            <>
              <path
                d={curve.d}
                fill="none"
                stroke="var(--accent-ink)"
                strokeWidth="1.5"
                strokeLinecap="round"
                vectorEffect="non-scaling-stroke"
              />
              <circle cx={curve.last[0]} cy={curve.last[1]} r="2" fill="var(--accent-ink)" className="glim-live" />
            </>
          )}
        </svg>
        <span className="flex justify-between text-[11px] leading-none text-carbon-textMuted">
          <span className="glim-num">{spanLabel(seconds)}</span>
          <span className="glim-num">0s</span>
        </span>
      </span>
      {/* Figure and unit are one token; an RTL locale must not reorder them. */}
      <span
        dir="ltr"
        className="glim-num flex items-center text-[12px] font-semibold leading-none text-carbon-text"
      >
        {fmtSpeed(value) || '0 B/s'}
      </span>
    </span>
  );
}
