// The pictures of the Pairing tab: the scene the three step cards play, the
// route picture of the relay card and the glyphs the two cards share. They
// draw with the theme's own tokens, so they follow dark and light and take the
// hue of the card they sit in through --accent. What moves is SMIL and the Web
// Animations API (GlimStone, "Illustrations"); a still picture shows the end
// frame.
import { useEffect, useId, type ReactNode, type RefObject, type SVGProps } from 'react';
import logoUrl from '../../../assets/logo.svg';
import type { RelayMode } from '../../../lib/api';
import { PARLEYPORT_SVG } from '../../../lib/appMarks';
import { IconCheck, IconClipboard } from '../../../lib/icons';
import { useStillPictures } from '../../../lib/motion';

const TILE = 'var(--carbon-surface2)';
const RAISED = 'var(--carbon-surface3)';
const INK = 'var(--carbon-text)';
const LABEL = 'var(--carbon-text-sub)';
const LINE = 'var(--carbon-text-muted)';
const KEY = 'var(--accent)';
// A detail inside a filled shape is cut out in the surface behind the figure,
// so it holds under every accent and every rainbow position.
const CUT = 'var(--carbon-surface)';
const OK = 'var(--status-ok-text)';
const OK_GROUND = 'var(--status-ok-bg)';
const FAIL = 'var(--status-fail-text)';

type Place = Pick<SVGProps<SVGSVGElement>, 'x' | 'y' | 'className'>;

function stroked(size: number, width: number, place: Place) {
  return {
    viewBox: '0 0 20 20',
    width: size,
    height: size,
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: width,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
    'aria-hidden': true as const,
    ...place,
  };
}

/** PhraseGlyph draws the two ways into a group and the arrows of the path to
 *  the button on the other instance. */
export function PhraseGlyph({ kind, size, ...place }: { kind: 'create' | 'enter' | 'arrow' | 'chevron'; size: number } & Place) {
  switch (kind) {
    case 'create':
      return (
        <svg {...stroked(size, 1.8, place)}>
          <rect x="3" y="3" width="14" height="14" rx="3.5" />
          <path d="M10 6.8v6.4M6.8 10h6.4" />
        </svg>
      );
    case 'enter':
      return (
        <svg {...stroked(size, 1.6, place)}>
          <rect x="2.5" y="5" width="15" height="10" rx="2.2" />
          <path d="M5.5 8.2h.01M8.5 8.2h.01M11.5 8.2h.01M14.5 8.2h.01M6.5 11.8h7" />
        </svg>
      );
    case 'arrow':
      return (
        <svg {...stroked(size, 2.4, place)}>
          <path d="M3.8 10h11.9M11.3 5.6 15.7 10l-4.4 4.4" />
        </svg>
      );
    case 'chevron':
      return (
        <svg {...stroked(size, 2.4, place)}>
          <path d="M7.5 4.2 13.3 10l-5.8 5.8" />
        </svg>
      );
  }
}

/** FactGlyph opens a fact line of the relay card. */
export function FactGlyph({ kind }: { kind: 'route' | 'need' | 'search' | 'shield' | 'lock' | 'sees' | 'hidden' }) {
  const box = {
    viewBox: '0 0 16 16',
    width: 16,
    height: 16,
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 1.5,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
    'aria-hidden': true as const,
  };
  switch (kind) {
    case 'route':
      return (
        <svg {...box} viewBox="0 0 20 20" strokeWidth={1.7}>
          <path d="M3.5 7h12M12.5 4l3 3-3 3M16.5 13h-12M7.5 10l-3 3 3 3" />
        </svg>
      );
    case 'need':
      return (
        <svg {...box}>
          <path d="M5 2.5h6M5 2.5a1 1 0 0 0-1 1V14h8V3.5a1 1 0 0 0-1-1" />
          <path d="M6.2 7.3l1.3 1.3 2.4-2.6M6.2 11h3.6" />
        </svg>
      );
    case 'search':
      return (
        <svg {...box}>
          <circle cx="7" cy="7" r="4.4" />
          <path d="M10.3 10.3 14 14" />
        </svg>
      );
    case 'shield':
      return (
        <svg {...box}>
          <path d="M8 1.6 13.2 3.7v4c0 3.1-2.2 5.4-5.2 6.7-3-1.3-5.2-3.6-5.2-6.7v-4z" />
          <path d="M5.8 8.1 7.4 9.7 10.4 6.4" />
        </svg>
      );
    case 'lock':
      return (
        <svg {...box}>
          <rect x="3" y="7" width="10" height="7" rx="1.6" fill="currentColor" stroke="none" />
          <path d="M5.2 7V5.1a2.8 2.8 0 0 1 5.6 0V7" />
        </svg>
      );
    case 'sees':
      return (
        <svg {...box}>
          <path d="M1.5 8S4 3.5 8 3.5 14.5 8 14.5 8 12 12.5 8 12.5 1.5 8 1.5 8z" />
          <circle cx="8" cy="8" r="2.1" />
        </svg>
      );
    case 'hidden':
      return (
        <svg {...box}>
          <path d="M1.5 8S4 3.5 8 3.5 14.5 8 14.5 8 12 12.5 8 12.5 1.5 8 1.5 8z" />
          <circle cx="8" cy="8" r="2.1" />
          <path d="M2.6 2.6l10.8 10.8" />
        </svg>
      );
  }
}

const CLOUD = 'M5.4 16.5h9.2a3.9 3.9 0 0 0 .6-7.8A5.4 5.4 0 0 0 4.9 7.3a4.6 4.6 0 0 0 .5 9.2z';

/**
 * RouteGlyph draws a relay route as a filled glyph for the route picker: a
 * cloud for the project relay, a house for an own relay and a crossed-out
 * cloud for none.
 */
export function RouteGlyph({ kind }: { kind: RelayMode }) {
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
  }
}

/** A sealed message. The small lock on it says the relay cannot read it. */
function Envelope() {
  return (
    <>
      <rect x="-9" y="-6.5" width="18" height="13" rx="2.5" fill={KEY} />
      <path d="M-6.8 -4.2 0 .8 6.8 -4.2" fill="none" stroke={CUT} strokeWidth="1.5" strokeLinejoin="round" strokeLinecap="round" />
      <g transform="translate(7.5 5)">
        <circle r="4.6" fill={RAISED} />
        <rect x="-2.1" y="-.7" width="4.2" height="3.4" rx=".8" fill={KEY} />
        <path d="M-1.3 -.7v-1a1.3 1.3 0 0 1 2.6 0v1" fill="none" stroke={KEY} strokeWidth="1" />
      </g>
    </>
  );
}

function LockBadge() {
  return (
    <>
      <circle r="9" fill={KEY} />
      <rect x="-4" y="-1.2" width="8" height="6.4" rx="1.4" fill={CUT} />
      <path d="M-2.4 -1.2v-1.9a2.4 2.4 0 0 1 4.8 0v1.9" fill="none" stroke={CUT} strokeWidth="1.5" />
    </>
  );
}

/** The app's own mark stands for an instance. */
function Instance({ x, y, faded = false }: { x: number; y: number; faded?: boolean }) {
  return <image href={logoUrl} x={x} y={y} width="56" height="56" className="kl-pic-lift" opacity={faded ? 0.45 : undefined} />;
}

const RELAY_MARK = /viewBox="([^"]+)"[^>]*>([\s\S]*)<\/svg>\s*$/.exec(PARLEYPORT_SVG)!;

const round = (t: number) => +t.toFixed(3);

/** SMIL takes key times and values as two parallel lists; the pairs keep each
 *  value next to its moment. */
function keyed(points: [number, number | string][]) {
  return { values: points.map((p) => p[1]).join(';'), keyTimes: points.map((p) => p[0]).join(';') };
}

// A translated label can be longer than its room in the drawing. The estimate
// counts a wide glyph as a whole em and anything else as a little over half.
function drawnWidth(text: string, size: number): number {
  let ems = 0;
  for (const ch of text) ems += /[ᄀ-ᇿ⺀-꓏가-힣豈-﫿＀-￯]/.test(ch) ? 1 : 0.56;
  return ems * size;
}

/** squeezed presses a label into its room where it would run past it. */
function squeezed(text: string, size: number, room: number) {
  return drawnWidth(text, size) > room ? { textLength: room, lengthAdjust: 'spacingAndGlyphs' as const } : {};
}

export type RouteKind = 'project' | 'own' | 'instance' | 'off';

export interface RouteDiagramLabels {
  yourNetwork: string;
  otherNetwork: string;
  /** What the instance that serves the relay wears. */
  relay: string;
  /** The relay's own name under its mark. */
  relayName: string;
  /** The line under an own relay. */
  selfHosted: string;
}

// Envelopes travel at one speed in all four pictures, in viewBox units per
// second; a short hop just takes less time.
const MESSAGE_SPEED = 70;

function Region({ x, w, label }: { x: number; w: number; label: string }) {
  return (
    <>
      <rect
        className="kl-pic-card"
        x={x}
        y="20"
        width={w}
        height="98"
        rx="18"
        fill={TILE}
        stroke={LINE}
        strokeOpacity=".35"
        strokeDasharray="3 5"
      />
      <text x={x + w / 2} y="138" textAnchor="middle" fontSize="12" fill={LABEL} {...squeezed(label, 12, w)}>
        {label}
      </text>
    </>
  );
}

function Flow({ d, calm }: { d: string; calm: boolean }) {
  return (
    <path d={d} fill="none" stroke={KEY} strokeOpacity=".45" strokeWidth="2" strokeLinecap="round" strokeDasharray="2 7">
      {!calm && <animate attributeName="stroke-dashoffset" from="0" to="-18" dur="1.4s" repeatCount="indefinite" />}
    </path>
  );
}

/** A message that stays in sight from one instance to the other. */
function Message({ path, length, begin, dur }: { path: string; length: number; begin: number; dur: number }) {
  const from = 0.04;
  const to = round(from + length / MESSAGE_SPEED / dur);
  const loop = { dur: `${dur}s`, begin: `${begin}s`, repeatCount: 'indefinite' };
  return (
    <g opacity="0">
      <Envelope />
      <animateMotion path={path} calcMode="linear" keyPoints="0;0;1;1" keyTimes={`0;${from};${to};1`} {...loop} />
      <animate attributeName="opacity" values="0;1;1;0;0" keyTimes={`0;${from};${to};${round(to + 0.03)};1`} {...loop} />
    </g>
  );
}

function Tag({ x, y, label }: { x: number; y: number; label: string }) {
  const w = Math.min(104, Math.max(44, Math.ceil(drawnWidth(label, 9.5)) + 18));
  return (
    <g transform={`translate(${x} ${y})`}>
      <rect className="kl-pic-pill" x={-w / 2} y="-9" width={w} height="18" rx="6" fill={RAISED} />
      <text y="3.5" textAnchor="middle" fontSize="9.5" fontWeight="600" fill={INK} {...squeezed(label, 9.5, w - 12)}>
        {label}
      </text>
    </g>
  );
}

/**
 * RouteDiagram shows how a message travels on one relay route: who runs the
 * relay and that only sealed messages pass it. Addresses stay out of the
 * picture.
 */
export function RouteDiagram({ kind, labels, alt }: { kind: RouteKind; labels: RouteDiagramLabels; alt: string }) {
  const calm = useStillPictures();
  const dur = kind === 'off' ? 2.6 : 6.4;
  const half = dur / 2;
  let body: ReactNode;
  if (kind === 'off') {
    // Without a relay the other network stays out of reach: its instance is
    // greyed out and a message to it stops at the cross.
    const there = 'M98 70 L158 70';
    const bounce = { dur: '3.2s', repeatCount: 'indefinite' };
    body = (
      <>
        <Region x={8} w={232} label={labels.yourNetwork} />
        <Region x={268} w={84} label={labels.otherNetwork} />
        <Flow d={there} calm={calm} />
        <path d="M218 70 H280" stroke={LINE} strokeWidth="2" strokeLinecap="round" strokeDasharray="3 5" />
        <path d="M249 64l10 12M259 64l-10 12" stroke={FAIL} strokeWidth="2.4" strokeLinecap="round" />
        <Instance x={40} y={42} />
        <Instance x={160} y={42} />
        <Instance x={282} y={42} faded />
        {calm ? (
          <>
            <g transform="translate(128 70)">
              <Envelope />
            </g>
            <g transform="translate(232 70)">
              <Envelope />
            </g>
          </>
        ) : (
          <>
            <Message path={there} length={60} begin={0} dur={dur} />
            <Message path="M158 70 L98 70" length={60} begin={half} dur={dur} />
            <g opacity="0">
              <Envelope />
              <animateMotion path="M222 70 L240 70" calcMode="linear" keyPoints="0;1;1" keyTimes="0;.08;1" {...bounce} />
              <animate attributeName="opacity" values="0;1;1;0;0" keyTimes="0;.02;.3;.42;1" {...bounce} />
            </g>
            <circle cx="254" cy="70" r="6" fill="none" stroke={FAIL} strokeWidth="2" opacity="0">
              <animate attributeName="r" values="6;6;16;16" keyTimes="0;.08;.28;1" {...bounce} />
              <animate attributeName="opacity" values="0;0;.7;0;0" keyTimes="0;.07;.09;.28;1" {...bounce} />
            </circle>
          </>
        )}
      </>
    );
  } else if (kind === 'instance') {
    // An instance that serves the relay itself stands on the left and passes
    // the messages on without a third box.
    const track = 'M90 70 Q180 46 270 70';
    const answer = { dur: `${half}s`, repeatCount: 'indefinite' };
    body = (
      <>
        <Region x={8} w={104} label={labels.yourNetwork} />
        <Region x={248} w={104} label={labels.otherNetwork} />
        <Flow d={track} calm={calm} />
        {!calm && (
          <circle cx="60" cy="70" r="34" fill="none" stroke={KEY} strokeWidth="2" opacity="0">
            <animate attributeName="r" values="34;34;50;50" keyTimes="0;.89;.99;1" {...answer} />
            <animate attributeName="opacity" values="0;0;.55;0;0" keyTimes="0;.88;.89;.99;1" {...answer} />
          </circle>
        )}
        <Instance x={32} y={42} />
        <Tag x={60} y={114} label={labels.relay} />
        <Instance x={272} y={42} />
        {calm ? (
          <g transform="translate(180 58)">
            <Envelope />
          </g>
        ) : (
          <>
            <Message path={track} length={183} begin={0} dur={dur} />
            <Message path="M270 70 Q180 46 90 70" length={183} begin={half} dur={dur} />
          </>
        )}
      </>
    );
  } else {
    // A message goes into the relay and comes out on the other side, and the
    // relay answers each arrival with a ring and a small bump. One direction
    // finishes before the other starts.
    const answer = { dur: `${half}s`, repeatCount: 'indefinite' };
    body = (
      <>
        <Region x={8} w={104} label={labels.yourNetwork} />
        <Region x={248} w={104} label={labels.otherNetwork} />
        <Flow d="M90 70 Q120 58 148 70 M212 70 Q240 82 270 70" calm={calm} />
        {!calm && (
          <circle cx="180" cy="70" r="30" fill="none" stroke={KEY} strokeWidth="2" opacity="0">
            <animate attributeName="r" values="30;30;50;50" keyTimes="0;.45;.62;1" {...answer} />
            <animate attributeName="opacity" values="0;0;.6;0;0" keyTimes="0;.44;.45;.62;1" {...answer} />
          </circle>
        )}
        <g transform="translate(180 69)">
          <g>
            {!calm && (
              <animateTransform attributeName="transform" type="scale" values="1;1;1.1;1;1" keyTimes="0;.45;.5;.58;1" {...answer} />
            )}
            <svg
              x="-32"
              y="-31"
              width="64"
              height="62"
              viewBox={RELAY_MARK[1]}
              className="kl-pic-lift"
              dangerouslySetInnerHTML={{ __html: RELAY_MARK[2] }}
            />
          </g>
        </g>
        <text x="180" y="138" textAnchor="middle" fontSize="12" fontWeight="600" fill={INK}>
          {labels.relayName}
        </text>
        {kind === 'own' && <Tag x={180} y={155} label={labels.selfHosted} />}
        <Instance x={32} y={42} />
        <Instance x={272} y={42} />
        {calm ? (
          <>
            <g transform="translate(120 64)">
              <Envelope />
            </g>
            <g transform="translate(240 76)">
              <Envelope />
            </g>
          </>
        ) : (
          <>
            <Through into="M90 70 Q120 58 150 70 L180 70" out="M180 70 L210 70 Q240 82 270 70" begin={0} dur={dur} />
            <Through into="M270 70 Q240 82 210 70 L180 70" out="M180 70 L150 70 Q120 58 90 70" begin={half} dur={dur} />
          </>
        )}
      </>
    );
  }
  return (
    <svg
      // A fresh element per picture, so its clock starts with it.
      key={`${kind}-${calm}`}
      viewBox="0 0 360 166"
      role="img"
      aria-label={alt}
      className="kl-pic mx-auto block h-auto w-full max-w-[420px]"
    >
      {body}
    </svg>
  );
}

function Leg({
  path,
  begin,
  dur,
  times,
  points,
  shown,
  size,
}: {
  path: string;
  begin: number;
  dur: number;
  times: string;
  points: string;
  shown: string;
  size: string;
}) {
  const loop = { dur: `${dur}s`, begin: `${begin}s`, repeatCount: 'indefinite' };
  return (
    <g opacity="0">
      <g>
        <Envelope />
        <animateTransform attributeName="transform" type="scale" values={size} keyTimes={times} {...loop} />
      </g>
      <animateMotion path={path} calcMode="linear" keyPoints={points} keyTimes={times} {...loop} />
      <animate attributeName="opacity" values={shown} keyTimes={times} {...loop} />
    </g>
  );
}

/** A message into the relay and the one that leaves it on the other side.
 *  Each leg is 91 units long, so at the shared speed it takes a fifth of the
 *  loop. */
function Through({ into, out, begin, dur }: { into: string; out: string; begin: number; dur: number }) {
  return (
    <>
      <Leg path={into} begin={begin} dur={dur} times="0;.02;.2;.225;1" points="0;0;.878;1;1" shown="0;1;1;0;0" size="1;1;1;.3;.3" />
      <Leg
        path={out}
        begin={begin}
        dur={dur}
        times="0;.26;.285;.465;.5;1"
        points="0;0;.122;1;1;1"
        shown="0;0;1;1;0;0"
        size=".3;.3;1;1;1;1"
      />
    </>
  );
}

// The three step cards play one scene: the pointer generates and copies the
// phrase on the first instance, pastes it on the next, and the pair celebrates.
const STEP_LOOP_S = 12;
const STEP_LOOP = `${STEP_LOOP_S}s`;

// Words from the real list, so the scene shows what a phrase looks like.
const SAMPLE = ['orbit', 'wagon', 'lemon', 'crisp', 'absent', 'tunnel', 'galaxy', 'harbor', 'pencil', 'ribbon', 'velvet', 'yellow'];

const CONFETTI = [KEY, OK, '#78a9ff', '#ff7eb6'];

export interface StepLabels {
  create: string;
  enter: string;
  copy: string;
  copied: string;
  paste: string;
  words: string;
  paired: string;
}

/** StepPicture is the drawing on step card 1, 2 or 3. */
export function StepPicture({ step, labels }: { step: 1 | 2 | 3; labels: StepLabels }) {
  const calm = useStillPictures();
  const loop = { dur: STEP_LOOP, repeatCount: 'indefinite' };
  const anim = (attribute: string, points: [number, number | string][]) =>
    calm ? null : <animate attributeName={attribute} {...keyed(points)} {...loop} />;
  const appear = (t: number, until = 1) =>
    anim(
      'opacity',
      until < 1
        ? [[0, 0], [t, 0], [round(t + 0.012), 1], [until, 1], [round(until + 0.012), 0], [1, 0]]
        : [[0, 0], [t, 0], [round(t + 0.012), 1], [1, 1]],
    );
  const hidden = calm ? {} : { opacity: 0 };

  // The pointer's click darkens the control for a moment, before its result shows.
  const press = (x: number, y: number, w: number, h: number, rx: number, t: number, corner: string) =>
    calm ? null : (
      <rect className={corner} x={x} y={y} width={w} height={h} rx={rx} fill="#000" opacity="0">
        {anim('opacity', [[0, 0], [t, 0], [round(t + 0.008), 0.28], [round(t + 0.03), 0], [1, 0]])}
      </rect>
    );

  // The same choice the Phrase card shows, drawn small.
  const choice = (kind: 'create' | 'enter', title: string) => (
    <>
      <rect className="kl-pic-control" x="8" y="8" width="224" height="38" rx="9" fill={RAISED} />
      <rect className="kl-pic-control" x="14" y="14" width="26" height="26" rx="7" fill={KEY} />
      <g color="var(--accent-contrast)">
        <PhraseGlyph kind={kind} size={20} x={17} y={17} />
      </g>
      <text x="48" y="30.5" fontSize="11" fontWeight="600" fill={INK} {...squeezed(title, 11, 176)}>
        {title}
      </text>
    </>
  );

  const button = (label: string, t: number, done?: string) => (
    <>
      <rect className="kl-pic-pill" x="152" y="104" width="80" height="20" rx="10" fill={RAISED} />
      <g>
        {done &&
          anim('opacity', [[0, 1], [round(t + 0.006), 1], [round(t + 0.008), 0], [round(t + 0.15), 0], [round(t + 0.152), 1], [1, 1]])}
        <g color={LABEL}>
          <IconClipboard x={160} y={109} width={10} height={10} />
        </g>
        <text x="174" y="117.5" fontSize="8.5" fontWeight="600" fill={INK} {...squeezed(label, 8.5, 52)}>
          {label}
        </text>
      </g>
      {done && !calm && (
        <g opacity="0">
          {appear(round(t + 0.008), round(t + 0.15))}
          <g color={OK}>
            <IconCheck x={160} y={109} width={10} height={10} />
          </g>
          <text x="174" y="117.5" fontSize="8.5" fontWeight="600" fill={OK} {...squeezed(done, 8.5, 52)}>
            {done}
          </text>
        </g>
      )}
      {press(152, 104, 80, 20, 10, t, 'kl-pic-pill')}
    </>
  );

  const words = (x0: number, y0: number, w: number, gap: number, from: number, each: number, fill: string) =>
    SAMPLE.map((word, k) => {
      const x = x0 + (k % 4) * (w + gap);
      const y = y0 + Math.floor(k / 4) * 15;
      return (
        <g key={word} {...hidden}>
          {appear(round(from + k * each))}
          <rect className="kl-pic-pill" x={x} y={y} width={w} height="12" rx="6" fill={fill} />
          <text x={x + w / 2} y={y + 8.5} textAnchor="middle" fontSize="6.8" fontWeight="600" fill={INK}>
            {word}
          </text>
        </g>
      );
    });

  let body: ReactNode;
  if (step === 1) {
    body = (
      <>
        {choice('create', labels.create)}
        {press(8, 8, 224, 38, 9, 0.09, 'kl-pic-control')}
        {words(8, 56, 50, 8.67, 0.11, 0.006, RAISED)}
        {button(labels.copy, 0.21, labels.copied)}
      </>
    );
  } else if (step === 2) {
    body = (
      <>
        {choice('enter', labels.enter)}
        {press(8, 8, 224, 38, 9, 0.43, 'kl-pic-control')}
        <rect
          className="kl-pic-control"
          x="8"
          y="52"
          width="224"
          height="48"
          rx="9"
          fill={RAISED}
          stroke={LINE}
          strokeOpacity=".5"
          strokeDasharray="3 4"
        />
        {!calm && (
          <>
            <rect className="kl-pic-control" x="8" y="52" width="224" height="48" rx="9" fill="none" stroke={KEY} strokeWidth="1.5" opacity="0">
              {appear(0.44)}
            </rect>
            <text x="120" y="80" textAnchor="middle" fontSize="8" fill={LINE} {...squeezed(labels.words, 8, 200)}>
              {labels.words}
              {anim('opacity', [[0, 1], [0.52, 1], [0.53, 0], [1, 0]])}
            </text>
          </>
        )}
        {words(14, 56, 48, 6, 0.525, 0.005, TILE)}
        {button(labels.paste, 0.51)}
      </>
    );
  } else {
    body = (
      <>
        <path d="M88 80 H152" stroke={LINE} strokeWidth="2.4" strokeLinecap="round" strokeDasharray="3 5" />
        <path d="M88 80 H152" stroke={KEY} strokeWidth="2.4" strokeLinecap="round" {...hidden}>
          {appear(0.7)}
        </path>
        <Instance x={28} y={52} />
        <Instance x={156} y={52} />
        <g transform="translate(120 80)">
          <g {...hidden}>
            <LockBadge />
            {appear(0.72)}
          </g>
        </g>
        {!calm && (
          <>
            <g opacity="0">
              <Envelope />
              <animateMotion path="M92 80 H148" calcMode="linear" keyPoints="0;0;1;1" keyTimes="0;.72;.78;1" {...loop} />
              {anim('opacity', [[0, 0], [0.72, 0], [0.725, 1], [0.78, 1], [0.785, 0], [1, 0]])}
            </g>
            <circle cx="120" cy="22" r="14" fill="none" stroke={OK} strokeWidth="2" opacity="0">
              {anim('r', [[0, 14], [0.79, 14], [0.86, 52], [1, 52]])}
              {anim('opacity', [[0, 0], [0.789, 0], [0.79, 0.7], [0.86, 0], [1, 0]])}
            </circle>
          </>
        )}
        <g transform="translate(120 22)">
          <g {...hidden}>
            {!calm && (
              <animateTransform attributeName="transform" type="scale" values=".4;.4;1.18;1;1" keyTimes="0;.79;.81;.83;1" {...loop} />
            )}
            {appear(0.79)}
            <rect className="kl-pic-pill" x="-50" y="-13" width="100" height="26" rx="13" fill={OK_GROUND} />
            <g color={OK}>
              <IconCheck x={-38} y={-6} width={12} height={12} />
            </g>
            <text x="7" y="4.5" textAnchor="middle" fontSize="12" fontWeight="700" fill={OK} {...squeezed(labels.paired, 12, 62)}>
              {labels.paired}
            </text>
          </g>
        </g>
        {/* A short burst of confetti from the badge that rises, spins and falls
            over the card. The still picture leaves it out. */}
        {!calm &&
          Array.from({ length: 28 }, (_, k) => {
            const dx = ((k * 37) % 220) - 110;
            const dy = 30 + ((k * 53) % 75);
            const spin = k % 2 ? 360 : -360;
            const peak = `${round(120 + dx / 2)} ${-4 - (k % 4) * 6}`;
            const rest = `${120 + dx} ${22 + dy}`;
            return (
              <g key={k} opacity="0">
                <animateTransform
                  attributeName="transform"
                  type="translate"
                  values={`120 22;120 22;${peak};${rest};${rest}`}
                  keyTimes="0;.79;.83;.93;1"
                  {...loop}
                />
                {anim('opacity', [[0, 0], [0.789, 0], [0.79, 1], [0.89, 1], [0.93, 0], [1, 0]])}
                <rect x="-2" y="-3.5" width="4" height="7" rx="1" fill={CONFETTI[k % 4]}>
                  <animateTransform attributeName="transform" type="rotate" values={`0;0;${spin};${spin}`} keyTimes="0;.79;.93;1" {...loop} />
                </rect>
              </g>
            );
          })}
      </>
    );
  }
  return (
    <svg
      key={String(calm)}
      viewBox="0 0 240 130"
      overflow="visible"
      aria-hidden="true"
      data-step={step}
      className="kl-pic mx-auto block h-auto w-full max-w-[230px]"
    >
      {body}
    </svg>
  );
}

// The spots the pointer visits, as [card, x, y] in that card's own picture.
const SCENE_STOPS = {
  generate: [0, 120, 26],
  copy: [0, 192, 114],
  enter: [1, 120, 26],
  paste: [1, 192, 114],
  hold: [2, 120, 80],
  done: [2, 168, 30],
} as const;

type Spot = [number, number];

/**
 * useStepScene walks one pointer over all three step pictures. It lives above
 * the grid, so its path is measured from the drawn pictures, and it restarts
 * with them when the page is redrawn or the window changes size. `redraw` is
 * whatever changes what the pictures show.
 */
export function useStepScene(
  grid: RefObject<HTMLElement | null>,
  pointer: RefObject<HTMLElement | null>,
  redraw: unknown,
): boolean {
  const calm = useStillPictures();
  useEffect(() => {
    const box = grid.current;
    const hand = pointer.current;
    // The test environment draws the pictures without a Web Animations API.
    if (calm || !box || !hand || typeof hand.animate !== 'function') return;
    const pictures = [...box.querySelectorAll<SVGSVGElement>('svg[data-step]')];
    let gone = false;
    let timer: number | undefined;

    const play = () => {
      if (gone) return;
      // The page slides in when it opens. Measured before it settles, the
      // pointer would aim beside the buttons.
      const settling = document.getAnimations().filter((a) => {
        const effect = a.effect;
        return (
          a.playState === 'running' &&
          effect instanceof KeyframeEffect &&
          !!effect.target?.contains(box) &&
          effect.getComputedTiming().iterations !== Infinity
        );
      });
      if (settling.length > 0) {
        pictures.forEach((svg) => svg.pauseAnimations());
        void Promise.allSettled(settling.map((a) => a.finished)).then(play);
        return;
      }
      const frame = box.getBoundingClientRect();
      const at = ([card, x, y]: readonly [number, number, number]): Spot => {
        const svg = pictures[card];
        const p = svg.createSVGPoint();
        p.x = x;
        p.y = y;
        const q = p.matrixTransform(svg.getScreenCTM()!);
        return [q.x - frame.left, q.y - frame.top];
      };
      const generate = at(SCENE_STOPS.generate);
      const copy = at(SCENE_STOPS.copy);
      const enter = at(SCENE_STOPS.enter);
      const paste = at(SCENE_STOPS.paste);
      const hold = at(SCENE_STOPS.hold);
      const done = at(SCENE_STOPS.done);
      const start: Spot = [generate[0] - 60, generate[1] + 40];
      const key = (offset: number, [x, y]: Spot, scale = 1, opacity = 1) => ({
        offset,
        transform: `translate(${x}px, ${y}px) scale(${scale})`,
        opacity,
        easing: 'ease-in-out',
      });
      const frames = [
        key(0, start, 1, 0),
        key(0.02, start),
        key(0.08, generate),
        key(0.09, generate, 0.8),
        key(0.105, generate),
        key(0.2, copy),
        key(0.21, copy, 0.8),
        key(0.225, copy),
        key(0.31, copy),
        key(0.42, enter),
        key(0.43, enter, 0.8),
        key(0.445, enter),
        key(0.5, paste),
        key(0.51, paste, 0.8),
        key(0.525, paste),
        key(0.62, paste),
        key(0.74, hold),
        key(0.8, done),
        key(0.95, done),
        key(1, done, 1, 0),
      ];
      pictures.forEach((svg) => {
        svg.setCurrentTime(0);
        svg.unpauseAnimations();
      });
      hand.getAnimations().forEach((a) => a.cancel());
      hand.animate(frames, { duration: STEP_LOOP_S * 1000, iterations: Infinity });
    };

    play();
    const moved = () => {
      window.clearTimeout(timer);
      timer = window.setTimeout(play, 200);
    };
    window.addEventListener('resize', moved);
    // A change of reading direction swaps the cards without resizing anything.
    const direction = new MutationObserver(moved);
    direction.observe(document.documentElement, { attributes: true, attributeFilter: ['dir'] });
    return () => {
      gone = true;
      window.clearTimeout(timer);
      window.removeEventListener('resize', moved);
      direction.disconnect();
      hand.getAnimations().forEach((a) => a.cancel());
    };
  }, [calm, grid, pointer, redraw]);
  return calm;
}

/** The pointer of the step scene. */
export function ScenePointer() {
  return (
    <svg viewBox="0 0 14 19" width="14" height="19" aria-hidden="true" className="block">
      <path d="M0 0v16l4.2-4 3 7 2.8-1.2-3-6.8H13z" fill="#fff" stroke="#161616" strokeWidth="1.2" strokeLinejoin="round" />
    </svg>
  );
}
