import type { ReactElement } from 'react';
import type { QRMatrix } from '../lib/api';

/**
 * QRCode renders the server-computed module grid from routes_remote.go as
 * inline SVG. It stays black on white in every colour mode, because themed
 * colours can drop the contrast below what a phone camera reads.
 */
export function QRCode({
  matrix,
  label,
  size = 176,
  bare = false,
}: {
  matrix: QRMatrix;
  /** The accessible name: what scanning the code leads to. */
  label: string;
  size?: number;
  /** Without the white card around it, for a caller that is white already. */
  bare?: boolean;
}) {
  // The quiet zone the QR spec requires around the modules.
  const quiet = 4;
  const total = matrix.size + quiet * 2;
  const modules: ReactElement[] = [];
  for (let y = 0; y < matrix.size; y++) {
    const row = matrix.bits[y] ?? '';
    for (let x = 0; x < matrix.size; x++) {
      if (row[x] === '1') {
        modules.push(<rect key={`${x}-${y}`} x={x + quiet} y={y + quiet} width={1} height={1} />);
      }
    }
  }

  const code = (
    <svg
      viewBox={`0 0 ${total} ${total}`}
      width={size}
      height={size}
      shapeRendering="crispEdges"
      role="img"
      aria-label={label}
    >
      <rect x={0} y={0} width={total} height={total} fill="#ffffff" />
      <g fill="#000000">{modules}</g>
    </svg>
  );
  if (bare) return code;
  return (
    <div className="inline-block rounded-[var(--radius-card)] bg-white p-3 shadow-[0_1px_3px_rgba(0,0,0,0.25)]">
      {code}
    </div>
  );
}
