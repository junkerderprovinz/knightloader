import { useEffect, useRef, useState } from 'react';
import { fmtSpeed } from '../lib/format';

// SpeedGraph samples a live value (bytes/s) once a second into a rolling window
// and draws a smooth accent area with a soft gradient, a faint baseline and a
// labelled endpoint. It is the hero element of a page — nothing else on the
// same screen should compete with it.
export function SpeedGraph({
  value,
  height = 96,
  points = 60,
  showPeak = true,
}: {
  value: number;
  height?: number;
  points?: number;
  showPeak?: boolean;
}) {
  const [samples, setSamples] = useState<number[]>(() => Array(points).fill(0));
  const valRef = useRef(value);
  valRef.current = value;

  useEffect(() => {
    const iv = setInterval(() => setSamples((s) => [...s.slice(1), valRef.current]), 1000);
    return () => clearInterval(iv);
  }, []);

  const W = 600;
  const H = height;
  const pad = 6;
  const peak = Math.max(...samples);
  const max = Math.max(1, peak);
  const step = W / (samples.length - 1);
  const y = (v: number) => H - pad - (v / max) * (H - pad * 2);

  // Smooth the polyline with a light cardinal spline so the curve reads as a
  // signal rather than a saw.
  const pts = samples.map((v, i) => [i * step, y(v)] as const);
  let d = `M${pts[0][0]},${pts[0][1].toFixed(1)}`;
  for (let i = 0; i < pts.length - 1; i++) {
    const [x0, y0] = pts[i];
    const [x1, y1] = pts[i + 1];
    const cx = (x0 + x1) / 2;
    d += ` C${cx.toFixed(1)},${y0.toFixed(1)} ${cx.toFixed(1)},${y1.toFixed(1)} ${x1.toFixed(1)},${y1.toFixed(1)}`;
  }
  const area = `${d} L${W},${H} L0,${H} Z`;
  const last = pts[pts.length - 1];

  return (
    <div className="relative">
      <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="w-full block" style={{ height }} aria-hidden>
        <defs>
          <linearGradient id="kl-speed-fill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--accent)" stopOpacity="0.28" />
            <stop offset="100%" stopColor="var(--accent)" stopOpacity="0" />
          </linearGradient>
        </defs>
        <path d={area} fill="url(#kl-speed-fill)" />
        <path
          d={d}
          fill="none"
          stroke="var(--accent)"
          strokeWidth="1.75"
          strokeLinecap="round"
          vectorEffect="non-scaling-stroke"
        />
        <circle cx={last[0]} cy={last[1]} r="3" fill="var(--accent)" vectorEffect="non-scaling-stroke" />
      </svg>
      {showPeak && peak > 0 && (
        <span className="absolute right-0 top-0 kl-eyebrow kl-num">peak {fmtSpeed(peak)}</span>
      )}
    </div>
  );
}
