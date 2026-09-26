// Monochrome inline icons (currentColor), 20-22px, in the GlimStone house style.
//
// Every glyph is a filled shape, never a stroked outline, so it matches the
// weight of the filled badges and switches around it (GlimStone, "Icon
// glyphs"). A thin line is drawn as a thin filled shape. A gap inside a fill
// is carved with fillRule="evenodd" rather than painted over in a background
// colour, which would only look right on one background.
//
// The Settings cog and the settings tab glyphs GlimStone fixes (glyphs.md,
// "Settings tabs") come from Streamline's free Core Solid set
// (https://streamlinehq.com, CC BY 4.0) and Material Design Icons (Apache 2.0).
import type { SVGProps } from 'react';

const base = (p: SVGProps<SVGSVGElement>) => ({
  width: 22,
  height: 22,
  viewBox: '0 0 20 20',
  fill: 'currentColor',
  className: 'shrink-0',
  'aria-hidden': true,
  ...p,
});

// Streamline draws on a 14-unit grid edge to edge, where the glyphs here fill
// about three quarters of theirs; this box gives it the same share, so the
// cog stands as tall as the rail glyphs around it.
const STREAMLINE_BOX = '-2.33 -2.33 18.67 18.67';

export const IconDownloads = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M8.5 2.5H11.5V10H16L10 16.5L4 10H8.5Z" />
  </svg>
);

/** IconDownloads mirrored vertically (y' = 19 - y), for the restore/upload button. */
export const IconUpload = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M8.5 16.5H11.5V9H16L10 2.5L4 9H8.5Z" />
  </svg>
);

/** IconSettings is Streamline's cog, which stands for Settings and for nothing inside it. */
export const IconSettings = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base({ viewBox: STREAMLINE_BOX, ...p })}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="m5.557 0.69 -0.463 1.195 -1.594 0.904 -1.27 -0.194a1.077 1.077 0 0 0 -1.078 0.528l-0.43 0.754a1.077 1.077 0 0 0 0.086 1.217l0.807 1.001v1.81L0.83 8.906a1.077 1.077 0 0 0 -0.086 1.217l0.43 0.754a1.077 1.077 0 0 0 1.078 0.528l1.27 -0.194 1.573 0.904 0.463 1.196a1.076 1.076 0 0 0 1 0.689h0.905a1.076 1.076 0 0 0 1.002 -0.69l0.463 -1.195 1.572 -0.904 1.27 0.194a1.077 1.077 0 0 0 1.078 -0.528l0.43 -0.754a1.077 1.077 0 0 0 -0.086 -1.217l-0.807 -1.001v-1.81l0.786 -1.001a1.077 1.077 0 0 0 0.086 -1.217l-0.43 -0.754a1.076 1.076 0 0 0 -1.078 -0.528l-1.27 0.194 -1.573 -0.904L8.443 0.689A1.077 1.077 0 0 0 7.442 0h-0.884a1.077 1.077 0 0 0 -1.001 0.69ZM7 9.25a2.25 2.25 0 1 0 0 -4.5 2.25 2.25 0 0 0 0 4.5Z"
    />
  </svg>
);

export const IconMoon = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M17.5 12.5A7.5 7.5 0 017.5 2.5a7.5 7.5 0 100 15 7.5 7.5 0 0010-5z" />
  </svg>
);

export const IconSun = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <circle cx="10" cy="10" r="3.5" />
    <rect x="9.25" y="1" width="1.5" height="3" rx="0.75" />
    <rect x="9.25" y="16" width="1.5" height="3" rx="0.75" />
    <rect x="1" y="9.25" width="3" height="1.5" rx="0.75" />
    <rect x="16" y="9.25" width="3" height="1.5" rx="0.75" />
    <rect x="9.25" y="1" width="1.5" height="3" rx="0.75" transform="rotate(45 10 10)" />
    <rect x="9.25" y="1" width="1.5" height="3" rx="0.75" transform="rotate(135 10 10)" />
    <rect x="9.25" y="1" width="1.5" height="3" rx="0.75" transform="rotate(225 10 10)" />
    <rect x="9.25" y="1" width="1.5" height="3" rx="0.75" transform="rotate(315 10 10)" />
  </svg>
);

export const IconPause = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="5" y="4" width="3.5" height="12" rx="1" />
    <rect x="11.5" y="4" width="3.5" height="12" rx="1" />
  </svg>
);

export const IconPlay = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M6 4.5v11a.75.75 0 0 0 1.14.64l9-5.5a.75.75 0 0 0 0-1.28l-9-5.5A.75.75 0 0 0 6 4.5z" />
  </svg>
);

export const IconStop = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="4.5" y="4.5" width="11" height="11" rx="2" />
  </svg>
);

export const IconTrash = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="8" y="2" width="4" height="2" rx="1" />
    <rect x="3.5" y="4.5" width="13" height="2.2" rx="1.1" />
    <path d="M5.3 7.5h9.4l-.9 9.1a1.5 1.5 0 0 1-1.5 1.4H7.7a1.5 1.5 0 0 1-1.5-1.4L5.3 7.5Z" />
  </svg>
);

export const IconPlus = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="8.5" y="3" width="3" height="14" rx="1.2" />
    <rect x="3" y="8.5" width="14" height="3" rx="1.2" />
  </svg>
);

export const IconDashboard = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="2.5" y="2.5" width="6.5" height="6.5" rx="1.5" />
    <rect x="11" y="2.5" width="6.5" height="6.5" rx="1.5" opacity=".6" />
    <rect x="2.5" y="11" width="6.5" height="6.5" rx="1.5" opacity=".6" />
    <rect x="11" y="11" width="6.5" height="6.5" rx="1.5" opacity=".4" />
  </svg>
);

export const IconCollector = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path
      opacity=".55"
      d="M3 5.5A1.5 1.5 0 0 1 4.5 4H8l1.6 2H16a1.5 1.5 0 0 1 1.5 1.5v7A1.5 1.5 0 0 1 16 16H4.5A1.5 1.5 0 0 1 3 14.5Z"
    />
    <path d="M13 3.5h4v4h-2v-1.6l-4.4 4.4-1.4-1.4 4.4-4.4H13Z" />
  </svg>
);

export const IconInstances = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="2.5" y="3" width="15" height="5" rx="1.5" opacity=".55" />
    <rect x="2.5" y="12" width="15" height="5" rx="1.5" opacity=".55" />
    <circle cx="5.5" cy="5.5" r="1" />
    <circle cx="5.5" cy="14.5" r="1" />
  </svg>
);

export const IconAccounts = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <circle cx="10" cy="6.5" r="3.2" />
    <path d="M10 11c-3.6 0-6.5 2.5-6.5 5.5 0 .3.2.5.5.5h12a.5.5 0 0 0 .5-.5c0-3-2.9-5.5-6.5-5.5Z" />
  </svg>
);

export const IconGlobe = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
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

export const IconRetry = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M10 3a7 7 0 1 1-6.53 8.98 1 1 0 0 1 1.94-.5A5 5 0 1 0 10 5.1V7.9L6.3 5.2 10 2.5V3Z"
    />
  </svg>
);

export const IconSearch = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path fillRule="evenodd" clipRule="evenodd" d="M9 2.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13Zm0 2.3a4.2 4.2 0 1 1 0 8.4 4.2 4.2 0 0 1 0-8.4Z" />
    <rect x="12.2" y="13.6" width="2.2" height="6" rx="1.1" transform="rotate(-45 13.3 16.6)" />
  </svg>
);

export const IconCheck = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M7.4 15.1 2.6 10.3l1.9-1.9 2.9 2.9 7.1-7.9 2 1.8-9.1 10Z" />
  </svg>
);

/**
 * IconCheckDrawn is IconCheck as one stroke of its weight, for a copy button's
 * "Copied": a stroke is what glim-check-draw can trace, so the check draws
 * itself when the copy lands. pathLength="1" lets the dash offset run from 1 to
 * 0 whatever the path's real length.
 */
export const IconCheckDrawn = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base({ fill: 'none', ...p })}>
    <path
      className="glim-check-draw"
      pathLength="1"
      d="M3.55 9.35 7.4 13.2l8.1-8.9"
      stroke="currentColor"
      strokeWidth="2.7"
      strokeLinejoin="miter"
    />
  </svg>
);

export const IconSwords = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base({ viewBox: '0 0 24 24', ...p })}>
    <path d="M3 3h4l11.5 11.5-2.5 2.5L4.5 5.5V3H3Z" />
    <path d="M14.5 15.5 19 20l2-2-4.5-4.5-2 2Z" />
    <path d="M21 3h-3L6.5 14.5l2 2L21 4.5V3Z" />
    <path d="M5.5 15.5 1 20l2 2 4.5-4.5-2-2Z" />
  </svg>
);

export const IconFolder = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M2.5 6.5A1.5 1.5 0 0 1 4 5h3.2l1.4 1.8H16a1.5 1.5 0 0 1 1.5 1.5v6.2A1.5 1.5 0 0 1 16 16H4a1.5 1.5 0 0 1-1.5-1.5z" />
  </svg>
);

export const IconFolderOpen = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M2.5 13.6V6.5A1.5 1.5 0 0 1 4 5h3.2l1.4 1.8H15a1.5 1.5 0 0 1 1.5 1.5v.9H6.4a2 2 0 0 0-1.8 1.1z" />
    <path d="M5.5 10.6a1.2 1.2 0 0 1 1.1-.7h11.1a.8.8 0 0 1 .7 1.1l-2 4.2a1.5 1.5 0 0 1-1.4.8H3.6a.6.6 0 0 1-.5-.9z" />
  </svg>
);

export const IconFolderPlus = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M2.5 6.5A1.5 1.5 0 0 1 4 5h3.2l1.4 1.8H16a1.5 1.5 0 0 1 1.5 1.5v6.2A1.5 1.5 0 0 1 16 16H4a1.5 1.5 0 0 1-1.5-1.5zM9 8.3h2v2h2v2h-2v2H9v-2H7v-2h2z"
    />
  </svg>
);

export const IconArrowUp = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M8.5 17.5v-7.5H4l6-6 6 6h-4.5v7.5z" />
  </svg>
);

export const IconArrowDown = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M11.5 2.5v7.5H16l-6 6-6-6h4.5V2.5z" />
  </svg>
);

export const IconTop = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="4" y="3" width="12" height="2" rx="1" />
    <path d="M8.5 16.5V9H5l5-5 5 5h-3.5v7.5z" />
  </svg>
);

export const IconBottom = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="4" y="15" width="12" height="2" rx="1" />
    <path d="M11.5 3.5V11H15l-5 5-5-5h3.5V3.5z" />
  </svg>
);

export const IconClose = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="9" y="2" width="2" height="16" rx="1" transform="rotate(45 10 10)" />
    <rect x="9" y="2" width="2" height="16" rx="1" transform="rotate(-45 10 10)" />
  </svg>
);

export const IconEdit = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M3.5 13.3 12.8 4l3.2 3.2-9.3 9.3H3.5v-3.2Z" />
    <path d="M13.6 3.2 15 1.8a1.3 1.3 0 0 1 1.8 0l1.4 1.4a1.3 1.3 0 0 1 0 1.8L16.8 6.4 13.6 3.2Z" />
  </svg>
);

// Renew/Buy Premium: a box the arrow leaves, for a link that goes to the
// service's own site rather than doing anything in this app.
export const IconExternalLink = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path fillRule="evenodd" clipRule="evenodd" d="M4.5 8.5a1 1 0 0 1 1-1H8v2H6.5v5H12v-1.5h2V17a1 1 0 0 1-1 1H5.5a1 1 0 0 1-1-1Z" />
    <path d="M9 4.5h7v7h-2V8l-6 6-1.4-1.4 6-6H9Z" />
  </svg>
);

export const IconSignOut = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path opacity=".7" d="M5.5 3.5A1 1 0 0 1 6.5 2.5h6a1 1 0 0 1 1 1V6h-2V4.5h-4v11h4V14h2v2.5a1 1 0 0 1-1 1h-6a1 1 0 0 1-1-1Z" />
    <path d="M14 7l3 3-3 3v-2H8.5v-2H14Z" />
  </svg>
);

// The password reveal toggle: a lens with the pupil carved out. The "off"
// version adds a filled diagonal bar.
export const IconEye = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M2.5 10C2.5 10 6 4.3 10 4.3C14 4.3 17.5 10 17.5 10C17.5 10 14 15.7 10 15.7C6 15.7 2.5 10 2.5 10Z M12.6 10a2.6 2.6 0 1 1 -5.2 0 2.6 2.6 0 0 1 5.2 0Z"
    />
  </svg>
);

export const IconEyeOff = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path
      opacity=".55"
      fillRule="evenodd"
      clipRule="evenodd"
      d="M2.5 10C2.5 10 6 4.3 10 4.3C14 4.3 17.5 10 17.5 10C17.5 10 14 15.7 10 15.7C6 15.7 2.5 10 2.5 10Z M12.6 10a2.6 2.6 0 1 1 -5.2 0 2.6 2.6 0 0 1 5.2 0Z"
    />
    <rect x="9.1" y="0" width="1.8" height="20" rx="0.9" transform="rotate(45 10 10)" />
  </svg>
);

// The settings tabs. Every tab has a glyph (GlimStone's rule), each drawn to
// read at 16px. Downloads, Accounts, Connections and Reconnect reuse the
// sidebar, connection and retry glyphs: one idea, one drawing. General, Look,
// App, Remote access and Advanced wear the glyphs every GlimStone app gives
// those tabs.

/** General: Material's tune, a row of sliders set by hand. */
export const IconTabGeneral = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base({ viewBox: '0 0 24 24', ...p })}>
    <path d="M3,17V19H9V17H3M3,5V7H13V5H3M13,21V19H21V17H13V15H11V21H13M7,9V11H3V13H7V15H9V9H7M21,13V11H11V13H21M15,9H17V7H21V5H17V3H15V9Z" />
  </svg>
);

/**
 * Advanced: Material's hammer-wrench. Its ink is cropped to the share of the
 * box tune's takes, so the two read as a pair of equal weight.
 */
export const IconTabAdvanced = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base({ viewBox: '-1.19 -1.19 26.37 26.37', ...p })}>
    <path d="M13.78 15.3L19.78 21.3L21.89 19.14L15.89 13.14L13.78 15.3M17.5 10.1C17.11 10.1 16.69 10.05 16.36 9.91L4.97 21.25L2.86 19.14L10.27 11.74L8.5 9.96L7.78 10.66L6.33 9.25V12.11L5.63 12.81L2.11 9.25L2.81 8.55H5.62L4.22 7.14L7.78 3.58C8.95 2.41 10.83 2.41 12 3.58L9.89 5.74L11.3 7.14L10.59 7.85L12.38 9.63L14.2 7.75C14.06 7.42 14 7 14 6.63C14 4.66 15.56 3.11 17.5 3.11C18.09 3.11 18.61 3.25 19.08 3.53L16.41 6.2L17.91 7.7L20.58 5.03C20.86 5.5 21 6 21 6.63C21 8.55 19.45 10.1 17.5 10.1Z" />
  </svg>
);

/** The App page: Streamline's computer with a device beside it. */
export const IconTabApp = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base({ viewBox: '-2.33 -1.33 18.67 18.67', ...p })}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M8 2c-0.55229 0 -1 0.44772 -1 1v0.46875h0.5c1.51878 0 2.75 1.23122 2.75 2.75V6.375l3.75 0V3c0 -0.55228 -0.4477 -1 -1 -1H8Zm-0.10887 11.8713C7.96169 13.6668 8 13.4472 8 13.2188c0 -0.0991 -0.0072 -0.1964 -0.0211 -0.2916 1.29049 -0.2266 2.2711 -1.353 2.2711 -2.7084V7.625l3.75 0V13c0 0.5523 -0.4477 1 -1 1H8.5c-0.21677 0 -0.4228 -0.046 -0.60887 -0.1287Zm4.78077 -9.6838c0 0.42284 -0.3428 0.76562 -0.7657 0.76562 -0.4228 0 -0.7656 -0.34278 -0.7656 -0.76562s0.3428 -0.76562 0.7656 -0.76562c0.4229 0 0.7657 0.34278 0.7657 0.76562ZM0 6.21875c0 -0.82843 0.671573 -1.5 1.5 -1.5h6c0.82843 0 1.5 0.67157 1.5 1.5v4.00005c0 0.8284 -0.67157 1.5 -1.5 1.5H5.25v0.75H6c0.41421 0 0.75 0.3357 0.75 0.75 0 0.4142 -0.33579 0.75 -0.75 0.75H3c-0.41421 0 -0.75 -0.3358 -0.75 -0.75 0 -0.4143 0.33579 -0.75 0.75 -0.75h0.75v-0.75H1.5c-0.828427 0 -1.5 -0.6716 -1.5 -1.5V6.21875Z"
    />
  </svg>
);

/** Modules: a switch, because the page is a column of them. */
export const IconModules = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="2.5" y="5.75" width="15" height="8.5" rx="4.25" opacity=".4" />
    <circle cx="13.25" cy="10" r="3.2" />
  </svg>
);

/** Archives: a lidded box. */
export const IconArchive = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="2.5" y="3.5" width="15" height="4" rx="1" />
    <path opacity=".8" d="M4 8.5h12v7A1.5 1.5 0 0 1 14.5 17h-9A1.5 1.5 0 0 1 4 15.5Z" />
  </svg>
);

/** Rules: a funnel, which is what the packagizer and the link filter do to a list. */
export const IconFilter = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M3 4.5h14l-5.4 6.2v5.1l-3.2 1.7v-6.8Z" />
  </svg>
);

/** Captcha: the ticked box everyone has clicked to prove they are a person. */
export const IconCaptcha = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M3 3h14v14H3V3Zm3.75 7.1 2.4 2.4 4.1-4.6-1.5-1.3-2.75 3.05-1-1Z"
    />
  </svg>
);

/** Schedule: a clock. */
export const IconClock = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <circle cx="10" cy="10" r="7.25" opacity=".3" />
    <path d="M10 4.9a1 1 0 0 1 1 1V9.3l2.4 1.4a1 1 0 1 1-1 1.7l-2.9-1.7a1 1 0 0 1-.5-.9V5.9a1 1 0 0 1 1-1Z" />
  </svg>
);

/** Events: a bell, for the sidebar's notification log. The clapper is a
 *  separate shape, which is what makes it read as a bell at 22px. */
export const IconBell = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M10 2a1 1 0 0 1 1 1v.6a4.9 4.9 0 0 1 3.9 4.8v2.4l1.1 2a.75.75 0 0 1-.65 1.1H4.65A.75.75 0 0 1 4 12.8l1.1-2V8.4A4.9 4.9 0 0 1 9 3.6V3a1 1 0 0 1 1-1Z" />
    <path d="M8.15 15.05h3.7a1.85 1.85 0 0 1-3.7 0Z" />
  </svg>
);

/** Look: Streamline's colour palette, three overlapping discs. */
export const IconLook = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base({ viewBox: STREAMLINE_BOX, ...p })}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M3.97352 4.06315C4.44759 2.85522 5.62392 2 7 2c1.37608 0 2.55241 0.85522 3.0265 2.06315C9.77381 4.02161 9.51443 4 9.25 4c-0.81407 0 -1.5803 0.20479 -2.25 0.56565C6.3303 4.20479 5.56407 4 4.75 4c-0.26443 0 -0.52381 0.02161 -0.77648 0.06315Zm-1.69089 0.62715C2.5596 2.33072 4.566 0.5 7 0.5c2.43399 0 4.4404 1.83072 4.7174 4.1903C13.0861 5.52393 14 7.03024 14 8.75c0 2.6234 -2.1266 4.75 -4.75 4.75 -0.81407 0 -1.5803 -0.2048 -2.25 -0.5657 -0.6697 0.3609 -1.43593 0.5657 -2.25 0.5657C2.12665 13.5 0 11.3734 0 8.75c0 -1.71976 0.913945 -3.22607 2.28263 -4.0597Zm7.94307 0.95872C9.91774 5.5522 9.58997 5.5 9.25 5.5c-0.31851 0 -0.62633 0.04582 -0.9172 0.13123 0.46551 0.53432 0.81345 1.17377 1.00351 1.87801 0.4786 -0.49482 0.80139 -1.14123 0.88939 -1.86022ZM9.46737 9.3097c1.02703 -0.62555 1.79803 -1.62987 2.11893 -2.81893 0.5656 0.58481 0.9137 1.38137 0.9137 2.25923 0 1.7949 -1.4551 3.25 -3.25 3.25 -0.31851 0 -0.62633 -0.0458 -0.9172 -0.1312 0.61163 -0.7021 1.0203 -1.5856 1.13457 -2.5591Zm-1.49162 -0.95872C7.88253 7.58958 7.526 6.90956 7 6.40479c-0.526 0.50477 -0.88253 1.18479 -0.97575 1.94619C6.33226 8.4478 6.66003 8.5 7 8.5s0.66774 -0.0522 0.97575 -0.14902ZM6.22353 9.93685C6.47619 9.97839 6.73557 10 7 10c0.26443 0 0.52381 -0.02161 0.77648 -0.06315 -0.17323 0.44135 -0.44023 0.83565 -0.77648 1.15835 -0.33625 -0.3227 -0.60325 -0.717 -0.77647 -1.15835Zm-1.6909 -0.62715c0.11427 0.9735 0.52294 1.857 1.13458 2.5591 -0.29088 0.0854 -0.5987 0.1312 -0.91721 0.1312 -1.79493 0 -3.25 -1.4551 -3.25 -3.25 0 -0.87786 0.34805 -1.67443 0.91369 -2.25924 0.3209 1.18906 1.09189 2.19339 2.11894 2.81894Zm0.13106 -1.80046c-0.4786 -0.49482 -0.80142 -1.14123 -0.88944 -1.86022C4.08226 5.5522 4.41003 5.5 4.75 5.5c0.31851 0 0.62633 0.04582 0.91721 0.13123 -0.46552 0.53432 -0.81346 1.17376 -1.00352 1.87801Z"
    />
  </svg>
);

/** Remote access: Streamline's padlock, the Security tab's glyph. */
export const IconLock = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base({ viewBox: STREAMLINE_BOX, ...p })}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M7 2a2 2 0 0 0 -2 2v1h4V4a2 2 0 0 0 -2 -2ZM3 4v1a1.5 1.5 0 0 0 -1.5 1.5v6A1.5 1.5 0 0 0 3 14h8a1.5 1.5 0 0 0 1.5 -1.5v-6A1.5 1.5 0 0 0 11 5V4a4 4 0 1 0 -8 0Zm4 6.75a1.25 1.25 0 1 0 0 -2.5 1.25 1.25 0 0 0 0 2.5Z"
    />
  </svg>
);

/**
 * The second factor: a shield with a check carved out (GlimStone 2.1.0). It
 * sits on the enrolment button, whose label matches the passkey's, so the
 * glyph tells them apart. Not IconLock, which is this page's own rail glyph.
 * The check keeps it from reading as a copy of KnightLoader's shield logo.
 */
export const IconShieldCheck = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M10 2.7 4 4.8v5.7c0 2.9 2.4 5.4 6 6.4 3.6-1 6-3.5 6-6.4V4.8L10 2.7Zm-.95 10.05L6.15 9.85l1.4-1.4 1.5 1.5L12.6 6.4 14 7.8l-4.95 4.95Z"
    />
  </svg>
);

/** Faders: a schedule's speed limit, a value set by hand. */
export const IconSliders = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="3" y="6.25" width="4.25" height="1" rx=".5" />
    <rect x="11.25" y="6.25" width="5.75" height="1" rx=".5" />
    <rect x="3" y="12.75" width="5.75" height="1" rx=".5" />
    <rect x="12.75" y="12.75" width="4.25" height="1" rx=".5" />
    <circle cx="9.25" cy="6.75" r="2" />
    <circle cx="10.75" cy="13.25" r="2" />
  </svg>
);

/** Diagnostics: a medical cross, for the page that says whether the process is alive. */
export const IconDiagnostics = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <circle cx="10" cy="10" r="7.25" opacity=".3" />
    <path d="M8.9 4.9h2.2v3.9h3.9v2.2h-3.9v3.9H8.9v-3.9H5v-2.2h3.9z" />
  </svg>
);

/** Help: a question mark, same circle radius as Schedule's clock and Connections' globe. */
export const IconHelp = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    {/* One evenodd path, so the "?" is carved out of the circle; as a separate
        shape in the same colour it would be invisible. */}
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M17.25 10A7.25 7.25 0 1 0 2.75 10a7.25 7.25 0 0 0 14.5 0Z
         M7.7 7.9a2.35 2.35 0 1 1 3.4 2.1c-.6.32-.95.75-1.05 1.3h-1.8c.1-1.05.55-1.75 1.4-2.25a.85.85 0 1 0-1.25-.75Zm1.3 5.4a1 1 0 1 0 2 0 1 1 0 0 0-2 0Z"
    />
  </svg>
);

/** Scripts: angle brackets, which read as "code" at 16px without looking like
 *  a browser tab or a terminal. */
export const IconCode = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M7.9 5.3 2.6 10l5.3 4.7 1.3-1.5L5.4 10l3.8-3.2Z" />
    <path d="M12.1 5.3l5.3 4.7-5.3 4.7-1.3-1.5L14.6 10l-3.8-3.2Z" />
  </svg>
);

/** The one-shot "paste from clipboard" button: a clipboard, clip and all. */
export const IconClipboard = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="5" y="4.5" width="10" height="12.5" rx="1.5" />
    <path opacity=".7" d="M7.5 3a1.25 1.25 0 0 1 1.25-1.25h2.5A1.25 1.25 0 0 1 12.5 3v1.75h-5V3Z" />
  </svg>
);

/** "Open natively" (desktop only): an app window, handing off to another
 *  application on this machine. IconExternalLink means "goes to a website". */
export const IconApp = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="3.5" y="4.5" width="13" height="11" rx="1.5" opacity=".3" />
    <path d="M3.5 6A1.5 1.5 0 0 1 5 4.5h10A1.5 1.5 0 0 1 16.5 6v1.5h-13Z" />
  </svg>
);

/** A QR code: three finder squares and a corner of modules. */
export const IconQr = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M2.5 2.5h6v6h-6Zm1.4 1.4v3.2h3.2V3.9ZM4.75 4.75h1.5v1.5h-1.5ZM11.5 2.5h6v6h-6Zm1.4 1.4v3.2h3.2V3.9Zm.85.85h1.5v1.5h-1.5ZM2.5 11.5h6v6h-6Zm1.4 1.4v3.2h3.2v-3.2Zm.85.85h1.5v1.5h-1.5Z"
    />
    <path d="M11.5 11.5h2v2h-2Zm4 0h2v2h-2Zm-2 2h2v2h-2Zm-2 2h2v2h-2Zm4 0h2v2h-2Z" />
  </svg>
);

/** A warning triangle with the "!" carved out, for remote access's loud warning. */
export const IconWarning = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M10 2.75 18 16.75H2L10 2.75Zm-.9 4.75v4.5h1.8v-4.5Zm.9 6.4a1 1 0 1 0 0 2 1 1 0 0 0 0-2Z"
    />
  </svg>
);

/** API tokens: a key, as distinct from IconLock's padlock (the shared
 *  password) as the two credentials are. */
export const IconKey = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    {/* Horizontal on whole coordinates: rotated shapes land between pixels at
        this size and look broken. */}
    <path d="M6.5 6a4 4 0 1 0 0 8 4 4 0 0 0 0-8Zm0 2.4a1.6 1.6 0 1 1 0 3.2 1.6 1.6 0 0 1 0-3.2Z" />
    <path d="M10.2 9h7.3v2h-1.3v2.4h-2V11h-1.2v2.4h-2V11h-.8V9Z" />
  </svg>
);

/** Keyboard shortcuts: a keyboard with a row of keys and a spacebar. */
export const IconKeyboard = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="2.5" y="5.5" width="15" height="9" rx="1.5" opacity=".3" />
    <rect x="4.5" y="7.5" width="2" height="1.6" rx=".4" />
    <rect x="7.5" y="7.5" width="2" height="1.6" rx=".4" />
    <rect x="10.5" y="7.5" width="2" height="1.6" rx=".4" />
    <rect x="13.5" y="7.5" width="2" height="1.6" rx=".4" />
    <rect x="4.5" y="10.5" width="2" height="1.6" rx=".4" />
    <rect x="7.5" y="10.5" width="6" height="1.6" rx=".4" />
    <rect x="13.5" y="10.5" width="2" height="1.6" rx=".4" />
  </svg>
);

/** The menu trigger: three bars. Never a sliders glyph, which means "adjust a
 *  value" (GlimStone's rule). */
export const IconMenu = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="3" y="5" width="14" height="2" rx="1" />
    <rect x="3" y="9" width="14" height="2" rx="1" />
    <rect x="3" y="13" width="14" height="2" rx="1" />
  </svg>
);

/** Three dots, for a badge that holds the verbs too rare for a badge of their own. */
export const IconMore = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <circle cx="4.5" cy="10" r="1.75" />
    <circle cx="10" cy="10" r="1.75" />
    <circle cx="15.5" cy="10" r="1.75" />
  </svg>
);

/** System: the power glyph, for the page that quits, restarts, backs up and restores. */
export const IconPower = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M10 3a1 1 0 0 1 1 1v5a1 1 0 1 1-2 0V4a1 1 0 0 1 1-1Zm-4.2 2.1a1 1 0 0 1 0 1.42A5.25 5.25 0 1 0 14.2 6.52a1 1 0 1 1 1.4-1.42 7.25 7.25 0 1 1-11.2 0 1 1 0 0 1 1.4 0Z"
    />
  </svg>
);

/** Browser tools: a browser window, for the page with the bookmarklet, the
 *  extension and the PWA install. */
export const IconBrowser = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="2.5" y="3.5" width="15" height="13" rx="1.5" opacity=".3" />
    <path d="M2.5 5A1.5 1.5 0 0 1 4 3.5h12A1.5 1.5 0 0 1 17.5 5v1.5h-15Z" />
    <circle cx="5" cy="5" r=".6" />
    <circle cx="7" cy="5" r=".6" />
    <circle cx="9" cy="5" r=".6" />
  </svg>
);

/** A coffee cup with a handle and a saucer: the About card's thank-you. */
export const IconCoffee = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M3 4h10.3v6.6a4.5 4.5 0 0 1-4.5 4.5H7.5A4.5 4.5 0 0 1 3 10.6V4Z" />
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M14.4 5.4h1.5a2.6 2.6 0 0 1 0 5.2h-1.5V8.7h1.5a1 1 0 0 0 0-2h-1.5V5.4Z"
    />
    <rect x="1.9" y="16.2" width="13.5" height="1.9" rx=".95" />
  </svg>
);

/** GitHub's mark, the one third-party logo in this set, for the button that
 *  goes there. Published on a 16 viewBox, so the group scales it by 1.25
 *  instead of rewriting its coordinates. */
export const IconGithub = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <g transform="scale(1.25)">
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.012 8.012 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
    </g>
  </svg>
);

// The context menu's glyphs, kept here so there is one drawing per idea.

/** Force start: a lightning bolt, "now" rather than "sooner". */
export const IconBolt = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M11.3 2.4 4.8 11.2h3.6L7.6 17.6 15.2 8.8h-3.7z" />
  </svg>
);

/** The fold twisty, a solid wedge: a stroked V at 14px looks like a missing icon. */
export const IconChevronDown = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M10 13.4 3.6 7l1.7-1.7L10 10l4.7-4.7L16.4 7 10 13.4Z" />
  </svg>
);

export const IconChevronUp = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M10 6.6 16.4 13l-1.7 1.7L10 10l-4.7 4.7L3.6 13 10 6.6Z" />
  </svg>
);

/** Onward to another page. Points right; mirror it with rtl:-scale-x-100. */
export const IconChevronEnd = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M13.4 10 7 16.4l-1.7-1.7L10 10 5.3 5.3 7 3.6 13.4 10Z" />
  </svg>
);

/** Back to the previous step. Points left; mirror it with rtl:-scale-x-100. */
export const IconChevronStart = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M6.6 10 13 3.6l1.7 1.7L10 10l4.7 4.7-1.7 1.7L6.6 10Z" />
  </svg>
);

/** Hold/release: a pushpin, upright because a rotated one lands between pixels
 *  at this size. */
export const IconPin = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M6 2.5h8v2h-1.8v4.5l2.8 2v1.6h-4.3v3.9L10 18l-.7-1.5v-3.9H5V11l2.8-2V4.5H6Z" />
  </svg>
);

/** Queue priority: three descending bars. Not an arrow, since the one-step
 *  moves beside it in the menu use arrows. */
export const IconPriority = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="4" y="4.7" width="12" height="1.6" rx=".8" />
    <rect x="4" y="9.2" width="8" height="1.6" rx=".8" />
    <rect x="4" y="13.7" width="4" height="1.6" rx=".8" />
  </svg>
);

/** The stop mark: a flag where the queue stops. A pennant, so it is not
 *  mistaken for IconStop's square. */
export const IconStopMark = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="4.4" y="2.5" width="1.8" height="15" rx=".9" />
    <path d="M6.8 3.2h8.8l-2.1 3.4 2.1 3.4H6.8Z" />
  </svg>
);

/** "Remove and delete the files": IconTrash with two slits carved through it.
 *  Erasing files is a different act from removing rows, so it gets its own glyph. */
export const IconTrashFiles = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="8" y="2" width="4" height="2" rx="1" />
    <rect x="3.5" y="4.5" width="13" height="2.2" rx="1.1" />
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M5.3 7.5h9.4l-.9 9.1a1.5 1.5 0 0 1-1.5 1.4H7.7a1.5 1.5 0 0 1-1.5-1.4L5.3 7.5Z
         M7 9.6h6v1.3H7Z
         M7 12.4h6v1.3H7Z"
    />
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
  // 5 tall per wedge, 1 of air between them, centred in the 20-unit box the
  // rest of the icon set is drawn in.
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

/** IconGrip is the drag handle of a row: two columns of dots, taller than wide
 *  because the drag is vertical. */
export function IconGrip(p: SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 8 12" fill="currentColor" aria-hidden focusable="false" {...p}>
      {[1.5, 6, 10.5].map((cy) =>
        [1.5, 6.5].map((cx) => <circle key={`${cx}-${cy}`} cx={cx} cy={cy} r="1.2" />),
      )}
    </svg>
  );
}

/** IconShield is IconShieldCheck's silhouette without the check, for the
 *  parade in lib/toast.tsx: at 10px the carved check would blur. */
export const IconShield = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M10 2.7 4 4.8v5.7c0 2.9 2.4 5.4 6 6.4 3.6-1 6-3.5 6-6.4V4.8L10 2.7Z" />
  </svg>
);
