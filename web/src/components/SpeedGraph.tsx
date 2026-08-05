import { useEffect, useRef, useState } from 'react';
import { fmtSpeed } from '../lib/format';
import { useT } from '../lib/i18n';

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
  const [samples, setSamples] = useState<number[]>(() => Array(points).fill(0));
  const valRef = useRef(value);
  valRef.current = value;
  const ceilingRef = useRef(0);

  useEffect(() => {
    const iv = setInterval(() => setSamples((s) => [...s.slice(1), valRef.current]), 1000);
    return () => clearInterval(iv);
  }, []);

  const W = 600;
  const H = height;
  const pad = 6;
  const peak = Math.max(...samples);

  // Ceiling jumps up at once, eases down 8% per tick, and never drops below a
  // floor so small transfers stay visually small.
  const FLOOR = 64 * 1024;
  const target = Math.max(peak, FLOOR);
  ceilingRef.current =
    target > ceilingRef.current ? target : Math.max(target, ceilingRef.current * 0.92);
  const max = ceilingRef.current;

  const idle = peak === 0;
  const y = (v: number) => H - pad - (v / max) * (H - pad * 2);
  const pts = samples.map((v, i) => [i * (W / (samples.length - 1)), y(v)] as const);

  let d = `M${pts[0][0]},${pts[0][1].toFixed(1)}`;
  for (let i = 0; i < pts.length - 1; i++) {
    const [x0, y0] = pts[i];
    const [x1, y1] = pts[i + 1];
    const cx = (x0 + x1) / 2;
    d += ` C${cx.toFixed(1)},${y0.toFixed(1)} ${cx.toFixed(1)},${y1.toFixed(1)} ${x1.toFixed(1)},${y1.toFixed(1)}`;
  }
  const last = pts[pts.length - 1];

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
              <linearGradient id="keep-speed-fill" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="var(--accent)" stopOpacity="0.26" />
                <stop offset="100%" stopColor="var(--accent)" stopOpacity="0" />
              </linearGradient>
            </defs>
            <path d={`${d} L${W},${H} L0,${H} Z`} fill="url(#keep-speed-fill)" />
            <path
              d={d}
              fill="none"
              stroke="var(--accent)"
              strokeWidth="1.75"
              strokeLinecap="round"
              vectorEffect="non-scaling-stroke"
            />
            <circle cx={last[0]} cy={last[1]} r="3" fill="var(--accent)" className="keep-live" />
          </>
        )}
      </svg>
      <span className="keep-eyebrow keep-num absolute right-0 top-0">
        {idle ? t('overview.idle') : `${t('overview.peak')} ${fmtSpeed(peak)}`}
      </span>
    </div>
  );
}
