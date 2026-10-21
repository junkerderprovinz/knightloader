import { Fragment, memo, useEffect, useMemo, useRef, useState } from 'react';
import { Animated, Easing, StyleSheet, View, type LayoutChangeEvent } from 'react-native';
import { useAppearance } from '../theme/AppearanceContext';
import { useMotion } from '../theme/MotionContext';
import { ACCENTS } from '../theme/appearance';
import { inkFor, NUM, TYPE } from '../theme/tokens';
import { fmtSpeed } from '../api/stats';
import { useT } from '../i18n/I18nContext';
import { Text } from './Text';
import { boxForInk, SpeedLimit } from './IconBadge';
import {
  busyOf,
  ceilingOf,
  curve,
  labelTops,
  limitMark,
  newFeed,
  seedFeed,
  seedingDefault,
  spanLabel,
  tick,
  washOf,
  type LimitMark,
  type Point,
  type Samples,
} from './speedPlot';

/**
 * The download speed, and what the torrents upload, over the last half minute
 * as the web interface's Downloads page draws them: a filled curve for each in
 * its own colour, the ceiling and the current rates above the plot, the window
 * and a legend below it. Colour means a live value: the rates over the plot are
 * in their curves' colours, the limits' marks are not.
 *
 * Curves from plain views, as short rotated strokes and thin columns, because
 * this app has no react-native-svg and a native module for one graph would
 * mean a new prebuild.
 *
 * The download and upload limits are marked at the right edge as on the web,
 * with no line across the plot: inside the scale a notch and the figure at the
 * limit's height, above it the figure and a chevron pointing up on the top edge.
 */

// The web meter's default window: one sample a second for half a minute.
const STEP_MS = 1000;
const SPAN = 30;

// Samples kept beyond the span on screen. While the curve glides the newest
// waits past the right edge and one more hangs past the left, so the plot
// never shows an end of the curve as it moves.
const KEEP = SPAN + 2;

// The plot's padding above and below, which the curves stand inside.
const PAD = 4;

// Straight pieces per sample; at a phone's width a few points each, which
// reads as a curve.
const STEPS = 3;

const STROKE = 1.5;

// How strongly a filled curve's area shows, light enough that the download's
// still reads through the seeding's laid over it.
const WASH = 0.26;

const NO_SAMPLES: number[] = [];
const NO_POINTS: Point[] = [];

/** Stroke draws a polyline as one rotated view per piece. */
const Stroke = memo(function Stroke({ points, color }: { points: Point[]; color: string }) {
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
});

/** Wash fills the ground under a polyline with columns one piece wide. */
const Wash = memo(function Wash({ points, color, base }: { points: Point[]; color: string; base: number }) {
  const cols = useMemo(() => washOf(points, base), [points, base]);
  return (
    // Faded as one layer, so the overlap between columns does not darken.
    <View style={[StyleSheet.absoluteFill, { opacity: WASH }]} needsOffscreenAlphaCompositing pointerEvents="none">
      {cols.map((col, i) => (
        <View key={i} style={{ position: 'absolute', ...col, backgroundColor: color }} />
      ))}
    </View>
  );
});

// The figure beside a limit's mark is this tall, the glyph at the right edge
// this wide on each side, and the sign before the figure sized to this much ink.
const LABEL_LINE = TYPE.caption + 2;
const GLYPH = 4;
const SIGN = boxForInk(LABEL_LINE - 3);

/** One limit as LimitMarks draws it. */
interface Marked {
  limit: number;
  mark: LimitMark;
  name: string;
}

/**
 * LimitMarks draws each limit set as a notch at the right edge and a limit sign
 * with the figure beside it, with no line across the plot, which two limits and
 * two curves would crowd. A limit above the scale sits on the top edge with a
 * chevron pointing up. The marks are muted, since a curve's colour stands for a
 * live value. The figures sit on the card's own ground so a curve behind them
 * does not cut through the digits.
 */
function LimitMarks({ marks, height }: { marks: Marked[]; height: number }) {
  const { c } = useAppearance();
  const ink = c.textMuted;
  const tops = labelTops(marks.map((m) => m.mark.y), height, LABEL_LINE);
  return marks.map(({ limit, mark, name }, i) => (
    <Fragment key={name}>
      <View
        style={[styles.glyph, mark.over ? { top: mark.y, ...chevron(ink) } : { top: mark.y - GLYPH, ...notch(ink) }]}
        pointerEvents="none"
      />
      <View style={[styles.limitLabel, { top: tops[i], backgroundColor: c.surface }]} pointerEvents="none">
        <SpeedLimit color={ink} size={SIGN} />
        <Text style={[styles.limitFigure, { color: ink }]} numberOfLines={1} accessibilityLabel={name}>
          {fmtSpeed(limit)}
        </Text>
      </View>
    </Fragment>
  ));
}

// Triangles from borders, since the app draws without svg: one pointing left
// into the plot, and one pointing up out of it.
const notch = (ink: string) => ({
  borderTopWidth: GLYPH,
  borderBottomWidth: GLYPH,
  borderRightWidth: GLYPH,
  borderTopColor: 'transparent',
  borderBottomColor: 'transparent',
  borderRightColor: ink,
});

const chevron = (ink: string) => ({
  borderLeftWidth: GLYPH,
  borderRightWidth: GLYPH,
  borderBottomWidth: GLYPH,
  borderLeftColor: 'transparent',
  borderRightColor: 'transparent',
  borderBottomColor: ink,
});

/** Key names one curve, its swatch in the look of a filled curve. */
function Key({ color, label }: { color: string; label: string }) {
  const { c } = useAppearance();
  return (
    <View style={styles.key}>
      <View style={[styles.swatch, { borderTopColor: color }]}>
        <View style={[StyleSheet.absoluteFill, { backgroundColor: color, opacity: WASH }]} />
      </View>
      <Text style={[styles.tick, { color: c.textMuted }]} numberOfLines={1}>
        {label}
      </Text>
    </View>
  );
}

function SpeedGraph({
  speed,
  upload,
  height = 56,
  limit = 0,
  uploadLimit = 0,
  seed,
}: {
  speed: number;
  /** Current torrent upload in bytes/s. Left out, the graph has no seeding line. */
  upload?: number;
  height?: number;
  /** The speed limit in force in bytes/s, 0 for none. */
  limit?: number;
  /** The torrents' upload limit in bytes/s, 0 for none. */
  uploadLimit?: number;
  /** Reads the server's record of the last two minutes, so the curve does not
   *  start from nothing. */
  seed?: () => Promise<Samples>;
}) {
  const { t } = useT();
  const { c, dark, accent, accentInk } = useAppearance();
  const seeding = upload !== undefined;
  const feed = useRef(newFeed(seed !== undefined));
  const [history, setHistory] = useState(feed.current.samples);
  const [w, setW] = useState(0);
  // A ref beside the state, so the interval below reads the current readings
  // without being torn down on every new value, which would reset its phase.
  const latest = useRef({ down: speed, up: upload ?? 0 });
  latest.current = { down: speed, up: upload ?? 0 };

  useEffect(() => {
    const id = setInterval(() => {
      if (tick(feed.current, latest.current.down, latest.current.up, KEEP)) setHistory(feed.current.samples);
    }, STEP_MS);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    const f = feed.current;
    if (!seed) {
      f.waiting = false;
      return;
    }
    f.waiting = true;
    let alive = true;
    // Without a record, from an older server or a connection that may not read
    // it, the window fills from live samples alone.
    seed().then(
      (s) => {
        if (!alive) return;
        seedFeed(f, s, KEEP);
        setHistory(f.samples);
      },
      () => {
        if (alive) f.waiting = false;
      },
    );
    return () => {
      alive = false;
    };
  }, [seed]);

  // The curve glides left at a steady pace between samples instead of hopping
  // a whole step as each one arrives, as the web meter does. The gate is the
  // web's too: not at the quiet level, and not while the system asks for less
  // motion, the storm included, since the glide never stops.
  const { motion, reduced } = useMotion();
  const glide = motion !== 'off' && !reduced;
  const lead = glide ? 1 : 0;

  const down = history.down;
  const up = seeding ? history.up : NO_SAMPLES;
  // What the plot shows, the sample about to glide in included, so it fits
  // under the ceiling before it appears.
  const shownDown = useMemo(() => down.slice(-(SPAN + 1 + lead)), [down, lead]);
  const shownUp = useMemo(() => up.slice(-(SPAN + 1 + lead)), [up, lead]);
  const ceiling = useMemo(
    () => ceilingOf(shownDown.concat(shownUp), limit, uploadLimit),
    [shownDown, shownUp, limit, uploadLimit],
  );
  const busy = busyOf(shownDown, shownUp);
  const base = height - PAD;
  const dx = w / SPAN;

  // A new value per sample, so the new drawing and its offset of one step
  // reach the screen in the same frame. The step then runs out on the native
  // thread.
  const offset = useMemo(() => new Animated.Value(glide ? dx : 0), [history, glide, dx]);
  useEffect(() => {
    if (!glide) return;
    const run = Animated.timing(offset, { toValue: 0, duration: STEP_MS, easing: Easing.linear, useNativeDriver: true });
    run.start();
    return () => run.stop();
  }, [offset, glide]);

  const downInk = accentInk;
  const upHex = seedingDefault(accent, ACCENTS.map((a) => a.hex));
  const upInk = dark ? upHex : inkFor(upHex);
  const marks = [
    { limit, name: t('speed.limit.download', { speed: fmtSpeed(limit) }) },
    { limit: uploadLimit, name: t('speed.limit.upload', { speed: fmtSpeed(uploadLimit) }) },
  ].flatMap((m) => {
    const mark = limitMark(m.limit, ceiling, height, PAD);
    return mark ? [{ ...m, mark }] : [];
  });

  const downLine = useMemo(
    () => (busy && w > 0 ? curve(down, w, height, PAD, SPAN, ceiling, STEPS, lead) : NO_POINTS),
    [busy, down, w, height, ceiling, lead],
  );
  const upLine = useMemo(
    () => (busy && w > 0 && seeding ? curve(up, w, height, PAD, SPAN, ceiling, STEPS, lead) : NO_POINTS),
    [busy, seeding, up, w, height, ceiling, lead],
  );

  const onLayout = (e: LayoutChangeEvent) => {
    const next = Math.round(e.nativeEvent.layout.width);
    if (next !== w) setW(next);
  };

  return (
    <View style={styles.wrap}>
      {/* The ceiling over the plot's start and the current rates over its
          newest end, each in its curve's colour, as on the web meter. */}
      <View style={styles.row}>
        <Text style={[styles.tick, NUM, { color: c.textMuted }]} numberOfLines={1}>
          {fmtSpeed(ceiling)}
        </Text>
        <View style={styles.rates}>
          <Text
            style={[styles.rate, NUM, { color: downInk }]}
            numberOfLines={1}
            accessibilityLabel={`${t('speed.legend.download')}: ${fmtSpeed(speed)}`}
          >
            {fmtSpeed(speed)}
          </Text>
          {seeding && (
            <Text
              style={[styles.rate, NUM, { color: upInk }]}
              numberOfLines={1}
              accessibilityLabel={`${t('speed.legend.seeding')}: ${fmtSpeed(upload)}`}
            >
              {fmtSpeed(upload)}
            </Text>
          )}
        </View>
      </View>
      <View style={[styles.plot, { height }]} onLayout={onLayout}>
        <View style={[styles.ground, { top: base, backgroundColor: c.border }]} />
        {busy ? (
          <Animated.View style={[StyleSheet.absoluteFill, { transform: [{ translateX: offset }] }]} pointerEvents="none">
            <Wash points={downLine} color={downInk} base={base} />
            {/* Over the download's wash, which still shows through it where the
                two overlap. */}
            <Wash points={upLine} color={upInk} base={base} />
            {/* Under the download line, which stays the one to read where the
                two meet. */}
            <Stroke points={upLine} color={upInk} />
            <Stroke points={downLine} color={downInk} />
          </Animated.View>
        ) : (
          seeding && (
            <View
              style={[styles.ground, { top: base - STROKE / 2, height: STROKE, backgroundColor: upInk }]}
            />
          )
        )}
        <LimitMarks marks={marks} height={height} />
      </View>
      {/* The time axis, oldest on the left, with the legend between its ends. */}
      <View style={styles.row}>
        <Text style={[styles.tick, NUM, { color: c.textMuted }]}>{spanLabel(SPAN)}</Text>
        <View style={styles.legend}>
          <Key color={downInk} label={t('speed.legend.download')} />
          {seeding && <Key color={upInk} label={t('speed.legend.seeding')} />}
        </View>
        <Text style={[styles.tick, NUM, { color: c.textMuted }]}>0s</Text>
      </View>
    </View>
  );
}

export default memo(SpeedGraph);

const styles = StyleSheet.create({
  // The time axis runs left to right in every language, and the labels have to
  // stay at the ends of the plot they name.
  wrap: { gap: 4, direction: 'ltr' },
  row: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 8 },
  tick: { fontSize: TYPE.caption },
  rates: { flexDirection: 'row', gap: 10 },
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
  swatch: { width: 12, height: 8, borderTopWidth: STROKE },
  glyph: { position: 'absolute', right: 0, width: 0, height: 0 },
  limitLabel: {
    position: 'absolute',
    right: 2 * GLYPH + 3,
    height: LABEL_LINE,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 2,
    paddingHorizontal: 2,
  },
  limitFigure: { fontSize: TYPE.caption, lineHeight: LABEL_LINE, fontVariant: ['tabular-nums'] },
});
