// Monochrome inline icons (currentColor) at 22px, in the GlimStone house style.
//
// Every meaning GlimStone fixes a drawing for comes from glyphs.tsx, which is
// generated from its glyph list. The drawings below are for meanings that
// list does not have, drawn for KnightLoader or taken from Streamline's free
// Core Solid set (https://streamlinehq.com, CC BY 4.0). They follow the same
// rules: a filled shape, never a stroked outline, with a gap carved by
// fillRule="evenodd" rather than painted over in a background colour, and the
// measured ink of each handed to glyph(), which gives it the same share of the
// box.
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
  <svg {...glyph([4.67, 2.75, 10.66, 11.47], p)}>
    <path d="M10 2.75a1.1 1.1 0 0 1 1.1 1.1v6.6l2.35-2.35a1.1 1.1 0 0 1 1.56 1.56l-4.23 4.23a1.1 1.1 0 0 1-1.56 0L4.99 9.66a1.1 1.1 0 0 1 1.56-1.56L8.9 10.45v-6.6A1.1 1.1 0 0 1 10 2.75Z" />
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

/** A video file: Streamline images-photography/film-slate.svg. */
export const IconFileVideo = (p: GlyphProps) => (
  <svg {...glyph([0.612, 0.205, 12.775, 13.591], p)}>
    <path fillRule="evenodd" d="m1.17563 6.08064 2.29468 -0.61486c0.00235 -0.01499 0.00526 -0.03001 0.00875 -0.04502l0.76923 -3.31469 1.36813 -0.36659 -0.00305 0.01385 -0.77657 3.3463 2.87361 -0.76998 0.00274 -0.01204 0.79642 -3.35335 1.372 -0.367627 -0.00225 0.009817 -0.79695 3.35559 3.88783 -1.04173 -0.529 -1.97426c-0.143 -0.533466 -0.6913 -0.8500491 -1.2248 -0.707107L1.35373 2.88164c-0.533462 0.14294 -0.850045 0.69128 -0.707103 1.22474l0.529003 1.97426Zm0.00137 0.00002h12.2106v6.71474c0 0.5522 -0.4477 1 -1 1H2.177c-0.55228 0 -1 -0.4478 -1 -1V6.08066Zm2.17407 5.00314c0 -0.3451 0.27983 -0.625 0.625 -0.625h1.36451c0.34518 0 0.625 0.2799 0.625 0.625 0 0.3452 -0.27982 0.625 -0.625 0.625H3.97607c-0.34517 0 -0.625 -0.2798 -0.625 -0.625Z" clipRule="evenodd" />
  </svg>
);

/** An audio file: Streamline entertainment/music-note-1.svg. */
export const IconFileAudio = (p: GlyphProps) => (
  <svg {...glyph([1.055, 0.126, 11.889, 13.747], p)}>
    <path fillRule="evenodd" d="M6.44473 0.126343c-0.55228 0 -1 0.447715 -1 0.999997v6.58881c-0.36907 -0.14892 -0.77232 -0.2309 -1.19473 -0.2309 -1.76438 0 -3.1947 1.43032 -3.1947 3.19475 0 1.7643 1.43032 3.1947 3.1947 3.1947 1.76439 0 3.19471 -1.4304 3.19471 -3.1947 0 -0.0058 -0.00002 -0.0115 -0.00005 -0.0172 0.00005 -0.0041 0.00007 -0.0081 0.00007 -0.0122V2.23056c0.25345 0.0535 0.50184 0.12733 0.74232 0.22085 0.55063 0.21413 1.04702 0.52649 1.4623 0.91638 0.41515 0.38977 0.74085 0.84905 0.96185 1.34983 0.2208 0.50059 0.3335 1.0347 0.3335 1.5724 0 0.55228 0.4477 1 1 1s1 -0.44772 1 -1c0 -0.8185 -0.1718 -1.62733 -0.5037 -2.37971 -0.3319 -0.75219 -0.8164 -1.43139 -1.4227 -2.00061C10.4121 1.34061 9.69588 0.892263 8.91193 0.587398 8.12805 0.282559 7.28987 0.126343 6.44473 0.126343Z" clipRule="evenodd" />
  </svg>
);

/** An image, a thumbnail: Streamline images-photography/landscape-2.svg. */
export const IconFileImage = (p: GlyphProps) => (
  <svg {...glyph([0, 0, 14, 14], p)}>
    <path fillRule="evenodd" d="M14 12.5c0 0.8284 -0.6716 1.5 -1.5 1.5h-11C0.671573 14 0 13.3284 0 12.5v-11C0 0.671573 0.671573 0 1.5 0h11c0.8284 0 1.5 0.671573 1.5 1.5v11ZM3.75 2C2.7835 2 2 2.7835 2 3.75S2.7835 5.5 3.75 5.5 5.5 4.7165 5.5 3.75 4.7165 2 3.75 2Zm4.65099 4.59901L2.5 12.5h10v-4L9.68934 6.49238c-0.39779 -0.28413 -0.94269 -0.23903 -1.28835 0.10663Z" clipRule="evenodd" />
  </svg>
);

/** A subtitle file: Streamline mail/chat-bubble-text-square.svg. */
export const IconFileSubtitles = (p: GlyphProps) => (
  <svg {...glyph([0, 0, 14, 14], p)}>
    <path fillRule="evenodd" d="M2.50001 0c-0.39782 0 -0.77936 0.158035 -1.06066 0.43934 -0.2813 0.281304 -0.43934 0.66284 -0.43934 1.06066v8.9189l-0.9743413 2.923c-0.0577086 0.1731 -0.01651266 0.3639 0.1074803 0.4978 0.123993 0.1339 0.311087 0.1896 0.488129 0.1454L4.56156 13H12.5c0.3978 0 0.7794 -0.158 1.0607 -0.4393S14 11.8978 14 11.5v-10c0 -0.39783 -0.158 -0.779356 -0.4393 -1.06066C13.2794 0.158035 12.8978 0 12.5 0H2.50001ZM3.875 4.84558c0 -0.34518 0.27982 -0.625 0.625 -0.625h6c0.3452 0 0.625 0.27982 0.625 0.625s-0.2798 0.625 -0.625 0.625h-6c-0.34518 0 -0.625 -0.27982 -0.625 -0.625ZM4.5 7.52942c-0.34518 0 -0.625 0.27982 -0.625 0.625s0.27982 0.625 0.625 0.625h4c0.34518 0 0.625 -0.27982 0.625 -0.625s-0.27982 -0.625 -0.625 -0.625h-4Z" clipRule="evenodd" />
  </svg>
);

/** A description or another text file: Streamline interface-essential/paragraph.svg. */
export const IconFileText = (p: GlyphProps) => (
  <svg {...glyph([0, 0, 14, 14], p)}>
    <path fillRule="evenodd" d="M4.78646 0C2.14297 0 0 2.14297 0 4.78646s2.14297 4.78646 4.78646 4.78646h0.9427V13.25c0 0.4142 0.33578 0.75 0.75 0.75 0.41421 0 0.75 -0.3358 0.75 -0.75V8.82683c0 -0.0013 0.00001 -0.00261 0.00001 -0.00391V1.5H9.375v11.75c0 0.4142 0.33579 0.75 0.75 0.75 0.4142 0 0.75 -0.3358 0.75 -0.75V1.5h2.375c0.4142 0 0.75 -0.33579 0.75 -0.75 0 -0.414214 -0.3358 -0.75 -0.75 -0.75H4.78646Z" clipRule="evenodd" />
  </svg>
);

/** A file, an attachment: Streamline interface-essential/paperclip-1.svg. */
export const IconFile = (p: GlyphProps) => (
  <svg {...glyph([0.506, 0, 12.988, 14], p)}>
    <path fillRule="evenodd" d="M9.38287 1.5c-0.1538 0 -0.30607 0.03045 -0.44805 0.0896 -0.14197 0.05915 -0.27082 0.14582 -0.37913 0.25502l-0.00134 0.00134L2.48546 7.934c-0.30713 0.31273 -0.47923 0.73355 -0.47923 1.17193 0 0.43907 0.17265 0.86054 0.4807 1.17337l1.70469 1.7335c0.15124 0.1541 0.33222 0.277 0.53131 0.3606 0.1991 0.0836 0.41285 0.1266 0.62877 0.1266 0.21592 0 0.42967 -0.043 0.62876 -0.1266 0.1991 -0.0836 0.37953 -0.206 0.53077 -0.3601l0.00273 -0.0028 5.69724 -5.7451c0.2917 -0.29412 0.7666 -0.2961 1.0607 -0.00444 0.2941 0.29167 0.2961 0.76654 0.0044 1.06065L7.5818 13.064c-0.00048 0.0005 -0.00096 0.001 -0.00144 0.0014 -0.29058 0.2957 -0.6371 0.5307 -1.01938 0.6911 -0.38291 0.1607 -0.79401 0.2435 -1.20928 0.2435 -0.41527 0 -0.82638 -0.0828 -1.20929 -0.2435 -0.38286 -0.1607 -0.72986 -0.3961 -1.02071 -0.6924 -0.00003 0 0.00004 0 0 0l-1.70365 -1.7324C0.83373 10.7382 0.506226 9.93878 0.506226 9.10593c0 -0.83286 0.327504 -1.63231 0.911824 -2.22579l0.00326 -0.00332L7.49069 0.788317c0.00026 -0.000263 0.00052 -0.000525 0.00078 -0.000788 0.24759 -0.24942 0.54207 -0.44741 0.86649 -0.582568C8.68272 0.0696598 9.03106 0 9.38287 0c0.35182 0 0.70013 0.0696596 1.02493 0.204961 0.3242 0.135091 0.6186 0.33295 0.8661 0.582193 0.0004 0.000388 0.0008 0.000775 0.0012 0.001163l0.6755 0.675493c0.0003 0.00039 0.0007 0.00077 0.0011 0.00116 0.2493 0.24751 0.4471 0.54185 0.5822 0.86611 0.1353 0.32476 0.205 0.6731 0.205 1.02491 0 0.35182 -0.0697 0.70016 -0.205 1.02492 -0.135 0.32405 -0.3327 0.61823 -0.5817 0.86564 -0.0006 0.00055 -0.0011 0.00109 -0.0016 0.00163L6.57362 10.6443c-0.00028 0.0002 -0.00055 0.0005 -0.00082 0.0008 -0.15857 0.1596 -0.34713 0.2864 -0.55485 0.3729 -0.20808 0.0867 -0.43126 0.1313 -0.65668 0.1313 -0.22541 0 -0.44859 -0.0446 -0.65667 -0.1313s-0.39693 -0.2137 -0.55567 -0.3737l-0.00527 -0.0054 -0.33104 -0.3405c-0.15744 -0.1576 -0.28255 -0.34448 -0.36824 -0.55016 -0.08668 -0.20808 -0.13132 -0.43126 -0.13132 -0.65668 0 -0.22541 0.04463 -0.44859 0.13132 -0.65667 0.08668 -0.20805 0.21369 -0.39689 0.37371 -0.55561 0.00002 -0.00002 -0.00002 0.00002 0 0l3.60985 -3.58116c0.29406 -0.29172 0.76893 -0.28982 1.06065 0.00424 0.29172 0.29406 0.28983 0.76893 -0.00423 1.06065L4.87451 8.94416c-0.01945 0.0193 -0.03495 0.04231 -0.04549 0.06759 -0.01053 0.02529 -0.01596 0.05242 -0.01596 0.07981 0 0.0274 0.00543 0.05452 0.01596 0.07981 0.01054 0.02529 0.02598 0.04824 0.04543 0.06754l0.00968 0.0096 0.33149 0.34113c0.01893 0.01866 0.04129 0.03352 0.06584 0.04375 0.02529 0.01053 0.05242 0.01596 0.07981 0.01596 0.0274 0 0.05452 -0.00543 0.07981 -0.01596 0.02529 -0.01054 0.04824 -0.02598 0.06754 -0.04543l0.00122 -0.00123 5.38126 -5.40044 0.0031 -0.00311c0.1092 -0.10831 0.1959 -0.23717 0.2551 -0.37914 0.0591 -0.14197 0.0896 -0.29424 0.0896 -0.44805 0 -0.1538 -0.0305 -0.30608 -0.0896 -0.44805 -0.0592 -0.14197 -0.1459 -0.27082 -0.2551 -0.37913l-0.0021 -0.00217 -0.6799 -0.67984 -0.0021 -0.00218c-0.1083 -0.1092 -0.2372 -0.19587 -0.37917 -0.25502 -0.14198 -0.05915 -0.29426 -0.0896 -0.44806 -0.0896Z" clipRule="evenodd" />
  </svg>
);
