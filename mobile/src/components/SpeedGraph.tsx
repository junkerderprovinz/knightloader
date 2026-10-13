import { useEffect, useRef, useState } from 'react';
import { StyleSheet, View, type LayoutChangeEvent } from 'react-native';
import { useAppearance } from '../theme/AppearanceContext';
import { ACCENTS } from '../theme/appearance';
import { inkFor, NUM, TYPE } from '../theme/tokens';
import { fmtSpeed } from '../api/stats';
import { useT } from '../i18n/I18nContext';
import { Text } from './Text';
import {
  busyOf,
  ceilingOf,
  curve,
  GRACE_TICKS,
  limitMark,
  reading,
  seedingDefault,
  spanLabel,
  type Lane,
  type LimitMark,
  type Point,
} from './speedPlot';

/**
 * The download speed, and what the torrents upload, over the last half minute
 * as the web interface's Downloads page draws them: a filled curve for the
 * download, a line for seeding, the ceiling and the current rate above the
 * plot, the window and a legend below it.
 *
 * Curves from plain views, as short rotated strokes and thin columns, because
 * this app has no react-native-svg and a native module for one graph would
 * mean a new prebuild.
 *
 * A speed limit inside the scale is a red line with its figure, as on the web.
 * A limit the scale leaves out is named in the legend instead of being pinned
 * to the top edge, where it would read as a download running at the limit.
 */

// The web meter's default window: one sample a second for half a minute.
const STEP_MS = 1000;
const SPAN = 30;

// The plot's padding above and below, which the curves stand inside.
const PAD = 4;

// Straight pieces per sample; at a phone's width a few points each, which
// reads as a curve.
const STEPS = 3;

const STROKE = 1.5;

/** The server's record of the last two minutes, oldest first. */
export interface SpeedSeed {
  down: number[];
  up: number[];
}

/** Stroke draws a polyline as one rotated view per piece. */
function Stroke({ points, color }: { points: Point[]; color: string }) {
  return (
    <>
      {points.slice(1).map((q, i) => {
        const p = points[i];
        const dx = q.x - p.x;
        const dy = q.y - p.y;
        // A little longer than the piece, so neighbours overlap at the joints.
        const len = Math.hypot(dx, dy) + STROKE * 0.6;
        return (
          <View
            key={i}
            style={{
              position: 'absolute',
              left: (p.x + q.x) / 2 - len / 2,
              top: (p.y + q.y) / 2 - STROKE / 2,
              width: len,
              height: STROKE,
              borderRadius: STROKE / 2,
              backgroundColor: color,
              transform: [{ rotate: `${Math.atan2(dy, dx)}rad` }],
            }}
          />
        );
      })}
    </>
  );
}

/** Wash fills the ground under a polyline with columns one piece wide. */
function Wash({ points, color, base }: { points: Point[]; color: string; base: number }) {
  return (
    // Faded as one layer, so the overlap between columns does not darken.
    <View
      style={[StyleSheet.absoluteFill, { opacity: 0.26 }]}
      needsOffscreenAlphaCompositing
      pointerEvents="none"
    >
      {points.slice(1).map((q, i) => {
        const p = points[i];
        const top = (p.y + q.y) / 2;
        return (
          <View
            key={i}
            style={{
              position: 'absolute',
              left: p.x,
              width: q.x - p.x + 0.5,
              top,
              height: Math.max(0, base - top),
              backgroundColor: color,
            }}
          />
        );
      })}
    </View>
  );
}

/**
 * LimitLine is the limit across the plot. The figure sits over the line where
 * there is room and under it otherwise, on the card's own ground so a curve
 * behind it does not cut through the digits.
 */
function LimitLine({ limit, mark, height }: { limit: number; mark: LimitMark; height: number }) {
  const { c } = useAppearance();
  const { bottom, labelAbove } = mark;
  return (
    <>
      <View style={[styles.limit, { bottom, backgroundColor: c.statusFailSolid }]} pointerEvents="none" />
      <Text
        style={[
          styles.limitLabel,
          { color: c.statusFailText, backgroundColor: c.surface },
          labelAbove ? { bottom: bottom + 1 } : { top: height - bottom + 1 },
        ]}
        numberOfLines={1}
      >
        {fmtSpeed(limit)}
      </Text>
    </>
  );
}

function Key({ color, label }: { color: string; label: string }) {
  const { c } = useAppearance();
  return (
    <View style={styles.key}>
      <View style={[styles.swatch, { backgroundColor: color }]} />
      <Text style={[styles.tick, { color: c.textMuted }]} numberOfLines={1}>
        {label}
      </Text>
    </View>
  );
}

export default function SpeedGraph({
  speed,
  upload,
  height = 56,
  limit = 0,
  seed,
}: {
  speed: number;
  /** Current torrent upload in bytes/s. Left out, the graph has no seeding line. */
  upload?: number;
  height?: number;
  /** The speed limit in force in bytes/s, 0 for none. */
  limit?: number;
  /** Reads the server's record, so the curve does not start from nothing. */
  seed?: () => Promise<SpeedSeed>;
}) {
  const { t } = useT();
  const { c, dark, accent, accentInk } = useAppearance();
  const seeding = upload !== undefined;
  const [history, setHistory] = useState<SpeedSeed>({ down: [], up: [] });
  const [w, setW] = useState(0);
  // A ref beside the state, so the interval below reads the current readings
  // without being torn down on every new value, which would reset its phase.
  const latest = useRef({ down: speed, up: upload ?? 0 });
  latest.current = { down: speed, up: upload ?? 0 };
  const lanes = useRef<{ down: Lane; up: Lane }>({
    down: { grace: GRACE_TICKS, carry: 0 },
    up: { grace: GRACE_TICKS, carry: 0 },
  });

  useEffect(() => {
    const id = setInterval(() => {
      const down = reading(lanes.current.down, latest.current.down);
      const up = reading(lanes.current.up, latest.current.up);
      setHistory((h) => ({ down: [...h.down, down].slice(-(SPAN + 1)), up: [...h.up, up].slice(-(SPAN + 1)) }));
    }, STEP_MS);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    if (!seed) return;
    let alive = true;
    // Without a record, from an older server or a connection that may not read
    // it, the window fills from live samples alone.
    seed().then(
      (s) => {
        if (!alive) return;
        lanes.current.down = { grace: GRACE_TICKS, carry: s.down[s.down.length - 1] ?? 0 };
        lanes.current.up = { grace: GRACE_TICKS, carry: s.up[s.up.length - 1] ?? 0 };
        setHistory((h) => ({
          down: s.down.concat(h.down).slice(-(SPAN + 1)),
          up: s.up.concat(h.up).slice(-(SPAN + 1)),
        }));
      },
      () => {},
    );
    return () => {
      alive = false;
    };
  }, [seed]);

  const down = history.down;
  const up = seeding ? history.up : [];
  const ceiling = ceilingOf(down.concat(up), limit);
  const mark = limitMark(limit, ceiling, height, PAD, TYPE.caption);
  const busy = busyOf(down, up);
  const base = height - PAD;

  const downInk = accentInk;
  const upHex = seedingDefault(accent, ACCENTS.map((a) => a.hex));
  const upInk = dark ? upHex : inkFor(upHex);

  const downLine = busy && w > 0 ? curve(down, w, height, PAD, SPAN, ceiling, STEPS) : [];
  const upLine = busy && w > 0 && seeding ? curve(up, w, height, PAD, SPAN, ceiling, STEPS) : [];

  const onLayout = (e: LayoutChangeEvent) => {
    const next = Math.round(e.nativeEvent.layout.width);
    if (next !== w) setW(next);
  };

  return (
    <View style={styles.wrap}>
      {/* The ceiling over the plot's start and the current rate over its newest
          end, as on the web meter. */}
      <View style={styles.row}>
        <Text style={[styles.tick, NUM, { color: c.textMuted }]} numberOfLines={1}>
          {fmtSpeed(ceiling)}
        </Text>
        <Text style={[styles.rate, NUM, { color: c.text }]} numberOfLines={1}>
          {fmtSpeed(speed)}
        </Text>
      </View>
      <View style={[styles.plot, { height }]} onLayout={onLayout}>
        <View style={[styles.ground, { top: base, backgroundColor: c.border }]} />
        {busy ? (
          <>
            <Wash points={downLine} color={downInk} base={base} />
            {/* Under the download line, which stays the one to read where the
                two meet. */}
            <Stroke points={upLine} color={upInk} />
            <Stroke points={downLine} color={downInk} />
          </>
        ) : (
          seeding && (
            <View
              style={[styles.ground, { top: base - STROKE / 2, height: STROKE, backgroundColor: upInk }]}
            />
          )
        )}
        {mark && <LimitLine limit={limit} mark={mark} height={height} />}
      </View>
      {/* The time axis, oldest on the left, with the legend between its ends. */}
      <View style={styles.row}>
        <Text style={[styles.tick, NUM, { color: c.textMuted }]}>{spanLabel(SPAN)}</Text>
        <View style={styles.legend}>
          <Key color={downInk} label={t('speed.legend.download')} />
          {seeding && <Key color={upInk} label={t('speed.legend.seeding')} />}
          {limit > 0 && !mark && (
            <Key color={c.statusFailSolid} label={`${t('speed.legend.limit')} ${fmtSpeed(limit)}`} />
          )}
        </View>
        <Text style={[styles.tick, NUM, { color: c.textMuted }]}>0s</Text>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  // The time axis runs left to right in every language, and the labels have to
  // stay at the ends of the plot they name.
  wrap: { gap: 4, direction: 'ltr' },
  row: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 8 },
  tick: { fontSize: TYPE.caption },
  rate: { fontSize: TYPE.dense, fontWeight: '600' },
  plot: { overflow: 'hidden' },
  ground: { position: 'absolute', left: 0, right: 0, height: 1 },
  legend: {
    flexShrink: 1,
    flexDirection: 'row',
    flexWrap: 'wrap',
    justifyContent: 'center',
    columnGap: 12,
    rowGap: 2,
  },
  key: { flexDirection: 'row', alignItems: 'center', gap: 4 },
  swatch: { width: 12, height: 2, borderRadius: 1 },
  limit: { position: 'absolute', left: 0, right: 0, height: 1 },
  limitLabel: {
    position: 'absolute',
    left: 4,
    paddingHorizontal: 2,
    fontSize: TYPE.caption,
    lineHeight: TYPE.caption + 2,
    fontVariant: ['tabular-nums'],
  },
});
