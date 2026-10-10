// What the animated pictures share: the theme's tokens as paint, the motion
// gate, and the timing of whatever travels. The pictures are SVG moved with
// SMIL, so the stylesheet's motion levels cannot reach them and each picture
// asks here whether it may move.
import { useLayoutEffect, useRef, useSyncExternalStore, type RefObject } from 'react';

export const INK = 'var(--carbon-text)';
export const LABEL = 'var(--carbon-text-sub)';
export const QUIET = 'var(--carbon-text-muted)';
export const SURFACE = 'var(--carbon-surface)';
export const GROUND = 'var(--carbon-surface2)';
/** The accent as a fill. A detail drawn inside it is cut out in the colour of the surface behind. */
export const FILL = 'var(--accent)';
/** The accent where it is read against the page: a line, a ring, a liquid. */
export const HUE = 'var(--accent-ink)';
export const OK = 'var(--status-ok-solid)';
export const WARN = 'var(--status-warn-solid)';
export const FAIL = 'var(--status-fail-solid)';

const REDUCED_MOTION = '(prefers-reduced-motion: reduce)';

function isStill(): boolean {
  return window.matchMedia(REDUCED_MOTION).matches || document.documentElement.dataset.motion === 'off';
}

function subscribeStill(onChange: () => void): () => void {
  const query = window.matchMedia(REDUCED_MOTION);
  query.addEventListener('change', onChange);
  const level = new MutationObserver(onChange);
  level.observe(document.documentElement, { attributes: true, attributeFilter: ['data-motion'] });
  return () => {
    query.removeEventListener('change', onChange);
    level.disconnect();
  };
}

/**
 * useStill says whether a picture shows its still frame: the motion level is
 * off, or the system asks for less motion. A picture that loops counts as
 * continuous motion, so no level is exempt.
 */
export function useStill(): boolean {
  return useSyncExternalStore(subscribeStill, isStill);
}

/**
 * useStage holds a picture on its first frame until half of it is on screen,
 * so nobody scrolls into the middle of a scene. `take` names what is playing;
 * a new take starts over.
 */
export function useStage(take = ''): { stage: RefObject<SVGSVGElement | null>; still: boolean } {
  const stage = useRef<SVGSVGElement>(null);
  const still = useStill();
  useLayoutEffect(() => {
    const svg = stage.current;
    if (still || !svg) return;
    svg.pauseAnimations();
    svg.setCurrentTime(0);
    const watch = new IntersectionObserver(
      (seen) => {
        if (!seen.some((s) => s.isIntersecting)) return;
        watch.disconnect();
        svg.unpauseAnimations();
      },
      { threshold: 0.5 },
    );
    watch.observe(svg);
    return () => watch.disconnect();
  }, [still, take]);
  return { stage, still };
}

const round = (n: number) => Math.round(n * 1000) / 1000;

/** SMIL takes key times and values as two parallel lists; pairs keep each value beside its moment. */
export function keyed(points: [at: number, value: number | string][]): { values: string; keyTimes: string } {
  return {
    values: points.map((p) => p[1]).join(';'),
    keyTimes: points.map((p) => round(p[0])).join(';'),
  };
}

/** Viewbox units a second for everything that travels. A short path takes less time, never less speed. */
export const TRAVEL = 70;

export interface Trip {
  /** Seconds one loop takes. */
  dur: number;
  /** When the traveller reaches each point and when it leaves, as shares of the loop. */
  stops: { arrive: number; leave: number }[];
  /** What animateMotion needs to walk the path on those times. */
  motion: { path: string; keyPoints: string; keyTimes: string };
}

/**
 * trip times a walk along `points`: every leg takes as long as it is, and the
 * traveller rests `rest(i)` seconds at point i. It waits `lead` seconds before
 * the first point counts as reached and stays `tail` seconds at the last.
 */
export function trip(points: [x: number, y: number][], rest: (i: number) => number, lead = 0.5, tail = 1.8): Trip {
  const legs = points.slice(1).map(([x, y], i) => Math.hypot(x - points[i][0], y - points[i][1]));
  const length = legs.reduce((sum, leg) => sum + leg, 0);

  const times: { arrive: number; leave: number }[] = [];
  let t = lead;
  points.forEach((_, i) => {
    const leave = t + rest(i);
    times.push({ arrive: t, leave });
    t = leave + (legs[i] ?? 0) / TRAVEL;
  });
  const dur = t + tail;

  const keyTimes = [0];
  const keyPoints = [0];
  let walked = 0;
  times.forEach(({ arrive, leave }, i) => {
    const share = walked / length;
    if (i > 0) {
      keyTimes.push(arrive / dur);
      keyPoints.push(share);
    }
    if (leave > arrive || i === 0) {
      keyTimes.push(leave / dur);
      keyPoints.push(share);
    }
    walked += legs[i] ?? 0;
  });
  keyTimes.push(1);
  keyPoints.push(1);

  return {
    dur,
    stops: times.map(({ arrive, leave }) => ({ arrive: arrive / dur, leave: leave / dur })),
    motion: {
      path: `M${points.map(([x, y]) => `${x} ${y}`).join('L')}`,
      keyPoints: keyPoints.map(round).join(';'),
      keyTimes: keyTimes.map(round).join(';'),
    },
  };
}
