// Monochrome inline icons (currentColor), 20-22px, in the GlimStone house style.
//
// Every glyph is a filled shape, never a stroked outline, so it matches the
// weight of the filled badges and switches around it (GlimStone, "Icon
// glyphs"). A thin line is drawn as a thin filled shape. A gap inside a fill
// is carved with fillRule="evenodd" rather than painted over in a background
// colour, which would only look right on one background.
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

export const IconSettings = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M11.49 3.17c-.38-1.56-2.6-1.56-2.98 0a1.532 1.532 0 0 1-2.286.948c-1.372-.836-2.942.734-2.106 2.106.54.886.061 2.042-.947 2.287-1.561.379-1.561 2.6 0 2.978a1.532 1.532 0 0 1 .947 2.287c-.836 1.372.734 2.942 2.106 2.106a1.532 1.532 0 0 1 2.287.947c.379 1.561 2.6 1.561 2.978 0a1.533 1.533 0 0 1 2.287-.947c1.372.836 2.942-.734 2.106-2.106a1.533 1.533 0 0 1 .947-2.287c1.561-.379 1.561-2.6 0-2.978a1.532 1.532 0 0 1-.947-2.287c.836-1.372-.734-2.942-2.106-2.106a1.532 1.532 0 0 1-2.287-.947zM10 13a3 3 0 1 0 0-6 3 3 0 0 0 0 6z"
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
// read at 16px. Downloads, Accounts, Connections, Reconnect and General reuse
// the sidebar, connection, retry and gear glyphs: one idea, one drawing.

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

/** Look: a drop of colour. */
export const IconLook = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path d="M10 2.75c2.9 3.1 5 5.8 5 8.05a5 5 0 0 1-10 0c0-2.25 2.1-4.95 5-8.05Z" />
  </svg>
);

/** Access: a padlock. */
export const IconLock = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="3.75" y="8.5" width="12.5" height="8.25" rx="1.75" />
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M10 3.9a3.1 3.1 0 0 0-3.1 3.1v1.5h1.8V7a1.3 1.3 0 1 1 2.6 0v1.5h1.8V7A3.1 3.1 0 0 0 10 3.9Z"
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

/** Advanced: faders, for the page where every value can be set by hand. */
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

export const IconMail = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M1.8 4.4h16.4v11.2H1.8V4.4Zm2.1 1.8L10 10.6l6.1-4.4H3.9Z"
    />
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
