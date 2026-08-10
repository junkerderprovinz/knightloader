// tabIndicator is the browser-tab equivalent of a desktop tray tooltip: while
// the queue owes work, the favicon carries a percent ring and the title
// carries the numbers behind it; the moment nothing is owed, both go back to
// exactly what they were before this file ever touched them.
//
// Framework-free on purpose, the same split appearance.ts uses for the same
// reason: this is DOM and canvas work with nothing React-specific in it, and
// keeping it that way is what makes the restore-on-idle contract simple
// enough to read in one pass. The hook that drives it from live task data is
// components/TabIndicator.tsx.

import type { Task } from './api';
import { fmtSpeed, pct } from './format';
import { DEFAULT_ACCENT } from './appearance';

/**
 * A row still owed work - mirrors components/Counters.tsx's own `owed()`,
 * which is itself the server's rule (app.Counters, see that file's comment):
 * finished, failed and staged-in-the-collector rows are not "the queue" any
 * more. Kept as a private copy rather than an import: Counters.tsx does not
 * export it, and this feature has no reason to change a component file that
 * is not its own.
 */
function owed(t: Task): boolean {
  return t.status !== 'done' && t.status !== 'error' && t.status !== 'collected';
}

export interface Activity {
  /** Actually transferring bytes right now - status === 'running', not queued, not extracting. */
  running: number;
  /** Still owed: queued, running, paused or extracting. Zero is the idle state everything restores to. */
  total: number;
  /** Loaded/size bytes over every owed, enabled row with a known size - the same rows Counters.tsx's shell strip weighs by default (includeDisabled false). */
  percent: number;
  /** Sum of the speed field on rows actually running. */
  speed: number;
}

/** measureActivity reduces the live task record to the four numbers a tab can show. */
export function measureActivity(tasks: Record<string, Task>): Activity {
  let running = 0;
  let total = 0;
  let loaded = 0;
  let size = 0;
  let speed = 0;
  for (const t of Object.values(tasks)) {
    if (!owed(t)) continue;
    total++;
    if (t.status === 'running') {
      running++;
      speed += t.speed;
    }
    // A disabled link is never going to fetch, so - matching weigh()'s own
    // default view in Counters.tsx - it stays out of the byte math, or a
    // queue with a few switched-off links parks the ring at a permanent
    // partial fill nothing is ever going to close.
    if (t.enabled && t.size > 0) {
      size += t.size;
      loaded += t.loaded;
    }
  }
  return { running, total, percent: pct(loaded, size, false), speed };
}

/**
 * formatTabTitle. `base` is whatever document.title held before this feature
 * touched it, captured once by the caller - never a literal "KnightLoader"
 * here, so a page that ever starts setting its own title keeps it.
 */
export function formatTabTitle(a: Activity, base: string): string {
  const parts = [`${a.running}/${a.total}`, `${a.percent}%`, fmtSpeed(a.speed) || '0 B/s'];
  return `(${parts.join(' · ')}) ${base}`;
}

// --- The ring -----------------------------------------------------------
//
// A favicon is a 16-32px circle, which is room for one glanceable shape and
// one short number, not two counts and a percent all at once. The arc
// carries percent - the thing that actually changes second to second - and
// the centre carries the running count, capped at one digit, because that is
// the number that answers "is anything happening" at a glance. The exact
// running/total pair lives in the title instead, where hovering the tab
// shows it in full, the same way hovering a tray icon would.
//
// Colours are fixed rather than read from the live --accent custom property:
// the accent can be mid rainbow-rotation, which a canvas snapshot has no way
// to follow, and DEFAULT_ACCENT is the one colour every install already
// agrees means "active" (index.css's own --status-info-solid is the same
// hex). The grey is index.css's --status-neutral-solid, the same tone
// StatusPill gives a paused or queued row.

const RING_SIZE = 64;
const RING_STROKE = 8;
const DISC_FILL = '#161616';
const DISC_EDGE = 'rgba(255, 255, 255, 0.35)';
const TRACK = 'rgba(255, 255, 255, 0.18)';
const RING_ACTIVE = DEFAULT_ACCENT;
const RING_WAITING = '#8d8d8d';
const LABEL_INK = '#f4f4f4';

/** renderRingFavicon returns a data: URL, or '' if canvas is unavailable (never thrown - a missing favicon is not worth failing anything over). */
export function renderRingFavicon(a: Activity): string {
  const canvas = document.createElement('canvas');
  canvas.width = RING_SIZE;
  canvas.height = RING_SIZE;
  const ctx = canvas.getContext('2d');
  if (!ctx) return '';

  const c = RING_SIZE / 2;
  const r = c - RING_STROKE / 2 - 2;

  ctx.clearRect(0, 0, RING_SIZE, RING_SIZE);

  // The disc, so the ring reads as one solid roundel against a light OR a
  // dark browser chrome rather than a stray arc that half-vanishes on one of
  // the two.
  ctx.beginPath();
  ctx.arc(c, c, r - RING_STROKE / 2 + 1, 0, Math.PI * 2);
  ctx.fillStyle = DISC_FILL;
  ctx.fill();
  ctx.lineWidth = 1.5;
  ctx.strokeStyle = DISC_EDGE;
  ctx.stroke();

  // Track: the full circle, unfilled.
  ctx.beginPath();
  ctx.arc(c, c, r, 0, Math.PI * 2);
  ctx.lineWidth = RING_STROKE;
  ctx.strokeStyle = TRACK;
  ctx.stroke();

  // Progress: from 12 o'clock, clockwise.
  if (a.percent > 0) {
    const start = -Math.PI / 2;
    const end = start + (Math.PI * 2 * a.percent) / 100;
    ctx.beginPath();
    ctx.arc(c, c, r, start, end);
    ctx.lineWidth = RING_STROKE;
    ctx.lineCap = 'round';
    ctx.strokeStyle = a.running > 0 ? RING_ACTIVE : RING_WAITING;
    ctx.stroke();
  }

  if (a.running > 0) {
    const label = a.running > 9 ? '9+' : String(a.running);
    ctx.fillStyle = LABEL_INK;
    ctx.font = `700 ${label.length > 1 ? 22 : 28}px system-ui, sans-serif`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillText(label, c, c + 1);
  }

  return canvas.toDataURL('image/png');
}

// --- Applying it to the document, and undoing that exactly ----------------

/** What the page's own <link rel="icon"> looked like before this feature ever ran, so idle can restore it verbatim rather than guessing at a default. */
export interface IconSnapshot {
  existed: boolean;
  href: string;
  type: string;
}

function iconLink(): HTMLLinkElement | null {
  return document.querySelector<HTMLLinkElement>("link[rel~='icon']");
}

export function captureIcon(): IconSnapshot {
  const link = iconLink();
  return { existed: !!link, href: link?.getAttribute('href') ?? '', type: link?.getAttribute('type') ?? '' };
}

export function applyIcon(dataUrl: string): void {
  if (!dataUrl) return;
  let link = iconLink();
  if (!link) {
    link = document.createElement('link');
    link.rel = 'icon';
    document.head.appendChild(link);
  }
  link.setAttribute('type', 'image/png');
  link.setAttribute('href', dataUrl);
}

/**
 * restoreIcon undoes applyIcon. No link existed before this feature touched
 * the page today (index.html ships none), so the ordinary path removes the
 * one it created; the `existed` branch only matters if a real favicon is
 * ever added later, and it puts that one back exactly as it was rather than
 * leaving today's assumption baked in.
 */
export function restoreIcon(snap: IconSnapshot): void {
  const link = iconLink();
  if (!link) return;
  if (!snap.existed) {
    link.remove();
    return;
  }
  if (snap.type) link.setAttribute('type', snap.type);
  else link.removeAttribute('type');
  link.setAttribute('href', snap.href);
}
