import { useT } from '../../lib/i18n';
import { HUE, INK, OK, QUIET, useStage } from './scene';

const RADIUS = 27;
const AROUND = 2 * Math.PI * RADIUS;

/**
 * SeedRing draws a ratio against the seed target and turns green once it is
 * there. A target of 0 means none is set, drawn as an open, dashed track.
 */
export function SeedRing({
  ratio,
  target,
  label,
  size = 56,
}: {
  ratio: number;
  target: number;
  /** What the ring measures, read out with the figures. */
  label: string;
  /** Pixels across. A ring under 40 has no room for the figure in its middle. */
  size?: number;
}) {
  const { stage, still } = useStage();
  const aimed = target > 0;
  const reached = aimed && ratio >= target;
  const arc = aimed ? Math.min(1, ratio / target) * AROUND : 0;
  const dashes = `${arc.toFixed(2)} ${AROUND.toFixed(2)}`;

  return (
    <svg
      ref={stage}
      viewBox="0 0 64 64"
      width={size}
      height={size}
      className="shrink-0"
      data-new="seed-ring"
      role={aimed ? 'progressbar' : 'img'}
      aria-label={aimed ? label : `${label} ${ratio.toFixed(2)}`}
      aria-valuemin={aimed ? 0 : undefined}
      aria-valuemax={aimed ? target : undefined}
      aria-valuenow={aimed ? Math.min(ratio, target) : undefined}
      aria-valuetext={aimed ? `${ratio.toFixed(2)} / ${target.toFixed(2)}` : undefined}
    >
      <g transform="rotate(-90 32 32)" fill="none" strokeWidth="6">
        {/* The track is a wash of the text colour, which shows on a card and on the overview's darker tile alike. */}
        <circle cx="32" cy="32" r={RADIUS} stroke={QUIET} strokeOpacity={aimed ? 0.3 : 0.5} strokeDasharray={aimed ? undefined : '3 6'} />
        {arc > 0 && (
          <circle
            cx="32"
            cy="32"
            r={RADIUS}
            stroke={reached ? OK : HUE}
            strokeLinecap="round"
            strokeDasharray={dashes}
            // A new reading moves the end of the arc instead of jumping.
            style={still ? undefined : { transition: 'stroke-dasharray 600ms ease-out' }}
          >
            {!still && (
              <animate
                attributeName="stroke-dasharray"
                values={`0 ${AROUND.toFixed(2)};${dashes}`}
                keyTimes="0;1"
                calcMode="spline"
                keySplines=".3 0 .2 1"
                dur="0.9s"
              />
            )}
          </circle>
        )}
      </g>
      {size >= 40 && (
        <text x="32" y="37" textAnchor="middle" fontSize="15" fontWeight="700" fill={INK} className="glim-num">
          {ratio.toFixed(2)}
        </text>
      )}
    </svg>
  );
}

/** SeedGauge is the ring with its words beside it: what it measures and the target it measures against. */
export function SeedGauge({ ratio, target }: { ratio: number; target: number }) {
  const { t } = useT();
  return (
    <div className="flex min-w-0 items-center gap-3">
      <SeedRing ratio={ratio} target={target} label={t('columns.ratio')} />
      <div className="min-w-0">
        <div className="glim-eyebrow truncate">{t('columns.ratio')}</div>
        <div className="glim-num mt-1 truncate text-xs text-carbon-textMuted">
          {target > 0 ? t('picture.seed.target', { ratio: target.toFixed(2) }) : t('overview.torrents.noTarget')}
        </div>
      </div>
    </div>
  );
}
