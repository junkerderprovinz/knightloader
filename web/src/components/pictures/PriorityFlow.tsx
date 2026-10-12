import type { CSSProperties, ReactNode } from 'react';
import type { GlyphProps } from '../../lib/glyphs';
import { IconLink } from '../../lib/icons';
import { ENTRY, Entry, LANE, Name, PITCH, RADIUS, Stop, Track, WIDTH, numbered, stopAt } from './lane';
import { FILL, SURFACE, trip, useStage } from './scene';
import { Traveller, loopOf } from './station';

// Seconds a backend looks at the link before it goes on to the next.
const ASK = 0.7;

const heightFor = (count: number) => stopAt(count - 1) + RADIUS + 9;
const share = (part: number, whole: number) => `${(part / whole) * 100}%`;

/**
 * nameBand places the list that stands in for the names beside the stations:
 * one row per station, each as tall as the gap between two of them, so a row
 * sits beside its station at every width the picture is drawn at. Left and
 * right rather than start and end, since the picture does not mirror.
 */
export function nameBand(count: number): CSSProperties {
  const height = heightFor(count);
  return {
    position: 'absolute',
    top: share(stopAt(0) - PITCH / 2, height),
    bottom: share(height - stopAt(count - 1) - PITCH / 2, height),
    left: share(LANE + RADIUS + 4, WIDTH),
    right: 0,
  };
}

/**
 * PriorityFlow shows the order backends are asked in: a link comes in at the
 * top and is offered to each backend in turn, first to the one that stands
 * first. Every backend the link reaches takes its colour and answers. `glyph`
 * is what comes in, the link unless a captcha is handed down the order.
 *
 * `children` replaces the names with rows of the caller's own, placed with
 * nameBand, such as rows that can be dragged into another order. The stations
 * stay where they are, since a station is a place in the order and its number
 * does not move with the backend that stands at it.
 */
export function PriorityFlow({
  backends,
  label,
  glyph: Glyph = IconLink,
  children,
}: {
  backends: { id: string; name: string }[];
  label: string;
  glyph?: (p: GlyphProps) => ReactNode;
  children?: ReactNode;
}) {
  // The order is the scene, so a changed order starts it over.
  const { stage, still } = useStage(backends.map((b) => b.id).join('\n'));
  const points: [number, number][] = [[LANE, ENTRY], ...backends.map((_, i): [number, number] => [LANE, stopAt(i)])];
  const walk = trip(points, (i) => (i === 0 ? 0 : ASK));
  const loop = loopOf(walk);
  const named = children === undefined;

  return (
    <div className="relative w-full max-w-[24rem]">
      <svg
        ref={stage}
        viewBox={`0 0 ${WIDTH} ${heightFor(backends.length)}`}
        role={named ? 'img' : undefined}
        aria-label={named ? `${label}: ${backends.map((b, i) => `${i + 1}. ${b.name}`).join(', ')}` : undefined}
        aria-hidden={named ? undefined : true}
        className="block w-full overflow-visible"
      >
        <Track d={walk.motion.path} />
        <Entry>
          <Glyph x={-7} y={-7} width={14} height={14} />
        </Entry>

        {backends.map(({ id, name }, i) => (
          <g key={id}>
            <Stop y={stopAt(i)} state="takes" face={numbered(i + 1)} at={walk.stops[i + 1].arrive} loop={loop} still={still} />
            {named && <Name y={stopAt(i)}>{name}</Name>}
          </g>
        ))}

        {!still && (
          <Traveller trip={walk} loop={loop} taking={walk.stops} stays>
            <LinkParcel glyph={Glyph} />
          </Traveller>
        )}
      </svg>
      {children}
    </div>
  );
}

/** The link on its way: a filled disc with the link glyph, or `glyph`, cut out. */
export function LinkParcel({ glyph: Glyph = IconLink }: { glyph?: (p: GlyphProps) => ReactNode }) {
  return (
    <>
      <circle r="8" fill={FILL} />
      <g color={SURFACE}>
        <Glyph x={-6} y={-6} width={12} height={12} />
      </g>
    </>
  );
}
