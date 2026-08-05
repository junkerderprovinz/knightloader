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
