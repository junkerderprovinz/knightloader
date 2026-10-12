import { useId, type CSSProperties } from 'react';
import { useTooltip } from '../ui';
import { FAIL, FILL, LABEL, QUIET, WARN, useStage } from './scene';

// The picture is 34 by 40. The liquid stands between the floor and the brim,
// the dashed line where the vessel counts as full.
const BRIM = 8;
const FLOOR = 35.5;
const INSIDE = 'M7.5 3V31a4.5 4.5 0 0 0 4.5 4.5h10a4.5 4.5 0 0 0 4.5-4.5V3z';
// The liquid reaches well past the walls, so it still fills the vessel while
// it tilts. The back layer stands a little higher and shows where the front
// one has swung away from it.
const BODY = 'M-20 0H54V45H-20z';
const BACK = 'M-20 -.5H54V45H-20z';
// A crest stands at a wall and runs up it as the stylesheet stretches it; the
// hump crosses the middle. Both rest squashed flat.
const CREST = 'M-5 .6V-1H0C.9 -.55 2.7 -.03 6 0V.6z';
const HUMP = 'M11 .6V0C13 0 14.5 -1 17 -1S21 0 23 0V.6z';
// Each is [x, radius]. The stylesheet times each by its place among its
// kind, so their order here is part of the animation.
const BUBBLES = [
  [10.5, 0.5],
  [16.5, 0.45],
  [24.5, 0.7],
];
const POPS = [
  [13.5, 0.7],
  [19, 0.6],
  [21, 0.65],
];
// White in every theme: it only needs to be lighter than the liquid.
const BUBBLE = '#fff';
// Where the liquid rests in a vessel with no brim to measure against.
const UNMEASURED = 27;

const heightOf = (share: number) => FLOOR - Math.max(0, Math.min(1, share)) * (FLOOR - BRIM);

export interface VesselMark {
  /** The fill share the line stands at, 0 to 1. */
  at: number;
  tone: 'warn' | 'fail';
  label: string;
}

// The liquid is a fill like a progress bar's, so it takes the accent itself.
const PAINT = { accent: FILL, warn: WARN, fail: FAIL };

/**
 * Vessel draws a fill level against the dashed line that means full, with each
 * mark a line to read the level against. A `share` of null is a vessel whose
 * size nobody set, drawn open at the top.
 */
export function Vessel({
  share,
  label,
  tone = 'accent',
  marks = [],
}: {
  share: number | null;
  /** What the level measures, read out with the percentage. */
  label: string;
  tone?: keyof typeof PAINT;
  marks?: VesselMark[];
}) {
  const clip = useId();
  const { stage, still } = useStage();
  const measured = share !== null;
  const level = measured ? heightOf(share) : UNMEASURED;

  return (
    <span className="relative block h-[52px] w-[44px] shrink-0">
      <svg
        ref={stage}
        viewBox="0 0 34 40"
        className="block h-full w-full overflow-visible"
        role={measured ? 'progressbar' : 'img'}
        aria-label={label}
        aria-valuemin={measured ? 0 : undefined}
        aria-valuemax={measured ? 100 : undefined}
        aria-valuenow={measured ? Math.round(Math.max(0, Math.min(1, share)) * 100) : undefined}
        style={{ '--liquid-depth': (FLOOR - level).toFixed(2) } as CSSProperties}
      >
        <clipPath id={clip}>
          <path d={INSIDE} />
        </clipPath>
        {/* A wash of the text colour rather than a surface step, since a vessel stands on cards and in wells alike. */}
        <path d={INSIDE} fill={QUIET} fillOpacity=".18" />
        <g clipPath={`url(#${clip})`}>
          <g transform={`translate(0 ${level.toFixed(2)})`}>
            <g>
              {!still && (
                <animateTransform
                  attributeName="transform"
                  type="translate"
                  values={`0 ${(FLOOR - level + 3).toFixed(2)};0 0`}
                  keyTimes="0;1"
                  calcMode="spline"
                  keySplines=".2 .8 .3 1"
                  dur="1.1s"
                  fill="freeze"
                />
              )}
              {/* The liquid sloshes for as long as it is shown, on the motion
                  engine's dials (glim-slosh in index.css), which the motion
                  setting and a request for less motion both stop. */}
              <path d={BACK} fill={PAINT[tone]} fillOpacity=".45" className="glim-slosh-back" />
              <g className="glim-slosh">
                <path d={BODY} fill={PAINT[tone]} />
                <path d={HUMP} fill={PAINT[tone]} className="glim-slosh-hump" />
                <g transform="translate(7.5 0)">
                  <path d={CREST} fill={PAINT[tone]} className="glim-slosh-crest glim-slosh-crest-left" />
                </g>
                <g transform="translate(26.5 0) scale(-1 1)">
                  <path d={CREST} fill={PAINT[tone]} className="glim-slosh-crest glim-slosh-crest-right" />
                </g>
              </g>
              <g fill={BUBBLE}>
                {BUBBLES.map(([cx, r]) => (
                  <circle key={cx} className="glim-slosh-bubble" cx={cx} cy="0" r={r} />
                ))}
              </g>
              <g>
                {POPS.map(([cx, r]) => (
                  <g key={cx} className="glim-slosh-pop">
                    <circle className="glim-slosh-pop-bubble" cx={cx} cy="0" r={r} fill={BUBBLE} stroke={PAINT[tone]} strokeWidth=".25" />
                    <circle className="glim-slosh-pop-ring" cx={cx} cy="0" r={r} fill="none" stroke={PAINT[tone]} strokeWidth=".22" />
                    {[0, 1, 2].map((d) => (
                      <circle key={d} className="glim-slosh-pop-drop" cx={cx} cy="0" r=".3" fill={PAINT[tone]} />
                    ))}
                  </g>
                ))}
              </g>
            </g>
          </g>
        </g>
        {measured ? (
          <>
            <path d="M6 3V31a6 6 0 0 0 6 6h10a6 6 0 0 0 6-6V3" fill="none" stroke={QUIET} strokeWidth="1.6" strokeLinecap="round" />
            <path d={`M2 ${BRIM}H32`} stroke={LABEL} strokeWidth="1.2" strokeDasharray="2.5 2.5" />
          </>
        ) : (
          <>
            <path d="M6 17V31a6 6 0 0 0 6 6h10a6 6 0 0 0 6-6V17" fill="none" stroke={QUIET} strokeWidth="1.6" strokeLinecap="round" />
            <path d="M6 15V3M28 15V3" fill="none" stroke={QUIET} strokeWidth="1.6" strokeLinecap="round" strokeDasharray="2 3" />
          </>
        )}
        {marks.map((m) => (
          <path key={m.label} d={`M3 ${heightOf(m.at).toFixed(2)}H31`} stroke={PAINT[m.tone]} strokeWidth="1.6" strokeLinecap="round" />
        ))}
      </svg>
      {marks.map((m) => (
        <MarkTarget key={m.label} mark={m} />
      ))}
    </span>
  );
}

// The line itself is too thin to point at, so a taller strip over it carries
// its words. A component of its own because useTooltip is a hook.
function MarkTarget({ mark }: { mark: VesselMark }) {
  const tip = useTooltip<HTMLSpanElement>(mark.label);
  return (
    <>
      <span
        {...tip.triggerProps}
        role="img"
        aria-label={mark.label}
        style={{ top: `${(heightOf(mark.at) / 40) * 100}%` }}
        className="absolute inset-x-0 h-2 -translate-y-1/2"
      />
      {tip.node}
    </>
  );
}
