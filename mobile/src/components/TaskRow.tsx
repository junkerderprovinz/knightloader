import type { ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';
import { fmtBytes, fmtSpeed } from '../api/stats';
import type { Task } from '../api/types';
import { useAppearance } from '../theme/AppearanceContext';
import { NUM, TYPE, inkFor, type Palette } from '../theme/tokens';
import { useT, type TranslationKey } from '../i18n/I18nContext';
import { explainFailure } from '../api/taskError';
import { isParked, rowWord, unpackPercent, type StateWord, type UnpackProgress } from '../api/taskState';
import { InfoTip } from './InfoTip';
import IconBadge, { Power } from './IconBadge';
import { Text } from './Text';

// The debrid services that fetch torrents, by the resolver id the server sends.
// Their product names are no words to translate, and the web writes them the
// same way (web/src/lib/resolverLabels.ts).
const SERVICE_NAMES: Record<string, string> = {
  torbox: 'TorBox',
  realdebrid: 'Real-Debrid',
  alldebrid: 'AllDebrid',
  premiumize: 'Premiumize.me',
  debridlink: 'Debrid-Link',
};

const SEGMENTS = Array.from({ length: 80 }, (_, i) => i);

/** serviceName is the service behind a resolver id such as "realdebrid#work". */
function serviceName(resolver: string): string {
  const id = resolver.split('#')[0];
  return SERVICE_NAMES[id] ?? id;
}

/** The key each state word reads as, for a row and a package header alike. */
export const STATE_KEYS: Record<StateWord, TranslationKey> = {
  collected: 'status.collected',
  queued: 'status.queued',
  running: 'status.running',
  paused: 'status.paused',
  extracting: 'status.extracting',
  seeding: 'status.seeding',
  finished: 'status.finished',
  failed: 'status.failed',
  notUnpacked: 'status.notUnpacked',
  disabled: 'status.disabled',
};

/**
 * blend lays `over` on `base` at `alpha`, returning an opaque colour.
 *
 * Computed rather than layered as a translucent view: React Native has no
 * colour-mix and no inset shadow, and a second absolutely-positioned view
 * inside every row would sit above the row's own children and swallow their
 * touches.
 */
function blend(base: string, over: string, alpha: number): string {
  const b = rgb(base);
  const o = rgb(over);
  if (!b || !o) return base;
  const mix = (x: number, y: number) => Math.round(x + (y - x) * alpha);
  return `rgb(${mix(b.r, o.r)}, ${mix(b.g, o.g)}, ${mix(b.b, o.b)})`;
}

function rgb(hex: string): { r: number; g: number; b: number } | null {
  if (!/^#[0-9a-fA-F]{6}$/.test(hex)) return null;
  const n = parseInt(hex.slice(1), 16);
  return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
}

/**
 * The status word's colour: `accentInk` rather than the accent.
 *
 * A colour on a background is the accent; a colour a reader has to read is the
 * accent darkened until they can, because gold text on a white card is
 * unreadable at 12 points whichever gold it is. The row's own fill, the wash
 * and the progress bar, takes the undarkened one, which is why the palette and
 * the ink arrive as two arguments. A parked row (isParked) says its word in
 * the muted ink, whatever the word is.
 */
export function statusColor(word: StateWord | null, c: Palette, accentInk: string, parked = false): string {
  if (parked) return c.textMuted;
  switch (word) {
    case 'running':
    case 'seeding':
      return accentInk;
    case 'finished':
      return c.statusOkSolid;
    case 'failed':
    case 'notUnpacked':
      return c.statusFailSolid;
    case 'paused':
      return c.statusWarnSolid;
    default:
      return c.textMuted;
  }
}

export default function TaskRow({
  task,
  index,
  onSwitch,
  files,
  unpack,
}: {
  task: Task;
  index: number;
  /** How far the unpacking of this file's archive has got, which the bar
   *  shows in place of the finished download (unpackProgress). */
  unpack?: UnpackProgress | null;
  /** Disables this link, or enables it again; the package header's badge
   *  does the same for all of its links. */
  onSwitch?: () => void;
  /** A torrent's files, drawn under the rest of the card in the row's own
   *  colour. */
  files?: (hue: string) => ReactNode;
}) {
  const { t } = useT();
  const { c, accent, dark, corners, hueAt, rainbow } = useAppearance();
  // The rainbow hands colours out by position, so this row's colour comes from
  // where it sits rather than from its id. A hash keeps a row's colour when the
  // rows above it finish, and with three rows and eight colours it gives two
  // neighbours the same one, which the mode exists to prevent.
  const hue = hueAt(index);
  // The colour this row paints activity in: its own when the mode is on, the
  // single accent otherwise.
  const rowAccent = hue ?? accent;
  // The same colour where it has to be read instead of filled, by the rule the
  // whole family uses: the accent 55% of the way to black on a light ground and
  // itself on a dark one. The context builds this pair for the flat accent; the
  // rainbow half has no counterpart, so a hue would reach a `color:` undarkened.
  const rowInk = dark ? rowAccent : inkFor(rowAccent);
  // Nothing of a torrent comes here while a debrid service fetches it, so the
  // bar and the percentage show how far the service has got.
  const remote = task.status === 'running' ? task.remote : undefined;
  const pct = remote
    ? Math.min(100, Math.floor(remote.progress * 100))
    : task.size > 0
      ? Math.min(100, Math.round((task.loaded / task.size) * 100))
      : null;
  const unpackPct = unpack ? unpackPercent(unpack) : null;
  // The other parts of a set are done as far as the instance is concerned,
  // while their bar shows the unpacking. The word says so on each of them.
  const word = unpack && !unpack.failed ? 'extracting' : rowWord(task);
  // Greyed by colour rather than opacity, so the switch beside the row keeps
  // its full strength.
  const parked = isParked([task]);
  const name = task.name || task.url;
  const failure = explainFailure(t, task, {
    part: name,
    file: name,
    path: task.dir || name,
    service: serviceName(task.resolver),
  });

  return (
    <View
      style={[
        styles.row,
        { backgroundColor: c.surface, ...corners.card },
        // The row carries a wash of its colour. Without it the hue reaches the
        // row only through the progress bar, which turns green when a download
        // finishes, so a list of finished downloads would show nothing of the
        // mode that exists for lists.
        //
        // "reactive" is the restrained reading: rest neutral, colour what is
        // running. A phone has no hover, so running is the whole of it here.
        //
        // 16%, because GlimStone's changelog names 7% as the value that sat
        // under the threshold anyone registers as change. The running row gets
        // the stronger 22% the web gives the selected row, running being this
        // list's active state.
        //
        // A parked row has none: the wash is its colour, and colour is what a
        // parked row gives up.
        hue && !parked && (!rainbow.reactive || task.status === 'running')
          ? { backgroundColor: blend(c.surface, hue, task.status === 'running' ? 0.22 : 0.16) }
          : null,
      ]}
    >
      <View style={styles.body}>
        <View style={styles.header}>
          <Text style={[styles.name, { color: parked ? c.textMuted : c.text }]} numberOfLines={1}>
            {task.name || task.url}
          </Text>
          <Text style={[styles.status, { color: statusColor(word, c, rowInk, parked) }]}>
            {word ? t(STATE_KEYS[word]) : task.status}
          </Text>
        </View>

        {task.status === 'running' && (
          <View style={[styles.progressTrack, { backgroundColor: c.surface2, ...corners.pill }]}>
            <View
              style={[
                styles.progressFill,
                // The neutral fill rather than the text grey, as on the web: a
                // bar in the text grey outweighs the live ones on a light theme.
                { width: `${pct ?? 0}%`, backgroundColor: parked ? c.statusNeutralSolid : rowAccent },
              ]}
            />
          </View>
        )}
        {unpack && (
          <View style={[styles.progressTrack, { backgroundColor: c.surface2, ...corners.pill }]}>
            {/* Cut into segments where the web stripes it: the row's own colour
                already means working, so the texture is what tells unpacking
                from downloading. A size nobody knows fills the track. */}
            <View style={[styles.progressFill, styles.segments, { width: `${unpackPct ?? 100}%` }]}>
              {SEGMENTS.map((i) => (
                <View
                  key={i}
                  style={[
                    styles.segment,
                    {
                      backgroundColor: parked
                        ? c.statusNeutralSolid
                        : unpack.failed
                          ? c.statusFailSolid
                          : rowAccent,
                    },
                  ]}
                />
              ))}
            </View>
          </View>
        )}

        <View style={styles.footer}>
          {unpack ? (
            <Text style={[styles.meta, { color: c.textMuted }]}>
              {fmtBytes(unpack.unpacked)}
              {unpackPct !== null ? ` / ${fmtBytes(unpack.size)} · ${unpackPct}%` : ''}
            </Text>
          ) : (
            <Text style={[styles.meta, { color: c.textMuted }]}>
              {remote ? `${pct}%` : fmtBytes(task.loaded)}
              {!remote && task.size > 0 ? ` / ${fmtBytes(task.size)}` : ''}
              {!remote && pct !== null ? ` · ${pct}%` : ''}
            </Text>
          )}
          {task.speed > 0 && <Text style={[styles.meta, { color: c.textMuted }]}>{fmtSpeed(task.speed)}</Text>}
          {remote ? (
            <Text style={[styles.meta, { color: c.textMuted }]} numberOfLines={1}>
              {t('task.remote', { service: serviceName(task.resolver) })}
            </Text>
          ) : null}
          {/* The backend's own word for what is happening, when "running" is not
              the whole truth. The web list carries the same note column. */}
          {task.note ? (
            <Text style={[styles.meta, { color: c.textMuted }]} numberOfLines={1}>
              {task.note}
            </Text>
          ) : null}
          {/* Whether this goes out on an account, in the same muted metadata ink
              as the byte count beside it. A word rather than a badge or a colour,
              because "free" is an answer and not a warning, and nothing at all
              for an ordinary file, which is neither. */}
          {task.mode ? (
            <Text style={[styles.meta, { color: c.textMuted }]}>
              {t(task.mode === 'premium' ? 'task.mode.premium' : 'task.mode.free')}
            </Text>
          ) : null}
        </View>

        {failure ? (
          <View style={styles.failure}>
            <View style={styles.failureText}>
              <Text style={[styles.failureLine, { color: parked ? c.textMuted : c.statusFailSolid }]} numberOfLines={2}>
                {failure.line}
              </Text>
              <Text style={[styles.failureLine, { color: c.textMuted }]} numberOfLines={3}>
                {failure.next}
              </Text>
            </View>
            {/* The instance's own sentence stays one tap away for a report,
                the way the web keeps it in the row's bubble. */}
            {failure.raw ? <InfoTip text={`${t('failure.raw')}: ${failure.raw}`} /> : null}
          </View>
        ) : null}
        {files?.(rowAccent)}
      </View>

      {/* The package header's switch, per link, so one mirror or one part can
          be parked on its own. */}
      {onSwitch && (
        <IconBadge
          icon={<Power color={c.textSub} hole={c.surface2} />}
          onPress={onSwitch}
          accessibilityLabel={t(task.enabled ? 'task.disable' : 'task.enable')}
        />
      )}
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens rather than
// baked in here: a stylesheet is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  row: {
    padding: 12,
    marginBottom: 8,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
  },
  body: { flex: 1, minWidth: 0 },
  header: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  // marginEnd rather than marginRight: the app ships Arabic, Hebrew and
  // Persian, and a physical edge keeps its gap on the same side of the screen
  // while everything around it mirrors.
  name: { fontSize: TYPE.body, fontWeight: '500', flex: 1, marginEnd: 8 },
  status: { fontSize: TYPE.dense, fontWeight: '600', textTransform: 'uppercase' },
  progressTrack: {
    height: 4,
    marginTop: 8,
    overflow: 'hidden',
  },
  progressFill: { height: '100%' },
  // Clipped at the fill's width, so the count only has to cover the widest
  // row: 80 steps of 8 points outrun the list's 640-point cap.
  segments: { flexDirection: 'row', overflow: 'hidden', gap: 2 },
  segment: { width: 6, height: '100%', flexShrink: 0 },
  footer: { flexDirection: 'row', justifyContent: 'space-between', marginTop: 6 },
  // Tabular figures: a byte count, a total and a percentage rewritten every
  // refresh, in a row that stacks down the whole screen. With proportional
  // digits the footer shuffles sideways on every tick.
  meta: { fontSize: TYPE.dense, ...NUM },
  failure: { flexDirection: 'row', alignItems: 'flex-start', gap: 8, marginTop: 6 },
  failureText: { flex: 1, minWidth: 0, gap: 2 },
  failureLine: { fontSize: TYPE.dense },
});
