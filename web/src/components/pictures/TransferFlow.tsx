import { useEffect, useLayoutEffect, useRef, type ComponentType, type ReactNode, type RefObject, type SVGProps } from 'react';
import type { Settings } from '../../lib/api';
import { fmtRate, fmtTotal } from '../../lib/format';
import { useT } from '../../lib/i18n';
import { IconAccounts, IconForward, IconGlobe, IconMore, IconNetwork } from '../../lib/icons';
import { density, type Flow, type FlowFolder, type FlowRoute, type FlowSource } from '../../lib/transferFlow';
import { floorMarks, floorTone } from '../DiskSpaceTile';
import { HosterIcon } from '../HosterIcon';
import { FILL, GROUND, HUE, QUIET, SURFACE, TRAVEL, useStage } from './scene';
import { splash, Vessel } from './Vessel';

type Glyph = ComponentType<SVGProps<SVGSVGElement>>;

const ROUTE_GLYPH: Record<FlowRoute['kind'], Glyph> = {
  direct: IconForward,
  proxy: IconNetwork,
  account: IconAccounts,
};

type Point = [x: number, y: number];

/** A line through figures, bent so it leaves and reaches each of them level. */
interface Run {
  d: string;
  length: number;
  /** The point `share` of the way along, 0 to 1. */
  at: (share: number) => Point;
}

const CHORDS = 24;

function run(...through: Point[]): Run {
  let d = `M${through[0][0]} ${through[0][1]}`;
  // The line as chords, for its length and the places along it, since packets keep one speed.
  const marks: Point[] = [through[0]];
  for (let leg = 1; leg < through.length; leg++) {
    const [x1, y1] = through[leg - 1];
    const [x2, y2] = through[leg];
    const mid = (x1 + x2) / 2;
    d += `C${mid} ${y1} ${mid} ${y2} ${x2} ${y2}`;
    for (let i = 1; i <= CHORDS; i++) {
      const u = i / CHORDS;
      const v = 1 - u;
      marks.push([
        v ** 3 * x1 + 3 * v * u * mid + u ** 3 * x2,
        v ** 3 * y1 + 3 * v * v * u * y1 + 3 * v * u * u * y2 + u ** 3 * y2,
      ]);
    }
  }
  const walked = [0];
  for (let i = 1; i < marks.length; i++) {
    walked.push(walked[i - 1] + Math.hypot(marks[i][0] - marks[i - 1][0], marks[i][1] - marks[i - 1][1]));
  }
  const length = walked[walked.length - 1];
  return {
    d,
    length,
    at: (share) => {
      const want = Math.max(0, Math.min(1, share)) * length;
      const i = Math.max(1, walked.findIndex((w) => w >= want));
      const part = (want - walked[i - 1]) / (walked[i] - walked[i - 1] || 1);
      return [
        marks[i - 1][0] + (marks[i][0] - marks[i - 1][0]) * part,
        marks[i - 1][1] + (marks[i][1] - marks[i - 1][1]) * part,
      ];
    },
  };
}

// Viewbox units between two packets, by the step `density` puts a speed on.
const GAP = [0, 112, 70, 44, 28, 18];

const fixed = (n: number) => n.toFixed(3);

/**
 * packetTimes is when the packets on a line leave, in seconds: one every
 * `every`, each taking `crossing` to arrive, `count` of them on a loop of
 * `loop`. Null for a line nothing moves on.
 */
function packetTimes(line: Run, speed: number) {
  const step = density(speed);
  if (step === 0) return null;
  const every = GAP[step] / TRAVEL;
  const crossing = line.length / TRAVEL;
  const count = Math.max(1, Math.ceil(crossing / every));
  return { every, crossing, count, loop: count * every };
}

/**
 * Packets are what travels on a line that carries data: one leaves every
 * `GAP` units, and all of them keep the shared speed, so a faster line is a
 * denser one. The still frame stands them where a moment of that would.
 */
function Packets({ line, speed, still, size }: { line: Run; speed: number; still: boolean; size: number }) {
  const time = packetTimes(line, speed);
  if (!time) return null;
  const { every, crossing, count, loop } = time;
  const share = Math.min(1, crossing / loop);

  if (still) {
    return (
      <g data-packets={count}>
        {Array.from({ length: count }, (_, i) => {
          const [x, y] = line.at((i + 0.5) / count);
          return <circle key={i} cx={x} cy={y} r={size} fill={FILL} />;
        })}
      </g>
    );
  }
  const timing = { dur: `${fixed(loop)}s`, repeatCount: 'indefinite' as const };
  return (
    // Keyed by the count, so a line that changes step starts its packets over rather than retiming them mid-flight.
    <g key={count} data-packets={count}>
      {Array.from({ length: count }, (_, i) => (
        <g key={i} opacity="0">
          <animateMotion
            path={line.d}
            begin={`${fixed(i * every)}s`}
            calcMode="linear"
            keyPoints="0;1;1"
            keyTimes={`0;${fixed(share)};1`}
            {...timing}
          />
          <animate
            attributeName="opacity"
            begin={`${fixed(i * every)}s`}
            calcMode="discrete"
            values="1;0"
            keyTimes={`0;${fixed(share)}`}
            {...timing}
          />
          <circle r={size} fill={FILL} />
        </g>
      ))}
    </g>
  );
}

/**
 * Line is one line of the picture: drawn through while data moves on it,
 * dashed and at rest while it waits. `fine` draws it for the strip.
 */
function Line({
  line,
  speed,
  still,
  to,
  fine = false,
}: {
  line: Run;
  speed: number;
  still: boolean;
  /** The figure the line ends at. */
  to: string;
  fine?: boolean;
}) {
  const moving = speed > 0;
  return (
    <g data-line={moving ? 'moving' : 'still'} data-to={to}>
      <path
        d={line.d}
        fill="none"
        stroke={moving ? HUE : QUIET}
        strokeOpacity={moving ? 0.75 : 0.6}
        strokeWidth={fine ? 1.5 : moving ? 2.5 : 2}
        strokeLinecap="round"
        strokeDasharray={moving ? undefined : fine ? '1.5 5' : '2 7'}
      />
      <Packets line={line} speed={speed} still={still} size={fine ? 2.4 : 4} />
    </g>
  );
}

/** Where the three columns stand and how wide the words under a figure may run. */
interface Arrangement {
  width: number;
  source: number;
  route: number;
  folder: number;
  caption: number;
  className: string;
}

const WIDE: Arrangement = {
  width: 616,
  source: 70,
  route: 308,
  folder: 546,
  caption: 136,
  className: 'hidden max-w-[44rem] @[36rem]:block',
};

const NARROW: Arrangement = {
  width: 328,
  source: 46,
  route: 164,
  folder: 282,
  caption: 90,
  className: 'block max-w-[24rem] @[36rem]:hidden',
};

// How much room one figure takes with its words, and half the height of the figure itself.
const SOURCE = { pitch: 64, half: 12 };
const ROUTE = { pitch: 76, half: 17 };
const FOLDER = { pitch: 96, half: 26 };

function Caption({ x, y, width, children }: { x: number; y: number; width: number; children: ReactNode }) {
  return (
    <foreignObject x={x - width / 2} y={y} width={width} height="32">
      <div className="flex h-full flex-col items-center text-center text-meta leading-[15px]">{children}</div>
    </foreignObject>
  );
}

/**
 * TransferFlow draws the way the running downloads take: where each loads
 * from on the left, what carries it in the middle and the folder it lands in
 * as a vessel on the right. With nothing loading it rests and says so.
 */
export function TransferFlow({ flow, title, cfg }: { flow: Flow; title: string; cfg: Settings | null }) {
  const { t } = useT();
  const label = `${title}: ${flowWords(flow, t)}`;
  return (
    <div data-new="transfer-flow" className="@container flex flex-col gap-2">
      <Stage arrangement={WIDE} flow={flow} label={label} cfg={cfg} />
      <Stage arrangement={NARROW} flow={flow} label={label} cfg={cfg} />
      {flow.sources.length === 0 && <p className="text-center text-sm text-carbon-textMuted">{t('picture.flow.idle')}</p>}
    </div>
  );
}

/** flowWords says what the picture shows, one way after the other. */
export function flowWords(flow: Flow, t: ReturnType<typeof useT>['t']): string {
  if (flow.paths.length === 0) return t('picture.flow.idle');
  return flow.paths
    .map((p) => {
      const speed = p.speed > 0 ? fmtRate(p.speed) : t('picture.flow.still');
      return p.route
        ? t('picture.flow.path', { source: p.source, route: p.route, folder: p.folder, speed })
        : t('picture.flow.pathPlain', { source: p.source, folder: p.folder, speed });
    })
    .join('; ');
}

/** A line into a folder: its first packet lands `crossing` seconds in, and one more every `every`. */
interface Landing {
  folder: string;
  crossing: number;
  every: number;
}

/** landed says whether a packet lands after `from` and by `to`, both read off the picture's clock. */
function landed({ crossing, every }: Landing, from: number, to: number): boolean {
  const landings = (t: number) => Math.floor((t - crossing) / every);
  return to >= crossing && landings(to) > landings(from);
}

// Seconds a vessel's splash settles before a packet can start the next one,
// so a dense line keeps the liquid lively without shaking it.
const SETTLE = 0.8;

/**
 * useSplashes makes a vessel splash as each packet lands in it. The packets
 * run on the picture's SMIL clock, which useStage holds until the picture is
 * seen, so landings are counted on that clock from frame to frame rather than
 * on timers that would run ahead of it.
 */
function useSplashes(stage: RefObject<SVGSVGElement | null>, landings: Landing[], still: boolean) {
  const current = useRef(landings);
  useLayoutEffect(() => {
    current.current = landings;
  });
  const moving = landings.length > 0;
  useEffect(() => {
    const svg = stage.current;
    if (still || !moving || !svg) return;
    const splashed = new Map<string, number>();
    let last = svg.getCurrentTime();
    let frame = requestAnimationFrame(function tick() {
      const now = svg.getCurrentTime();
      if (now < last) splashed.clear();
      for (const l of current.current) {
        if (!landed(l, last, now) || now - (splashed.get(l.folder) ?? -Infinity) < SETTLE) continue;
        const vessel = [...svg.querySelectorAll('[data-folder]')]
          .find((f) => f.getAttribute('data-folder') === l.folder)
          ?.querySelector('svg');
        if (!vessel) continue;
        splashed.set(l.folder, now);
        splash(vessel);
      }
      last = now;
      frame = requestAnimationFrame(tick);
    });
    return () => cancelAnimationFrame(frame);
  }, [stage, still, moving]);
}

function Stage({
  arrangement: a,
  flow,
  label,
  cfg,
}: {
  arrangement: Arrangement;
  flow: Flow;
  label: string;
  cfg: Settings | null;
}) {
  const { stage, still } = useStage();
  const idle = flow.sources.length === 0;
  const straight = (l: { from: string; to: string }) => l.from.startsWith('s:') && l.to.startsWith('f:');
  const unnamed = flow.links.some(straight);
  const height = Math.max(
    flow.sources.length * SOURCE.pitch,
    (flow.routes.length + (unnamed ? 1 : 0)) * ROUTE.pitch,
    flow.folders.length * FOLDER.pitch,
    FOLDER.pitch,
  );
  // Every column stands centred on the picture's height.
  const centre = (count: number, of: { pitch: number; half: number }) => (i: number) =>
    (height - count * of.pitch) / 2 + i * of.pitch + of.half;
  const sourceY = centre(flow.sources.length, SOURCE);
  // A way nothing names what carries has a place of its own in the middle,
  // below the figures, so its line passes none of them.
  const routeY = centre(flow.routes.length + (unnamed ? 1 : 0), ROUTE);
  const pass: Point = [a.route, routeY(flow.routes.length)];
  const folderY = centre(flow.folders.length, FOLDER);

  // Where a line leaves a figure on its way right, and where it reaches one.
  const leaves = new Map<string, Point>();
  const reaches = new Map<string, Point>();
  flow.sources.forEach((s, i) => leaves.set(s.id, [a.source + 20, sourceY(i)]));
  flow.routes.forEach((r, i) => {
    reaches.set(r.id, [a.route - 22, routeY(i)]);
    leaves.set(r.id, [a.route + 22, routeY(i)]);
  });
  flow.folders.forEach((f, i) => reaches.set(f.id, [a.folder - 30, folderY(i)]));

  const drawn = flow.links.flatMap((l) => {
    const from = leaves.get(l.from);
    const to = reaches.get(l.to);
    if (!from || !to) return [];
    return [{ link: l, line: straight(l) ? run(from, pass, to) : run(from, to) }];
  });
  const landings = drawn.flatMap(({ link, line }) => {
    const time = link.to.startsWith('f:') ? packetTimes(line, link.speed) : null;
    return time ? [{ folder: link.to, crossing: time.crossing, every: time.every }] : [];
  });
  useSplashes(stage, landings, still);

  return (
    <svg
      ref={stage}
      viewBox={`0 0 ${a.width} ${height}`}
      role="img"
      aria-label={label}
      data-state={idle ? 'idle' : 'loading'}
      className={`mx-auto w-full overflow-visible ${a.className}`}
    >
      {idle && <Rest a={a} y={height / 2} folder={flow.folders.length > 0} />}

      {drawn.map(({ link: l, line }) => (
        <Line key={`${l.from}\n${l.to}`} line={line} speed={l.speed} still={still} to={l.to} />
      ))}

      {flow.sources.map((s, i) => (
        <SourceFigure key={s.id} source={s} x={a.source} y={sourceY(i)} caption={a.caption} />
      ))}
      {flow.routes.map((r, i) => (
        <RouteFigure key={r.id} route={r} x={a.route} y={routeY(i)} caption={a.caption} />
      ))}
      {flow.folders.map((f, i) => (
        <FolderFigure key={f.id} folder={f} x={a.folder} y={folderY(i)} caption={a.caption} cfg={cfg} />
      ))}
    </svg>
  );
}

/**
 * Rest is the picture with nothing loading: an empty place where a source
 * would stand and the line at rest. The folder is its vessel where the disk
 * report names it, and an empty place too until then.
 */
function Rest({ a, y, folder }: { a: Arrangement; y: number; folder: boolean }) {
  const ghost = { fill: 'none', stroke: QUIET, strokeOpacity: 0.6, strokeDasharray: '3 4' };
  return (
    <g>
      <circle cx={a.source} cy={y} r="12" {...ghost} />
      <path
        d={`M${a.source + 20} ${y}H${a.folder - 30}`}
        stroke={QUIET}
        strokeOpacity=".6"
        strokeWidth="2"
        strokeLinecap="round"
        strokeDasharray="2 7"
      />
      {!folder && <rect x={a.folder - 14} y={y - 18} width="28" height="36" rx="7" {...ghost} />}
    </g>
  );
}

function SourceFigure({ source, x, y, caption }: { source: FlowSource; x: number; y: number; caption: number }) {
  const { t } = useT();
  const Mark = source.id === 's:+' ? IconMore : IconGlobe;
  return (
    <g data-source={source.id}>
      {source.host ? (
        <foreignObject x={x - 12} y={y - 12} width="24" height="24">
          {/* A box of its own, or the icon sits on a text line taller than the figure. */}
          <div className="flex">
            <HosterIcon host={source.host} size={24} />
          </div>
        </foreignObject>
      ) : (
        <>
          <circle cx={x} cy={y} r="12" fill={GROUND} />
          <g color={QUIET}>
            <Mark x={x - 7} y={y - 7} width={14} height={14} />
          </g>
        </>
      )}
      <Caption x={x} y={y + SOURCE.half + 5} width={caption}>
        {/* dir="auto": a host name stays left to right in every language. */}
        <span dir="auto" className="max-w-full truncate text-carbon-text">
          {source.name}
        </span>
        <span className="glim-num text-carbon-textMuted">
          {source.speed > 0 ? fmtRate(source.speed) : t('picture.flow.still')}
        </span>
      </Caption>
    </g>
  );
}

function RouteFigure({ route, x, y, caption }: { route: FlowRoute; x: number; y: number; caption: number }) {
  const Mark = route.id === 'r:+' ? IconMore : ROUTE_GLYPH[route.kind];
  const carrying = route.speed > 0;
  return (
    <g data-route={route.id} data-kind={route.kind}>
      <circle cx={x} cy={y} r={ROUTE.half} fill={carrying ? FILL : GROUND} />
      {/* A detail inside the filled shape is cut out in the colour of the surface behind. */}
      <g color={carrying ? SURFACE : QUIET}>
        <Mark x={x - 9} y={y - 9} width={18} height={18} />
      </g>
      <Caption x={x} y={y + ROUTE.half + 5} width={caption}>
        <span dir="auto" className="max-w-full truncate text-carbon-text">
          {route.name}
        </span>
        {route.via && (
          <span dir="ltr" className="max-w-full truncate text-carbon-textMuted">
            {route.via}
          </span>
        )}
      </Caption>
    </g>
  );
}

function FolderFigure({
  folder,
  x,
  y,
  caption,
  cfg,
}: {
  folder: FlowFolder;
  x: number;
  y: number;
  caption: number;
  cfg: Settings | null;
}) {
  const { t } = useT();
  const v = folder.volume;
  const measured = v !== null && v.known && v.total > 0;
  return (
    <g data-folder={folder.id}>
      <foreignObject x={x - 22} y={y - FOLDER.half} width="44" height="52">
        {measured ? (
          <Vessel share={v.used / v.total} label={folder.name} tone={floorTone(v, cfg)} marks={floorMarks(v, cfg, t)} />
        ) : (
          <Vessel share={null} label={folder.name} />
        )}
      </foreignObject>
      <Caption x={x} y={y + FOLDER.half + 5} width={caption}>
        {/* dir="auto": a path can be in a right-to-left script. */}
        <span dir="auto" className="max-w-full truncate text-carbon-text">
          {folder.name}
        </span>
        {measured && (
          <span className="glim-num max-w-full truncate text-carbon-textMuted">
            {t('disk.free')} {fmtTotal(v.free)}
          </span>
        )}
      </Caption>
    </g>
  );
}
