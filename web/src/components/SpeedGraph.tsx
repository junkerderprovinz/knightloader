import { useEffect, useRef, useState } from 'react';
import { fmtSpeed } from '../lib/format';
import { useT } from '../lib/i18n';

/**
 * useSpeedSamples is the rolling window both readings share: the live value
 * (bytes/s) taken once a second, oldest first.
 *
 * The value is read through a ref rather than being a dependency of the
 * interval. It changes on every websocket frame, and re-arming the timer each
 * time would restart the second — so the window would sample whenever the
 * traffic happened to arrive instead of at a steady tick, and the curve would
 * bunch and stretch with the very thing it is measuring.
 */
function useSpeedSamples(value: number, points: number): number[] {
  const [samples, setSamples] = useState<number[]>(() => Array(points).fill(0));
  const valRef = useRef(value);
  valRef.current = value;
  useEffect(() => {
    const iv = setInterval(() => setSamples((s) => [...s.slice(1), valRef.current]), 1000);
    return () => clearInterval(iv);
  }, []);
  return samples;
}

/**
 * ceilingFor is the vertical scale: up at once, down 8% a tick, never below a
 * floor. Rising instantly keeps a spike inside the box; relaxing slowly stops a
 * brief blip near idle from re-normalising the whole curve into a mountain, and
 * the floor keeps a small transfer looking small instead of filling the frame.
 */
const FLOOR = 64 * 1024;

function ceilingFor(previous: number, peak: number): number {
  const target = Math.max(peak, FLOOR);
  return target > previous ? target : Math.max(target, previous * 0.92);
}

/** smoothPath draws the samples as one curve, scaled into a box. */
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

// SpeedGraph samples a live value (bytes/s) once a second into a rolling window
// and draws a smooth accent area. It is the hero of the Overview page.
//
// Two things keep it honest: while nothing is downloading it shows a flat
// hairline rather than a gold smear pinned to the floor, and the vertical scale
// rises instantly but relaxes slowly, so a brief blip near idle doesn't
// re-normalise the whole curve into a mountain.
export function SpeedGraph({
  value,
  height = 96,
  points = 60,
}: {
  value: number;
  height?: number;
  points?: number;
}) {
  const { t } = useT();
  const samples = useSpeedSamples(value, points);
  const ceilingRef = useRef(0);

  const W = 600;
  const H = height;
  const pad = 6;
  const peak = Math.max(...samples);

  ceilingRef.current = ceilingFor(ceilingRef.current, peak);
  const idle = peak === 0;
  const { d, last } = smoothPath(samples, W, H, pad, ceilingRef.current);

  return (
    <div className="relative">
      <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="block w-full" style={{ height }} aria-hidden>
        {idle ? (
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
          <>
            <defs>
              <linearGradient id="glim-speed-fill" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="var(--accent)" stopOpacity="0.26" />
                <stop offset="100%" stopColor="var(--accent)" stopOpacity="0" />
              </linearGradient>
            </defs>
            <path d={`${d} L${W},${H} L0,${H} Z`} fill="url(#glim-speed-fill)" />
            <path
              d={d}
              fill="none"
              stroke="var(--accent)"
              strokeWidth="1.75"
              strokeLinecap="round"
              vectorEffect="non-scaling-stroke"
            />
            <circle cx={last[0]} cy={last[1]} r="3" fill="var(--accent)" className="glim-live" />
          </>
        )}
      </svg>
      {/* The peak, and nothing when there is none (jdp, 2026-09-01: "der ruhig
          text soll weg"). "ruhig" restated what an empty curve already shows,
          in a corner of the very graph that was showing it - and a word for
          "nothing is happening" is the one caption a graph never needs, because
          a flat line at zero says it better and takes no words. The peak stays:
          that is a number the picture cannot carry on its own. */}
      {!idle && (
        <span className="glim-eyebrow glim-num absolute right-0 top-0">
          {`${t('overview.peak')} ${fmtSpeed(peak)}`}
        </span>
      )}
    </div>
  );
}

/**
 * SpeedMeter is the same reading at shell-bar size: the figure, with the last
 * half-minute of it drawn behind.
 *
 * `onOpen` makes the whole thing a button, and that is the point rather than a
 * convenience. Somebody watching the speed is already looking at the thing they
 * want to change, and making them find a gear two controls away is asking them
 * to leave what they are reading. The gear stays as well, for anyone who never
 * discovers that a number can be pressed.
 *
 * It keeps a window of its own instead of taking the graph's. Two components
 * cannot share one rolling buffer without hoisting it above both, and the
 * Overview page's hero is mounted and unmounted by navigation while this is not
 * — so a shared window would empty itself whenever somebody left that page.
 */
export function SpeedMeter({
  value,
  onOpen,
  label,
  points = 30,
}: {
  value: number;
  /** Absent when the panel would act on a different machine than the list. */
  onOpen?: () => void;
  /** What pressing it does, for a screen reader. */
  label: string;
  points?: number;
}) {
  const samples = useSpeedSamples(value, points);
  const ceilingRef = useRef(0);

  const W = 72;
  const H = 18;
  const peak = Math.max(...samples);
  ceilingRef.current = ceilingFor(ceilingRef.current, peak);
  const idle = peak === 0;
  const { d } = smoothPath(samples, W, H, 2, ceilingRef.current);

  const inner = (
    <>
      <svg
        viewBox={`0 0 ${W} ${H}`}
        preserveAspectRatio="none"
        width={W}
        height={H}
        className="shrink-0"
        aria-hidden
        focusable="false"
      >
        {idle ? (
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
          <path
            d={d}
            fill="none"
            stroke="var(--accent)"
            strokeWidth="1.5"
            strokeLinecap="round"
            vectorEffect="non-scaling-stroke"
          />
        )}
      </svg>
      {/* dir="ltr": the number and its unit are one token and must not be
          reordered into "s/BiM 4.2" in an Arabic or Hebrew locale. */}
      <span dir="ltr" className="glim-num text-[13px] font-semibold leading-none text-carbon-text">
        {fmtSpeed(value) || '0 B/s'}
      </span>
    </>
  );

  const shell = 'flex items-center gap-2 rounded-[var(--radius-control)] px-1.5 py-1';

  if (!onOpen) return <span className={shell}>{inner}</span>;

  return (
    <button
      type="button"
      onClick={onOpen}
      aria-label={label}
      title={label}
      className={`${shell} transition-colors hover:bg-carbon-hover
        outline-none focus-visible:shadow-[0_0_0_2px_var(--focus-ring)]`}
    >
      {inner}
    </button>
  );
}
