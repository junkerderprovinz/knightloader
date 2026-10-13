// The form the pictures of an order share: one lane down the left edge, a
// station per entry and its name beside it. The priority order, the captcha
// chain and the rule list are drawn from these parts, so they read alike.
import type { ReactNode } from 'react';
import { FILL, GROUND, LABEL, QUIET, SURFACE } from './scene';
import { Answer, Hop, litFrom, type Loop } from './station';

export const WIDTH = 320;
export const LANE = 22;
/** Where whatever travels comes in, above the first station. */
export const ENTRY = 20;
export const FIRST = 64;
export const PITCH = 36;
export const RADIUS = 11;

/** How far down the picture station `i` stands. */
export const stopAt = (i: number) => FIRST + i * PITCH;

/** Track is a way something can take, drawn before anything takes it. */
export function Track({ d }: { d: string }) {
  return (
    <path d={d} fill="none" stroke={QUIET} strokeOpacity=".6" strokeWidth="2" strokeLinecap="round" strokeDasharray="2 7" />
  );
}

/** Entry is the place the traveller comes from, with its glyph. */
export function Entry({ children }: { children: ReactNode }) {
  return (
    <>
      <circle cx={LANE} cy={ENTRY} r={RADIUS} fill={GROUND} />
      <g color={QUIET} transform={`translate(${LANE} ${ENTRY})`}>
        {children}
      </g>
    </>
  );
}

/** numbered is a station's face that says its place in the order. */
export const numbered = (n: number) => (paint: string) => (
  <text y="4" textAnchor="middle" fontSize="11" fontWeight="700" fill={paint} className="glim-num">
    {n}
  </text>
);

/**
 * Stop is one station. One that takes the traveller waits in grey and takes
 * the accent in one step at `at`, with a ring and a small hop; in a still
 * frame it stands in the accent. One that is passed stays grey, and one that
 * is switched off is an empty, dashed place.
 */
export function Stop({
  x = LANE,
  y,
  state,
  face,
  fill = FILL,
  at,
  loop,
  still,
}: {
  x?: number;
  y: number;
  state: 'takes' | 'passed' | 'off';
  /** What stands in the station, drawn in the paint it is handed. */
  face: (paint: string) => ReactNode;
  /** What a station that takes the traveller fills with, the accent unless it says a verdict. */
  fill?: string;
  /** When the traveller arrives, as a share of the loop. */
  at: number;
  loop: Loop;
  still: boolean;
}) {
  const answers = state === 'takes' && !still;
  return (
    <g transform={`translate(${x} ${y})`}>
      {answers && <Answer radius={RADIUS} at={at} loop={loop} />}
      <g>
        {answers && <Hop at={at} loop={loop} />}
        {state === 'off' ? (
          <circle r={RADIUS} fill={GROUND} stroke={QUIET} strokeOpacity=".6" strokeDasharray="3 4" />
        ) : (
          <circle r={RADIUS} fill={GROUND} />
        )}
        <g opacity={state === 'off' ? 0.6 : undefined}>{face(state === 'off' ? QUIET : LABEL)}</g>
        {state === 'takes' && (
          <g opacity={still ? undefined : 0}>
            {!still && <animate attributeName="opacity" {...litFrom(at, loop)} {...loop.smil} />}
            <circle r={RADIUS} fill={fill} />
            {face(SURFACE)}
          </g>
        )}
      </g>
    </g>
  );
}

/**
 * Name is the words beside a station: what it is and, under it, a note such as
 * why it is passed. `quiet` sets the name back for a station nothing reaches,
 * and `onOpen` makes the words the button that opens what the station stands
 * for.
 */
export function Name({
  x = LANE + 22,
  y,
  quiet = false,
  note,
  onOpen,
  children,
}: {
  x?: number;
  y: number;
  quiet?: boolean;
  note?: ReactNode;
  onOpen?: () => void;
  children: ReactNode;
}) {
  const words = (
    <>
      {/* dir="auto": a hoster login's row is a host name. */}
      <span dir="auto" className={`block truncate text-[13px] leading-[15px] ${quiet ? 'text-carbon-textMuted' : 'text-carbon-text'}`}>
        {children}
      </span>
      {note && <span className="block truncate text-meta leading-[14px] text-carbon-textMuted">{note}</span>}
    </>
  );
  return (
    <foreignObject x={x} y={y - 15} width={WIDTH - x - 2} height="30">
      {onOpen ? (
        <button
          type="button"
          onClick={onOpen}
          aria-haspopup="dialog"
          className="flex h-full w-full min-w-0 flex-col justify-center rounded-[var(--radius-control)] text-start
            transition-colors hover:bg-carbon-hover"
        >
          {words}
        </button>
      ) : (
        <div className="flex h-full min-w-0 flex-col justify-center">{words}</div>
      )}
    </foreignObject>
  );
}
