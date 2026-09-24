import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useSyncExternalStore } from 'react';
import { fmtSpeed } from '../lib/format';
import { useT, type TranslationKey } from '../lib/i18n';
import { isLeet } from '../lib/leet';
import { useUIState } from '../lib/uistate';
import { useSpeedWindow, type SpeedScale, type SpeedWindow } from '../lib/speedHistory';
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

// The top of the plot never drops below this, so a blip near idle does not fill
// the box and a small transfer looks small.
const FLOOR = 64 * 1024;

/** ceilingOf is the top of the plot: the window's peak, so every sample fits. */
function ceilingOf(samples: readonly number[]): number {
  return Math.max(FLOOR, ...samples);
}

// Samples fetched beyond the span on screen. While the curve glides, the two
// newest wait past the right edge until the tangent where they join is known,
// and two old ones hang past the left edge, so no segment on screen changes
// shape when a sample arrives or drops out.
const OFFSTAGE = 4;

// Time constants for the drawn ceiling following the window's peak. It rises
// faster than it falls: a new peak waits a step past the right edge, and the box
// has to have made room by the time it shows.
const RISE_MS = 250;
const FALL_MS = 700;

const REDUCED_MOTION = '(prefers-reduced-motion: reduce)';

// The glide runs in script, out of reach of the stylesheet's motion gate, so it
// asks the gate's two questions itself.
function glideAllowed(): boolean {
  return !window.matchMedia(REDUCED_MOTION).matches && document.documentElement.dataset.motion !== 'off';
}

function subscribeGlide(onChange: () => void): () => void {
  const query = window.matchMedia(REDUCED_MOTION);
  query.addEventListener('change', onChange);
  const level = new MutationObserver(onChange);
  level.observe(document.documentElement, { attributes: true, attributeFilter: ['data-motion'] });
  return () => {
    query.removeEventListener('change', onChange);
    level.disconnect();
  };
}

interface Geometry {
  line: string;
  area: string;
  /** The segment ending at the anchor, as its four Bezier y values. The tip rides it. */
  tail: readonly [number, number, number, number];
}

/**
 * geometry draws the samples dx apart with the anchor on the right edge, as a
 * monotone cubic: the curve never swings past a sample, so it stays above zero
 * and below the peak.
 */
function geometry(
  samples: readonly number[],
  anchor: number,
  w: number,
  h: number,
  pad: number,
  dx: number,
  ceiling: number,
): Geometry {
  const base = h - pad;
  const n = samples.length;
  const xs = samples.map((_, i) => w + (i - anchor) * dx);
  const ys = samples.map((v) => base - (v / ceiling) * (base - pad));
  const slope = (i: number) => (ys[i + 1] - ys[i]) / dx;

  // Steffen's tangents: flat at a turn, and never steeper than twice either
  // neighbouring slope, which is what rules out an overshoot.
  const m = new Array<number>(n);
  for (let i = 1; i < n - 1; i++) {
    const a = slope(i - 1);
    const b = slope(i);
    m[i] = a * b <= 0 ? 0 : Math.sign(a) * Math.min(2 * Math.abs(a), 2 * Math.abs(b), Math.abs(a + b) / 2);
  }
  // The ends lean on their one neighbour, as d3's curveMonotoneX does.
  m[0] = n > 2 ? (3 * slope(0) - m[1]) / 2 : slope(0);
  m[n - 1] = n > 2 ? (3 * slope(n - 2) - m[n - 2]) / 2 : slope(0);

  // Two decimals: a rising ceiling stretches the old curve many times over for
  // a frame or two, and one decimal shows as a wobble then.
  const f = (v: number) => v.toFixed(2);
  const t = dx / 3;
  let line = `M${f(xs[0])},${f(ys[0])}`;
  for (let i = 1; i < n; i++) {
    line +=
      `C${f(xs[i - 1] + t)},${f(ys[i - 1] + m[i - 1] * t)} ` +
      `${f(xs[i] - t)},${f(ys[i] - m[i] * t)} ${f(xs[i])},${f(ys[i])}`;
  }
  const area = `${line}L${f(xs[n - 1])},${f(base)}L${f(xs[0])},${f(base)}Z`;
  const a = anchor;
  return { line, area, tail: [ys[a - 1], ys[a - 1] + m[a - 1] * t, ys[a] - m[a] * t, ys[a]] };
}

/** What a frame needs to place the curve, refreshed on every sample. */
interface Frame {
  geo: Geometry;
  glide: boolean;
  newestAt: number;
  stepMs: number;
  dx: number;
  top: number;
  base: number;
  ceiling: number;
}

/** The glide's own state, carried from frame to frame. */
interface Glide {
  /** The ceiling as drawn, easing towards Frame.ceiling. */
  eased: number;
  /** When the previous frame was placed, 0 before the first. */
  at: number;
  /** The attributes last written, so a frame that moves nothing costs no repaint. */
  drawn: string;
}

/**
 * place moves the curve to where it is at `now`. The paths are drawn once per
 * sample; between samples only three attributes change.
 */
function place(
  f: Frame,
  g: Glide,
  els: { mover: SVGGElement; tip: SVGGElement; wash: SVGLinearGradientElement },
  now: number,
): void {
  // How far the step has run: 0 as the newest sample arrives, 1 a step later.
  const p = f.glide ? Math.min(Math.max((now - f.newestAt) / f.stepMs, 0), 1) : 1;
  if (f.glide && g.at > 0) {
    const tau = f.ceiling > g.eased ? RISE_MS : FALL_MS;
    g.eased += (f.ceiling - g.eased) * (1 - Math.exp(-(now - g.at) / tau));
  } else {
    g.eased = f.ceiling;
  }
  g.at = now;

  // The paths are drawn against the target ceiling; k stretches them to the
  // eased one about the zero line.
  const k = f.ceiling / g.eased;
  const q = 1 - p;
  const [a, b, c, d] = f.geo.tail;
  const tipY = q * q * q * a + 3 * q * q * p * b + 3 * q * p * p * c + p * p * p * d;

  const moved = `translate(${(f.dx * q).toFixed(2)} ${f.base}) scale(1 ${k.toFixed(4)}) translate(0 ${-f.base})`;
  // The wash is pinned to the box, not to the stretched curve, so its shade
  // holds still while the ceiling moves.
  const washTop = (f.base - (f.base - f.top) / k).toFixed(4);
  const tipAt = `translate(0 ${(f.base + (tipY - f.base) * k).toFixed(2)})`;
  const drawn = `${moved}|${washTop}|${tipAt}`;
  if (drawn === g.drawn) return;
  g.drawn = drawn;
  els.mover.setAttribute('transform', moved);
  els.wash.setAttribute('y1', washTop);
  els.tip.setAttribute('transform', tipAt);
}

interface PlotProps {
  win: SpeedWindow;
  /** How many steps the width shows. */
  span: number;
  w: number;
  h: number;
  pad: number;
  ceiling: number;
  /** The live tip's radius. */
  dot: number;
  stroke: number;
  /** Whether the tip throws a ring on every pulse. */
  halo?: boolean;
}

/**
 * Plot is everything inside a speed svg: the zero line, and the curve once
 * anything on screen is above it.
 */
function Plot(props: PlotProps) {
  const glide = useSyncExternalStore(subscribeGlide, glideAllowed, () => false);
  const { win, w, h, pad } = props;
  // The sample on the right edge once a step has run. While gliding, the
  // newest one is still past the edge and only shapes the tangent there.
  const anchor = win.samples.length - (glide ? 2 : 1);
  // Idle means nothing on screen, not a current speed of zero.
  const busy = anchor >= 1 && win.samples.slice(0, anchor + 1).some((v) => v > 0);
  return (
    <>
      <line
        x1="0"
        y1={h - pad}
        x2={w}
        y2={h - pad}
        stroke="var(--carbon-border)"
        strokeWidth="1"
        vectorEffect="non-scaling-stroke"
      />
      {busy && <Curve {...props} anchor={anchor} glide={glide} />}
    </>
  );
}

/**
 * Curve draws the filled area, the line and the live tip. While motion is
 * allowed it glides left between samples and eases its ceiling; otherwise it
 * moves once per sample.
 */
function Curve({
  win,
  span,
  w,
  h,
  pad,
  ceiling,
  dot,
  stroke,
  halo = false,
  anchor,
  glide,
}: PlotProps & { anchor: number; glide: boolean }) {
  const id = useId();
  const dx = w / span;
  const geo = useMemo(
    () => geometry(win.samples, anchor, w, h, pad, dx, ceiling),
    [win.samples, anchor, w, h, pad, dx, ceiling],
  );

  const mover = useRef<SVGGElement>(null);
  const tip = useRef<SVGGElement>(null);
  const wash = useRef<SVGLinearGradientElement>(null);
  const frame = useRef<Frame | null>(null);
  const state = useRef<Glide>({ eased: ceiling, at: 0, drawn: '' });

  const draw = useCallback((now: number) => {
    const f = frame.current;
    if (f && mover.current && tip.current && wash.current) {
      place(f, state.current, { mover: mover.current, tip: tip.current, wash: wash.current }, now);
    }
  }, []);

  // Before paint, so a new sample and the offset that hides its arrival land
  // in the same frame.
  useLayoutEffect(() => {
    frame.current = {
      geo,
      glide,
      newestAt: win.newestAt,
      stepMs: win.step * 1000,
      dx,
      top: pad,
      base: h - pad,
      ceiling,
    };
    draw(performance.now());
  }, [draw, geo, glide, win.newestAt, win.step, dx, h, pad, ceiling]);

  useEffect(() => {
    const svg = mover.current?.ownerSVGElement;
    if (!glide || !svg) return;
    let raf = 0;
    const loop = () => {
      draw(performance.now());
      raf = requestAnimationFrame(loop);
    };
    // Only while the plot is on screen: the shell meter stays mounted, hidden,
    // on every page but Downloads.
    const seen = new IntersectionObserver((entries) => {
      cancelAnimationFrame(raf);
      raf = entries[entries.length - 1].isIntersecting ? requestAnimationFrame(loop) : 0;
    });
    seen.observe(svg);
    return () => {
      seen.disconnect();
      cancelAnimationFrame(raf);
    };
  }, [draw, glide]);

  return (
    <>
      <defs>
        {/* In user space, so place() can pin it to the box by moving y1. */}
        <linearGradient ref={wash} id={`${id}wash`} gradientUnits="userSpaceOnUse" x1="0" x2="0" y2={h - pad}>
          <stop offset="0" stopColor="var(--accent)" stopOpacity="0.42" />
          <stop offset="1" stopColor="var(--accent)" stopOpacity="0.14" />
        </linearGradient>
        <clipPath id={`${id}box`}>
          <rect width={w} height={h} />
        </clipPath>
      </defs>
      {/* The clip sits outside the moving group, or it would travel with it. */}
      <g clipPath={`url(#${id}box)`}>
        <g ref={mover}>
          <path d={geo.area} fill={`url(#${id}wash)`} />
          {/* --accent-ink for strokes, since the plain accent is too pale on
              the light theme; the wash keeps --accent. */}
          <path
            d={geo.line}
            fill="none"
            stroke="var(--accent-ink)"
            strokeWidth={stroke}
            strokeLinecap="round"
            strokeLinejoin="round"
            vectorEffect="non-scaling-stroke"
          />
        </g>
      </g>
      <g ref={tip}>
        {/* A halo leaving the live dot on the same --motion-pulse-dur, drawn
            first so it expands from under the dot. Decoration only: index.css
            removes it at motion "off" and under reduced motion, but keeps the
            dot. */}
        {halo && (
          <circle
            cx={w}
            r={dot}
            fill="none"
            stroke="var(--accent-ink)"
            strokeWidth="1.25"
            vectorEffect="non-scaling-stroke"
            className="kl-tip-halo"
          />
        )}
        <circle cx={w} r={dot} fill="var(--accent-ink)" className="glim-live" />
      </g>
    </>
  );
}

/**
 * spanLabel labels the left end of the time axis with the span the plot
 * covers. A window that is not full yet draws from the right and leaves the
 * rest empty. The units are fmtEta's, untranslated symbols.
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
  const span = scale === 'hour' ? HOUR_POINTS : points;

  const win = useSpeedWindow('', value, span + OFFSTAGE, scale);
  const ceiling = useMemo(() => ceilingOf(win.samples), [win.samples]);

  const W = 600;

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
      {/* overflow-visible: the live tip sits on the right edge, and the svg
          would clip the halo it throws. The curve clips itself. */}
      <svg
        viewBox={`0 0 ${W} ${height}`}
        preserveAspectRatio="none"
        className="block w-full overflow-visible"
        style={{ height }}
        aria-hidden
      >
        <Plot win={win} span={span} w={W} h={height} pad={6} ceiling={ceiling} dot={3} stroke={1.75} halo />
      </svg>
      {/* Both ends of the time axis, oldest on the left. */}
      <div className="flex justify-between">
        <span className="glim-num text-[11px] leading-none text-carbon-textMuted">{spanLabel(span * win.step)}</span>
        <span className="glim-num text-[11px] leading-none text-carbon-textMuted">0s</span>
      </div>
    </div>
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
  const win = useSpeedWindow(instance, value, points + OFFSTAGE, 'minute');
  const ceiling = useMemo(() => ceilingOf(win.samples), [win.samples]);

  // The viewBox is a coordinate system: with preserveAspectRatio="none" the svg
  // stretches to whatever box the card gives it.
  const W = 148;
  const H = 40;

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
          <Plot win={win} span={points} w={W} h={H} pad={2} ceiling={ceiling} dot={2} stroke={1.5} />
        </svg>
        <span className="flex justify-between text-[11px] leading-none text-carbon-textMuted">
          <span className="glim-num">{spanLabel(points * win.step)}</span>
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
