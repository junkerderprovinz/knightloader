import { StyleSheet, Text, View } from 'react-native';
import type { Task } from '../api/types';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE, type Palette } from '../theme/tokens';
import { useT, type TranslationKey } from '../i18n/I18nContext';

const STATUS_KEYS: Record<string, TranslationKey> = {
  queued: 'status.queued',
  running: 'status.running',
  paused: 'status.paused',
  finished: 'status.finished',
  failed: 'status.failed',
  extracting: 'status.extracting',
};

function formatBytes(n: number): string {
  if (n <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  return `${(n / 1024 ** i).toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

// The palette is handed in rather than read from a module: this runs outside
// any component, where a hook cannot go, and a fixed palette here would be the
// one colour on the row that never follows a theme change.
/**
 * blend lays `over` on `base` at `alpha`, returning an opaque colour.
 *
 * Computed rather than layered as a translucent view: React Native has no
 * colour-mix and no inset shadow, and a second absolutely-positioned view
 * inside every row would sit above the row's own children and swallow their
 * touches. Mixing the value is the version with no side effects.
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

function statusColor(status: string, c: Palette, accent: string): string {
  switch (status) {
    case 'running':
      return accent;
    case 'finished':
      return c.statusOkSolid;
    case 'failed':
      return c.statusFailSolid;
    case 'paused':
      return c.statusWarnSolid;
    default:
      return c.textMuted;
  }
}

export default function TaskRow({ task, index }: { task: Task; index: number }) {
  const { t } = useT();
  const { c, accent, radii, hueAt, rainbow } = useAppearance();
  // The rainbow hands colours out by POSITION, so this row's colour comes from
  // where it sits, not from its id. A hash keeps a row's colour when the rows
  // above it finish, which sounds better until three rows and eight colours
  // give two neighbours the same one - the single thing the mode exists to
  // prevent.
  const hue = hueAt(index);
  // The colour this row paints activity in: its own when the mode is on, the
  // single accent otherwise.
  const rowAccent = hue ?? accent;
  const pct = task.size > 0 ? Math.min(100, Math.round((task.loaded / task.size) * 100)) : null;
  const statusKey = STATUS_KEYS[task.status];

  return (
    <View
      style={[
        styles.row,
        { backgroundColor: c.surface, borderRadius: radii.card },
        // The row itself carries a wash of its colour, and it has to: without
        // it the hue reaches the row only through the progress bar - and that
        // turns green when a download finishes, because green means finished
        // everywhere. On a list of finished downloads the mode that exists for
        // lists would then show nothing at all.
        //
        // "reactive" is the restrained reading: rest neutral, colour what is
        // running. There is no hover on a phone, so what is running is the
        // whole of it here.
        // 16%, not the 7% this shipped with. GlimStone's own changelog names
        // that exact number as the one that drew three independent "the mode
        // does nothing when I turn it on" reports on the web: it applied
        // correctly the whole time and sat under the threshold anyone
        // registers as change. The running row gets the stronger 22% the web
        // gives the selected row - a phone has no hover, so "running" is this
        // list's active state (jdp, 2026-08-29: "Regenbogenmodus ...
        // funktionieren nicht").
        hue && (!rainbow.reactive || task.status === 'running')
          ? { backgroundColor: blend(c.surface, hue, task.status === 'running' ? 0.22 : 0.16) }
          : null,
      ]}
    >
      <View style={styles.header}>
        <Text style={[styles.name, { color: c.text }]} numberOfLines={1}>
          {task.name || task.url}
        </Text>
        <Text style={[styles.status, { color: statusColor(task.status, c, rowAccent) }]}>
          {statusKey ? t(statusKey) : task.status}
        </Text>
      </View>

      {task.status === 'running' && (
        <View style={[styles.progressTrack, { backgroundColor: c.surface2, borderRadius: radii.pill }]}>
          <View style={[styles.progressFill, { width: `${pct ?? 0}%`, backgroundColor: rowAccent }]} />
        </View>
      )}

      <View style={styles.footer}>
        <Text style={[styles.meta, { color: c.textMuted }]}>
          {formatBytes(task.loaded)}
          {task.size > 0 ? ` / ${formatBytes(task.size)}` : ''}
          {pct !== null ? ` · ${pct}%` : ''}
        </Text>
        {task.speed > 0 && <Text style={[styles.meta, { color: c.textMuted }]}>{formatBytes(task.speed)}/s</Text>}
        {/* Whether this goes out on an account, in the same muted metadata ink
            as the byte count beside it (jdp, 2026-09-02: "Wenn man links
            runterladen möchte für die kein premium account hinterlegt ist muss
            das angezeigt werden"). A word, not a badge and not a colour: "free"
            is an answer, not a warning, and a link with no account behind it
            looked exactly like one with an account behind it right up until it
            was slow or asking for a captcha. Nothing at all for an ordinary
            file, which is neither. */}
        {/* The backend's own word for what is happening, when "running" is not
            the whole truth. Same reasoning as the web list's own note column. */}
        {task.note ? (
          <Text style={[styles.meta, { color: c.textMuted }]} numberOfLines={1}>
            {task.note}
          </Text>
        ) : null}
        {task.mode ? (
          <Text style={[styles.meta, { color: c.textMuted }]}>
            {t(task.mode === 'premium' ? 'task.mode.premium' : 'task.mode.free')}
          </Text>
        ) : null}
      </View>

      {task.error ? (
        <Text style={[styles.errorText, { color: c.statusFailSolid }]} numberOfLines={2}>
          {task.error}
        </Text>
      ) : null}
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens, never baked
// in here: a stylesheet is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  row: {
    padding: 12,
    marginBottom: 8,
  },
  header: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  name: { fontSize: TYPE.body, fontWeight: '500', flex: 1, marginRight: 8 },
  status: { fontSize: TYPE.dense, fontWeight: '600', textTransform: 'uppercase' },
  progressTrack: {
    height: 4,
    marginTop: 8,
    overflow: 'hidden',
  },
  progressFill: { height: '100%' },
  footer: { flexDirection: 'row', justifyContent: 'space-between', marginTop: 6 },
  meta: { fontSize: TYPE.dense },
  errorText: { fontSize: TYPE.dense, marginTop: 6 },
});
