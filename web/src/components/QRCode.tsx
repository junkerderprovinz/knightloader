import type { ReactElement } from 'react';
import type { QRMatrix } from '../lib/api';

/**
 * QRCode renders a server-computed module grid (api.QRMatrix,
 * routes_remote.go's renderQR) as inline SVG.
 *
 * Always plain black modules on a plain white ground, in every colour mode -
 * deliberately the one place in this app that does not follow
 * always-integrate-new-elements-into-color-modes. A scanner reads contrast
 * between "dark" and "light" modules, and this app's own dark-mode surface
 * tokens or accent colour can drop that contrast low enough to fail an
 * ordinary phone camera in a normal room. What the code decodes to comes
 * first; the white frame below is part of making that true, not a themed
 * choice.
 *
 * The quiet zone (the blank margin the QR spec requires around the modules
 * for a scanner to find the code at all) is added here rather than by the
 * server: it is a rendering concern, the same reason padding lives in CSS
 * and not in a JSON payload, and this component is the one place that
 * already knows the module size in pixels.
 *
 * Encoding itself is not done here, or anywhere in this codebase by hand -
 * see routes_remote.go's own comment on why that stays server-side, on a
 * small, well-established Go library, rather than a hand-rolled or
 * hand-vendored encoder on either side.
 */
export function QRCode({
  matrix,
  label,
  size = 176,
}: {
  matrix: QRMatrix;
  /** The accessible name for the image - what scanning it leads to, in
   *  words, since nothing here reads pixels back into meaning. */
  label: string;
  size?: number;
}) {
  const quiet = 4;
  const total = matrix.size + quiet * 2;
  // ReactElement, not the global JSX namespace: React 19 stopped declaring
  // that namespace globally, so `JSX.Element` no longer resolves without
  // pulling it off React itself.
  const modules: ReactElement[] = [];
  for (let y = 0; y < matrix.size; y++) {
    const row = matrix.bits[y] ?? '';
    for (let x = 0; x < matrix.size; x++) {
      if (row[x] === '1') {
        modules.push(<rect key={`${x}-${y}`} x={x + quiet} y={y + quiet} width={1} height={1} />);
      }
    }
  }

  return (
    <div className="inline-block rounded-[var(--radius-card)] bg-white p-3 shadow-[0_1px_3px_rgba(0,0,0,0.25)]">
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
    </div>
  );
}
