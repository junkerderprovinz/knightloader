import { useLayoutEffect, useRef } from 'react';
import { useT, type TranslationKey } from '../../lib/i18n';
import { useTooltip } from '../ui';
import { useStill } from './scene';

/** What the band needs of a schedule. Days are 0 = Sunday to 6 = Saturday. */
export interface WeekEntry {
  name?: string;
  days: number[];
  start: string;
  end: string;
  action: string;
  disabled?: boolean;
}

/** One schedule's hours on one day, in minutes from midnight. */
export interface Stretch {
  entry: number;
  day: number;
  from: number;
  to: number;
  /** Which line of its day it is drawn on, so two that overlap stay apart. */
  track: number;
}

const DAY = 24 * 60;
// The week runs Monday to Sunday.
const WEEK = [1, 2, 3, 4, 5, 6, 0];
const HOURS = [0, 6, 12, 18, 24];
const ACTIONS = ['pause', 'resume', 'limit'];

function minuteOf(clock: string): number | null {
  const m = /^(\d{1,2}):(\d{2})/.exec(clock.trim());
  if (!m) return null;
  const hour = Number(m[1]);
  const minute = Number(m[2]);
  return hour > 23 || minute > 59 ? null : hour * 60 + minute;
}

/**
 * stretches lays the schedules out over the week. A window past midnight
 * belongs to the day it opens on and runs on into the next, as the server
 * reads it, and one that ends when it starts covers nothing.
 */
export function stretches(entries: WeekEntry[]): Stretch[] {
  const out: Stretch[] = [];
  entries.forEach((e, entry) => {
    const from = minuteOf(e.start);
    const to = minuteOf(e.end);
    if (from === null || to === null || from === to) return;
    // A hand-edited settings file can name a day twice or one that does not exist.
    for (const day of new Set(e.days)) {
      if (day < 0 || day > 6) continue;
      if (to > from) {
        out.push({ entry, day, from, to, track: 0 });
        continue;
      }
      out.push({ entry, day, from, to: DAY, track: 0 });
      if (to > 0) out.push({ entry, day: (day + 1) % 7, from: 0, to, track: 0 });
    }
  });

  for (let day = 0; day < 7; day++) {
    const ends: number[] = [];
    for (const s of out.filter((x) => x.day === day).sort((a, b) => a.from - b.from)) {
      let track = ends.findIndex((end) => end <= s.from);
      if (track < 0) track = ends.length;
      ends[track] = s.to;
      s.track = track;
    }
  }
  return out;
}

const share = (minutes: number) => `${((minutes / DAY) * 100).toFixed(3)}%`;

// A pause is neutral like every paused thing, a resume is activity and takes
// the accent, and a limit is the accent at half height. Shape tells the three
// apart where the accent happens to be grey.
function strokeClass(action: string): string {
  if (action === 'pause') return 'h-2 bg-statusNeutralSolid';
  if (action === 'resume') return 'h-2 bg-accentInk';
  if (action === 'limit') return 'my-0.5 h-1 bg-accentInk';
  return 'h-2 border border-carbon-textMuted';
}

/**
 * WeekBand draws the schedules over one week: a lane per day from Monday to
 * Sunday, each schedule as a stretch of its action, and a needle at the
 * present moment. `quiet` is for a timetable that is set aside or switched
 * off, which is drawn but applies to nothing.
 */
export function WeekBand({
  entries,
  dayLabels,
  now,
  nowLabel,
  quiet = false,
  label,
}: {
  entries: WeekEntry[];
  /** The weekdays' short names, 0 = Sunday. */
  dayLabels: string[];
  now: Date;
  nowLabel: string;
  quiet?: boolean;
  label: string;
}) {
  const { t } = useT();
  const still = useStill();
  const band = useRef<HTMLDivElement>(null);
  const all = stretches(entries);

  const actionLabel = (action: string) =>
    ACTIONS.includes(action) ? t(`settings.schedule.action.${action}` as TranslationKey) : action;

  // The stretches draw in once, from the start of their hours.
  useLayoutEffect(() => {
    if (still) return;
    band.current?.querySelectorAll('[data-entry]').forEach((el, i) => {
      el.animate([{ transform: 'scaleX(0)' }, { transform: 'none' }], {
        duration: 420,
        delay: Math.min(i * 30, 480),
        easing: 'cubic-bezier(.2, .8, .3, 1)',
        fill: 'backwards',
      });
    });
  }, [still]);

  const today = now.getDay();
  const nowAt = now.getHours() * 60 + now.getMinutes();

  return (
    <div ref={band} role="img" aria-label={label} className="flex flex-col gap-3">
      <div className="flex flex-col gap-1.5">
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

        {WEEK.map((day) => {
          const mine = all.filter((s) => s.day === day);
          const tracks = Math.max(1, ...mine.map((s) => s.track + 1));
          return (
            <div key={day} data-day={day} className="grid grid-cols-[2.5rem_minmax(0,1fr)] items-center">
              <span className={`text-xs font-semibold ${day === today ? 'text-accentInk' : 'text-carbon-textMuted'}`}>
                {dayLabels[day]}
              </span>
              <div
                className="relative rounded-[var(--radius-control)] bg-carbon-surface2"
                style={{
                  height: tracks * 10 + 6,
                  // A hairline every six hours, under the figures of the axis.
                  backgroundImage:
                    'repeating-linear-gradient(to right, color-mix(in srgb, var(--carbon-text-muted) 28%, transparent) 0 1px, transparent 1px 25%)',
                }}
              >
                {mine.map((s) => {
                  const e = entries[s.entry];
                  return (
                    <StretchMark
                      key={`${s.entry}-${s.from}`}
                      stretch={s}
                      action={e.action}
                      faint={quiet || e.disabled === true}
                      tip={`${e.name?.trim() || actionLabel(e.action)} · ${dayLabels[day]} ${e.start}-${e.end}`}
                    />
                  );
                })}
                {day === today && (
                  <span
                    data-now
                    style={{ insetInlineStart: share(nowAt) }}
                    className="pointer-events-none absolute -inset-y-1 z-10 w-0.5 -translate-x-1/2 rounded-[var(--radius-pill)] bg-carbon-text"
                  >
                    <span className="glim-live absolute -top-1 left-1/2 h-1.5 w-1.5 -translate-x-1/2 rounded-full bg-carbon-text" />
                    <span
                      className={`glim-num absolute top-1/2 -translate-y-1/2 rounded bg-carbon-text px-1 text-[10px] font-bold leading-[14px]
                        text-carbon-surface ${nowAt > DAY * 0.85 ? 'end-1.5' : 'start-1.5'}`}
                    >
                      {nowLabel}
                    </span>
                  </span>
                )}
              </div>
            </div>
          );
        })}
      </div>

      <div className="flex flex-wrap gap-x-4 gap-y-1 ps-10 text-xs text-carbon-textSub">
        {ACTIONS.map((action) => (
          <span key={action} className="inline-flex items-center gap-1.5">
            <span className={`w-4 rounded-[var(--radius-pill)] ${strokeClass(action)}`} />
            {actionLabel(action)}
          </span>
        ))}
      </div>
    </div>
  );
}

// A component of its own because useTooltip is a hook.
function StretchMark({ stretch, action, faint, tip: words }: { stretch: Stretch; action: string; faint: boolean; tip: string }) {
  const tip = useTooltip<HTMLSpanElement>(words);
  // The rows under the band say the same and take the keyboard, so the
  // stretches answer the pointer only.
  const { role: _role, tabIndex: _tabIndex, ...hover } = tip.triggerProps;
  return (
    <>
      <span
        {...hover}
        data-entry={stretch.entry}
        data-action={action}
        style={{
          insetInlineStart: share(stretch.from),
          width: share(stretch.to - stretch.from),
          top: 3 + stretch.track * 10,
        }}
        className={`absolute min-w-1 origin-left rounded-[var(--radius-pill)] rtl:origin-right ${strokeClass(action)} ${
          faint ? 'opacity-40' : ''
        }`}
      />
      {tip.node}
    </>
  );
}
