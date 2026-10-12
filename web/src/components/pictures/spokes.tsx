// A hub with the things that reach it or leave it around it, each on a line of
// its own. The pictures of the routes out and of the ways into the collector
// share the arrangement, the labels and the packet that walks a line.
import type { ReactNode } from 'react';
import { FILL, keyed, trip } from './scene';
import { Answer, loopOf } from './station';

export type SpokeShape = 'wide' | 'narrow';

export interface Spoke {
  x: number;
  y: number;
  /** The two ends of its line, a little outside the rim of both figures. */
  from: [x: number, y: number];
  to: [x: number, y: number];
  label: { x: number; y: number; width: number; align: 'start' | 'end' };
}

export interface SpokeSizes {
  /** The distance between two nodes on one side. */
  pitch: number;
  hub: number;
  node: number;
}

const GAP = 5;
const LABEL_HEIGHT = 36;
// The hub's name stands under it, in a box this wide and high.
const HUB_LABEL = { width: 96, height: 18 };

const WIDE = 800;

/**
 * spokeLayout places a hub and n nodes in a box. A wide box has the nodes on
 * both sides of the hub with their names on the outside; a narrow one has room
 * for one side only. A line runs from the hub outwards.
 *
 * `room` widens a wide box past its own 800 units, for a picture drawn at the
 * width of its card: the lines grow longer and the names keep their room on
 * the outside, so the figures stay the size they are drawn at.
 */
export function spokeLayout(shape: SpokeShape, n: number, sizes: SpokeSizes, room = WIDE) {
  const wide = shape === 'wide';
  const width = wide ? Math.max(WIDE, Math.round(room)) : 360;
  const extra = (width - WIDE) / 2;
  const right = wide ? Math.ceil(n / 2) : n;
  const left = n - right;
  // A hub with nodes on one side only moves along with the extra room instead.
  const reach = wide ? 112 + (left ? extra : 0) : 68;
  const height = Math.max((sizes.hub + HUB_LABEL.height + 8) * 2, right * sizes.pitch + 8);
  const hub = { x: !wide ? HUB_LABEL.width / 2 : left ? width / 2 : 240 + extra, y: height / 2 };

  const spokes = Array.from({ length: n }, (_, j): Spoke => {
    const side = wide && j % 2 ? -1 : 1;
    const row = (wide ? j >> 1 : j) - ((side > 0 ? right : left) - 1) / 2;
    const x = hub.x + side * reach;
    const y = hub.y + row * sizes.pitch;
    const length = Math.hypot(x - hub.x, y - hub.y);
    const ux = (x - hub.x) / length;
    const uy = (y - hub.y) / length;
    const text = sizes.node + 10;
    return {
      x,
      y,
      from: [hub.x + ux * (sizes.hub + GAP), hub.y + uy * (sizes.hub + GAP)],
      to: [x - ux * (sizes.node + GAP), y - uy * (sizes.node + GAP)],
      label:
        side > 0
          ? { x: x + text, y: y - LABEL_HEIGHT / 2, width: width - x - text - 4, align: 'start' }
          : { x: 4, y: y - LABEL_HEIGHT / 2, width: x - text - 4, align: 'end' },
    };
  });
  const hubLabel = { x: hub.x - HUB_LABEL.width / 2, y: hub.y + sizes.hub + 4, ...HUB_LABEL };
  return { width, height, hub, hubLabel, spokes };
}

/**
 * HubLabel is the hub's name under it. It stands on a patch of the card's own
 * surface, so a line that leaves the hub steeply passes behind the word.
 */
export function HubLabel({ at, children }: { at: { x: number; y: number; width: number; height: number }; children: string }) {
  return (
    <foreignObject {...at}>
      <div className="flex h-full justify-center">
        <span className="max-w-full truncate rounded-[var(--radius-control)] bg-carbon-surface px-1 text-meta font-semibold leading-[18px] text-carbon-textSub">
          {children}
        </span>
      </div>
    </foreignObject>
  );
}

/** The line of a spoke as a path, outwards. */
export const lineOf = (s: Spoke) => `M${s.from.map(round).join(' ')}L${s.to.map(round).join(' ')}`;

const round = (n: number) => Math.round(n * 10) / 10;

/** SpokeLabel is a node's name with one line under it, on the side away from the hub. */
export function SpokeLabel({
  at,
  name,
  sub,
  faint = false,
  ltr = false,
}: {
  at: Spoke['label'];
  name: string;
  sub: ReactNode;
  faint?: boolean;
  /** For a name that is an address, which keeps its order on a right-to-left page. */
  ltr?: boolean;
}) {
  return (
    <foreignObject x={Math.round(at.x)} y={Math.round(at.y)} width={Math.round(at.width)} height={LABEL_HEIGHT}>
      <div
        className={`flex h-full min-w-0 flex-col justify-center leading-[1.3] ${at.align === 'end' ? 'text-right' : 'text-left'}`}
      >
        <b
          dir={ltr ? 'ltr' : 'auto'}
          className={`truncate text-[13px] font-semibold ${faint ? 'text-carbon-textMuted' : 'text-carbon-text'}`}
        >
          {name}
        </b>
        <span dir="auto" className="truncate text-meta text-carbon-textMuted">
          {sub}
        </span>
      </div>
    </foreignObject>
  );
}

// Seconds a line rests between two packets.
const REST = 0.9;

/**
 * Stream is a packet that walks a line again and again while something runs
 * over it, and the ring that answers each arrival. `lead` is the seconds it
 * waits before setting off, so the lines of one picture do not start as one.
 */
export function Stream({ from, to, lead }: { from: [number, number]; to: [number, number]; lead: number }) {
  const walk = trip([from, to], () => 0, lead, REST);
  const loop = loopOf(walk);
  const { leave } = walk.stops[0];
  const { arrive } = walk.stops[1];
  const step = 0.02 / loop.dur;
  return (
    <g data-stream>
      <g transform={`translate(${round(to[0])} ${round(to[1])})`}>
        <Answer radius={4} at={arrive} loop={loop} />
      </g>
      <circle r="4" fill={FILL} opacity="0">
        <animateMotion {...walk.motion} calcMode="linear" {...loop.smil} />
        <animate attributeName="opacity" {...keyed([[0, 0], [leave - step, 0], [leave, 1], [arrive, 1], [arrive + step, 0], [1, 0]])} {...loop.smil} />
      </circle>
    </g>
  );
}
