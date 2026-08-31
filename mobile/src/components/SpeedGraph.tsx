import { useEffect, useRef, useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { fmtBytes } from '../api/stats';

/**
 * The download speed over the last minute, as bars, with both axes labelled.
 *
 * Bars from plain views rather than a charting library or an SVG path: this
 * app has no react-native-svg, and pulling a native module in for a sparkline
 * would mean a new prebuild and a new .apk story (see IconBadge's own Trash
 * for the same call). Bars also degrade honestly - a gap is a sample that was
 * zero, not a line interpolating through it.
 *
 * Scaled to the tallest sample IN THE WINDOW, never to an absolute ceiling: a
 * home connection and a gigabit one both want to see their own shape, and a
 * graph pinned to a fixed maximum shows a flat line to everyone slower than it.
 *
 * **Both axes are always drawn** (jdp, 2026-09-01: "die abszisse und ordinate
 * soll immmer angezeigt werden und die geschwindigkeit in kB oder MB"). That is
 * the fix for what the scaling costs: a bar chart whose height means "relative
 * to your own peak" says nothing at all without a number on it, and the number
 * used to live outside the component in a line of text that could disappear.
 * Now the peak is printed on the vertical axis in the unit it deserves and the
 * window length on the horizontal one, so the picture carries its own meaning.
 */
const SAMPLES = 40;
const INTERVALL_MS = 1500;

export default function SpeedGraph({ speed, height = 44 }: { speed: number; height?: number }) {
  const { c, accent, radii } = useAppearance();
  const [history, setHistory] = useState<number[]>([]);
  // A ref beside the state: the interval below must read the CURRENT speed
  // without being torn down and rebuilt on every new value, which would reset
  // its own phase and make the sampling interval a lie.
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
      <View style={styles.plotRow}>
        {/* The ordinate: the window's own peak at the top, zero at the bottom.
            fmtBytes gives kB or MB by size, which is what makes the number
            readable on a slow line and on a fast one. */}
        <View style={[styles.yAxis, { height }]}>
          <Text style={[styles.tick, { color: c.textMuted }]} numberOfLines={1}>
            {`${fmtBytes(peak)}/s`}
          </Text>
          <Text style={[styles.tick, { color: c.textMuted }]} numberOfLines={1}>
            0
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
      </View>
      {/* The abscissa: oldest on the left, now on the right. Indented by the
          y-axis's own width so the two ends line up with the plot, not with the
          labels beside it. */}
      <View style={styles.xAxis}>
        <Text style={[styles.tick, { color: c.textMuted }]}>{`-${fenster}s`}</Text>
        <Text style={[styles.tick, { color: c.textMuted }]}>0s</Text>
      </View>
    </View>
  );
}

const Y_BREITE = 46;

const styles = StyleSheet.create({
  wrap: { gap: 2 },
  plotRow: { flexDirection: 'row', alignItems: 'stretch', gap: 6 },
  yAxis: { width: Y_BREITE, justifyContent: 'space-between', alignItems: 'flex-end' },
  xAxis: { flexDirection: 'row', justifyContent: 'space-between', marginStart: Y_BREITE + 6 },
  tick: { fontSize: TYPE.caption, fontVariant: ['tabular-nums'] },
  frame: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'flex-end',
    gap: 2,
    paddingHorizontal: 6,
    paddingVertical: 4,
    overflow: 'hidden',
  },
});
