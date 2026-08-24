import { StyleSheet, Text, View } from 'react-native';
import type { Task } from '../api/types';
import { colors } from '../theme';
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

function statusColor(status: string): string {
  switch (status) {
    case 'running':
      return colors.accent;
    case 'finished':
      return colors.success;
    case 'failed':
      return colors.danger;
    case 'paused':
      return colors.warning;
    default:
      return colors.textMuted;
  }
}

export default function TaskRow({ task }: { task: Task }) {
  const { t } = useT();
  const pct = task.size > 0 ? Math.min(100, Math.round((task.loaded / task.size) * 100)) : null;
  const statusKey = STATUS_KEYS[task.status];

  return (
    <View style={styles.row}>
      <View style={styles.header}>
        <Text style={styles.name} numberOfLines={1}>
          {task.name || task.url}
        </Text>
        <Text style={[styles.status, { color: statusColor(task.status) }]}>{statusKey ? t(statusKey) : task.status}</Text>
      </View>

      {task.status === 'running' && (
        <View style={styles.progressTrack}>
          <View style={[styles.progressFill, { width: `${pct ?? 0}%` }]} />
        </View>
      )}

      <View style={styles.footer}>
        <Text style={styles.meta}>
          {formatBytes(task.loaded)}
          {task.size > 0 ? ` / ${formatBytes(task.size)}` : ''}
          {pct !== null ? ` · ${pct}%` : ''}
        </Text>
        {task.speed > 0 && <Text style={styles.meta}>{formatBytes(task.speed)}/s</Text>}
      </View>

      {task.error ? (
        <Text style={styles.errorText} numberOfLines={2}>
          {task.error}
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    backgroundColor: colors.surface,
    borderRadius: 8,
    padding: 12,
    marginBottom: 8,
  },
  header: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  name: { color: colors.text, fontSize: 14, fontWeight: '500', flex: 1, marginRight: 8 },
  status: { fontSize: 12, fontWeight: '600', textTransform: 'uppercase' },
  progressTrack: {
    height: 4,
    backgroundColor: colors.surfaceRaised,
    borderRadius: 2,
    marginTop: 8,
    overflow: 'hidden',
  },
  progressFill: { height: '100%', backgroundColor: colors.accent },
  footer: { flexDirection: 'row', justifyContent: 'space-between', marginTop: 6 },
  meta: { color: colors.textMuted, fontSize: 12 },
  errorText: { color: colors.danger, fontSize: 12, marginTop: 6 },
});
