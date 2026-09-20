// The browser tab as a tray tooltip: while the queue owes work, the favicon
// carries a percent ring and the title the numbers behind it; when nothing is
// owed, both return to exactly what they were. Plain DOM and canvas;
// components/TabIndicator.tsx drives it from live task data.

import type { Task } from './api';
import { fmtSpeed, pct } from './format';
import { DEFAULT_ACCENT } from './appearance';

// owed mirrors owed() in components/Counters.tsx, the server's rule: finished,
// failed and collector rows are not part of the queue.
function owed(t: Task): boolean {
  return t.status !== 'done' && t.status !== 'error' && t.status !== 'collected';
}

export interface Activity {
  /** Rows transferring bytes right now (status 'running'). */
  running: number;
  /** Rows still owed: queued, running, paused or extracting. Zero means idle. */
  total: number;
  /** Loaded over size for owed, enabled rows of known size, as Counters.tsx weighs them. */
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
    // Disabled links never fetch and would keep the ring from ever closing.
    if (t.enabled && t.size > 0) {
      size += t.size;
      loaded += t.loaded;
    }
  }
  return { running, total, percent: pct(loaded, size, false), speed };
}

/** formatTabTitle prefixes the counts to `base`, the title the caller
 *  captured before changing it. */
export function formatTabTitle(a: Activity, base: string): string {
  const parts = [`${a.running}/${a.total}`, `${a.percent}%`, fmtSpeed(a.speed) || '0 B/s'];
  return `(${parts.join(' · ')}) ${base}`;
}

// A favicon has room for one shape and one short number: the arc shows the
// percent and the centre the running count, capped at one digit. The exact
// counts are in the title.
//
// Fixed colours, since a canvas snapshot cannot follow a rotating rainbow
// accent: DEFAULT_ACCENT (the --status-info-solid hex) for active and
// --status-neutral-solid's grey for waiting.

const RING_SIZE = 64;
const RING_STROKE = 8;
const DISC_FILL = '#161616';
const DISC_EDGE = 'rgba(255, 255, 255, 0.35)';
const TRACK = 'rgba(255, 255, 255, 0.18)';
const RING_ACTIVE = DEFAULT_ACCENT;
const RING_WAITING = '#8d8d8d';
const LABEL_INK = '#f4f4f4';

/** renderRingFavicon returns a data: URL, or '' when canvas is unavailable. */
export function renderRingFavicon(a: Activity): string {
  const canvas = document.createElement('canvas');
  canvas.width = RING_SIZE;
  canvas.height = RING_SIZE;
  const ctx = canvas.getContext('2d');
  if (!ctx) return '';

  const c = RING_SIZE / 2;
  const r = c - RING_STROKE / 2 - 2;

  ctx.clearRect(0, 0, RING_SIZE, RING_SIZE);

  // A disc behind the ring, so it reads on light and dark browser chrome alike.
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

/** The page's <link rel="icon"> before it was changed, so idle restores it exactly. */
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

/** restoreIcon undoes applyIcon: it removes a link it created, or restores
 *  the one that was there. */
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
