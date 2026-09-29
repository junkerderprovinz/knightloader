// The small pictures of the Pairing tab: one per step card, one per relay
// route and the glyphs of the route picker. They draw with the theme's own
// tokens, so they follow dark and light and take the hue of the card they sit
// in through --accent.
import { useId, type ReactNode } from 'react';
import type { RelayMode } from '../../lib/api';

const NODE = 'var(--carbon-surface2)';
const RELAY = 'var(--carbon-surface3)';
const INK = 'var(--carbon-text)';
const LABEL = 'var(--carbon-text-sub)';
const LINE = 'var(--carbon-text-muted)';
const KEY = 'var(--accent)';
const KEY_INK = 'var(--accent-contrast)';
const AREA = 'color-mix(in srgb, var(--carbon-text) 6%, transparent)';
const OK = 'var(--status-ok-solid)';

/** The key: it only ever sits on an instance. */
function Lock({ x, y }: { x: number; y: number }) {
  return (
    <g transform={`translate(${x} ${y})`}>
      <circle r="9" fill={KEY} />
      <rect x="-4" y="-1.2" width="8" height="6.4" rx="1.4" fill={KEY_INK} />
      <path d="M-2.4 -1.2v-1.9a2.4 2.4 0 0 1 4.8 0v1.9" fill="none" stroke={KEY_INK} strokeWidth="1.5" />
    </g>
  );
}

/** A sealed message on its way. */
function Envelope({ x, y }: { x: number; y: number }) {
  return (
    <g transform={`translate(${x} ${y})`}>
      <rect x="-8" y="-6" width="16" height="12" rx="2.2" fill={KEY} />
      <path d="M-6.2 -4 0 .9 6.2 -4" fill="none" stroke={KEY_INK} strokeWidth="1.5" strokeLinejoin="round" strokeLinecap="round" />
    </g>
  );
}

function Instance({ x, y, label, dim = false }: { x: number; y: number; label: string; dim?: boolean }) {
  return (
    <g opacity={dim ? 0.45 : undefined}>
      <rect x={x} y={y} width="56" height="44" rx="10" fill={NODE} />
      <text x={x + 28} y={y + 28} textAnchor="middle" fontSize="17" fontWeight="700" fill={INK}>
        {label}
      </text>
      <Lock x={x + 54} y={y + 2} />
    </g>
  );
}

function Line({ x1, x2, y, dashed = false }: { x1: number; x2: number; y: number; dashed?: boolean }) {
  return (
    <line
      x1={x1}
      y1={y}
      x2={x2}
      y2={y}
      stroke={LINE}
      strokeWidth="2"
      strokeLinecap="round"
      strokeDasharray={dashed ? '3 5' : undefined}
    />
  );
}

function Caption({ x, y, children, anchor = 'middle' }: { x: number; y: number; children: ReactNode; anchor?: 'middle' | 'start' }) {
  return (
    <text x={x} y={y} textAnchor={anchor} fontSize="12" fill={LABEL}>
      {children}
    </text>
  );
}

export interface RouteDiagramLabels {
  relay: string;
  projectHost: string;
  ownAddress: string;
  yourNetwork: string;
  otherNetwork: string;
  yourLan: string;
}

/** RouteDiagram shows how a message travels on one relay route. */
export function RouteDiagram({ mode, labels, alt }: { mode: RelayMode; labels: RouteDiagramLabels; alt: string }) {
  let body: ReactNode;
  if (mode === 'off') {
    body = (
      <>
        <rect x="2" y="8" width="170" height="96" rx="14" fill={AREA} />
        <Caption x={14} y={26} anchor="start">
          {labels.yourLan}
        </Caption>
        <Line x1={72} x2={106} y={58} />
        <Envelope x={89} y={58} />
        <Instance x={16} y={40} label="A" />
        <Instance x={106} y={40} label="B" />
        <Line x1={166} x2={196} y={62} dashed />
        <path d="M176 56l8 8M184 56l-8 8" stroke={LINE} strokeWidth="2" strokeLinecap="round" />
        <Instance x={200} y={40} label="C" dim />
        <Caption x={228} y={100}>
          {labels.otherNetwork}
        </Caption>
      </>
    );
  } else {
    const project = mode === 'project';
    body = (
      <>
        <Line x1={64} x2={project ? 98 : 104} y={58} />
        <Line x1={project ? 162 : 156} x2={196} y={58} />
        {project ? (
          <path d="M102 78h56a13 13 0 0 0 1-26A20 20 0 0 0 124 41 14 14 0 0 0 103 55 12 12 0 0 0 102 78z" fill={RELAY} />
        ) : (
          <rect x="104" y="37" width="52" height="42" rx="8" fill={RELAY} />
        )}
        <text x={project ? 131 : 130} y={project ? 68 : 62} textAnchor="middle" fontSize="11" fontWeight="600" fill={INK}>
          {labels.relay}
        </text>
        <Envelope x={project ? 81 : 84} y={58} />
        <Envelope x={project ? 179 : 176} y={58} />
        <Instance x={8} y={36} label="A" />
        <Instance x={196} y={36} label="B" />
        <Caption x={130} y={22}>
          {project ? labels.projectHost : labels.ownAddress}
        </Caption>
        <Caption x={36} y={100}>
          {labels.yourNetwork}
        </Caption>
        <Caption x={224} y={100}>
          {labels.otherNetwork}
        </Caption>
      </>
    );
  }
  return (
    <svg viewBox="0 0 272 108" role="img" aria-label={alt} className="mx-auto block h-auto w-full max-w-[400px] justify-self-center">
      <g transform="translate(4 0)">{body}</g>
    </svg>
  );
}

/** Legend entries for the two symbols the diagrams use. */
export function LegendKey() {
  return (
    <svg viewBox="-10 -10 20 20" width="20" height="20" aria-hidden="true">
      <Lock x={0} y={0} />
    </svg>
  );
}

export function LegendMessage() {
  return (
    <svg viewBox="-10 -10 20 20" width="20" height="20" aria-hidden="true">
      <Envelope x={0} y={0} />
    </svg>
  );
}

function Window({ x, y, w, h, label, lit }: { x: number; y: number; w: number; h: number; label: string; lit: boolean }) {
  return (
    <>
      <rect x={x} y={y} width={w} height={h} rx="10" fill={NODE} />
      <circle cx={x + 14} cy={y + 14} r="8" fill={lit ? KEY : RELAY} />
      <text x={x + 14} y={y + 17.5} textAnchor="middle" fontSize="10" fontWeight="700" fill={lit ? KEY_INK : INK}>
        {label}
      </text>
    </>
  );
}

/** Word pills in a grid: the first `filled` are typed, `cursor` is the one
 *  being typed. */
function Words({
  x0,
  y0,
  cols,
  rows,
  w,
  h,
  gx,
  gy,
  filled,
  cursor = -1,
}: {
  x0: number;
  y0: number;
  cols: number;
  rows: number;
  w: number;
  h: number;
  gx: number;
  gy: number;
  filled: number;
  cursor?: number;
}) {
  const out: ReactNode[] = [];
  for (let i = 0; i < cols * rows; i++) {
    const x = x0 + (i % cols) * (w + gx);
    const y = y0 + Math.floor(i / cols) * (h + gy);
    if (i === cursor) {
      out.push(
        <g key={i}>
          <rect x={x + 0.75} y={y + 0.75} width={w - 1.5} height={h - 1.5} rx={h / 2} fill="none" stroke={KEY} strokeWidth="1.5" />
          <path d={`M${x + 6} ${y + 2.5}v${h - 5}`} stroke={INK} strokeWidth="1.5" strokeLinecap="round" />
        </g>,
      );
    } else {
      out.push(<rect key={i} x={x} y={y} width={w} height={h} rx={h / 2} fill={i < filled ? KEY : RELAY} />);
    }
  }
  return <>{out}</>;
}

/** StepPicture is the drawing on step card 1, 2 or 3. */
export function StepPicture({ step }: { step: 1 | 2 | 3 }) {
  let body: ReactNode;
  if (step === 1) {
    body = (
      <>
        <Window x={40} y={6} w={120} h={108} label="A" lit />
        <Words x0={54} y0={34} cols={3} rows={4} w={28} h={10} gx={5} gy={8} filled={12} />
      </>
    );
  } else if (step === 2) {
    body = (
      <>
        <Window x={4} y={32} w={58} h={58} label="A" lit={false} />
        <Words x0={12} y0={56} cols={3} rows={4} w={12} h={4} gx={3} gy={3} filled={12} />
        <path d="M68 61h14" stroke={LINE} strokeWidth="2" strokeLinecap="round" />
        <path d="M80 56l6 5-6 5" fill="none" stroke={LINE} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        <Window x={92} y={6} w={104} h={108} label="B" lit />
        <Words x0={104} y0={34} cols={3} rows={4} w={24} h={10} gx={5} gy={8} filled={7} cursor={7} />
      </>
    );
  } else {
    body = (
      <>
        <rect x="4" y="6" width="192" height="108" rx="16" fill={AREA} />
        <Window x={16} y={34} w={64} h={62} label="A" lit />
        <Words x0={26} y0={62} cols={3} rows={3} w={12} h={5} gx={3} gy={4} filled={9} />
        <Window x={120} y={34} w={64} h={62} label="B" lit />
        <Words x0={130} y0={62} cols={3} rows={3} w={12} h={5} gx={3} gy={4} filled={9} />
        <path d="M80 65h40" stroke={LINE} strokeWidth="2" />
        <Lock x={100} y={65} />
        <g transform="translate(100 22)">
          <circle r="10" fill={OK} />
          <path
            d="M-4.4 .2-1.2 3.4 4.6-2.8"
            fill="none"
            stroke="var(--carbon-bg)"
            strokeWidth="2.2"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </g>
      </>
    );
  }
  return (
    <svg viewBox="0 0 200 120" aria-hidden="true" className="mx-auto block h-auto w-full max-w-[230px]">
      {body}
    </svg>
  );
}

const CLOUD = 'M5.4 16.5h9.2a3.9 3.9 0 0 0 .6-7.8A5.4 5.4 0 0 0 4.9 7.3a4.6 4.6 0 0 0 .5 9.2z';

/**
 * RouteGlyph draws a relay route, or one of the two places an own relay runs,
 * as a filled glyph like every other glyph in the app: a cloud for the project
 * relay, a house for an own relay, a crossed-out cloud for none, a container
 * and a server.
 */
export function RouteGlyph({ kind }: { kind: RelayMode | 'container' | 'server' }) {
  // useId has colons, which a url(#...) reference does not take in every browser.
  const cut = `route-cut${useId().replace(/[^\w-]/g, '')}`;
  const box = { viewBox: '0 0 20 20', width: 20, height: 20, fill: 'currentColor', 'aria-hidden': true as const };
  switch (kind) {
    case 'project':
      return (
        <svg {...box}>
          <path d={CLOUD} />
        </svg>
      );
    case 'own':
      return (
        <svg {...box}>
          <path d="M10 2.4 2.3 9.2c-.5.4-.2 1.2.5 1.2h1.6V17a1 1 0 0 0 1 1h3.1v-4.6h3V18h3.1a1 1 0 0 0 1-1v-6.6h1.6c.7 0 1-.8.5-1.2z" />
        </svg>
      );
    case 'off':
      // The slash is cut out of the cloud so it reads on any ground.
      return (
        <svg {...box}>
          <mask id={cut}>
            <rect width="20" height="20" fill="white" />
            <path d="M3 3l14 14" stroke="black" strokeWidth="3.6" strokeLinecap="round" />
          </mask>
          <path d={CLOUD} mask={`url(#${cut})`} />
          <path d="M3.2 3.2l13.6 13.6" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
        </svg>
      );
    case 'container':
      return (
        <svg {...box}>
          <path
            fillRule="evenodd"
            d="M3.5 5A1.5 1.5 0 0 0 2 6.5v7A1.5 1.5 0 0 0 3.5 15h13a1.5 1.5 0 0 0 1.5-1.5v-7A1.5 1.5 0 0 0 16.5 5h-13ZM5 7.5h1.5v5H5v-5Zm3 0h1.5v5H8v-5Zm3 0h1.5v5H11v-5Zm3 0h1.5v5H14v-5Z"
          />
        </svg>
      );
    case 'server':
      return (
        <svg {...box}>
          <path
            fillRule="evenodd"
            d="M4 3a1.5 1.5 0 0 0-1.5 1.5v3A1.5 1.5 0 0 0 4 9h12a1.5 1.5 0 0 0 1.5-1.5v-3A1.5 1.5 0 0 0 16 3H4Zm1.5 2.2h2v1.6h-2V5.2ZM4 11a1.5 1.5 0 0 0-1.5 1.5v3A1.5 1.5 0 0 0 4 17h12a1.5 1.5 0 0 0 1.5-1.5v-3A1.5 1.5 0 0 0 16 11H4Zm1.5 2.2h2v1.6h-2v-1.6Z"
          />
        </svg>
      );
  }
}
