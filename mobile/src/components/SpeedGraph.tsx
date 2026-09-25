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
 */
const SAMPLES = 40;
const INTERVALL_MS = 1500;

export default function SpeedGraph({ speed, height = 44 }: { speed: number; height?: number }) {
  const { c, accent, radii } = useAppearance();
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

  const peak = Math.max(1, ...history);
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
      <View style={[styles.frame, { height, backgroundColor: c.surface2, borderRadius: radii.control }]}>
        {history.map((v, i) => (
          <View
            key={i}
            style={{
              flex: 1,
              // A floor of 2 so a live-but-slow moment is still a mark rather
              // than a gap indistinguishable from "no sample yet".
              height: Math.max(v > 0 ? 2 : 0, Math.round((v / peak) * (height - 8))),
              backgroundColor: accent,
              borderRadius: 1,
            }}
          />
        ))}
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
    paddingVertical: 4,
    overflow: 'hidden',
  },
});
