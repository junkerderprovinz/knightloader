// Monochrome inline icons (currentColor) at 22px, in the GlimStone house style.
//
// Every meaning GlimStone fixes a drawing for comes from glyphs.tsx, which is
// generated from its glyph list. The drawings below are KnightLoader's own,
// for meanings that list does not have. They follow the same rules: a filled
// shape, never a stroked outline, with a gap carved by fillRule="evenodd"
// rather than painted over in a background colour, and the measured ink of
// each handed to glyph(), which gives it the same share of the box.
import type { SVGProps } from 'react';
import { glyph, type GlyphProps } from './glyphs';

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

/** The link collector: a magnifying glass with a dot in its lens, so it is not
 *  taken for IconSearch, which filters a list. */
export const IconCollector = (p: GlyphProps) => (
  <svg {...glyph([2.75, 2.75, 14.5, 14.5], p)}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M8.6 2.75a5.85 5.85 0 1 1 0 11.7 5.85 5.85 0 0 1 0-11.7Zm0 1.8a4.05 4.05 0 1 0 0 8.1 4.05 4.05 0 0 0 0-8.1Z"
    />
    <circle cx="8.6" cy="8.6" r="1.7" />
    <rect x="13.55" y="11.53" width="1.9" height="5.94" rx=".95" transform="rotate(-45 14.5 14.5)" />
  </svg>
);

/** The Downloads page: a bold arrow pointing down, with no disc round it, so
 *  it is not taken for IconDownload, which marks a transfer's state. */
export const IconDownloadArrow = (p: GlyphProps) => (
  <svg {...glyph([4.4, 2.75, 11.2, 12], p)}>
    <path d="M10 2.75a1.1 1.1 0 0 1 1.1 1.1v6.6l2.35-2.35a1.1 1.1 0 0 1 1.56 1.56l-4.23 4.23a1.55 1.55 0 0 1-2.76 0L4.99 9.66a1.1 1.1 0 0 1 1.56-1.56L8.9 10.45v-6.6A1.1 1.1 0 0 1 10 2.75Z" />
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

/** A container: Docker's whale from Dashboard Icons (Apache-2.0), the mark the
 *  Apps tab's Docker button wears, named here only for what it is. */
export const IconContainer = (p: GlyphProps) => (
  <svg {...glyph([-0.07, 71.3, 511.97, 369.5], p)}>
    <path d="M501.4 212.3c-11.5-8-38-11-58.6-7-2.4-20-13.5-37.5-32.7-53l-11-8-7.7 11.5c-9.6 15-14.4 36-13 56 .5 7 2.9 19.5 10.1 30.5-6.7 4-20.7 9-38.9 9H2.3l-1 4c-3.4 20-3.4 82.5 36 130.5 29.8 36.5 74 55 132.1 55 125.9 0 219.1-60.5 262.8-170 17.3.5 54.3 0 73-37.5.5-1 1.4-3 4.8-10.5l1.9-4zM280 71.3h-52.8v50H280zm0 60h-52.8v50H280zm-62.5 0h-52.8v50h52.8zm-62.4 0h-52.8v50h52.8zm-62.5 60H39.8v50h52.8zm62.5 0h-52.8v50h52.8zm62.4 0h-52.8v50h52.8zm62.5 0h-52.8v50H280zm62.4 0h-52.8v50h52.8z" />
  </svg>
);

/** A server run as a plain program: two rack units, each with its light and
 *  its slot. */
export const IconServer = (p: GlyphProps) => (
  <svg {...glyph([1, 1.5, 12, 11], p)}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M2.5 1.5h9A1.5 1.5 0 0 1 13 3v1.75a1.5 1.5 0 0 1-1.5 1.5h-9A1.5 1.5 0 0 1 1 4.75V3a1.5 1.5 0 0 1 1.5-1.5ZM4 3.125a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5ZM7 3.25h3.5a.625.625 0 0 1 0 1.25H7a.625.625 0 0 1 0-1.25ZM2.5 7.75h9A1.5 1.5 0 0 1 13 9.25V11a1.5 1.5 0 0 1-1.5 1.5h-9A1.5 1.5 0 0 1 1 11V9.25a1.5 1.5 0 0 1 1.5-1.5ZM4 9.375a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5ZM7 9.5h3.5a.625.625 0 0 1 0 1.25H7a.625.625 0 0 1 0-1.25Z"
    />
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

/** A speed limit: a sign's ring round a gauge needle, as the phone app draws
 *  it. Only the limit marks on the speed curves wear it. */
export const IconSpeedLimit = (p: GlyphProps) => (
  <svg {...glyph([0, 0, 14, 14], p)}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M7 0a7 7 0 1 1 0 14A7 7 0 0 1 7 0Zm0 1.75a5.25 5.25 0 1 0 0 10.5 5.25 5.25 0 0 0 0-10.5ZM8.01 9.27L10.29 6.45A.95.95 0 0 0 8.94 5.12L6.16 7.45A1.3 1.3 0 1 0 8.01 9.27Z"
    />
  </svg>
);

/** A plug: two prongs, the body and its cable, for what this instance is
 *  plugged into or plugs into it. */
export const IconPlug = (p: GlyphProps) => (
  <svg {...glyph([4.5, 2, 11, 16], p)}>
    <rect x="6.6" y="2" width="1.8" height="4.6" rx=".9" />
    <rect x="11.6" y="2" width="1.8" height="4.6" rx=".9" />
    <path d="M5.3 6.1h9.4a.8.8 0 0 1 .8.8v1.6a4.4 4.4 0 0 1-4.4 4.4H8.9a4.4 4.4 0 0 1-4.4-4.4V6.9a.8.8 0 0 1 .8-.8Z" />
    <rect x="9.1" y="12.4" width="1.8" height="5.6" rx=".9" />
  </svg>
);
