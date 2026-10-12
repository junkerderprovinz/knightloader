import { useLayoutEffect, useRef } from 'react';
import { fmtSpeed } from '../../lib/format';
import { useT } from '../../lib/i18n';
import { alignedUpload, type SpeedDay } from '../../lib/speedHistory';
import { DOWNLOAD_INK, SEEDING_INK } from '../../lib/speedLines';
import { INK, useStill } from './scene';
import { stretches, type WeekEntry } from './WeekBand';

/** What the plan needs of a schedule: the band's fields and the limit of a speed window. */
export interface PlanEntry extends WeekEntry {
  /** Bytes a second for a limit window; 0 lifts every limit. */
  limit?: number;
}

/** A stretch of one day in which the timetable asks for the same thing, in minutes from midnight. */
export interface PlanStretch {
  from: number;
  to: number;
  paused: boolean;
  /** Bytes a second, 0 for no limit. */
  limit: number;
}

/**
 * One measured stretch of today: the minute it ended at, its mean speed and
 * the mean the torrents sent meanwhile, in bytes a second.
 */
export interface DayPoint {
  at: number;
  speed: number;
  up?: number;
}

const DAY = 24 * 60;
const HOURS = [0, 6, 12, 18, 24];

/**
 * planOf is what the timetable asks for over one weekday, as schedule.At reads
 * it: every window that covers a minute applies in order, the last one wins,
 * and each action touches only its own field. `base` is the limit set by hand,
 * which holds where no speed window is open, and `quietLimit` what a quiet
 * window puts in its place, 0 for none.
 */
export function planOf(entries: PlanEntry[], weekday: number, base: number, quietLimit = 0): PlanStretch[] {
  const today = stretches(entries).filter((s) => s.day === weekday && !entries[s.entry].disabled);
  const edges = [...new Set([0, DAY, ...today.flatMap((s) => [s.from, s.to])])].sort((a, b) => a - b);

  const plan: PlanStretch[] = [];
  edges.slice(0, -1).forEach((from, i) => {
    let paused = false;
    let limit = base;
    let quiet = false;
    for (const s of today) {
      if (s.from > from || from >= s.to) continue;
      const entry = entries[s.entry];
      if (entry.action === 'pause') paused = true;
      else if (entry.action === 'resume') paused = false;
      else if (entry.action === 'limit') limit = entry.limit ?? 0;
      else if (entry.action === 'quiet') quiet = true;
    }
    if (quiet && quietLimit > 0) limit = quietLimit;

    const last = plan[plan.length - 1];
    if (last && last.paused === paused && last.limit === limit) last.to = edges[i + 1];
    else plan.push({ from, to: edges[i + 1], paused, limit });
  });
  return plan;
}

/**
 * todayOf puts the samples of a day's record on today's clock and leaves out
 * what was measured before midnight.
 */
export function todayOf(day: SpeedDay, now: Date): DayPoint[] {
  if (!day.from) return [];
  const midnight = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const first = new Date(day.from).getTime();
  const up = alignedUpload(day.samplesUp, day.samples.length);
  const points: DayPoint[] = [];
  day.samples.forEach((speed, i) => {
    const end = first + (i + 1) * day.step * 1000;
    if (end > midnight && end <= now.getTime()) points.push({ at: (end - midnight) / 60_000, speed, up: up[i] });
  });
  return points;
}

const share = (minutes: number) => `${((minutes / DAY) * 100).toFixed(3)}%`;
const clock = (minutes: number) =>
  `${String(Math.floor(minutes / 60) % 24).padStart(2, '0')}:${String(minutes % 60).padStart(2, '0')}`;

// The plot's own units: a minute wide and a hundred high.
const HEIGHT = 100;
// Minutes one sample covers, and how far apart two may end and still be one line.
const SAMPLE = 5;
const JOINED = 6;

/**
 * DayCurve draws today against the timetable: the limit it asks for as an
 * area, a pause as a bar on the ground, the speed measured so far as a line
 * over both with what the torrents uploaded beside it, and a hand on the
 * present moment. `quiet` is for a timetable that
 * is set aside or switched off, whose plan applies to nothing.
 */
export function DayCurve({
  plan,
  points,
  now,
  dayLabel,
  quiet = false,
  label,
}: {
  plan: PlanStretch[];
  points: DayPoint[];
  now: Date;
  /** Today's short name, as the week band writes it. */
  dayLabel: string;
  quiet?: boolean;
  label: string;
}) {
  const { t } = useT();
  const still = useStill();
  const line = useRef<SVGGElement>(null);

  const nowAt = now.getHours() * 60 + now.getMinutes();
  // The tallest thing drawn stands at the top, and its figure beside it.
  const ceiling = Math.max(0, ...points.map((p) => Math.max(p.speed, p.up ?? 0)), ...plan.map((s) => s.limit));
  const y = (speed: number) => (ceiling > 0 ? HEIGHT - (speed / ceiling) * HEIGHT : HEIGHT).toFixed(2);

  // A mean runs level over the minutes it was taken in, and steps to the next.
  const path = (of: (p: DayPoint) => number) =>
    points
      .map((p, i) => {
        const joins = i > 0 && points[i - 1].at > p.at - JOINED;
        const from = joins ? points[i - 1].at : Math.max(0, p.at - SAMPLE);
        return `${joins ? 'L' : 'M'}${from.toFixed(1)} ${y(of(p))}H${p.at.toFixed(1)}`;
      })
      .join('');
  const curve = path((p) => p.speed);
  const measured = curve !== '';
  // The torrents' upload, along the ground while they send nothing.
  const upload = path((p) => p.up ?? 0);

  // The line draws in once, from midnight on. A new sample extends it without drawing it again.
  useLayoutEffect(() => {
    if (still || !measured) return;
    const drawn = line.current!.animate([{ transform: 'scaleX(0)' }, { transform: 'none' }], {
      duration: 700,
      easing: 'cubic-bezier(.2, .8, .3, 1)',
    });
    return () => drawn.cancel();
  }, [still, measured]);

  const latest = points[points.length - 1];
  const said = [
    ...plan
      .filter((s) => s.paused || s.limit > 0)
      .map(
        (s) => `${clock(s.from)}-${clock(s.to)} ${s.paused ? t('settings.schedule.action.pause') : fmtSpeed(s.limit)}`,
      ),
    ...(latest ? [`${t('columns.speed')} ${clock(Math.round(latest.at))}: ${fmtSpeed(latest.speed) || '0'}`] : []),
    ...(latest ? [`${t('speed.legend.seeding')}: ${fmtSpeed(latest.up ?? 0) || '0'}`] : []),
  ];
  // Where the record begins. It is kept in memory, so it starts again with the server.
  const since = measured ? Math.max(0, Math.round(points[0].at) - SAMPLE) : nowAt;
  const faint = quiet ? 'opacity-40' : '';

  return (
    <div role="img" aria-label={`${label}: ${said.join(', ')}`} data-new="day-curve" className="flex flex-col gap-1.5">
      <div className="grid grid-cols-[2.5rem_minmax(0,1fr)] items-center">
        <span className="text-xs font-semibold text-accentInk">{dayLabel}</span>
        <div
          className="relative h-20 rounded-[var(--radius-control)] bg-carbon-surface2"
          style={{
            // The band's hairlines, so the two read on one clock.
            backgroundImage:
              'repeating-linear-gradient(to right, color-mix(in srgb, var(--carbon-text-muted) 28%, transparent) 0 1px, transparent 1px 25%)',
          }}
        >
          {/* Under the plot, so the line stays in sight where it runs along the ground. */}
          {plan
            .filter((s) => s.paused)
            .map((s) => (
              <span
                key={s.from}
                data-plan="pause"
                style={{ insetInlineStart: share(s.from), width: share(s.to - s.from) }}
                className={`absolute bottom-0 h-2 rounded-[var(--radius-pill)] bg-statusNeutralSolid ${faint}`}
              />
            ))}
          <svg
            viewBox={`0 0 ${DAY} ${HEIGHT}`}
            preserveAspectRatio="none"
            // The lane's other parts follow the page's direction, so the plot does too.
            className="absolute inset-0 h-full w-full overflow-visible rtl:-scale-x-100"
            aria-hidden
          >
            <g className={faint}>
              {/* In the text's ink, since the measured line takes the accent
                  by default and runs along the limit while it holds. */}
              {plan
                .filter((s) => s.limit > 0 && !s.paused)
                .map((s) => (
                  <g key={s.from} data-plan="limit" data-from={s.from} data-to={s.to}>
                    <path d={`M${s.from} ${HEIGHT}V${y(s.limit)}H${s.to}V${HEIGHT}z`} fill={INK} fillOpacity=".16" />
                    <path
                      d={`M${s.from} ${y(s.limit)}H${s.to}`}
                      fill="none"
                      stroke={INK}
                      strokeWidth="1.5"
                      vectorEffect="non-scaling-stroke"
                    />
                  </g>
                ))}
            </g>
            {measured && (
              <g ref={line}>
                <path
                  data-upload
                  d={upload}
                  fill="none"
                  stroke={SEEDING_INK}
                  strokeWidth="1.5"
                  strokeLinejoin="round"
                  strokeLinecap="round"
                  vectorEffect="non-scaling-stroke"
                />
                <path
                  data-measured
                  d={curve}
                  fill="none"
                  stroke={DOWNLOAD_INK}
                  strokeWidth="1.75"
                  strokeLinejoin="round"
                  strokeLinecap="round"
                  vectorEffect="non-scaling-stroke"
                />
              </g>
            )}
          </svg>
          {ceiling > 0 && (
            <span data-ceiling className="glim-num absolute end-1.5 top-1 text-meta leading-none text-carbon-textMuted">
              {fmtSpeed(ceiling)}
            </span>
          )}
          <span
            data-now
            style={{ insetInlineStart: share(nowAt) }}
            className="pointer-events-none absolute -inset-y-1 z-10 w-0.5 -translate-x-1/2 rounded-[var(--radius-pill)] bg-carbon-text"
          >
            <span className="glim-live absolute -top-1 left-1/2 h-1.5 w-1.5 -translate-x-1/2 rounded-[var(--radius-pill)] bg-carbon-text" />
          </span>
        </div>
      </div>

      <div className="grid grid-cols-[2.5rem_minmax(0,1fr)]">
        <span />
        <div className="relative h-4">
          {HOURS.map((hour) => (
            <span
              key={hour}
              style={{ insetInlineStart: share(hour * 60) }}
              className={`glim-num absolute top-0 text-meta leading-4 text-carbon-textMuted ${
                hour === 0 ? '' : hour === 24 ? '-translate-x-full rtl:translate-x-full' : '-translate-x-1/2 rtl:translate-x-1/2'
              }`}
            >
              {String(hour).padStart(2, '0')}
            </span>
          ))}
        </div>
      </div>

      <div className="flex flex-wrap gap-x-4 gap-y-1 ps-10 text-xs text-carbon-textSub">
        <span className="inline-flex items-center gap-1.5">
          <span
            className="h-2.5 w-4 rounded-[var(--radius-control)] border-t-[1.5px]"
            style={{
              borderColor: INK,
              backgroundColor: `color-mix(in srgb, ${INK} 16%, transparent)`,
            }}
          />
          {t('settings.schedule.limit')}
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="h-0.5 w-4 rounded-[var(--radius-pill)]" style={{ backgroundColor: DOWNLOAD_INK }} />
          {t('speed.legend.download')}
        </span>
        <span data-upload-key className="inline-flex items-center gap-1.5">
          <span className="h-0.5 w-4 rounded-[var(--radius-pill)]" style={{ backgroundColor: SEEDING_INK }} />
          {t('speed.legend.seeding')}
        </span>
        {since > 0 && (
          <span data-since className="text-carbon-textMuted">
            {t('picture.day.since', { time: clock(since) })}
          </span>
        )}
      </div>
    </div>
  );
}
