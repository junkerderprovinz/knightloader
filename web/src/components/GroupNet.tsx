// GroupNet draws a group as a net: this instance in the middle and everything
// paired with it around it, each under its name. It reads the page's own
// lists, so a member that comes or goes redraws it, and a node and its entry
// on the page light up together. A line through the relay is dashed, and links
// handed over travel along their line as a parcel.
import { useEffect, useRef, useState, type FocusEvent, type PointerEvent, type ReactNode } from 'react';
import logoUrl from '../assets/logo.svg';
import { useStillPictures } from '../lib/motion';
import { kindGlyph } from './InstanceCard';
import { FILL, HUE, SURFACE, TRAVEL } from './pictures/scene';

export interface NetNode {
  /** The key the node shares with its entry on the page (`data-nk`). */
  key: string;
  /** What the node is, as its card says: "container", "server", "desktop",
   *  "mobile" or "extension". It stands in a badge on the shield. */
  deployment?: string;
  name: string;
  /** The line under the name: how the node is reached, or what it is. */
  sub: string;
  /** Not reachable right now. */
  dim?: boolean;
  /** Reached through the relay, which its line says by being dashed. */
  relay?: boolean;
}

/** Links on their way along a node's line. */
export interface NetParcel {
  id: number;
  /** The node at the other end of the line. */
  key: string;
  /** From this instance to the node, or from the node to this instance. */
  out: boolean;
}

/** The key of this instance's own node and entry. */
export const SELF_KEY = 'self';

const RADIUS = 27;
const HUB_RADIUS = 34;

type Label = [x: number, y: number, width: number, align: 'start' | 'end' | 'mid'];

/**
 * netLayout places this instance and n others in a box. A wide box has the
 * others stacked on both sides with their names on the outside; a narrow one
 * has room for one side only.
 */
export function netLayout(shape: 'wide' | 'narrow', n: number) {
  const wide = shape === 'wide';
  const width = wide ? 720 : 340;
  const reach = wide ? 212 : 160;
  const pitch = wide ? 84 : 74;
  const right = wide ? Math.ceil(n / 2) : n;
  const left = n - right;
  const height = Math.max(150, right * pitch + 10);
  const hub = { x: !wide ? 42 : left ? 360 : 220, y: right > 1 ? height / 2 : 63 };
  const points = Array.from({ length: n }, (_, j) => {
    const side = wide && j % 2 ? -1 : 1;
    const row = (wide ? j >> 1 : j) - ((side > 0 ? right : left) - 1) / 2;
    const x = hub.x + side * (reach - 14 * Math.abs(row));
    const y = hub.y + row * pitch;
    const label: Label = side > 0 ? [x + 36, y - 18, width - x - 40, 'start'] : [4, y - 18, x - 40, 'end'];
    return { x, y, label };
  });
  const hubLabel: Label = [hub.x - 40, hub.y + 40, 80, 'mid'];
  return { width, height, hub: { ...hub, label: hubLabel }, points };
}

const xy = (x: number, y: number) => `${+x.toFixed(1)} ${+y.toFixed(1)}`;

const ALIGN = { start: 'text-left', end: 'text-right', mid: 'text-center' };

function NodeLabel({ at: [x, y, w, align], name, sub }: { at: Label; name: string; sub: string }) {
  return (
    <foreignObject x={Math.round(x)} y={Math.round(y)} width={Math.round(w)} height="36">
      <div className={`kl-net-label flex h-full min-w-0 flex-col justify-center leading-[1.3] ${ALIGN[align]}`}>
        <b className="truncate text-[13px] font-semibold text-carbon-text">{name}</b>
        <span className="truncate text-meta text-carbon-textMuted">{sub}</span>
      </div>
    </foreignObject>
  );
}

/**
 * Shape is the KnightLoader shield every node wears, since every one of them
 * is KnightLoader, with what kind it is in a badge at the shield's foot.
 */
function Shape({ deployment, x, y, size = 50 }: { deployment?: string; x: number; y: number; size?: number }) {
  const Glyph = kindGlyph(deployment);
  // The shield keeps its own proportions inside the square, so its right
  // edge stands at about 0.3 of the size from the middle.
  const bx = x + size * 0.3;
  const by = y + size * 0.32;
  const radius = size * 0.19;
  return (
    <>
      <image href={logoUrl} x={x - size / 2} y={y - size / 2} width={size} height={size} className="kl-pic-lift" />
      {Glyph && (
        <g className="kl-net-badge">
          <circle className="kl-net-tile" cx={bx} cy={by} r={radius} stroke={SURFACE} strokeWidth="2" />
          <g color="var(--accent-glyph)">
            <Glyph x={bx - radius * 0.62} y={by - radius * 0.62} width={radius * 1.24} height={radius * 1.24} />
          </g>
        </g>
      )}
    </>
  );
}

function NodeButton({
  name,
  x,
  y,
  r,
  onPick,
  children,
}: {
  name: string;
  x: number;
  y: number;
  r: number;
  onPick: () => void;
  children: ReactNode;
}) {
  return (
    <g
      className="kl-net-node"
      tabIndex={0}
      role="button"
      aria-label={name}
      onClick={onPick}
      onKeyDown={(e) => {
        if (e.key !== 'Enter' && e.key !== ' ') return;
        e.preventDefault();
        onPick();
      }}
    >
      <circle className="kl-net-ring" cx={+x.toFixed(1)} cy={+y.toFixed(1)} r={r + 6} />
      {children}
    </g>
  );
}

type Point = { x: number; y: number };

/** How long a parcel stays at its destination where nothing may move. */
export const PARCEL_REST_MS = 1600;
const ANSWER_MS = 450;

/**
 * Parcel is one handover on its line. It travels at the speed everything in a
 * picture travels at and stays in sight all the way, and the node it reaches
 * answers with a ring. A still picture shows it arrived.
 */
function Parcel({ from, to, at, r, onDone }: { from: Point; to: Point; at: Point; r: number; onDone: () => void }) {
  const box = useRef<SVGGElement>(null);
  const ring = useRef<SVGCircleElement>(null);
  const calm = useStillPictures();
  const done = useRef(onDone);
  done.current = onDone;

  useEffect(() => {
    const parcel = box.current;
    const answer = ring.current;
    if (calm || !parcel || !answer || typeof parcel.animate !== 'function') {
      const rest = window.setTimeout(() => done.current(), PARCEL_REST_MS);
      return () => window.clearTimeout(rest);
    }
    const place = (p: Point) => `translate(${p.x}px, ${p.y}px)`;
    const running: Animation[] = [];
    const trip = parcel.animate([{ transform: place(from) }, { transform: place(to) }], {
      duration: (Math.hypot(to.x - from.x, to.y - from.y) / TRAVEL) * 1000,
      easing: 'linear',
    });
    running.push(trip);
    trip.onfinish = () => {
      parcel.style.visibility = 'hidden';
      const rings = answer.animate(
        [
          { opacity: 0.8, transform: 'scale(1)' },
          { opacity: 0, transform: 'scale(1.3)' },
        ],
        { duration: ANSWER_MS, easing: 'ease-out' },
      );
      running.push(rings);
      rings.onfinish = () => done.current();
    };
    return () => running.forEach((a) => a.cancel());
  }, [calm, from.x, from.y, to.x, to.y]);

  return (
    <g className="kl-net-parcel" aria-hidden>
      <circle ref={ring} className="kl-net-answer" cx={at.x} cy={at.y} r={r + 6} fill="none" stroke={HUE} strokeWidth="2" opacity="0" />
      <g ref={box} transform={`translate(${xy(to.x, to.y)})`}>
        <rect x="-8" y="-6.5" width="16" height="13" rx="2.5" fill={FILL} />
        {/* The tape is cut out of the box, so it reads on every hue. */}
        <path d="M-8 -1.5H8M0 -6.5V-1.5" stroke={SURFACE} strokeWidth="1.5" />
      </g>
    </g>
  );
}

interface NetProps {
  /** This instance, the node in the middle. */
  self: { name: string; sub: string; deployment?: string };
  nodes: NetNode[];
  /** The handovers under way, each on the line of its node. */
  parcels?: NetParcel[];
  /** A parcel has arrived and goes out of the picture. */
  onDelivered?: (id: number) => void;
  /** The key under the pointer or the focus, here or on the page. */
  hot: string | null;
  /** Brings the node's entry on the page into view. */
  onPick: (key: string) => void;
  /** The picture's accessible name. */
  alt: string;
}

function NetSvg({
  shape,
  self,
  nodes,
  parcels = [],
  onDelivered,
  hot,
  onPick,
  alt,
}: NetProps & { shape: 'wide' | 'narrow' }) {
  const { width, height, hub, points } = netLayout(shape, nodes.length);
  return (
    <svg
      className={`kl-pic kl-net-svg ${shape}${hot && hot !== SELF_KEY ? ' picked' : ''}`}
      viewBox={`0 0 ${width} ${Math.round(height)}`}
      role="group"
      aria-label={alt}
    >
      {nodes.map((node, j) => {
        const p = points[j];
        const length = Math.hypot(p.x - hub.x, p.y - hub.y);
        const ux = (p.x - hub.x) / length;
        const uy = (p.y - hub.y) / length;
        // The link ends a little outside the rim of both nodes.
        const here = { x: hub.x + ux * (HUB_RADIUS + 5), y: hub.y + uy * (HUB_RADIUS + 5) };
        const there = { x: p.x - ux * (RADIUS + 5), y: p.y - uy * (RADIUS + 5) };
        const d = `M${xy(here.x, here.y)} L${xy(there.x, there.y)}`;
        return (
          <g
            key={node.key}
            className={`kl-net-spoke${node.dim ? ' dim' : ''}${hot === node.key ? ' hot' : ''}`}
            data-nk={node.key}
          >
            {/* A solid link is one dash as long as itself, which is what draws
                it in. A dashed one keeps its own dashes and fades in. */}
            {node.relay ? <path className="kl-net-link relay" d={d} /> : <path className="kl-net-link" pathLength="1" d={d} />}
            <NodeButton name={node.name} x={p.x} y={p.y} r={RADIUS} onPick={() => onPick(node.key)}>
              <Shape deployment={node.deployment} x={p.x} y={p.y} />
            </NodeButton>
            <NodeLabel at={p.label} name={node.name} sub={node.sub} />
            {parcels
              .filter((parcel) => parcel.key === node.key)
              .map((parcel) => (
                <Parcel
                  key={parcel.id}
                  from={parcel.out ? here : there}
                  to={parcel.out ? there : here}
                  at={parcel.out ? p : hub}
                  r={parcel.out ? RADIUS : HUB_RADIUS}
                  onDone={() => onDelivered?.(parcel.id)}
                />
              ))}
          </g>
        );
      })}
      <g className={`kl-net-spoke hub${hot === SELF_KEY ? ' hot' : ''}`} data-nk={SELF_KEY}>
        <NodeButton name={self.name} x={hub.x} y={hub.y} r={HUB_RADIUS} onPick={() => onPick(SELF_KEY)}>
          <Shape deployment={self.deployment} x={hub.x} y={hub.y} size={64} />
        </NodeButton>
        <NodeLabel at={hub.label} name={self.name} sub={self.sub} />
      </g>
    </svg>
  );
}

export function GroupNet(props: NetProps) {
  const box = useRef<HTMLDivElement>(null);
  const calm = useStillPictures();

  // The links draw in once, when the page opens and the picture is in sight.
  // The test environment has neither of the two interfaces this takes.
  useEffect(() => {
    const el = box.current;
    if (calm || !el || typeof el.animate !== 'function' || typeof IntersectionObserver === 'undefined') return;
    const drawn: Animation[] = [];
    el.querySelectorAll('.kl-net-svg').forEach((svg) =>
      svg.querySelectorAll('.kl-net-spoke:not(.hub)').forEach((spoke, j) => {
        const start = 150 + j * 140;
        const show = (parts: string, delay: number, frames: Keyframe[]) =>
          spoke.querySelectorAll(parts).forEach((part) =>
            drawn.push(
              part.animate(frames, { duration: 420, delay: start + delay, easing: 'cubic-bezier(.3,0,.2,1)', fill: 'backwards' }),
            ),
          );
        show('.kl-net-link:not(.relay)', 0, [{ strokeDashoffset: 1 }, { strokeDashoffset: 0 }]);
        show('.kl-net-link.relay', 0, [{ opacity: 0 }, { opacity: 1 }]);
        show('.kl-net-node, .kl-net-label', 300, [
          { opacity: 0, transform: 'scale(.7)' },
          { opacity: 1, transform: 'none' },
        ]);
      }),
    );
    drawn.forEach((a) => a.pause());
    const watch = new IntersectionObserver(
      (seen) => {
        if (!seen[0].isIntersecting) return;
        watch.disconnect();
        drawn.forEach((a) => a.play());
      },
      { threshold: 0.35 },
    );
    watch.observe(el);
    return () => {
      watch.disconnect();
      drawn.forEach((a) => a.cancel());
    };
  }, [calm]);

  return (
    <div ref={box} data-new="net-shields" className="kl-net" data-testid="group-net">
      <NetSvg shape="wide" {...props} />
      <NetSvg shape="narrow" {...props} />
    </div>
  );
}

/**
 * useNetHover links the net with the entries of the page around it. Whatever
 * carries `data-nk` inside the element that takes `scope` shares the hot key
 * while the pointer or the focus is on it, and `pick` brings a key's entry
 * into view.
 */
export function useNetHover<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [hot, setHot] = useState<string | null>(null);
  const calm = useStillPictures();
  const over = (e: PointerEvent | FocusEvent) =>
    setHot((e.target as Element).closest('[data-nk]')?.getAttribute('data-nk') ?? null);
  const leave = () => setHot(null);
  const pick = (key: string) => {
    const entry = [...(ref.current?.querySelectorAll('[data-nk]') ?? [])].find(
      (el) => el.getAttribute('data-nk') === key && !el.closest('svg'),
    );
    entry?.scrollIntoView({ block: 'nearest', behavior: calm ? 'auto' : 'smooth' });
  };
  return { hot, pick, scope: { ref, onPointerOver: over, onPointerLeave: leave, onFocus: over, onBlur: leave } };
}
