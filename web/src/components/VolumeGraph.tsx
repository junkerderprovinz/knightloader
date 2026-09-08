import { rainbowColor } from '../lib/appearance';
import { fmtGB } from '../lib/format';
import { useRainbow } from '../lib/useRainbow';

/**
 * One band of the stack: a hoster, a backend, or the fold-together band that
 * carries everything the top five did not.
 */
export interface VolumeSeries {
  /** Stable id. Used as a React key and for nothing else. */
  id: string;
  /** What the legend prints. Display-ready and, where it is a word, translated. */
  label: string;
  /** One figure per bucket, in the same order and of the same length as `labels`. */
  values: number[];
}

/**
 * VolumeGraph is the plot alone: N buckets across, one stacked bar each.
 *
 * BARS AND NOT A CURVE, which is the decision this file exists to hold. A day
 * is a bucket, not a sample. SpeedGraph reads a live figure once a second and a
 * Bezier through those points describes something that really was moving in
 * between; a Bezier through thirty finished days would invent a value for half
 * past the 14th, and "how much on the 14th" is the whole question here.
 *
 * It does NOT borrow SpeedGraph's ceilingFor either, and that is not tidiness.
 * That ceiling rises at once and relaxes 8% per tick, which settles only
 * because a tick arrives every second; these buckets do not tick at all, so the
 * same thirty days would draw differently depending on what the component had
 * been showing before it. The top of the scale is the tallest bucket, full stop.
 *
 * What it does keep from SpeedGraph is the rule that cost that component its
 * peak caption: both axes are printed in every state, empty window included,
 * because a number a state can take away is not part of the chart.
 */

// The box the plot is drawn IN, not the size it is drawn AT. The svg scales
// itself to whatever width the card hands it, UNIFORMLY - which is the one
// place this parts company with SpeedGraph's preserveAspectRatio="none". That
// chart carries no text; this one carries its scale inside the frame, and a
// stretched viewBox stretches the letters with it.
const W = 620;
const H = 210;
// The room the outermost labels need, which is all these four numbers are.
// The ordinate is right-aligned into the left gutter and the abscissa sits in
// the bottom band, so a label at either end of either scale is inside the
// viewBox rather than half-clipped by it.
const PAD_L = 62;
const PAD_R = 8;
const PAD_T = 10;
const PAD_B = 24;

const PLOT_W = W - PAD_L - PAD_R;
const PLOT_H = H - PAD_T - PAD_B;

// How wide a bar may get. Without it a 12-month curve draws twelve slabs 40
// units across, which reads as a colour chart rather than as a reading.
const BAR_MAX = 30;

// The step-down for the case where every band is the same colour: with rainbow
// mode off there is one accent, so weight is the only thing left to tell the
// hosters apart. In rainbow mode each band already owns a colour and this
// ladder would only muddy it, so it is not applied there.
const LADDER = [1, 0.78, 0.6, 0.46, 0.35, 0.27];

export function VolumeGraph({
  labels,
  series,
  label,
}: {
  /** The abscissa text per bucket, oldest first. Already formatted by the caller. */
  labels: string[];
  /** Stacked bottom-up in the order given. */
  series: VolumeSeries[];
  /** The chart's accessible name - it is the only thing carrying these figures. */
  label: string;
}) {
  // Subscribed, not merely read. Band colours are resolved during render, so a
  // chart that heard about a palette edit one paint late would keep the
  // previous key while the swatches beside it had already moved. Tabs does this
  // for its own callers for the same reason.
  const rainbow = useRainbow();

  const n = labels.length;
  const totals = labels.map((_, i) => series.reduce((sum, s) => sum + (s.values[i] ?? 0), 0));
  const peak = Math.max(0, ...totals);

  // ONE scale, and everything on the chart comes out of it: the bar heights,
  // the tick lines and the words beside them. Two scales is how a chart ends up
  // with a label that names a level no mark sits on.
  const scale = peak > 0 ? peak : 1;
  const y = (v: number) => PAD_T + PLOT_H - (v / scale) * PLOT_H;

  // Every label names a value the chart actually reaches: the top is the
  // tallest bucket (so the top edge of the box means something), the middle is
  // half of it, the foot is zero. An empty window keeps the foot alone - three
  // ticks all reading "0 GB" would be a scale pretending to have a range.
  const ticks = peak > 0 ? [1, 0.5, 0] : [0];

  const slot = n > 0 ? PLOT_W / n : PLOT_W;
  const barW = Math.min(slot * 0.72, BAR_MAX);
  const barX = (i: number) => PAD_L + slot * i + (slot - barW) / 2;

  const multi = series.length > 1;

  // The palette is read DIRECTLY here rather than through the .glim-hue class
  // every card, tab and nav row uses, and that is deliberate. In reactive
  // rainbow mode .glim-hue resolves --accent to muted grey until the element is
  // hovered - right for a nav item, where the colour is an affordance, and
  // wrong here, where the colour IS the legend: a key that only appears under
  // the pointer is a key nobody can read. rainbowColor answers undefined while
  // the mode is off entirely, and the single accent stands in.
  //
  // A lone band takes the flat accent whatever the mode: one series is not a
  // set, and the palette belongs to sets (index.css, "anything that is the only
  // one of its kind keeps the single accent").
  const bandColour = (i: number) => (multi ? (rainbowColor(i) ?? 'var(--accent)') : 'var(--accent)');
  const bandOpacity = (i: number) => (!multi || rainbow.on ? 1 : (LADDER[i] ?? 0.2));

  return (
    <div className="flex flex-col gap-3">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        className="block h-auto w-full"
        role="img"
        aria-label={label}
        focusable="false"
      >
        {ticks.map((f) => {
          const at = y(peak * f);
          return (
            <g key={f}>
              {/* The foot is the axis and reads as one; the levels above it are
                  a guide and stay behind the bars. Same --carbon-border ink
                  SpeedGraph's idle hairline uses, so the two charts have one
                  furniture colour between them in both themes. */}
              <line
                x1={PAD_L}
                y1={at}
                x2={W - PAD_R}
                y2={at}
                stroke="var(--carbon-border)"
                strokeWidth="1"
                strokeOpacity={f === 0 ? 1 : 0.45}
                vectorEffect="non-scaling-stroke"
              />
              {/* fill, not a Tailwind text colour: this is SVG, and the token is
                  the same one the muted ink outside the frame resolves to, so
                  the scale reads in the light theme and the dark one without a
                  second definition anywhere. */}
              <text
                x={PAD_L - 8}
                y={at}
                textAnchor="end"
                dominantBaseline="middle"
                fontSize="11"
                fill="var(--carbon-text-muted)"
              >
                {fmtGB(peak * f)}
              </text>
            </g>
          );
        })}

        {labels.map((lab, i) => {
          // Bottom-up, so the biggest band sits on the axis and the eye has a
          // straight edge to compare across buckets.
          let floor = 0;
          return (
            <g key={`${i}-${lab}`}>
              {/* The only way to read the 14th on a thirty-bar chart. No words
                  in it, so nothing here needs translating: a date the server
                  bucketed and a figure. */}
              <title>{`${lab} ${fmtGB(totals[i])}`}</title>
              {series.map((s, si) => {
                const v = s.values[i] ?? 0;
                if (v <= 0) return null;
                const bottom = y(floor);
                floor += v;
                const top = y(floor);
                // One unit of the card's own ground between stacked segments,
                // taken off the BOTTOM so the band still ends where the scale
                // says it does. Never off the lowest segment, which would lift
                // the whole stack off its own axis.
                const h = Math.max(0.6, bottom - top - (si > 0 ? 1 : 0));
                return (
                  <rect
                    key={s.id}
                    x={barX(i)}
                    y={top}
                    width={barW}
                    height={h}
                    fill={bandColour(si)}
                    fillOpacity={bandOpacity(si)}
                  />
                );
              })}
            </g>
          );
        })}

        {/* Both ends of the window, flush with the plot, oldest on the left.
            Only the ends: thirty dates across 556 units is a smear, and the
            bar's own tooltip answers "which day is this one" for everything in
            between. Unlike the ordinate, both ends are printed - the foot of a
            vertical scale is zero by definition, while neither end of a time
            window is anything by definition. */}
        {n > 0 && (
          <text x={PAD_L} y={H - 7} textAnchor="start" fontSize="11" fill="var(--carbon-text-muted)">
            {labels[0]}
          </text>
        )}
        {n > 1 && (
          <text x={W - PAD_R} y={H - 7} textAnchor="end" fontSize="11" fill="var(--carbon-text-muted)">
            {labels[n - 1]}
          </text>
        )}
      </svg>

      {/* No legend for a single band: it would name the one thing the card's own
          title already named, which is furniture rather than a key. */}
      {multi && (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[11px] text-carbon-textMuted">
          {series.map((s, si) => (
            <span key={s.id} className="flex min-w-0 items-center gap-1.5">
              <span
                className="h-2.5 w-2.5 shrink-0 rounded-[var(--radius-control)]"
                style={{ background: bandColour(si), opacity: bandOpacity(si) }}
              />
              <span className="truncate">{s.label}</span>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
