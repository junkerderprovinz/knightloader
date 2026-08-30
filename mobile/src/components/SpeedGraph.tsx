import { useEffect, useRef, useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { useAppearance } from '../theme/AppearanceContext';

/**
 * The download speed over the last minute, as bars.
 *
 * Bars from plain views rather than a charting library or an SVG path: this
 * app has no react-native-svg, and pulling a native module in for a sparkline
 * would mean a new prebuild and a new .apk story (see IconBadge's own Trash
 * for the same call). Bars also degrade honestly - a gap is a sample that was
 * zero, not a line interpolating through it.
 *
 * Scaled to the tallest sample IN THE WINDOW, never to an absolute ceiling: a
 * home connection and a gigabit one both want to see their own shape, and a
 * graph pinned to a fixed maximum shows a flat line to everyone slower than
 * it. The trade-off is that the height means "relative to your own peak", so
 * the number beside it carries the absolute value.
 *
 * Only mounted while something is running (jdp, 2026-08-30: "ein downloadgraph
 * soll die geschwindigkeit anzeigen wenn der download läuft") - an idle graph
 * is a row of nothing that still costs the height of a graph.
 */
const SAMPLES = 40;

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
    }, 1500);
    return () => clearInterval(id);
  }, []);

  const peak = Math.max(1, ...history);
  return (
    <View style={[styles.frame, { height, backgroundColor: c.surface2, borderRadius: radii.control }]}>
      {history.map((v, i) => (
        <View
          key={i}
          style={{
            flex: 1,
            // A floor of 2px so a live-but-slow moment is still a mark rather
            // than a gap indistinguishable from "no sample yet".
            height: Math.max(v > 0 ? 2 : 0, Math.round((v / peak) * (height - 8))),
            backgroundColor: accent,
            borderRadius: 1,
          }}
        />
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  frame: {
    flexDirection: 'row',
    alignItems: 'flex-end',
    gap: 2,
    paddingHorizontal: 6,
    paddingVertical: 4,
    overflow: 'hidden',
  },
});
