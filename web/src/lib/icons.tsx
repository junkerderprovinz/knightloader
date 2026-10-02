// Monochrome inline icons (currentColor) at 22px, in the GlimStone house style.
//
// Every meaning GlimStone fixes a drawing for comes from glyphs.tsx, generated
// from its glyph list. The few below are KnightLoader's own, for meanings that
// list does not have. They follow the same rules: a filled shape, never a
// stroked outline, with a gap carved by fillRule="evenodd" rather than painted
// over in a background colour, and a viewBox cropped square to the ink and
// widened by a third so the ink fills three quarters of the box.
import type { SVGProps } from 'react';
import { base } from './glyphs';

export * from './glyphs';

/** A monitor on its foot, for an instance that is the desktop app. */
export const IconDesktop = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base({ viewBox: '-0.667 -0.667 21.333 21.333', ...p })}>
    <path
      fillRule="evenodd"
      d="M3.5 2.5A1.5 1.5 0 0 0 2 4v8.5A1.5 1.5 0 0 0 3.5 14h5v1.5H6v2h8v-2h-2.5V14h5a1.5 1.5 0 0 0 1.5-1.5V4a1.5 1.5 0 0 0-1.5-1.5h-13ZM4 4.5h12V12H4V4.5Z"
    />
  </svg>
);

/** A clipboard, clip and all, for pasting from it. Copying wears IconCopy. */
export const IconClipboard = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base({ viewBox: '-0.167 -0.792 20.333 20.333', ...p })}>
    <rect x="5" y="4.5" width="10" height="12.5" rx="1.5" />
    <path opacity=".7" d="M7.5 3a1.25 1.25 0 0 1 1.25-1.25h2.5A1.25 1.25 0 0 1 12.5 3v1.75h-5V3Z" />
  </svg>
);

/** IconShield is a plain shield for the parade in lib/toast.tsx, where at 11px
 *  any detail carved into it would blur. */
export const IconShield = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base({ viewBox: '0 0 20 20', ...p })}>
    <path d="M10 2.7 4 4.8v5.7c0 2.9 2.4 5.4 6 6.4 3.6-1 6-3.5 6-6.4V4.8L10 2.7Z" />
  </svg>
);

/**
 * PriorityGlyph draws one rung of the priority ladder: one filled triangle per
 * step away from default, pointing the way the rung moves a download, and a
 * flat bar for default. The context menu and the download list both use it.
 * Triangles rather than arrows, since arrows mean a one-place move. The rung
 * comes from the server's value (-3..3, clamped), not from the id.
 */
export function PriorityGlyph({ steps }: { steps: number }) {
  const n = Math.min(3, Math.abs(steps));
  const up = steps > 0;
  // 5 tall per wedge, 1 of air between them, centred in a 20-unit box.
  const top = (20 - (n * 6 - 1)) / 2;
  return (
    <svg
      viewBox="0 0 20 20"
      width={14}
      height={14}
      fill="currentColor"
      className="shrink-0"
      aria-hidden
      focusable="false"
    >
      {n === 0 ? (
        <rect x="4.5" y="9.1" width="11" height="1.8" rx=".9" />
      ) : (
        Array.from({ length: n }, (_, i) => {
          const y = top + i * 6;
          return <path key={i} d={up ? `M10 ${y}L15 ${y + 5}H5Z` : `M5 ${y}H15L10 ${y + 5}Z`} />;
        })
      )}
    </svg>
  );
}
