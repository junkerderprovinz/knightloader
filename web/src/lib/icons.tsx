// Monochrome inline icons (currentColor) at 22px, in the GlimStone house style.
//
// Every meaning GlimStone fixes a drawing for comes from glyphs.tsx, which is
// generated from its glyph list. The drawings below are KnightLoader's own,
// for meanings that list does not have. They follow the same rules: a filled
// shape, never a stroked outline, with a gap carved by fillRule="evenodd"
// rather than painted over in a background colour, and the measured ink of
// each handed to glyph(), which gives it the same share of the box.
import type { SVGProps } from 'react';
import { glyph } from './glyphs';

export * from './glyphs';

/** Play a file: the play mark on a disc, so it is not taken for the arrow
 *  that starts or resumes a download. */
export const IconPlayFile = (p: SVGProps<SVGSVGElement>) => (
  <svg {...glyph([2.75, 2.75, 14.5, 14.5], p)}>
    <circle cx="10" cy="10" r="7.25" opacity=".3" />
    <path d="M8.2 6.6v6.8a.7.7 0 0 0 1.07.6l5.3-3.4a.7.7 0 0 0 0-1.2l-5.3-3.4a.7.7 0 0 0-1.07.6Z" />
  </svg>
);

/** A globe, for the internet and what is fetched from it. */
export const IconGlobe = (p: SVGProps<SVGSVGElement>) => (
  <svg {...glyph([2.75, 2.75, 14.5, 14.5], p)}>
    <path
      opacity=".35"
      fillRule="evenodd"
      clipRule="evenodd"
      d="M10 2.75a7.25 7.25 0 1 1 0 14.5 7.25 7.25 0 0 1 0-14.5Zm0 2.25a5 5 0 1 0 0 10 5 5 0 0 0 0-10Z"
    />
    <rect x="2.75" y="9.25" width="14.5" height="1.5" rx=".75" />
    <rect x="9.25" y="2.75" width="1.5" height="14.5" rx=".75" />
  </svg>
);

/** Manual: a closed book with its title lines and the page edge carved out. */
export const IconBook = (p: SVGProps<SVGSVGElement>) => (
  <svg {...glyph([5, 3.5, 10, 13], p)}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M5 3.5h8.5A1.5 1.5 0 0 1 15 5v11.5H6.8A1.8 1.8 0 0 1 5 14.7Zm2 2.7v1.5h6V6.2Zm0 3v1.4h4V9.2Zm-.2 4.3a.75.75 0 0 0 0 1.5h6.7v-1.5Z"
    />
  </svg>
);

/** The stop mark: a flag where the queue stops. A pennant, so it is not
 *  mistaken for IconStop's square. */
export const IconStopMark = (p: SVGProps<SVGSVGElement>) => (
  <svg {...glyph([4.4, 2.5, 11.2, 15], p)}>
    <rect x="4.4" y="2.5" width="1.8" height="15" rx=".9" />
    <path d="M6.8 3.2h8.8l-2.1 3.4 2.1 3.4H6.8Z" />
  </svg>
);

/** IconShield is a plain shield for the parade in lib/toast.tsx: at 11px a
 *  detail carved into it would blur. */
export const IconShield = (p: SVGProps<SVGSVGElement>) => (
  <svg {...glyph([4, 2.7, 12, 14.2], p)}>
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
