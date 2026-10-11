import { IconLink } from '../../lib/icons';
import { ENTRY, Entry, LANE, Name, RADIUS, Stop, Track, WIDTH, numbered, stopAt } from './lane';
import { FILL, SURFACE, trip, useStage } from './scene';
import { Traveller, loopOf } from './station';

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
  const points: [number, number][] = [[LANE, ENTRY], ...backends.map((_, i): [number, number] => [LANE, stopAt(i)])];
  const walk = trip(points, (i) => (i === 0 ? 0 : ASK));
  const loop = loopOf(walk);
  const height = stopAt(backends.length - 1) + RADIUS + 9;

  return (
    <svg
      ref={stage}
      viewBox={`0 0 ${WIDTH} ${height}`}
      role="img"
      aria-label={`${label}: ${backends.map((b, i) => `${i + 1}. ${b.name}`).join(', ')}`}
      data-new="priority-flow"
      className="block w-full max-w-[24rem] overflow-visible"
    >
      <Track d={walk.motion.path} />
      <Entry>
        <IconLink x={-7} y={-7} width={14} height={14} />
      </Entry>

      {backends.map(({ id, name }, i) => (
        <g key={id}>
          <Stop y={stopAt(i)} state="takes" face={numbered(i + 1)} at={walk.stops[i + 1].arrive} loop={loop} still={still} />
          <Name y={stopAt(i)}>{name}</Name>
        </g>
      ))}

      {!still && (
        <Traveller trip={walk} loop={loop} taking={walk.stops} stays>
          <LinkParcel />
        </Traveller>
      )}
    </svg>
  );
}

/** The link on its way: a filled disc with the link glyph cut out. */
export function LinkParcel() {
  return (
    <>
      <circle r="8" fill={FILL} />
      <g color={SURFACE}>
        <IconLink x={-6} y={-6} width={12} height={12} />
      </g>
    </>
  );
}
