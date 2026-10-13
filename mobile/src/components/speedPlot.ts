// The arithmetic behind SpeedGraph, by the rules of the web interface's speed
// curves (web/src/components/SpeedGraph.tsx, web/src/lib/speedHistory.ts and
// speedLines.ts). It imports nothing at run time, so check-speed-graph.mjs
// calls these functions as they are.

// The top of the plot never drops below this, so a blip near idle does not fill
// the box and a small transfer looks small.
const FLOOR = 64_000;

// The top of the plot sits this far above a speed limit it takes in, so the
// mark has room for its figure above it.
const HEADROOM = 1.2;

// A limit more than this many times the window's peak stays out of the scale,
// or a slow download under a generous limit would shrink to the bottom pixels.
const REACH = 4;

/**
 * ceilingOf is the top of the plot: the window's peak, so every sample fits,
 * raised to take in each limit given with room above it. A limit beyond REACH
 * is left out while anything moves; idle has no curve to flatten, so there the
 * limits set the scale.
 */
export function ceilingOf(samples: readonly number[], ...limits: number[]): number {
  const peak = Math.max(0, ...samples);
  const top = Math.max(FLOOR, peak);
  let ceiling = top;
  for (const limit of limits) {
    const room = limit * HEADROOM;
    if (limit > 0 && !(peak > 0 && room > REACH * top)) ceiling = Math.max(ceiling, room);
  }
  return ceiling;
}

/** Where a limit's mark sits in a plot `height` tall with `pad` above and below. */
export interface LimitMark {
  /** The limit's distance from the top edge. */
  y: number;
  /** The limit is above the scale, so the mark sits on the top edge with a chevron. */
  over: boolean;
}

/** limitMark places a limit against the ceiling, or returns null for none. */
export function limitMark(limit: number, ceiling: number, height: number, pad: number): LimitMark | null {
  if (limit <= 0) return null;
  if (limit > ceiling) return { y: pad, over: true };
  return { y: height - pad - Math.round((limit / ceiling) * (height - 2 * pad)), over: false };
}

// The gap between a limit's height and its figure.
const LABEL_GAP = 2;

/**
 * labelTops places the marks' figures, each `size` tall, in a plot `height`
 * tall: above its mark where there is room and under it otherwise, and one
 * that would cover an earlier one moved below that, or above it where the
 * plot ends.
 */
export function labelTops(ys: readonly number[], height: number, size: number): number[] {
  const tops: number[] = [];
  for (const y of ys) {
    let top = y - LABEL_GAP - size;
    if (top < 0) top = y + LABEL_GAP;
    for (const other of tops) {
      if (Math.abs(top - other) <= size) {
        const below = other + size + 1;
        const above = other - size - 1;
        top = below + size > height && above >= 0 ? above : below;
      }
    }
    tops.push(top);
  }
  return tops;
}

/**
 * uploadOf sums what the torrents send right now, by the rule the server's
 * record uses: a reading counts while its torrent downloads or seeds.
 */
export function uploadOf(tasks: Iterable<{ status: string; seeding?: boolean; uploadSpeed?: number }>): number {
  let total = 0;
  for (const t of tasks) {
    if (t.status === 'running' || t.seeding) total += t.uploadSpeed ?? 0;
  }
  return total;
}

/** spanLabel labels the left end of the time axis, in the web's symbols. */
export function spanLabel(seconds: number): string {
  if (seconds < 60) return `-${seconds}s`;
  if (seconds < 3600) return `-${Math.round(seconds / 6) / 10}min`;
  return `-${Math.round(seconds / 360) / 10}h`;
}

/** One direction's live readings before they become samples. */
export interface Lane {
  /** Ticks left in which a reading of 0 means "not known yet" rather than idle. */
  grace: number;
  /** The server's last recorded reading, which stands in during the grace. */
  carry: number;
}

// The task list arrives a moment after the graph mounts, and its zero until
// then would draw a notch where the server's record meets the live tail.
export const GRACE_TICKS = 2;

/** reading is the sample a tick takes from `value`, the first non-zero one ending the grace. */
export function reading(lane: Lane, value: number): number {
  if (lane.grace <= 0) return value;
  if (value !== 0) {
    lane.grace = 0;
    return value;
  }
  lane.grace--;
  return lane.carry;
}

/**
 * busyOf tells a plot with a curve to draw from an idle one: idle means nothing
 * on screen in either direction, not a current speed of zero.
 */
export function busyOf(down: readonly number[], up: readonly number[]): boolean {
  return down.length >= 2 && (down.some((v) => v > 0) || up.some((v) => v > 0));
}

function hueOf(hex: string): number {
  const n = parseInt(hex.slice(1), 16);
  const r = (n >> 16) & 255;
  const g = (n >> 8) & 255;
  const b = n & 255;
  const max = Math.max(r, g, b);
  const d = max - Math.min(r, g, b);
  if (d === 0) return 0;
  const h = max === r ? (g - b) / d : max === g ? 2 + (b - r) / d : 4 + (r - g) / d;
  return (h * 60 + 360) % 360;
}

/**
 * seedingDefault is the seeding line's colour: the preset farthest round the
 * colour wheel from the accent, so the two lines differ whatever the accent.
 */
export function seedingDefault(accent: string, presets: readonly string[]): string {
  const from = hueOf(accent);
  let best = presets[0];
  let farthest = -1;
  for (const hex of presets) {
    const turn = Math.abs(hueOf(hex) - from);
    const apart = Math.min(turn, 360 - turn);
    if (apart > farthest) {
      farthest = apart;
      best = hex;
    }
  }
  return best;
}

export interface Point {
  x: number;
  y: number;
}

/**
 * curve is the samples as a monotone cubic through points `w / span` apart,
 * the newest on the right edge, cut into `steps` straight pieces per sample so
 * plain views can draw it. Monotone, so it never swings past a sample: it
 * stays above zero and under the ceiling.
 */
export function curve(
  samples: readonly number[],
  w: number,
  h: number,
  pad: number,
  span: number,
  ceiling: number,
  steps: number,
): Point[] {
  const n = samples.length;
  if (n < 2) return [];
  const base = h - pad;
  const dx = w / span;
  const xs = samples.map((_, i) => w - (n - 1 - i) * dx);
  const ys = samples.map((v) => base - (v / ceiling) * (base - pad));
  const slope = (i: number) => (ys[i + 1] - ys[i]) / dx;

  // Steffen's tangents: flat at a turn, and never steeper than twice either
  // neighbouring slope, which is what rules out an overshoot.
  const m = new Array<number>(n);
  for (let i = 1; i < n - 1; i++) {
    const a = slope(i - 1);
    const b = slope(i);
    m[i] = a * b <= 0 ? 0 : Math.sign(a) * Math.min(2 * Math.abs(a), 2 * Math.abs(b), Math.abs(a + b) / 2);
  }
  m[0] = n > 2 ? (3 * slope(0) - m[1]) / 2 : slope(0);
  m[n - 1] = n > 2 ? (3 * slope(n - 2) - m[n - 2]) / 2 : slope(0);

  const t = dx / 3;
  const out: Point[] = [{ x: xs[0], y: ys[0] }];
  for (let i = 1; i < n; i++) {
    const y0 = ys[i - 1];
    const y1 = y0 + m[i - 1] * t;
    const y2 = ys[i] - m[i] * t;
    const y3 = ys[i];
    for (let k = 1; k <= steps; k++) {
      const s = k / steps;
      const r = 1 - s;
      // The control points sit a third of the way along x, so x runs straight.
      out.push({
        x: xs[i - 1] + s * dx,
        y: r * r * r * y0 + 3 * r * r * s * y1 + 3 * r * s * s * y2 + s * s * s * y3,
      });
    }
  }
  return out;
}

/** One column of a filled curve: its left edge, width, top and height. */
export interface Column {
  left: number;
  width: number;
  top: number;
  height: number;
}

/**
 * washOf is the ground under a curve as columns one piece wide, each reaching
 * from the middle of its piece down to `base`. A column runs half a point into
 * the next, so no gap shows between two.
 */
export function washOf(points: readonly Point[], base: number): Column[] {
  return points.slice(1).map((q, i) => {
    const p = points[i];
    const top = (p.y + q.y) / 2;
    return { left: p.x, width: q.x - p.x + 0.5, top, height: Math.max(0, base - top) };
  });
}
