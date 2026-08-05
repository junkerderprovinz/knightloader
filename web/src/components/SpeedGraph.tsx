import { useEffect, useRef, useState } from 'react';

// SpeedGraph samples a live value (bytes/s) once a second into a rolling window
// and draws a smooth accent area sparkline. Self-contained; just feed it `value`.
export function SpeedGraph({ value, height = 56, points = 48 }: { value: number; height?: number; points?: number }) {
  const [samples, setSamples] = useState<number[]>(() => Array(points).fill(0));
  const valRef = useRef(value);
  valRef.current = value;
  useEffect(() => {
    const iv = setInterval(() => setSamples((s) => [...s.slice(1), valRef.current]), 1000);
    return () => clearInterval(iv);
  }, []);

  const W = 300;
  const H = height;
  const max = Math.max(1, ...samples);
  const step = W / (samples.length - 1);
  const pts = samples.map((v, i) => [i * step, H - (v / max) * (H - 6) - 3] as const);
  const line = pts.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(1)},${y.toFixed(1)}`).join(' ');
  const area = `${line} L${W},${H} L0,${H} Z`;

  return (
    <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="w-full" style={{ height }} aria-hidden>
      <path d={area} fill="var(--accent)" opacity="0.12" />
      <path d={line} fill="none" stroke="var(--accent)" strokeWidth="1.75" vectorEffect="non-scaling-stroke" />
    </svg>
  );
}
