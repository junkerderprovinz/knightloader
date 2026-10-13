import {
  type RefObject,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react';
import { fmtRate, fmtSpeed } from '../lib/format';
import { useT } from '../lib/i18n';
import { IconSpeedLimit } from '../lib/icons';
import { isLeet } from '../lib/leet';
import { useLimitInForce, useUploadLimit } from '../lib/limitInForce';
import { useUIState } from '../lib/uistate';
import { stepOf, useSpeedWindow, type SpeedScale, type SpeedWindow } from '../lib/speedHistory';
import { DOWNLOAD_INK, SEEDING_INK } from '../lib/speedLines';
import { InfoBubble } from './ui';
import { Tabs } from './Tabs';

// The speed curves. The sample window comes from lib/speedHistory.ts, shared by
// both components and seeded from GET /api/stats/speed; this file only draws.

// The server's whole coarse ring: one point per ten seconds for an hour.
const HOUR_POINTS = 360;

// The server's fine ring, one point per second, covers this many seconds. A
// longer window reads the coarse ring.
const FINE_SPAN_S = 120;

/** What the shell meter's window may be, in seconds: up to the coarse ring's hour. */
export const METER_WINDOW = { min: 10, max: HOUR_POINTS * stepOf('hour'), fallback: 30 };

/**
 * meterWindow turns a stored or typed window into one the meter draws: whole
 * seconds below a minute and half minutes from there. Past the fine ring that
 * is also what the coarse one needs, a whole number of its ten-second steps.
 */
export function meterWindow(seconds: number): number {
  const s = Math.min(METER_WINDOW.max, Math.max(METER_WINDOW.min, seconds));
  return s < 60 ? Math.round(s) : Math.round(s / 30) * 30;
}

/**
 * useMeterWindow is the shell meter's window in seconds, kept in the interface
 * state like the Overview curve's. The stored field is read as a number, since
 * the server keeps whatever it was sent.
 */
export function useMeterWindow(): [number, (seconds: number) => void] {
  const [stored, setStored] = useUIState<unknown>('shell.speedWindow', METER_WINDOW.fallback);
  const seconds = typeof stored === 'number' && Number.isFinite(stored) ? meterWindow(stored) : METER_WINDOW.fallback;
  return [seconds, setStored];
}

// The top of the plot never drops below this, so a blip near idle does not fill
// the box and a small transfer looks small.
const FLOOR = 64_000;

// The top of the plot sits this far above a speed limit it takes in, so the
// mark has room for its figure above it.
const HEADROOM = 1.2;

// A limit more than this many times the window's peak stays out of the scale.
// Taken in, it would squeeze a slow download under a generous limit into the
// bottom few pixels; left out, the curve keeps at least a quarter of the box.
const REACH = 4;

/**
 * ceilingOf is the top of the plot: the window's peak, so every sample fits,
 * raised to take in each limit given with room above it. A limit beyond REACH
 * is left out while anything moves; idle has no curve to flatten, so there
 * the limits set the scale.
 */
export function ceilingOf(samples: readonly number[], ...limits: number[]): number {
  const peak = Math.max(0, ...samples);
  const top = Math.max(FLOOR, peak);
  let ceiling = top;
  for (const limit of limits) {
    const room = limit * HEADROOM;
    if (limit > 0 && !(peak > 0 && room > REACH * top)) ceiling = Math.max(ceiling, room);
  }
  return ceiling;
}

/** The two limits the graphs mark, in bytes per second, 0 for none. */
export interface Limits {
  download: number;
  upload: number;
}

const KINDS = ['download', 'upload'] as const;

// A limit's figure: its size, and its gap from the limit's height.
const LABEL_SIZE = 12;
const LABEL_GAP = 3;

// The limit sign before the figure: its size, its top within the figure's
// line, and the room between the two.
const SIGN = 9;
const SIGN_TOP = 3;
const SIGN_GAP = 3;

// A limit is a setting, not a reading, so its mark stays out of the curves'
// colours: colour on the graph always means a live value.
const LIMIT_INK = 'var(--carbon-text-muted)';

// Half the height of the glyph at the right edge, and how far the figure stays
// from that edge so the two never touch.
const GLYPH = 4;
const LABEL_X = 2 * GLYPH + 3;

// Drawn from the right edge at the limit's height: a notch pointing into the
// plot, or a chevron pointing up out of it.
const NOTCH = `M0 ${-GLYPH}L${-GLYPH} 0L0 ${GLYPH}Z`;
const CHEVRON = `M${-2 * GLYPH} ${GLYPH}L${-GLYPH} 0L0 ${GLYPH}Z`;

/** Where a limit's mark sits against the ceiling the plot is drawn to. */
export interface LimitMark {
  y: number;
  /** The limit is above the plot's top, so the mark sits on the top edge with a chevron. */
  over: boolean;
}

/** limitMark places a limit in a box whose plot runs from top to base, on a whole pixel. */
export function limitMark(limit: number, ceiling: number, top: number, base: number): LimitMark {
  const over = limit > ceiling;
  const exact = over ? top : base - (limit / ceiling) * (base - top);
  return { y: Math.round(exact), over };
}

/**
 * labelTops places the marks' figures in a box h tall: each above its mark
 * where there is room and under it otherwise, and one that would cover an
 * earlier one moved below that, or above it where the box ends.
 */
export function labelTops(ys: readonly number[], h: number): number[] {
  const tops: number[] = [];
  for (const y of ys) {
    let top = y - LABEL_GAP - LABEL_SIZE;
    if (top < 0) top = y + LABEL_GAP;
    for (const other of tops) {
      if (Math.abs(top - other) <= LABEL_SIZE) {
        const below = other + LABEL_SIZE + 1;
        const above = other - LABEL_SIZE - 1;
        top = below + LABEL_SIZE > h && above >= 0 ? above : below;
      }
    }
    tops.push(top);
  }
  return tops;
}

/** useLimits is the two limits an instance scope has set, kept as one value while neither changes. */
function useLimits(instance: string): Limits {
  const download = useLimitInForce(instance);
  const upload = useUploadLimit(instance);
  return useMemo(() => ({ download, upload }), [download, upload]);
}

/** marksOf is where each limit sits, in KINDS order, null for one not set. */
function marksOf(limits: Limits, ceiling: number, top: number, base: number): (LimitMark | null)[] {
  return KINDS.map((k) => (limits[k] > 0 ? limitMark(limits[k], ceiling, top, base) : null));
}

/**
 * applyMarks moves the drawn LimitMarks. They are set by hand rather than by
 * props, so the glide can move them every frame without a render.
 */
function applyMarks(gs: readonly (SVGGElement | null)[], marks: readonly (LimitMark | null)[], h: number): void {
  const shown = marks.flatMap((m, i) => {
    const g = gs[i];
    return m && g ? [{ g, m }] : [];
  });
  const tops = labelTops(shown.map(({ m }) => m.y), h);
  shown.forEach(({ g, m }, i) => {
    g.setAttribute('transform', `translate(0 ${m.y})`);
    const [glyph, figure] = [g.children[1], g.children[2]];
    glyph.setAttribute('d', m.over ? CHEVRON : NOTCH);
    figure.setAttribute('transform', `translate(0 ${tops[i] - m.y})`);
  });
}

/**
 * LimitMarks draws each limit set as a mark at the right edge with the limit
 * sign and its figure, with no line across the plot, which two limits and two
 * curves would crowd. Their place is left to applyMarks.
 */
function LimitMarks({ limits, w, marks }: { limits: Limits; w: number; marks: RefObject<(SVGGElement | null)[]> }) {
  const { t } = useT();
  return KINDS.map((kind, i) => {
    const limit = limits[kind];
    if (limit <= 0) return null;
    return (
      <g
        key={kind}
        ref={(el) => {
          marks.current[i] = el;
        }}
        data-limit-mark={kind}
      >
        <title>{t(`speed.limit.${kind}`, { speed: fmtSpeed(limit) })}</title>
        <path transform={`translate(${w} 0)`} d={NOTCH} fill={LIMIT_INK} />
        <LimitFigure figure={fmtSpeed(limit)} end={w - LABEL_X} />
      </g>
    );
  });
}

/**
 * LimitFigure is a limit's figure with the limit sign before it, ending at
 * `end`. The sign is placed once the figure's width is known. The halo in the
 * card's own colour keeps both legible where a curve runs under them. The plot
 * does not mirror in a right-to-left language, so neither does the figure.
 */
function LimitFigure({ figure, end }: { figure: string; end: number }) {
  const text = useRef<SVGTextElement>(null);
  const [x, setX] = useState<number | null>(null);
  useLayoutEffect(() => {
    if (text.current) setX(end - text.current.getComputedTextLength() - SIGN_GAP - SIGN);
  }, [figure, end]);
  return (
    <g>
      {x !== null && (
        <IconSpeedLimit
          tight
          x={x}
          y={SIGN_TOP}
          width={SIGN}
          height={SIGN}
          fill={LIMIT_INK}
          stroke="var(--carbon-surface)"
          // In the sign's own units, 14 to its 9px: the figure's halo.
          strokeWidth="4.5"
          strokeLinejoin="round"
          paintOrder="stroke"
          data-limit-sign=""
        />
      )}
      <text
        ref={text}
        x={end}
        y={LABEL_SIZE}
        direction="ltr"
        textAnchor="end"
        fontSize={LABEL_SIZE}
        fill={LIMIT_INK}
        stroke="var(--carbon-surface)"
        strokeWidth="3"
        strokeLinejoin="round"
        paintOrder="stroke"
        className="glim-num"
      >
        {figure}
      </text>
    </g>
  );
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
  return { line, area };
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
  limits: Limits;
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
 * sample; between samples only two attributes change.
 */
function place(
  f: Frame,
  g: Glide,
  els: { mover: SVGGElement; washes: SVGLinearGradientElement[]; marks: (SVGGElement | null)[] },
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

  const moved = `translate(${(f.dx * (1 - p)).toFixed(2)} ${f.base}) scale(1 ${k.toFixed(4)}) translate(0 ${-f.base})`;
  // The washes are pinned to the box, not to the stretched curves, so their
  // shade holds still while the ceiling moves.
  const washTop = (f.base - (f.base - f.top) / k).toFixed(4);
  // The limit marks follow the eased ceiling too, or they would jump while
  // the curves under them are still stretching.
  const marks = marksOf(f.limits, g.eased, f.top, f.base);
  const drawn = `${moved}|${washTop}|${marks.map((m) => (m ? `${m.y}${m.over}` : '')).join('|')}`;
  if (drawn === g.drawn) return;
  g.drawn = drawn;
  els.mover.setAttribute('transform', moved);
  for (const wash of els.washes) wash.setAttribute('y1', washTop);
  applyMarks(els.marks, marks, f.base + f.top);
}

interface PlotProps {
  win: SpeedWindow;
  /** How many steps the width shows. */
  span: number;
  w: number;
  h: number;
  pad: number;
  ceiling: number;
  stroke: number;
  limits: Limits;
}

/**
 * Plot is everything inside a speed svg: the zero line, the curves once
 * anything on screen is above it, and a mark for each limit set.
 * The seeding line is always there, flat along the zero line while nothing
 * goes out.
 */
export function Plot(props: PlotProps) {
  const glide = useSyncExternalStore(subscribeGlide, glideAllowed, () => false);
  const { win, w, h, pad } = props;
  // The sample on the right edge once a step has run. While gliding, the
  // newest one is still past the edge and only shapes the tangent there.
  const anchor = win.samples.length - (glide ? 2 : 1);
  // Idle means nothing on screen in either direction, not a current speed of zero.
  const busy =
    anchor >= 1 &&
    (win.samples.slice(0, anchor + 1).some((v) => v > 0) || win.upload.slice(0, anchor + 1).some((v) => v > 0));
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
      {busy ? (
        <Curve {...props} anchor={anchor} glide={glide} />
      ) : (
        <>
          <line
            data-upload-line=""
            x1="0"
            y1={h - pad}
            x2={w}
            y2={h - pad}
            stroke={SEEDING_INK}
            strokeWidth={props.stroke}
            strokeLinecap="round"
            vectorEffect="non-scaling-stroke"
          />
          <IdleLimits {...props} />
        </>
      )}
    </>
  );
}

/** IdleLimits are the limit marks with no curve to glide with, placed once per change. */
function IdleLimits({ limits, ceiling, w, h, pad }: PlotProps) {
  const marks = useRef<(SVGGElement | null)[]>([]);
  const { download, upload } = limits;
  useLayoutEffect(() => {
    applyMarks(marks.current, marksOf({ download, upload }, ceiling, pad, h - pad), h);
  }, [download, upload, ceiling, h, pad]);
  return <LimitMarks limits={limits} w={w} marks={marks} />;
}

/**
 * Curve draws the download and the upload each as a line over an area filled
 * in its own colour. While motion is allowed it glides left between samples
 * and eases its ceiling; otherwise it moves once per sample.
 */
function Curve({
  win,
  span,
  w,
  h,
  pad,
  ceiling,
  stroke,
  limits,
  anchor,
  glide,
}: PlotProps & { anchor: number; glide: boolean }) {
  const id = useId();
  const dx = w / span;
  const geo = useMemo(
    () => geometry(win.samples, anchor, w, h, pad, dx, ceiling),
    [win.samples, anchor, w, h, pad, dx, ceiling],
  );
  const upload = useMemo(
    () => geometry(win.upload, anchor, w, h, pad, dx, ceiling),
    [win.upload, anchor, w, h, pad, dx, ceiling],
  );

  const mover = useRef<SVGGElement>(null);
  const wash = useRef<SVGLinearGradientElement>(null);
  const seedWash = useRef<SVGLinearGradientElement>(null);
  const marks = useRef<(SVGGElement | null)[]>([]);
  const frame = useRef<Frame | null>(null);
  const state = useRef<Glide>({ eased: ceiling, at: 0, drawn: '' });

  const draw = useCallback((now: number) => {
    const f = frame.current;
    if (f && mover.current && wash.current && seedWash.current) {
      place(
        f,
        state.current,
        { mover: mover.current, washes: [wash.current, seedWash.current], marks: marks.current },
        now,
      );
    }
  }, []);

  // Before paint, so a new sample and the offset that hides its arrival land
  // in the same frame.
  const { download, upload: uploadLimit } = limits;
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
      limits: { download, upload: uploadLimit },
    };
    draw(performance.now());
  }, [draw, geo, glide, win.newestAt, win.step, dx, h, pad, ceiling, download, uploadLimit]);

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
        {/* In user space, so place() can pin them to the box by moving y1.
            Each in its own line's colour, so a line and its area never disagree. */}
        <linearGradient ref={wash} id={`${id}wash`} gradientUnits="userSpaceOnUse" x1="0" x2="0" y2={h - pad}>
          <stop offset="0" stopColor={DOWNLOAD_INK} stopOpacity="0.42" />
          <stop offset="1" stopColor={DOWNLOAD_INK} stopOpacity="0.14" />
        </linearGradient>
        <linearGradient ref={seedWash} id={`${id}seed`} gradientUnits="userSpaceOnUse" x1="0" x2="0" y2={h - pad}>
          <stop offset="0" stopColor={SEEDING_INK} stopOpacity="0.42" />
          <stop offset="1" stopColor={SEEDING_INK} stopOpacity="0.14" />
        </linearGradient>
        <clipPath id={`${id}box`}>
          <rect width={w} height={h} />
        </clipPath>
      </defs>
      {/* The clip sits outside the moving group, or it would travel with it. */}
      <g clipPath={`url(#${id}box)`}>
        <g ref={mover}>
          <path d={geo.area} fill={`url(#${id}wash)`} />
          {/* Over the download's area, which still shows through it where the
              two overlap, and under both lines. */}
          <path data-upload-area="" d={upload.area} fill={`url(#${id}seed)`} />
          {/* Under the download line, which stays the one to read where the
              two meet. */}
          <path
            data-upload-line=""
            d={upload.line}
            fill="none"
            stroke={SEEDING_INK}
            strokeWidth={stroke}
            strokeLinecap="round"
            strokeLinejoin="round"
            vectorEffect="non-scaling-stroke"
          />
          <path
            d={geo.line}
            fill="none"
            stroke={DOWNLOAD_INK}
            strokeWidth={stroke}
            strokeLinecap="round"
            strokeLinejoin="round"
            vectorEffect="non-scaling-stroke"
            className="kl-curve-line"
          />
        </g>
      </g>
      {/* Over the curves, which may run up against them. */}
      <LimitMarks limits={limits} w={w} marks={marks} />
    </>
  );
}

/**
 * spanLabel labels the left end of the time axis with the span the plot
 * covers. A window that is not full yet draws from the right and leaves the
 * rest empty. The units are fmtEta's, untranslated symbols, and switch where
 * the meter's window field switches, so a window of 1.5 min reads -1.5min.
 */
export function spanLabel(seconds: number): string {
  if (seconds < 60) return `-${seconds}s`;
  if (seconds < 3600) return `-${Math.round(seconds / 6) / 10}min`;
  return `-${Math.round(seconds / 360) / 10}h`;
}

// A legend key in the look of a filled curve: its line on top, its area below.
const areaKey = (ink: string) => ({
  borderColor: ink,
  backgroundColor: `color-mix(in srgb, ${ink} 28%, transparent)`,
});

/**
 * SpeedLegend names the two curves, which are both always drawn. The plot is
 * hidden from screen readers, so the limits it marks are read out here.
 */
export function SpeedLegend({ limits }: { limits: Limits }) {
  const { t } = useT();
  const marked = KINDS.filter((k) => limits[k] > 0).map((k) =>
    t(`speed.limit.${k}`, { speed: fmtSpeed(limits[k]) }),
  );
  return (
    <span
      data-speed-legend=""
      data-new="seeding-always"
      className="flex items-center gap-3 whitespace-nowrap text-meta leading-none text-carbon-textMuted"
    >
      <span className="inline-flex items-center gap-1">
        <span className="h-2 w-3 border-t-[1.5px]" style={areaKey(DOWNLOAD_INK)} />
        {t('speed.legend.download')}
      </span>
      <span className="inline-flex items-center gap-1">
        <span className="h-2 w-3 border-t-[1.5px]" style={areaKey(SEEDING_INK)} />
        {t('speed.legend.seeding')}
      </span>
      {marked.length > 0 && <span className="sr-only">{marked.join(', ')}</span>}
      <InfoBubble tip={t('speed.legend.hint', { page: t('settings.nav.appearance') })} />
    </span>
  );
}

/**
 * LiveRates are the download and upload speeds now, each in its curve's
 * colour, so a figure in a curve's colour is always a live one.
 */
function LiveRates({ download, upload }: { download: number; upload: number }) {
  const { t } = useT();
  return (
    <span data-live-rates="" dir="ltr" className="flex items-center gap-2.5 whitespace-nowrap">
      <span className="glim-num text-[12px] font-semibold leading-none" style={{ color: DOWNLOAD_INK }}>
        <span className="sr-only">{t('speed.legend.download')} </span>
        {fmtRate(download)}
      </span>
      <span className="glim-num text-[12px] font-semibold leading-none" style={{ color: SEEDING_INK }}>
        <span className="sr-only">{t('speed.legend.seeding')} </span>
        {fmtRate(upload)}
      </span>
    </span>
  );
}

/**
 * SpeedGraph draws this instance's download speed over the last minute or hour
 * as the Overview page's hero, and beside it what the torrents upload. Idle
 * shows a flat hairline. It always uses scope '', since the Overview page
 * reads only the local instance.
 */
export function SpeedGraph({
  value,
  upload = 0,
  height = 96,
  points = 60,
  limit = 0,
}: {
  value: number;
  /** What the torrents send now, in bytes per second. */
  upload?: number;
  height?: number;
  points?: number;
  /** The speed limit in bytes per second, or 0; the page already has settings. */
  limit?: number;
}) {
  const { t } = useT();

  // The stored window loads after first paint; both scales are already in the
  // store, so the switch costs no request.
  const [stored, setStored] = useUIState<SpeedScale>('overview.speedWindow', 'minute');
  const scale: SpeedScale = stored === 'hour' ? 'hour' : 'minute';
  const span = scale === 'hour' ? HOUR_POINTS : points;

  const win = useSpeedWindow('', value, upload, span + OFFSTAGE, scale);
  const limits = useLimits('');
  const ceiling = useMemo(
    () => ceilingOf(win.samples.concat(win.upload), limits.download, limits.upload),
    [win, limits],
  );
  // Drawn in pixels, so the limits' figures are not stretched with the plot.
  const svg = useRef<SVGSVGElement>(null);
  const { w } = usePixelBox(svg);

  return (
    /* kl-storm-curve is the 1337 easter egg (docs/easter-eggs.md): at a limit of
       exactly 1337 kB/s this plot runs on the hidden motion level. It only
       sets two custom properties, so motion "off" and reduced motion still
       win, and the reader's data-motion setting is left alone. */
    <div data-new="seeding-line" className={`relative ${isLeet(limit) ? 'kl-storm-curve' : ''}`}>
      {/* The ceiling above the plot, in every state, with the window chooser
          on the same line (GlimStone 1.6.0). */}
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
        <Tabs
          inline
          label={t('overview.speedWindow')}
          size="sm"
          select="one"
          active={scale}
          onSelect={(id) => setStored(id === 'hour' ? 'hour' : 'minute')}
          items={[
            { id: 'minute', label: t('overview.speedWindow.minute') },
            { id: 'hour', label: t('overview.speedWindow.hour') },
          ]}
        />
        <span className="ms-auto flex items-center gap-1.5">
          <LiveRates download={value} upload={upload} />
          <InfoBubble tip={t('overview.speedGraphHint')} />
          <span className="glim-num text-meta leading-none text-carbon-textMuted">
            {fmtSpeed(ceiling)}
          </span>
        </span>
      </div>
      <svg
        ref={svg}
        viewBox={`0 0 ${w} ${height}`}
        preserveAspectRatio="none"
        className="block w-full"
        style={{ height }}
        aria-hidden
        data-new="limit-markers"
      >
        <Plot win={win} span={span} w={w} h={height} pad={6} ceiling={ceiling} stroke={1.75} limits={limits} />
      </svg>
      {/* Both ends of the time axis, oldest on the left. The plot does not
          mirror in a right-to-left language, so neither do its labels. */}
      <div dir="ltr" className="flex items-center justify-between gap-3">
        <span className="glim-num text-meta leading-none text-carbon-textMuted">{spanLabel(span * win.step)}</span>
        <SpeedLegend limits={limits} />
        <span className="glim-num text-meta leading-none text-carbon-textMuted">0s</span>
      </div>
    </div>
  );
}

/**
 * usePixelBox follows an svg's rendered size, so its viewBox can be drawn in
 * pixels and the padding stays a few pixels at any height. A hidden bar
 * measures nothing, so the last real size is kept.
 */
function usePixelBox(ref: RefObject<SVGSVGElement | null>): { w: number; h: number } {
  const [box, setBox] = useState({ w: 148, h: 40 });
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const seen = new ResizeObserver(([entry]) => {
      const w = Math.round(entry.contentRect.width);
      const h = Math.round(entry.contentRect.height);
      if (w > 0 && h > 0) setBox((b) => (b.w === w && b.h === h ? b : { w, h }));
    });
    seen.observe(el);
    return () => seen.disconnect();
  }, [ref]);
  return box;
}

/**
 * SpeedMeter is the shell-bar reading: the window the quick settings choose
 * as a curve that takes the rest of the bar's row, newest sample on its right
 * edge, with the current figures above that end and the top of the plot above
 * the other. `instance` is the shell's scope, which can be a peer; only scope
 * '' is seeded, since /api/stats/speed is not forwarded, so a peer's curve
 * starts empty and grows in from the right.
 */
export function SpeedMeter({
  value,
  upload = 0,
  instance = '',
}: {
  value: number;
  /** What the torrents send now, in bytes per second. */
  upload?: number;
  instance?: string;
}) {
  const [seconds] = useMeterWindow();
  const scale: SpeedScale = seconds > FINE_SPAN_S ? 'hour' : 'minute';
  const points = seconds / stepOf(scale);
  const win = useSpeedWindow(instance, value, upload, points + OFFSTAGE, scale);
  const limits = useLimits(instance);
  const ceiling = useMemo(
    () => ceilingOf(win.samples.concat(win.upload), limits.download, limits.upload),
    [win, limits],
  );
  const svg = useRef<SVGSVGElement>(null);
  const { w, h } = usePixelBox(svg);

  return (
    // dir="ltr": the time axis runs left to right in every language, and its
    // labels have to stay at the ends of the plot they name. basis-60 is where
    // the bar wraps the meter under the squares instead of squeezing it.
    // self-stretch and no h-full: a percentage height would take the meter out
    // of the stretch against the card's indefinite height. The negative margin
    // lays the figures into the card's padding, so the plot reaches nearer the
    // card's edges than the squares do; min-h-22 is what makes the card taller
    // than the squares.
    <span dir="ltr" className="-my-2 flex min-h-22 min-w-0 grow basis-60 flex-col gap-0.5 self-stretch">
      <span className="flex items-end justify-between gap-3 whitespace-nowrap leading-none">
        <span className="glim-num text-meta text-carbon-textMuted">{fmtSpeed(ceiling)}</span>
        <LiveRates download={value} upload={upload} />
      </span>
      {/* h-0 with flex-auto: without a height the svg's aspect ratio would set
          the card's height from its width. flex-1 does not work here, because
          a 0% basis in an indefinite column falls back to content size.
          check-stretched-svg-height.mjs guards this. */}
      <svg
        ref={svg}
        viewBox={`0 0 ${w} ${h}`}
        preserveAspectRatio="none"
        className="h-0 w-full flex-auto"
        aria-hidden
        focusable="false"
        data-new="limit-markers"
      >
        <Plot win={win} span={points} w={w} h={h} pad={3} ceiling={ceiling} stroke={1.5} limits={limits} />
      </svg>
      <span className="flex items-center justify-between gap-3 text-meta leading-none text-carbon-textMuted">
        <span className="glim-num">{spanLabel(points * win.step)}</span>
        <SpeedLegend limits={limits} />
        <span className="glim-num">0s</span>
      </span>
    </span>
  );
}
