import type { ComponentType, SVGProps } from 'react';
import { useT } from '../../lib/i18n';
import {
  IconArchive,
  IconCheck,
  IconDownloads,
  IconFolderOpen,
  IconShield,
  IconShieldCheck,
  IconTrash,
} from '../../lib/icons';
import { FILL, GROUND, OK, QUIET, SURFACE, trip, useStage } from './scene';
import { Answer, Hop, Traveller, litFrom, loopOf } from './station';

/** Which of the steps after a download are switched on. The download itself always happens. */
export interface AfterDownloadSteps {
  verify: boolean;
  repair: boolean;
  unpack: boolean;
  move: boolean;
  cleanup: boolean;
}

interface Station {
  id: string;
  label: string;
  glyph: ComponentType<SVGProps<SVGSVGElement>>;
  on: boolean;
}

/** Where the stations stand and where their names go: side by side in a wide card, stacked in a narrow one. */
interface Arrangement {
  width: number;
  height: number;
  at: (i: number) => [x: number, y: number];
  caption: (i: number) => { x: number; y: number; width: number; height: number };
  captionClass: string;
  className: string;
}

const ROW: Arrangement = {
  width: 616,
  height: 98,
  at: (i) => [44 + i * 88, 30],
  caption: (i) => ({ x: 2 + i * 88, y: 54, width: 84, height: 44 }),
  captionClass: 'items-center text-center',
  className: 'hidden max-w-[44rem] @[38rem]:block',
};

const COLUMN: Arrangement = {
  width: 290,
  height: 292,
  at: (i) => [26, 26 + i * 40],
  caption: (i) => ({ x: 56, y: 8 + i * 40, width: 230, height: 36 }),
  captionClass: 'justify-center',
  className: 'block max-w-[20rem] @[38rem]:hidden',
};

const RADIUS = 17;
// Seconds a station works on the file.
const WORK = 0.9;

/**
 * AfterDownload plays what happens to a file once it has arrived, with the
 * steps that are switched on. A station that is on takes the file in and
 * answers; one that is off stays grey and the file passes it.
 */
export function AfterDownload({ steps, title }: { steps: AfterDownloadSteps; title: string }) {
  const { t } = useT();
  const stations: Station[] = [
    { id: 'download', label: t('status.running'), glyph: IconDownloads, on: true },
    { id: 'verify', label: t('status.verifying'), glyph: IconShieldCheck, on: steps.verify },
    { id: 'repair', label: t('status.repairing'), glyph: IconShield, on: steps.repair },
    { id: 'unpack', label: t('status.extracting'), glyph: IconArchive, on: steps.unpack },
    { id: 'move', label: t('picture.journey.move'), glyph: IconFolderOpen, on: steps.move },
    { id: 'cleanup', label: t('picture.journey.cleanup'), glyph: IconTrash, on: steps.cleanup },
  ];
  // Read out as the steps that run, since the picture says no more than that.
  const label = `${title}: ${stations.filter((s) => s.on).map((s) => s.label).join(', ')}`;
  return (
    <div className="@container">
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
      role="img"
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

      {stations.map((s, i) => {
        const [x, y] = points[i];
        const { arrive } = walk.stops[i];
        const Glyph = s.glyph;
        const glyph = <Glyph x={-9} y={-9} width={18} height={18} />;
        return (
          <g key={s.id} transform={`translate(${x} ${y})`} data-station={s.id} data-state={s.on ? 'on' : 'off'}>
            {s.on && !still && <Answer radius={RADIUS} at={arrive} loop={loop} />}
            <g>
              {s.on && !still && <Hop at={arrive} loop={loop} />}
              <circle
                r={RADIUS}
                fill={GROUND}
                stroke={s.on ? undefined : QUIET}
                strokeOpacity=".6"
                strokeDasharray="3 4"
              />
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
        );
      })}

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

      {[...stations.map((s) => ({ name: s.label, on: s.on })), { name: t('status.done'), on: true }].map((c, i) => (
        <foreignObject key={i} {...arrangement.caption(i)}>
          <div
            className={`flex h-full flex-col text-[11px] leading-tight ${arrangement.captionClass} ${
              c.on ? 'text-carbon-text' : 'text-carbon-textMuted'
            }`}
          >
            <span className="max-w-full break-words">{c.name}</span>
            {!c.on && <span>{t('settings.modules.off')}</span>}
          </div>
        </foreignObject>
      ))}
    </svg>
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
