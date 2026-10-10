import { IconLink } from '../../lib/icons';
import { FILL, GROUND, LABEL, QUIET, SURFACE, trip, useStage } from './scene';
import { Answer, Hop, Traveller, litFrom, loopOf } from './station';

const WIDTH = 320;
const LANE = 22;
const FIRST = 64;
const PITCH = 36;
const RADIUS = 11;
// Seconds a backend looks at the link before it goes on to the next.
const ASK = 0.7;

/**
 * PriorityFlow shows the order backends are asked in: a link comes in at the
 * top and is offered to each backend in turn, first to the one that stands
 * first. Every backend the link reaches takes its colour and answers.
 */
export function PriorityFlow({ backends, label }: { backends: { id: string; name: string }[]; label: string }) {
  // The order is the scene, so a changed order starts it over.
  const { stage, still } = useStage(backends.map((b) => b.id).join('\n'));
  const points: [number, number][] = [
    [LANE, 20],
    ...backends.map((_, i): [number, number] => [LANE, FIRST + i * PITCH]),
  ];
  const walk = trip(points, (i) => (i === 0 ? 0 : ASK));
  const loop = loopOf(walk);
  const height = FIRST + (backends.length - 1) * PITCH + RADIUS + 9;

  return (
    <svg
      ref={stage}
      viewBox={`0 0 ${WIDTH} ${height}`}
      role="img"
      aria-label={`${label}: ${backends.map((b, i) => `${i + 1}. ${b.name}`).join(', ')}`}
      className="block w-full max-w-[24rem] overflow-visible"
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
      <circle cx={LANE} cy="20" r={RADIUS} fill={GROUND} />
      <g color={QUIET}>
        <IconLink x={LANE - 7} y={13} width={14} height={14} />
      </g>

      {backends.map(({ id, name }, i) => {
        const y = FIRST + i * PITCH;
        const { arrive } = walk.stops[i + 1];
        const place = (fill: string) => (
          <text y="4" textAnchor="middle" fontSize="11" fontWeight="700" fill={fill} className="glim-num">
            {i + 1}
          </text>
        );
        return (
          <g key={id}>
            <g transform={`translate(${LANE} ${y})`}>
              {!still && <Answer radius={RADIUS} at={arrive} loop={loop} />}
              <g>
                {!still && <Hop at={arrive} loop={loop} />}
                <circle r={RADIUS} fill={GROUND} />
                {place(LABEL)}
                <g opacity={still ? undefined : 0}>
                  {!still && <animate attributeName="opacity" {...litFrom(arrive, loop)} {...loop.smil} />}
                  <circle r={RADIUS} fill={FILL} />
                  {place(SURFACE)}
                </g>
              </g>
            </g>
            <foreignObject x={LANE + 22} y={y - 12} width={WIDTH - LANE - 24} height="24">
              {/* dir="auto": a hoster login's row is a host name. */}
              <div dir="auto" className="truncate text-[13px] leading-[24px] text-carbon-text">
                {name}
              </div>
            </foreignObject>
          </g>
        );
      })}

      {!still && (
        <Traveller trip={walk} loop={loop} taking={walk.stops} stays>
          <circle r="8" fill={FILL} />
          <g color={SURFACE}>
            <IconLink x={-6} y={-6} width={12} height={12} />
          </g>
        </Traveller>
      )}
    </svg>
  );
}
