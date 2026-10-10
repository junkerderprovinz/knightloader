// GroupNet draws a group as a net: this instance in the middle and everything
// paired with it around it, each under its name. It reads the page's own
// lists, so a member that comes or goes redraws it, and a node and its entry
// on the page light up together.
import { useEffect, useRef, useState, type FocusEvent, type PointerEvent, type ReactNode } from 'react';
import logoUrl from '../assets/logo.svg';
import { IconBrowser, IconPhone } from '../lib/icons';
import { useStillPictures } from '../lib/motion';

export interface NetNode {
  /** The key the node shares with its entry on the page (`data-nk`). */
  key: string;
  kind: 'instance' | 'phone' | 'browser';
  name: string;
  /** The line under the name: how the node is reached, or what it is. */
  sub: string;
  /** Not reachable right now. */
  dim?: boolean;
}

/** The key of this instance's own node and entry. */
export const SELF_KEY = 'self';

/** What kind of node a phone or an extension of the group is drawn as. */
export function appNodeKind(deployment: string): NetNode['kind'] {
  return deployment === 'extension' ? 'browser' : 'phone';
}

const RADIUS: Record<NetNode['kind'], number> = { instance: 27, phone: 24, browser: 24 };
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
        <span className="truncate text-[11px] text-carbon-textMuted">{sub}</span>
      </div>
    </foreignObject>
  );
}

function Shape({ kind, x, y, size = 50 }: { kind: NetNode['kind']; x: number; y: number; size?: number }) {
  if (kind === 'instance') {
    return <image href={logoUrl} x={x - size / 2} y={y - size / 2} width={size} height={size} className="kl-pic-lift" />;
  }
  const [w, h] = kind === 'phone' ? [28, 42] : [42, 34];
  const Glyph = kind === 'phone' ? IconPhone : IconBrowser;
  return (
    <>
      <rect className="kl-net-tile" x={x - w / 2} y={y - h / 2} width={w} height={h} rx="8" />
      <g color="var(--accent-ink)">
        <Glyph x={x - 10} y={y - 10} width={20} height={20} />
      </g>
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

interface NetProps {
  /** This instance, the node in the middle. */
  self: { name: string; sub: string };
  nodes: NetNode[];
  /** The key under the pointer or the focus, here or on the page. */
  hot: string | null;
  /** Brings the node's entry on the page into view. */
  onPick: (key: string) => void;
  /** The picture's accessible name. */
  alt: string;
}

function NetSvg({ shape, self, nodes, hot, onPick, alt }: NetProps & { shape: 'wide' | 'narrow' }) {
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
        const r = RADIUS[node.kind];
        const length = Math.hypot(p.x - hub.x, p.y - hub.y);
        const ux = (p.x - hub.x) / length;
        const uy = (p.y - hub.y) / length;
        // The link ends a little outside the rim of both nodes.
        const from = xy(hub.x + ux * (HUB_RADIUS + 5), hub.y + uy * (HUB_RADIUS + 5));
        const to = xy(p.x - ux * (r + 5), p.y - uy * (r + 5));
        return (
          <g
            key={node.key}
            className={`kl-net-spoke${node.dim ? ' dim' : ''}${hot === node.key ? ' hot' : ''}`}
            data-nk={node.key}
          >
            <path className="kl-net-link" pathLength="1" d={`M${from} L${to}`} />
            <NodeButton name={node.name} x={p.x} y={p.y} r={r} onPick={() => onPick(node.key)}>
              <Shape kind={node.kind} x={p.x} y={p.y} />
            </NodeButton>
            <NodeLabel at={p.label} name={node.name} sub={node.sub} />
          </g>
        );
      })}
      <g className={`kl-net-spoke hub${hot === SELF_KEY ? ' hot' : ''}`} data-nk={SELF_KEY}>
        <NodeButton name={self.name} x={hub.x} y={hub.y} r={HUB_RADIUS} onPick={() => onPick(SELF_KEY)}>
          <Shape kind="instance" x={hub.x} y={hub.y} size={64} />
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
        show('.kl-net-link', 0, [{ strokeDashoffset: 1 }, { strokeDashoffset: 0 }]);
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
    <div ref={box} className="kl-net" data-testid="group-net">
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
