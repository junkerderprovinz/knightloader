import { StyleSheet, TouchableOpacity, View } from 'react-native';
import { fmtBytes } from '../api/stats';
import type { Task, TorrentFileView } from '../api/types';
import { useAppearance } from '../theme/AppearanceContext';
import { NUM, TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import { GlimToggle } from './glim';
import { Text } from './Text';

/** Whether a task is a torrent whose files are worth listing: a single file
 *  is the row itself. */
export function hasTorrentFiles(task: Task): boolean {
  return (task.torrentFileCount ?? 0) > 1;
}

/** A finished torrent, whose files can only be read. */
export function torrentFinished(task: Task): boolean {
  return task.status === 'done' || task.status === 'extracting';
}

/**
 * The line under a torrent's name that opens its files, and the files once it
 * is open: each one's path inside the torrent, its size, how much of it is here
 * and its switch. It sits inside the torrent's card rather than as rows of its
 * own, so a file never travels in a drag without its torrent. A finished
 * torrent's files have no switch: nothing a switch changed would be fetched.
 */
export function TorrentFiles({
  task,
  open,
  onToggle,
  files,
  onFlip,
  hue,
}: {
  task: Task;
  open: boolean;
  onToggle: () => void;
  /** Undefined until the list has come. */
  files: TorrentFileView[] | undefined;
  onFlip: (file: TorrentFileView) => void;
  /** The row's own colour, for the bars and switches. */
  hue: string;
}) {
  const { t } = useT();
  const { c, corners } = useAppearance();
  const finished = torrentFinished(task);
  return (
    <View style={styles.box}>
      <TouchableOpacity
        onPress={onToggle}
        accessibilityRole="button"
        accessibilityState={{ expanded: open }}
        accessibilityLabel={t(open ? 'torrent.hideFiles' : 'torrent.showFiles')}
        style={styles.opener}
      >
        {/* The package header's chevron, turned the same way. */}
        <Text style={[styles.chevron, { color: c.textSub }, open && styles.chevronOpen]}>›</Text>
        <Text style={[styles.count, { color: c.textMuted }]}>
          {t('instance.files', { n: task.torrentFileCount ?? 0 })}
        </Text>
      </TouchableOpacity>
      {open &&
        files?.map((f) => (
          <View key={f.path} style={styles.file}>
            <View style={styles.fileText}>
              <Text style={[styles.path, { color: f.selected ? c.textSub : c.textMuted }]} numberOfLines={1}>
                {f.path}
              </Text>
              <View style={styles.fileLine}>
                <Text style={[styles.size, { color: c.textMuted }]}>{fmtBytes(f.size)}</Text>
                {f.done !== undefined && (
                  <View style={[styles.track, { backgroundColor: c.surface2, ...corners.pill }]}>
                    <View
                      style={[
                        styles.fill,
                        {
                          width: `${f.size > 0 ? Math.min(100, Math.round((f.done / f.size) * 100)) : 100}%`,
                          backgroundColor: f.done >= f.size ? c.statusOkSolid : hue,
                        },
                      ]}
                    />
                  </View>
                )}
              </View>
            </View>
            {!finished && (
              <GlimToggle
                value={f.selected}
                onChange={() => onFlip(f)}
                label={t(f.selected ? 'torrent.skipFile' : 'torrent.fetchFile')}
              />
            )}
          </View>
        ))}
    </View>
  );
}

const styles = StyleSheet.create({
  box: { marginTop: 6, gap: 6 },
  opener: { flexDirection: 'row', alignItems: 'center', gap: 8, alignSelf: 'flex-start' },
  chevron: { fontSize: 17, lineHeight: 20, width: 12, textAlign: 'center' },
  chevronOpen: { transform: [{ rotate: '90deg' }] },
  count: { fontSize: TYPE.dense, ...NUM },
  file: { flexDirection: 'row', alignItems: 'center', gap: 12, marginStart: 20 },
  fileText: { flex: 1, minWidth: 0, gap: 4 },
  path: { fontSize: TYPE.dense },
  fileLine: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  size: { fontSize: TYPE.caption, ...NUM },
  track: { flex: 1, height: 4, overflow: 'hidden' },
  fill: { height: '100%' },
});
