import { useEffect, useRef, useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { fmtSpeed } from '../api/stats';
import { Text } from './Text';

/**
 * The download speed over the last minute, as bars, with both axes labelled.
 *
 * Bars from plain views rather than a charting library or an SVG path: this app
 * has no react-native-svg, and pulling a native module in for a sparkline would
 * mean a new prebuild and a new .apk story. A bar chart also leaves a gap where
 * a sample was zero rather than interpolating a line through it.
 *
 * Scaled to the tallest sample in the window rather than to an absolute
 * ceiling: a home connection and a gigabit one both want to see their own
 * shape, and a fixed maximum shows a flat line to everyone slower than it.
 *
 * Both axes are always drawn, because a bar chart whose height means "relative
 * to your own peak" says nothing without a number on it. The peak is printed on
 * the vertical axis and the window length on the horizontal one.
 *
 * Both labels sit above and below the plot rather than beside it, so the bars
 * span the full width of whatever holds them.
 *
 * A speed limit in force is a red line across the plot with its figure, as on
 * the web UI's curves, and the scale takes it in the same way (scaleTop).
 */
const SAMPLES = 40;
const INTERVALL_MS = 1500;

// The frame's vertical padding, which the bars stand inside.
const PAD = 4;

// The top of the scale sits this far above a limit it takes in.
const HEADROOM = 1.2;

// A limit more than this many times the busiest bar stays out of the scale, or
// a slow download under a generous limit would shrink to a row of stubs.
const REACH = 4;

/**
 * scaleTop is what a full-height bar stands for: the tallest sample, raised to
 * take in the speed limit with room above it. A limit beyond REACH is left out
 * while anything moves; with nothing moving it sets the scale.
 */
export function scaleTop(history: readonly number[], limit: number): number {
  const peak = Math.max(1, ...history);
  if (limit <= 0) return peak;
  const room = limit * HEADROOM;
  const moving = history.some((v) => v > 0);
  if (moving && room > REACH * peak) return peak;
  return Math.max(peak, room);
}

// Enough dashes to cross the widest card at a few points each.
const DASHES = 48;

/**
 * LimitLine is the limit across the plot, dashed and pinned to its top edge
 * when the scale left it out. The figure sits over the line where there is
 * room and under it otherwise, on the plot's own ground so a bar behind it
 * does not cut through the digits.
 */
function LimitLine({ limit, top, height }: { limit: number; top: number; height: number }) {
  const { c } = useAppearance();
  const over = limit > top;
  const plot = height - 2 * PAD;
  const bottom = over ? height - PAD - 1 : PAD + Math.round((limit / top) * plot);
  const labelAbove = height - bottom - 1 >= TYPE.caption + 4;
  return (
    <>
      <View style={[styles.limit, { bottom }]} pointerEvents="none">
        {over ? (
          Array.from({ length: DASHES }, (_, i) => (
            <View key={i} style={[styles.dash, { backgroundColor: c.statusFailSolid }]} />
          ))
        ) : (
          <View style={[styles.solid, { backgroundColor: c.statusFailSolid }]} />
        )}
      </View>
      <Text
        style={[
          styles.limitLabel,
          { color: c.statusFailText, backgroundColor: c.surface2 },
          labelAbove ? { bottom: bottom + 1 } : { top: height - bottom + 1 },
        ]}
        numberOfLines={1}
      >
        {fmtSpeed(limit)}
      </Text>
    </>
  );
}

export default function SpeedGraph({
  speed,
  height = 44,
  limit = 0,
}: {
  speed: number;
  height?: number;
  /** The speed limit in force in bytes/s, 0 for none. */
  limit?: number;
}) {
  const { c, accent, corners } = useAppearance();
  const [history, setHistory] = useState<number[]>([]);
  // A ref beside the state, so the interval below reads the current speed
  // without being torn down and rebuilt on every new value, which would reset
  // its phase and make the sampling interval a lie.
  const latest = useRef(speed);
  latest.current = speed;

  useEffect(() => {
    const id = setInterval(() => {
      setHistory((h) => [...h, latest.current].slice(-SAMPLES));
    }, INTERVALL_MS);
    return () => clearInterval(id);
  }, []);

  const peak = scaleTop(history, limit);
  // The window in whole seconds, from the two constants rather than a third
  // number to keep in step.
  const fenster = Math.round((SAMPLES * INTERVALL_MS) / 1000);

  return (
    <View style={styles.wrap}>
      {/* The ordinate, printed above the plot rather than in a column beside
          it. A label column costs 52 points of width, so the bars would start
          half an inch right of the heading over them and the graph would read
          as narrower than everything else in the card. Above the plot it costs
          one line of height, which the card has.

          Only the maximum is printed: the other end of this axis is the
          baseline of a bar chart, which is zero by definition. */}
      <View style={styles.yAxis}>
        <Text style={[styles.tick, { color: c.textMuted }]} numberOfLines={1}>
          {fmtSpeed(peak)}
        </Text>
      </View>
      <View style={[styles.frame, { height, backgroundColor: c.surface2, ...corners.control }]}>
        {history.map((v, i) => (
          <View
            key={i}
            style={{
              flex: 1,
              // A floor of 2 so a live-but-slow moment is still a mark rather
              // than a gap indistinguishable from "no sample yet".
              height: Math.max(v > 0 ? 2 : 0, Math.round((v / peak) * (height - 2 * PAD))),
              backgroundColor: accent,
              borderRadius: 1,
            }}
          />
        ))}
        {limit > 0 && <LimitLine limit={limit} top={peak} height={height} />}
      </View>
      {/* The abscissa: oldest on the left, now on the right, flush with the
          plot at both ends. */}
      <View style={styles.xAxis}>
        <Text style={[styles.tick, { color: c.textMuted }]}>{`-${fenster}s`}</Text>
        <Text style={[styles.tick, { color: c.textMuted }]}>0s</Text>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: 2 },
  yAxis: { alignItems: 'flex-end' },
  xAxis: { flexDirection: 'row', justifyContent: 'space-between' },
  tick: { fontSize: TYPE.caption, fontVariant: ['tabular-nums'] },
  frame: {
    // No flex: the plot is as wide as whatever holds it, which is the card.
    flexDirection: 'row',
    alignItems: 'flex-end',
    gap: 2,
    paddingHorizontal: 6,
    paddingVertical: PAD,
    overflow: 'hidden',
  },
  limit: {
    position: 'absolute',
    left: 0,
    right: 0,
    height: 1,
    flexDirection: 'row',
    justifyContent: 'space-between',
    overflow: 'hidden',
  },
  solid: { flex: 1 },
  dash: { width: 4 },
  limitLabel: {
    position: 'absolute',
    left: 6,
    paddingHorizontal: 2,
    fontSize: TYPE.caption,
    lineHeight: TYPE.caption + 2,
    fontVariant: ['tabular-nums'],
  },
});
