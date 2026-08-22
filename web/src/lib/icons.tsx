// Monochrome inline icons (currentColor), 20-22px, in the GlimStone house style.
//
// Every glyph is a filled solid shape (fill="currentColor"), never a stroked
// outline - GlimStone's own "Icon glyphs" section (added after this file was
// found to be a stroke/fill mix with no rule behind it): a line-drawn icon
// sits at a different visual weight than the filled badges, filled switches
// and filled active states everywhere around it. A detail that has to read
// as a thin line (a lock's shackle, a slider's track) is still drawn as a
// thin FILLED shape, never a <path stroke>. Where a glyph needs a visible
// gap inside a solid fill (a checkmark cut into a badge, the bar of a "!"),
// that gap is carved with fillRule="evenodd" rather than layering a second,
// background-coloured shape on top - the latter only looks right on the one
// background it was tuned against.
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

/* ---------------------------------------------------------------------------
   The settings tab bar.

   Ten glyphs for the ten settings sections that had no icon in the app yet
   (eight from the original settings shell, plus Diagnostics and Help from
   10C). The other five reuse what is already here, on purpose: Downloads,
   Accounts and Connections are the same idea as the sidebar entry and the
   connection row that already carry those glyphs, Reconnect is the retry
   arrow the task list uses for "do it again", and General is the gear — one
   idea, one drawing.

   Every one of these is drawn to survive 16px, which is the only size the tab
   bar ever asks for: no glyph here needs more than a handful of filled
   shapes, and none of them is a picture of a thing nobody could name out
   loud. Every settings tab has one - GlimStone's own rule (no exceptions).
   --------------------------------------------------------------------------- */

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

/** Rules: a funnel — what the packagizer and the link filter do to a list. */
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
    <circle cx="10" cy="10" r="7.25" />
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M7.7 7.9a2.35 2.35 0 1 1 3.4 2.1c-.6.32-.95.75-1.05 1.3h-1.8c.1-1.05.55-1.75 1.4-2.25a.85.85 0 1 0-1.25-.75Zm1.3 5.4a1 1 0 1 0 2 0 1 1 0 0 0-2 0Z"
    />
  </svg>
);

/** Scripts (Wave 11B): angle brackets, the one glyph that reads as "code" at
 *  16px without also reading as a browser tab or a terminal window. */
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

/**
 * "Open natively" (desktop only): a small app window, distinct from
 * IconExternalLink's arrow-leaving-a-box, which already means "goes to a
 * website" (Buy Premium/Renew). This one means "hands off to another
 * application on this machine".
 */
export const IconApp = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="3.5" y="4.5" width="13" height="11" rx="1.5" opacity=".3" />
    <path d="M3.5 6A1.5 1.5 0 0 1 5 4.5h10A1.5 1.5 0 0 1 16.5 6v1.5h-13Z" />
  </svg>
);

/**
 * Remote access's loud warning (build-plan.md's Wave 11 amendment on 11C): a
 * triangle with an exclamation mark, the one glyph this app has no quieter
 * equivalent of on purpose. The mark itself is carved out of the solid
 * triangle with fillRule="evenodd" rather than drawn as a second shape, so it
 * reads correctly regardless of whatever sits behind the icon.
 */
export const IconWarning = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M10 2.75 18 16.75H2L10 2.75Zm-.9 4.75v4.5h1.8v-4.5Zm.9 6.4a1 1 0 1 0 0 2 1 1 0 0 0 0-2Z"
    />
  </svg>
);

/** API tokens: a key, distinct from IconLock's padlock (the shared
 *  password) by shape alone, the same distinction the two credentials
 *  themselves keep. */
export const IconKey = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <circle cx="6.25" cy="13.75" r="3" />
    <rect x="7.7" y="10.6" width="10.6" height="2" rx="1" transform="rotate(-45 13 11.6)" />
    <rect x="14.6" y="4.3" width="1.8" height="2.6" transform="rotate(-45 15.5 5.6)" />
    <rect x="16.3" y="2.6" width="1.8" height="2.6" transform="rotate(-45 17.2 3.9)" />
  </svg>
);

/** Keyboard shortcuts: a keyboard's own outline with a row of keys and a
 *  spacebar, distinct from IconKey's API-token key by shape and by idea -
 *  nothing here is a credential. */
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

/** The menu/hamburger trigger: three horizontal filled bars, always exactly
 *  three, never a sliders/equalizer glyph standing in for it (GlimStone's
 *  own rule - the two read as different controls, "adjust a value" vs "open
 *  a menu", to anyone who has seen either convention before). */
export const IconMenu = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="3" y="5" width="14" height="2" rx="1" />
    <rect x="3" y="9" width="14" height="2" rx="1" />
    <rect x="3" y="13" width="14" height="2" rx="1" />
  </svg>
);

/** System: the standard power glyph, for the page that quits/restarts the
 *  process and backs up or restores its data - shipped without an icon
 *  originally ("does not yet warrant inventing a new glyph"); every settings
 *  tab has one, no exceptions (GlimStone's own rule). */
export const IconPower = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <path
      fillRule="evenodd"
      clipRule="evenodd"
      d="M10 3a1 1 0 0 1 1 1v5a1 1 0 1 1-2 0V4a1 1 0 0 1 1-1Zm-4.2 2.1a1 1 0 0 1 0 1.42A5.25 5.25 0 1 0 14.2 6.52a1 1 0 1 1 1.4-1.42 7.25 7.25 0 1 1-11.2 0 1 1 0 0 1 1.4 0Z"
    />
  </svg>
);

/** Browser tools: a browser window (chrome bar + three window-control dots),
 *  for the page that hands out the bookmarklet, the extension and the PWA
 *  install step - the same "shipped without one" gap IconPower's own doc
 *  comment explains. */
export const IconBrowser = (p: SVGProps<SVGSVGElement>) => (
  <svg {...base(p)}>
    <rect x="2.5" y="3.5" width="15" height="13" rx="1.5" opacity=".3" />
    <path d="M2.5 5A1.5 1.5 0 0 1 4 3.5h12A1.5 1.5 0 0 1 17.5 5v1.5h-15Z" />
    <circle cx="5" cy="5" r=".6" />
    <circle cx="7" cy="5" r=".6" />
    <circle cx="9" cy="5" r=".6" />
  </svg>
);
