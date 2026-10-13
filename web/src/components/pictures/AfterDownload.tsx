import type { ComponentType, SVGProps } from 'react';
import { useT } from '../../lib/i18n';
import { useTooltip } from '../ui';
import {
  IconArchive,
  IconCheck,
  IconDownload,
  IconFolderOpen,
  IconShield,
  IconShieldOn,
  IconTrash,
} from '../../lib/icons';
import { FILL, GROUND, OK, QUIET, SURFACE, trip, useStage, type Trip } from './scene';
import { Answer, Hop, Traveller, litFrom, loopOf, type Loop } from './station';

/** Which of the steps after a download are switched on. The download itself always happens. */
export interface AfterDownloadSteps {
  verify: boolean;
  repair: boolean;
  unpack: boolean;
  move: boolean;
  cleanup: boolean;
}

/** Where a station's setting is: the words that say so, and the way there. */
export interface StationWay {
  leadsTo: string;
  go: () => void;
}

/** The ways by station. The download itself and the finished file have nothing to set. */
export type StationWays = Partial<Record<keyof AfterDownloadSteps, StationWay>>;

interface Station {
  id: string;
  label: string;
  glyph: ComponentType<SVGProps<SVGSVGElement>>;
  on: boolean;
  way?: StationWay;
}

interface Box {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** Where the stations stand and where their names go: side by side in a wide card, stacked in a narrow one. */
interface Arrangement {
  width: number;
  height: number;
  at: (i: number) => [x: number, y: number];
  caption: (i: number) => Box;
  /** A station and its name together, which is what a station with a way answers the pointer on. */
  cell: (i: number) => Box;
  captionClass: string;
  className: string;
}

const ROW: Arrangement = {
  width: 616,
  height: 98,
  at: (i) => [44 + i * 88, 30],
  caption: (i) => ({ x: 2 + i * 88, y: 54, width: 84, height: 44 }),
  cell: (i) => ({ x: 2 + i * 88, y: 4, width: 84, height: 94 }),
  captionClass: 'items-center text-center',
  className: 'hidden max-w-[44rem] @[38rem]:block',
};

const COLUMN: Arrangement = {
  width: 290,
  height: 292,
  at: (i) => [26, 26 + i * 40],
  caption: (i) => ({ x: 56, y: 8 + i * 40, width: 230, height: 36 }),
  cell: (i) => ({ x: 2, y: 6 + i * 40, width: 286, height: 40 }),
  captionClass: 'justify-center',
  className: 'block max-w-[20rem] @[38rem]:hidden',
};

const RADIUS = 17;
// Seconds a station works on the file.
const WORK = 0.9;

/**
 * AfterDownload plays what happens to a file once it has arrived, with the
 * steps that are switched on. A station that is on takes the file in and
 * answers; one that is off stays grey and the file passes it. A station with a
 * way leads to its setting on a click or Enter.
 */
export function AfterDownload({
  steps,
  title,
  ways = {},
}: {
  steps: AfterDownloadSteps;
  title: string;
  ways?: StationWays;
}) {
  const { t } = useT();
  const stations: Station[] = [
    { id: 'download', label: t('status.running'), glyph: IconDownload, on: true },
    { id: 'verify', label: t('status.verifying'), glyph: IconShieldOn, on: steps.verify, way: ways.verify },
    { id: 'repair', label: t('status.repairing'), glyph: IconShield, on: steps.repair, way: ways.repair },
    { id: 'unpack', label: t('status.extracting'), glyph: IconArchive, on: steps.unpack, way: ways.unpack },
    { id: 'move', label: t('picture.journey.move'), glyph: IconFolderOpen, on: steps.move, way: ways.move },
    { id: 'cleanup', label: t('picture.journey.cleanup'), glyph: IconTrash, on: steps.cleanup, way: ways.cleanup },
  ];
  // Read out as the steps that run, since the picture says no more than that.
  const label = `${title}: ${stations.filter((s) => s.on).map((s) => s.label).join(', ')}`;
  return (
    <div data-new="after-download" className="@container">
      <Walk arrangement={ROW} stations={stations} label={label} />
      <Walk arrangement={COLUMN} stations={stations} label={label} />
    </div>
  );
}

function Walk({ arrangement, stations, label }: { arrangement: Arrangement; stations: Station[]; label: string }) {
  const { t } = useT();
  const { stage, still } = useStage(stations.map((s) => (s.on ? '1' : '0')).join(''));

  // The last point is the slot the finished file comes to rest in.
  const finish = stations.length;
  const points = Array.from({ length: finish + 1 }, (_, i) => arrangement.at(i));
  const walk = trip(points, (i) => (stations[i]?.on ? WORK : 0));
  const loop = loopOf(walk);
  const [endX, endY] = points[finish];

  return (
    <svg
      ref={stage}
      viewBox={`0 0 ${arrangement.width} ${arrangement.height}`}
      // One image, until a part of it can be pressed.
      role={stations.some((s) => s.way) ? 'group' : 'img'}
      aria-label={label}
      className={`mx-auto w-full overflow-visible ${arrangement.className}`}
    >
      <path
        d={walk.motion.path}
        fill="none"
        stroke={QUIET}
        strokeOpacity=".6"
        strokeWidth="2"
        strokeLinecap="round"
        strokeDasharray="2 7"
      />

      {stations.map((s, i) => (
        <StationMark key={s.id} station={s} index={i} arrangement={arrangement} walk={walk} loop={loop} still={still} />
      ))}

      <rect
        x={endX - 13}
        y={endY - 16}
        width="26"
        height="32"
        rx="6"
        fill="none"
        stroke={QUIET}
        strokeOpacity=".6"
        strokeDasharray="3 4"
      />
      {still ? (
        <g transform={`translate(${endX} ${endY})`}>
          <Sheet />
        </g>
      ) : (
        <Traveller trip={walk} loop={loop} taking={walk.stops.filter((_, i) => stations[i]?.on)}>
          <Sheet />
        </Traveller>
      )}
      <g transform={`translate(${endX + 11} ${endY - 14})`} opacity={still ? undefined : 0}>
        {!still && <animate attributeName="opacity" {...litFrom(walk.stops[finish].arrive, loop)} {...loop.smil} />}
        <circle r="7" fill={OK} stroke={SURFACE} strokeWidth="1.5" />
        <g color={SURFACE}>
          <IconCheck x={-4.5} y={-4.5} width={9} height={9} />
        </g>
      </g>

      <Caption box={arrangement.caption(finish)} className={arrangement.captionClass} name={t('status.done')} on />
    </svg>
  );
}

/** A station with its name under or beside it. With a way the two are one link. */
function StationMark({
  station: s,
  index,
  arrangement,
  walk,
  loop,
  still,
}: {
  station: Station;
  index: number;
  arrangement: Arrangement;
  walk: Trip;
  loop: Loop;
  still: boolean;
}) {
  const [x, y] = arrangement.at(index);
  const { arrive } = walk.stops[index];
  const Glyph = s.glyph;
  const glyph = <Glyph x={-9} y={-9} width={18} height={18} />;
  const tip = useTooltip<SVGGElement>(s.way?.leadsTo ?? null);

  const drawn = (
    <>
      <g transform={`translate(${x} ${y})`} data-station={s.id} data-state={s.on ? 'on' : 'off'}>
        {s.way && <circle className="kl-station-ring" r={RADIUS + 5} />}
        {s.on && !still && <Answer radius={RADIUS} at={arrive} loop={loop} />}
        <g>
          {s.on && !still && <Hop at={arrive} loop={loop} />}
          <circle r={RADIUS} fill={GROUND} stroke={s.on ? undefined : QUIET} strokeOpacity=".6" strokeDasharray="3 4" />
          <g color={QUIET} opacity={s.on ? undefined : 0.6}>
            {glyph}
          </g>
          {s.on && (
            <g opacity={still ? undefined : 0}>
              {!still && <animate attributeName="opacity" {...litFrom(arrive, loop)} {...loop.smil} />}
              <circle r={RADIUS} fill={FILL} />
              <g color={SURFACE}>{glyph}</g>
            </g>
          )}
        </g>
      </g>
      <Caption box={arrangement.caption(index)} className={arrangement.captionClass} name={s.label} on={s.on} />
    </>
  );
  if (!s.way) return drawn;

  const { go, leadsTo } = s.way;
  return (
    <g
      {...tip.triggerProps}
      role="link"
      aria-label={`${s.label}. ${leadsTo}`}
      // The ring around the station is its focus ring.
      className="kl-station-way glim-focus-drawn"
      onClick={go}
      onKeyDown={(e) => {
        if (e.key !== 'Enter') return;
        e.preventDefault();
        go();
      }}
    >
      {/* Unpainted, so the gap between a station and its name answers the pointer too. */}
      <rect {...arrangement.cell(index)} fill="none" pointerEvents="all" />
      {drawn}
      {tip.node}
    </g>
  );
}

function Caption({ box, className, name, on }: { box: Box; className: string; name: string; on: boolean }) {
  const { t } = useT();
  return (
    <foreignObject {...box}>
      <div
        className={`flex h-full flex-col text-meta leading-tight ${className} ${on ? 'text-carbon-text' : 'text-carbon-textMuted'}`}
      >
        <span className="max-w-full break-words">{name}</span>
        {!on && <span>{t('settings.modules.off')}</span>}
      </div>
    </foreignObject>
  );
}

/** The file: a filled sheet with its lines of text cut out. */
function Sheet() {
  return (
    <g transform="scale(.8)">
      <path d="M-10 -13h13l7 7v19h-20z" fill={FILL} />
      <path d="M3 -13v7h7" fill="none" stroke={SURFACE} strokeOpacity=".6" />
      <path d="M-6 0h12M-6 4h12M-6 8h7" stroke={SURFACE} strokeWidth="1.6" strokeLinecap="round" />
    </g>
  );
}
