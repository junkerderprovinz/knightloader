// How something travels past stations: it goes into a station that takes it,
// the station answers, and it comes out on the other side. The scenes that
// walk a file or a link along a line of stations share these parts.
import type { ReactNode } from 'react';
import { HUE, keyed, type Trip } from './scene';

// Seconds: the step in which a station takes its colour, the time the
// traveller takes to shrink into a station, and how long a ring widens.
const STEP = 0.05;
const ENTER = 0.15;
const RING = 0.5;

/** The timing every animated part of one walk shares. */
export interface Loop {
  dur: number;
  smil: { dur: string; repeatCount: 'indefinite' };
}

export function loopOf(trip: Trip): Loop {
  return { dur: trip.dur, smil: { dur: `${trip.dur.toFixed(2)}s`, repeatCount: 'indefinite' } };
}

/**
 * litFrom keys an opacity that comes on in one step at `from` and goes off
 * just before the loop starts over. A station waits in grey until its turn and
 * never fades in, which would show both states at once.
 */
export function litFrom(from: number, loop: Loop): { values: string; keyTimes: string } {
  return keyed([[0, 0], [from - STEP / loop.dur, 0], [from, 1], [0.97, 1], [0.985, 0], [1, 0]]);
}

/** Answer is a station replying to an arrival: a ring that widens and fades. */
export function Answer({ radius, at, loop }: { radius: number; at: number; loop: Loop }) {
  const gone = at + RING / loop.dur;
  return (
    <circle r={radius} fill="none" stroke={HUE} strokeWidth="2" opacity="0">
      <animate attributeName="r" {...keyed([[0, radius], [at, radius], [gone, radius + 13], [1, radius + 13]])} {...loop.smil} />
      <animate attributeName="opacity" {...keyed([[0, 0], [at - STEP / loop.dur, 0], [at, 0.7], [gone, 0], [1, 0]])} {...loop.smil} />
    </circle>
  );
}

/** Hop is the small jump a station makes as it answers. It goes inside the group that holds the station's shape. */
export function Hop({ at, loop }: { at: number; loop: Loop }) {
  return (
    <animateTransform
      attributeName="transform"
      type="scale"
      {...keyed([[0, 1], [at, 1], [at + 0.12 / loop.dur, 1.14], [at + 0.3 / loop.dur, 1], [1, 1]])}
      {...loop.smil}
    />
  );
}

/**
 * Traveller moves its children along the trip. At each of `taking`, the stops
 * that take it in, it shrinks into the station and is out of sight while the
 * station works; with `stays` the last one keeps it.
 */
export function Traveller({
  trip,
  loop,
  taking,
  stays = false,
  children,
}: {
  trip: Trip;
  loop: Loop;
  taking: { arrive: number; leave: number }[];
  stays?: boolean;
  children: ReactNode;
}) {
  const step = STEP / loop.dur;
  const enter = ENTER / loop.dur;
  const visible: [number, number][] = [[0, 0]];
  const size: [number, number][] = [[0, 0.3]];
  taking.forEach((stop, i) => {
    if (i > 0) {
      visible.push([stop.arrive, 1], [stop.arrive + step, 0]);
      size.push([stop.arrive - enter, 1], [stop.arrive, 0.3]);
    }
    if (stays && i === taking.length - 1) return;
    visible.push([stop.leave - step, 0], [stop.leave, 1]);
    size.push([stop.leave, 0.3], [stop.leave + enter, 1]);
  });
  if (stays) {
    visible.push([1, 0]);
    size.push([1, 0.3]);
  } else {
    visible.push([0.97, 1], [0.985, 0], [1, 0]);
    size.push([1, 1]);
  }

  return (
    <g opacity="0">
      <animateMotion {...trip.motion} calcMode="linear" {...loop.smil} />
      <animate attributeName="opacity" {...keyed(visible)} {...loop.smil} />
      <g>
        <animateTransform attributeName="transform" type="scale" {...keyed(size)} {...loop.smil} />
        {children}
      </g>
    </g>
  );
}
