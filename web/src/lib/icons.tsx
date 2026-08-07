// Monochrome inline icons (currentColor), 20-22px, in the BombVault house style.
import type { SVGProps } from 'react';

const base = (p: SVGProps<SVGSVGElement>) => ({
  width: 22,
  height: 22,
  viewBox: '0 0 20 20',
  fill: 'none',
  className: 'shrink-0',
  'aria-hidden': true,
  ...p,
});

export const IconDownloads = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
    <path d="M10 3v9M6 9l4 4 4-4" />
    <path d="M4 15.5h12" />
  </svg>
);

export const IconSettings = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} fill="currentColor">
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M11.49 3.17c-.38-1.56-2.6-1.56-2.98 0a1.532 1.532 0 0 1-2.286.948c-1.372-.836-2.942.734-2.106 2.106.54.886.061 2.042-.947 2.287-1.561.379-1.561 2.6 0 2.978a1.532 1.532 0 0 1 .947 2.287c-.836 1.372.734 2.942 2.106 2.106a1.532 1.532 0 0 1 2.287.947c.379 1.561 2.6 1.561 2.978 0a1.533 1.533 0 0 1 2.287-.947c1.372.836 2.942-.734 2.106-2.106a1.533 1.533 0 0 1 .947-2.287c1.561-.379 1.561-2.6 0-2.978a1.532 1.532 0 0 1-.947-2.287c.836-1.372-.734-2.942-2.106-2.106a1.532 1.532 0 0 1-2.287-.947zM10 13a3 3 0 1 0 0-6 3 3 0 0 0 0 6z"
    />
  </svg>
);

export const IconMoon = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.5} strokeLinejoin="round">
    <path d="M17.5 12.5A7.5 7.5 0 017.5 2.5a7.5 7.5 0 100 15 7.5 7.5 0 0010-5z" />
  </svg>
);

export const IconSun = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.5} strokeLinecap="round">
    <circle cx="10" cy="10" r="4" />
    <path d="M10 2v2M10 16v2M2 10h2M16 10h2M4.93 4.93l1.41 1.41M13.66 13.66l1.41 1.41M4.93 15.07l1.41-1.41M13.66 6.34l1.41-1.41" />
  </svg>
);

export const IconPause = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} fill="currentColor">
    <rect x="5" y="4" width="3.5" height="12" rx="1" />
    <rect x="11.5" y="4" width="3.5" height="12" rx="1" />
  </svg>
);

export const IconPlay = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} fill="currentColor">
    <path d="M6 4.5v11a.75.75 0 0 0 1.14.64l9-5.5a.75.75 0 0 0 0-1.28l-9-5.5A.75.75 0 0 0 6 4.5z" />
  </svg>
);

export const IconTrash = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
    <path d="M4 5.5h12M8 5.5V4a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v1.5M6 5.5l.7 10a1 1 0 0 0 1 .9h4.6a1 1 0 0 0 1-.9l.7-10" />
  </svg>
);

export const IconPlus = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.6} strokeLinecap="round">
    <path d="M10 4v12M4 10h12" />
  </svg>
);

export const IconDashboard = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} fill="currentColor">
    <rect x="2.5" y="2.5" width="6.5" height="6.5" rx="1.5" />
    <rect x="11" y="2.5" width="6.5" height="6.5" rx="1.5" opacity=".6" />
    <rect x="2.5" y="11" width="6.5" height="6.5" rx="1.5" opacity=".6" />
    <rect x="11" y="11" width="6.5" height="6.5" rx="1.5" opacity=".4" />
  </svg>
);

export const IconCollector = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
    <path d="M8 3H4.5A1.5 1.5 0 0 0 3 4.5v11A1.5 1.5 0 0 0 4.5 17h11a1.5 1.5 0 0 0 1.5-1.5V12" />
    <path d="M13 3h4v4M17 3l-7 7" />
  </svg>
);

export const IconInstances = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.5} strokeLinejoin="round">
    <rect x="2.5" y="3" width="15" height="5" rx="1.5" />
    <rect x="2.5" y="12" width="15" height="5" rx="1.5" />
    <circle cx="5.5" cy="5.5" r=".8" fill="currentColor" stroke="none" />
    <circle cx="5.5" cy="14.5" r=".8" fill="currentColor" stroke="none" />
  </svg>
);

export const IconAccounts = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
    <circle cx="10" cy="6.5" r="3" />
    <path d="M4 16.5a6 6 0 0 1 12 0" />
  </svg>
);

export const IconGlobe = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.5}>
    <circle cx="10" cy="10" r="7.25" />
    <path d="M2.9 7.5h14.2M2.9 12.5h14.2" strokeLinecap="round" />
    <path d="M10 2.75c2 2.2 3 4.6 3 7.25s-1 5.05-3 7.25c-2-2.2-3-4.6-3-7.25s1-5.05 3-7.25Z" />
  </svg>
);

export const IconRetry = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" strokeLinejoin="round">
    <path d="M15.5 8.5A5.75 5.75 0 1 0 16 11" />
    <path d="M16 3.5v4h-4" />
  </svg>
);

export const IconSearch = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.6} strokeLinecap="round">
    <circle cx="9" cy="9" r="5.5" />
    <path d="M13.5 13.5l3 3" />
  </svg>
);

export const IconCheck = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
    <path d="M4 10.5l4 4 8-9" />
  </svg>
);

export const IconSwords = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base({ viewBox: '0 0 24 24', ...p })} stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" strokeLinejoin="round">
    <path d="M14.5 17.5 3 6V3h3l11.5 11.5" />
    <path d="m13 19 6-6M16 16l4 4M19 21l2-2M21 3h-3L6.5 14.5" />
    <path d="m5 13-2 2M5 19l-2-2M3 21l2-2" />
  </svg>
);

export const IconFolder = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" strokeLinejoin="round">
    <path d="M2.5 6.5A1.5 1.5 0 0 1 4 5h3.2l1.4 1.8H16a1.5 1.5 0 0 1 1.5 1.5v6.2A1.5 1.5 0 0 1 16 16H4a1.5 1.5 0 0 1-1.5-1.5z" />
  </svg>
);

export const IconArrowUp = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round">
    <path d="M10 16V4.5M5.5 9 10 4.5 14.5 9" />
  </svg>
);

export const IconArrowDown = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round">
    <path d="M10 4v11.5M5.5 11l4.5 4.5L14.5 11" />
  </svg>
);

export const IconTop = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round">
    <path d="M4 3.5h12M10 16.5V7M6 11l4-4 4 4" />
  </svg>
);

export const IconBottom = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round">
    <path d="M4 16.5h12M10 3.5V13M6 9l4 4 4-4" />
  </svg>
);

export const IconClose = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.7} strokeLinecap="round">
    <path d="M5.5 5.5l9 9M14.5 5.5l-9 9" />
  </svg>
);

export const IconSignOut = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" strokeLinejoin="round">
    <path d="M12.5 6.5V4.5a1 1 0 0 0-1-1h-6a1 1 0 0 0-1 1v11a1 1 0 0 0 1 1h6a1 1 0 0 0 1-1v-2" />
    <path d="M8.5 10h8M14 7.5l2.5 2.5L14 12.5" />
  </svg>
);

/* ---------------------------------------------------------------------------
   The settings tab bar.

   Eight glyphs for the eight settings sections that had no icon in the app yet.
   The other five reuse what is already here, on purpose: Downloads, Accounts
   and Connections are the same idea as the sidebar entry and the connection row
   that already carry those glyphs, Reconnect is the retry arrow the task list
   uses for "do it again", and General is the gear — one idea, one drawing.

   Every one of these is drawn to survive 16px, which is the only size the tab
   bar ever asks for: no glyph here needs more than four strokes, and none of
   them is a picture of a thing nobody could name out loud.
   --------------------------------------------------------------------------- */

/** Modules: a switch, because the page is a column of them. */
export const IconModules = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.5}>
    <rect x="2.5" y="5.75" width="15" height="8.5" rx="4.25" />
    <circle cx="13.25" cy="10" r="2.25" fill="currentColor" stroke="none" />
  </svg>
);

/** Archives: a lidded box. */
export const IconArchive = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
    <rect x="2.5" y="3.5" width="15" height="4" rx="1" />
    <path d="M4 7.5v8A1.5 1.5 0 0 0 5.5 17h9a1.5 1.5 0 0 0 1.5-1.5v-8" />
    <path d="M8.25 11h3.5" />
  </svg>
);

/** Rules: a funnel — what the packagizer and the link filter do to a list. */
export const IconFilter = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
    <path d="M3 4.5h14l-5.4 6.2v5.1l-3.2 1.7v-6.8L3 4.5Z" />
  </svg>
);

/** Captcha: the ticked box everyone has clicked to prove they are a person. */
export const IconCaptcha = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
    <rect x="3" y="3" width="14" height="14" rx="2.5" />
    <path d="M6.75 10.1l2.4 2.4 4.1-4.6" />
  </svg>
);

/** Schedule: a clock. */
export const IconClock = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
    <circle cx="10" cy="10" r="7.25" />
    <path d="M10 5.9V10l2.9 1.7" />
  </svg>
);

/** Look: a drop of colour. */
export const IconLook = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.5} strokeLinejoin="round">
    <path d="M10 2.75c2.9 3.1 5 5.8 5 8.05a5 5 0 0 1-10 0c0-2.25 2.1-4.95 5-8.05Z" />
  </svg>
);

/** Access: a padlock. */
export const IconLock = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.5} strokeLinejoin="round">
    <rect x="3.75" y="8.5" width="12.5" height="8.25" rx="1.75" />
    <path d="M6.9 8.5V6.6a3.1 3.1 0 0 1 6.2 0v1.9" strokeLinecap="round" />
  </svg>
);

/** Advanced: faders, for the page where every value can be set by hand. */
export const IconSliders = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)} stroke="currentColor" strokeWidth={1.5} strokeLinecap="round">
    <path d="M3 6.75h4.25M11.25 6.75H17M3 13.25h5.75M12.75 13.25H17" />
    <circle cx="9.25" cy="6.75" r="2" />
    <circle cx="10.75" cy="13.25" r="2" />
  </svg>
);
