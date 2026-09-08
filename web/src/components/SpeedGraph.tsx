import { useCallback, useMemo } from 'react';
import { fmtSpeed } from '../lib/format';
import { useT, type TranslationKey } from '../lib/i18n';
import { useUIState } from '../lib/uistate';
import { useSpeedWindow, type SpeedScale } from '../lib/speedHistory';
import { InfoBubble } from './ui';
import { Tabs } from './Tabs';

/**
 * The window both readings draw is no longer kept here.
 *
 * There used to be a `useSpeedSamples` in this file: a component-state buffer
 * created as `Array(points).fill(0)` and advanced by a one-second interval.
 * Everything it did wrong followed from where it lived. Two components could
 * not share it (the comment on SpeedMeter below has said so since it was
 * written), so the hero curve and the shell meter kept two private buffers of
 * the same number; the hero's emptied itself whenever somebody navigated off
 * Overview, because that unmounts it; and every reload started both at a flat
 * line that took a minute to fill in, although the server had been watching the
 * entire time.
 *
 * lib/speedHistory.ts is that buffer hoisted above both components and seeded
 * from GET /api/stats/speed. What is left in this file is drawing.
 */

// How many samples the hour view draws: the server's whole coarse ring, one
// point per ten seconds. Not a prop, because it is not a choice a caller has -
// the hour view exists to show the hour, and a shorter one would be the minute
// view with a wrong label on it.
const HOUR_POINTS = 360;

/**
 * The strings this component needs are not in en.ts yet - the locale files are
 * one writer's lane per wave, and there are 43 of them that must land together
 * or `tsc --noEmit` fails for everybody else in the tree. The lookup asks the
 * real catalogue first, exactly as settings/Diagnostics.tsx does, so the day
 * these keys land this map stops being consulted.
 */
const PENDING = {
  'overview.speedWindow': 'Speed window',
  'overview.speedWindow.minute': 'Last minute',
  'overview.speedWindow.hour': 'Last hour',
  'overview.speedGraphHint':
    'The instance records this curve itself: one sample a second for the last two minutes, one every ten seconds for the last hour. That is why a reload draws it already filled instead of starting flat. The record is held in memory only, so restarting KnightLoader empties it and the curve starts as a flat line again and fills up as it runs.',
} as const;

type PendingKey = keyof typeof PENDING;

function useCx() {
  const { t } = useT();
  return useCallback(
    (key: PendingKey) => {
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      return translated ?? PENDING[key];
    },
    [t],
  );
}

/**
 * ceilingStep is the vertical scale's rule, one sample at a time: up at once,
 * down 8%, never below a floor. Rising instantly keeps a spike inside the box;
 * relaxing slowly stops a brief blip near idle from re-normalising the whole
 * curve into a mountain, and the floor keeps a small transfer looking small
 * instead of filling the frame.
 */
const FLOOR = 64 * 1024;

function ceilingStep(previous: number, value: number): number {
  const target = Math.max(value, FLOOR);
  return target > previous ? target : Math.max(target, previous * 0.92);
}

/**
 * ceilingFor replays that rule across the whole window, from the floor.
 *
 * IT IS A PURE FOLD OVER THE SAMPLES, and that is a fix rather than a
 * refactoring. Both components used to keep the ceiling in a ref and mutate it
 * DURING RENDER (`ceilingRef.current = ceilingFor(ceilingRef.current, peak)`),
 * which made "down 8% a tick" mean "down 8% a render" - and renders happen on
 * every websocket task frame, which under load is many a second, doubled again
 * by StrictMode in development. With a sixty second window of live samples the
 * error was small enough not to be noticed. Seeded with an hour that contains
 * one 300 MB/s burst, the first render would pin the axis at 300 MB/s and then
 * collapse it at whatever rate the task stream happened to be re-rendering at.
 *
 * Folded over the samples instead, the ceiling is a function of what is drawn
 * and nothing else: the same window always produces the same axis, a spike that
 * has scrolled most of the way out of the window has already decayed by the
 * time the newest sample is reached, and there is no render-order dependency
 * left to get wrong.
 */
function ceilingFor(samples: readonly number[]): number {
  let c = FLOOR;
  for (const v of samples) c = ceilingStep(c, v);
  return c;
}

/** smoothPath draws the samples as one curve, scaled into a box. Needs at least two. */
function smoothPath(samples: number[], w: number, h: number, pad: number, max: number) {
  const y = (v: number) => h - pad - (v / max) * (h - pad * 2);
  const pts = samples.map((v, i) => [i * (w / (samples.length - 1)), y(v)] as const);
  let d = `M${pts[0][0]},${pts[0][1].toFixed(1)}`;
  for (let i = 0; i < pts.length - 1; i++) {
    const [x0, y0] = pts[i];
    const [x1, y1] = pts[i + 1];
    const cx = (x0 + x1) / 2;
    d += ` C${cx.toFixed(1)},${y0.toFixed(1)} ${cx.toFixed(1)},${y1.toFixed(1)} ${x1.toFixed(1)},${y1.toFixed(1)}`;
  }
  return { d, last: pts[pts.length - 1] };
}

/**
 * spanLabel is the left end of the abscissa, derived from how much the window
 * actually holds rather than from how much it can hold.
 *
 * A ring that has been recording for forty seconds says `-40s`. The old code
 * printed `points * SAMPLE_MS` and was right only because the buffer was always
 * exactly `points` long, zero-filled; against a real record it would claim an
 * hour of history on an instance that booted a minute ago.
 *
 * The units are fmtEta's own (`s`, then `m`, then `h`), untranslated, for the
 * same reason every other duration in this app is: they are read as symbols
 * beside a number, not as words.
 */
function spanLabel(seconds: number): string {
  if (seconds < 120) return `-${seconds}s`;
  if (seconds < 3600) return `-${Math.round(seconds / 60)}m`;
  return `-${Math.round(seconds / 360) / 10}h`;
}

// SpeedGraph draws the aggregate download speed of this instance over the last
// minute or the last hour. It is the hero of the Overview page.
//
// Two things keep it honest: while nothing is downloading it shows a flat
// hairline rather than a gold smear pinned to the floor, and the vertical scale
// rises instantly but relaxes slowly, so a brief blip near idle doesn't
// re-normalise the whole curve into a mountain.
//
// It is always scope '' - the Overview page reads useTasks('') and nothing
// else. Only SpeedMeter, which sits in the shell and follows whatever instance
// the shell is scoped to, has a scope worth passing.
export function SpeedGraph({
  value,
  height = 96,
  points = 60,
}: {
  value: number;
  height?: number;
  points?: number;
}) {
  const cx = useCx();

  // Remembered, and defaulted to the minute. Somebody who has been running this
  // for two years opens Overview after the upgrade and sees exactly the view
  // they have always had, only already drawn instead of flat; the hour is one
  // press away.
  //
  // useUIState hands out its fallback until the stored value has loaded (see
  // OnboardingWizard.tsx:25), so the first paint after a reload is always the
  // minute view and flips a moment later for somebody who chose the hour. That
  // flip costs nothing here: the store is keyed by instance scope alone, so
  // both scales are already in hand and switching between them never asks the
  // server for anything.
  const [stored, setStored] = useUIState<SpeedScale>('overview.speedWindow', 'minute');
  const scale: SpeedScale = stored === 'hour' ? 'hour' : 'minute';

  const { samples, seconds } = useSpeedWindow('', value, scale === 'hour' ? HOUR_POINTS : points, scale);

  const W = 600;
  const H = height;
  const pad = 6;
  const ceiling = useMemo(() => ceilingFor(samples), [samples]);

  // Fewer than two samples is a genuine state now, and it used to be a crash
  // waiting for a reason. smoothPath divides by `samples.length - 1`, which is
  // Infinity for one sample, and reads pts[0] on an empty array, which is a
  // TypeError. Neither ever fired because the old buffer was always exactly
  // `points` long - the moment the samples come from a record that may hold
  // fewer, both fire on the first second of a cold boot.
  const drawable = samples.length >= 2;
  // `idle` now means "nothing anywhere in this window", not "nothing is
  // downloading right now": after a busy hour the curve is drawn in full while
  // the current speed is 0 B/s, which is the whole point of seeding it.
  const peak = drawable ? Math.max(...samples) : 0;
  const idle = peak === 0;

  return (
    <div className="relative">
      {/* The ordinate, printed ABOVE the plot rather than in a column beside it
          (GlimStone 1.6.0). A vertical scale that follows its own window means
          "tall for you" and nothing else, so the height carries no meaning
          without a number on it - and the number this graph used to show was a
          peak caption that disappeared at idle, which is exactly the case the
          rule was written from: a number a state can remove is not part of the
          chart. It is the ceiling, not the peak, because the ceiling is what
          the top edge of the box actually means.

          Above the plot it costs one line of height, which the card has,
          instead of a fifth of the width, which it does not. The window chooser
          shares that line: it belongs to this plot and to nothing else on the
          page, and a row that already exists is cheaper than a row that does
          not. */}
      <div className="flex items-center justify-between gap-3">
        <Tabs
          label={cx('overview.speedWindow')}
          size="sm"
          select="one"
          active={scale}
          onSelect={(id) => setStored(id === 'hour' ? 'hour' : 'minute')}
          items={[
            { id: 'minute', label: cx('overview.speedWindow.minute') },
            { id: 'hour', label: cx('overview.speedWindow.hour') },
          ]}
        />
        <span className="flex items-center gap-1.5">
          {/* The one place the two questions this feature creates are answered
              in front of the person asking them: where the already-drawn curve
              came from, and why it is flat again after a restart. */}
          <InfoBubble tip={cx('overview.speedGraphHint')} />
          <span className="glim-num text-[11px] leading-none text-carbon-textMuted">{fmtSpeed(ceiling)}</span>
        </span>
      </div>
      <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="block w-full" style={{ height }} aria-hidden>
        {!drawable || idle ? (
          <line
            x1="0"
            y1={H - pad}
            x2={W}
            y2={H - pad}
            stroke="var(--carbon-border)"
            strokeWidth="1"
            vectorEffect="non-scaling-stroke"
          />
        ) : (
          <Curve samples={samples} w={W} h={H} pad={pad} ceiling={ceiling} />
        )}
      </svg>
      {/* The abscissa: oldest on the left, now on the right, flush with the plot
          at both ends because there is nothing beside the plot to indent past.
          Both ends are printed here, unlike the ordinate: the bottom of the
          vertical axis is zero by definition, while neither end of a time
          window is.

          What used to sit here was a peak in the top-right corner, shown only
          while something was downloading. The idle caption before it was
          removed for restating what a flat line already says (jdp, 2026-09-01:
          "der ruhig text soll weg"), and that stays right - the axis is a
          number, not a sentence. */}
      <div className="flex justify-between">
        <span className="glim-num text-[11px] leading-none text-carbon-textMuted">{spanLabel(seconds)}</span>
        <span className="glim-num text-[11px] leading-none text-carbon-textMuted">0s</span>
      </div>
    </div>
  );
}

/**
 * Curve is the filled area, the line and the live dot, split out only so that
 * smoothPath is called from one place that has already established there are at
 * least two samples to draw. Inlined it would have been guarded twice, and one
 * of the two would have drifted.
 */
function Curve({
  samples,
  w,
  h,
  pad,
  ceiling,
}: {
  samples: number[];
  w: number;
  h: number;
  pad: number;
  ceiling: number;
}) {
  const { d, last } = smoothPath(samples, w, h, pad, ceiling);
  return (
    <>
      <defs>
        <linearGradient id="glim-speed-fill" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="var(--accent)" stopOpacity="0.26" />
          <stop offset="100%" stopColor="var(--accent)" stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={`${d} L${w},${h} L0,${h} Z`} fill="url(#glim-speed-fill)" />
      <path
        d={d}
        fill="none"
        stroke="var(--accent)"
        strokeWidth="1.75"
        strokeLinecap="round"
        vectorEffect="non-scaling-stroke"
      />
      <circle cx={last[0]} cy={last[1]} r="3" fill="var(--accent)" className="glim-live" />
    </>
  );
}

/**
 * SpeedMeter is the same reading at shell-bar size: the figure, with the last
 * half-minute of it drawn behind.
 *
 * `onOpen` makes the whole thing a button, and that is the point rather than a
 * convenience. Somebody watching the speed is already looking at the thing they
 * want to change, and making them find a gear two controls away is asking them
 * to leave what they are reading. The gear stays as well, for anyone who never
 * discovers that a number can be pressed.
 *
 * It no longer keeps a window of its own. That was never a choice: two
 * components cannot share one rolling buffer without hoisting it above both,
 * and until lib/speedHistory.ts existed there was nowhere to hoist it to. Now
 * the meter and the Overview hero read the same store, tick on the same clock,
 * and are seeded from the same fetch.
 *
 * `instance` is the scope the SHELL is on, which can be a peer - ShellStrip
 * reads useTasks(instance) and the figure beside this curve describes that peer.
 * It has to be passed through, because the store seeds scope '' and only scope
 * '': /api/stats/speed is on neither forwarding allowlist, so a peer scope that
 * defaulted to '' would draw THIS box's last hour underneath a number
 * describing somebody else's. A peer therefore starts empty and fills as it
 * goes, which is what it did before and is the only honest answer available.
 *
 * There is no window chooser here. The shell bar is a reading, not a control
 * surface, and the minute is the only window that fits behind a figure this
 * size.
 */
export function SpeedMeter({
  value,
  points = 30,
  instance = '',
}: {
  value: number;
  points?: number;
  instance?: string;
}) {
  const { samples, seconds } = useSpeedWindow(instance, value, points, 'minute');

  // The plot fills whatever height AND width the card gives it (jdp,
  // 2026-09-06: "der downloadgraph soll in der kopfcard ganz rechts sein und
  // auch in der höhe die ganze kopfcard ausfüllen", and 2026-09-07: "der
  // downloadgraph in der kopfzeile soll viel breiter sein"), so the viewBox is
  // a coordinate system and not a size: preserveAspectRatio="none" plus a
  // stretching svg lets one path describe a box of any shape.
  //
  // W is now only the resolution the path is drawn AT, not the width it is
  // drawn at. The svg itself grows, so a wider card means a longer curve rather
  // than a stretched one - and 148 stays as the number of horizontal units
  // because the sample count has not changed.
  const W = 148;
  const H = 40;
  const ceiling = useMemo(() => ceilingFor(samples), [samples]);
  // Same two guards as the hero above, and for the same reasons - see there.
  const drawable = samples.length >= 2;
  const peak = drawable ? Math.max(...samples) : 0;
  const idle = peak === 0;

  // Nothing to press (jdp, 2026-09-06: "klick auf den downloadgraph soll keine
  // funktione haben"). It used to open the quick-settings panel, which the
  // hamburger beside it still does - a reading that also acts is a reading
  // somebody triggers while trying to look at it.
  return (
    <span className="flex h-full flex-1 items-stretch gap-2 px-1.5">
      {/* Both axes, always, idle included - the case the rule was written from
          is exactly a number that a state can take away. The ordinate is the
          ceiling, because that is what the top edge of this box means. */}
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="glim-num self-end text-[10px] leading-none text-carbon-textMuted">
          {fmtSpeed(ceiling)}
        </span>
        <svg
          viewBox={`0 0 ${W} ${H}`}
          preserveAspectRatio="none"
          className="min-h-[26px] w-full flex-1"
          aria-hidden
          focusable="false"
        >
          {!drawable || idle ? (
            <line
              x1="0"
              y1={H - 2}
              x2={W}
              y2={H - 2}
              stroke="var(--carbon-border)"
              strokeWidth="1"
              vectorEffect="non-scaling-stroke"
            />
          ) : (
            <path
              d={smoothPath(samples, W, H, 2, ceiling).d}
              fill="none"
              stroke="var(--accent)"
              strokeWidth="1.5"
              strokeLinecap="round"
              vectorEffect="non-scaling-stroke"
            />
          )}
        </svg>
        <span className="flex justify-between text-[10px] leading-none text-carbon-textMuted">
          <span className="glim-num">{spanLabel(seconds)}</span>
          <span className="glim-num">0s</span>
        </span>
      </span>
      {/* dir="ltr": the number and its unit are one token and must not be
          reordered into "s/BiM 4.2" in an Arabic or Hebrew locale. */}
      <span
        dir="ltr"
        className="glim-num flex items-center text-[13px] font-semibold leading-none text-carbon-text"
      >
        {fmtSpeed(value) || '0 B/s'}
      </span>
    </span>
  );
}
