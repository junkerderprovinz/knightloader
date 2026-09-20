import { rainbowColor } from '../lib/appearance';
import { fmtGB } from '../lib/format';
import { useRainbow } from '../lib/useRainbow';

/** One band of the stack: a hoster, a backend, or the "other" band. */
export interface VolumeSeries {
  id: string;
  label: string;
  /** One figure per bucket, aligned with `labels`. */
  values: number[];
}

// The viewBox. The svg scales uniformly, unlike SpeedGraph's stretched one,
// because it carries axis text that would stretch with it.
const W = 620;
const H = 210;
// Room for the outermost labels. The value axis is printed above the plot, so
// neither side needs a gutter.
const PAD_L = 8;
const PAD_R = 8;
const PAD_T = 10;
const PAD_B = 24;

const PLOT_W = W - PAD_L - PAD_R;
const PLOT_H = H - PAD_T - PAD_B;

// Keeps twelve monthly bars from turning into slabs.
const BAR_MAX = 30;

// Without rainbow mode every band shares the accent, so opacity tells them apart.
const LADDER = [1, 0.78, 0.6, 0.46, 0.35, 0.27];

/**
 * VolumeGraph draws one stacked bar per bucket. Bars, not a curve, because a
 * day is a bucket rather than a sample. The scale tops out at the tallest
 * bucket; SpeedGraph's decaying ceiling needs a tick these buckets never get.
 */
export function VolumeGraph({
  labels,
  series,
  label,
}: {
  /** Formatted bucket labels, oldest first. */
  labels: string[];
  /** Stacked bottom-up in the order given. */
  series: VolumeSeries[];
  label: string;
}) {
  // Subscribed, so band colours follow a palette edit in the same paint.
  const rainbow = useRainbow();

  const n = labels.length;
  const totals = labels.map((_, i) => series.reduce((sum, s) => sum + (s.values[i] ?? 0), 0));
  const peak = Math.max(0, ...totals);

  const scale = peak > 0 ? peak : 1;
  const y = (v: number) => PAD_T + PLOT_H - (v / scale) * PLOT_H;

  const slot = n > 0 ? PLOT_W / n : PLOT_W;
  const barW = Math.min(slot * 0.72, BAR_MAX);
  const barX = (i: number) => PAD_L + slot * i + (slot - barW) / 2;

  const multi = series.length > 1;

  // The palette is read directly rather than through .glim-hue, which reactive
  // rainbow mode greys out until hover; here the colour is the legend. A lone
  // band keeps the single accent.
  const bandColour = (i: number) => (multi ? (rainbowColor(i) ?? 'var(--accent)') : 'var(--accent)');
  const bandOpacity = (i: number) => (!multi || rainbow.on ? 1 : (LADDER[i] ?? 0.2));

  return (
    <div className="flex flex-col gap-3">
      {/* The top of the scale; the foot is zero. Printed in every state. */}
      <span className="glim-num self-end text-[11px] leading-none text-carbon-textMuted">{fmtGB(peak)}</span>
      <svg
        viewBox={`0 0 ${W} ${H}`}
        className="block h-auto w-full"
        role="img"
        aria-label={label}
        focusable="false"
      >
        <line
          x1={PAD_L}
          y1={y(0)}
          x2={W - PAD_R}
          y2={y(0)}
          stroke="var(--carbon-border)"
          strokeWidth="1"
          vectorEffect="non-scaling-stroke"
        />

        {labels.map((lab, i) => {
          let floor = 0;
          return (
            <g key={`${i}-${lab}`}>
              <title>{`${lab} ${fmtGB(totals[i])}`}</title>
              {series.map((s, si) => {
                const v = s.values[i] ?? 0;
                if (v <= 0) return null;
                const bottom = y(floor);
                floor += v;
                const top = y(floor);
                // A one-unit gap under every segment but the lowest, so the
                // stack still stands on the axis.
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

        {/* Only the two ends; each bar's tooltip names its own date. */}
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
