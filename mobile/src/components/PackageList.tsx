import { FlatList, StyleSheet, Text, View } from 'react-native';
import type { Task } from '../api/types';
import TaskRow from './TaskRow';
import IconBadge from './IconBadge';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import { fmtBytes } from '../api/stats';

/**
 * The task list, grouped into the packages the instance already put it in.
 *
 * The app listed every link on its own (jdp, 2026-08-30: "die Links sind jetzt
 * alle einzeln und nicht in der ordner ansicht wie in der containerversion").
 * A container is one thing somebody added, usually a dozen or a hundred files,
 * and a flat list of them is a wall that says nothing about what was added -
 * the same reason the web interface and JDownloader both group by package.
 *
 * One flattened array rather than a SectionList: the header and its rows are
 * the same virtualised list either way, and flattening keeps ONE FlatList with
 * one keyExtractor instead of a second component's worth of section plumbing.
 */
export interface Pkg {
  name: string;
  tasks: Task[];
  size: number;
  loaded: number;
}

/** Grouped in first-seen order, which is the order the instance returned them
 *  in - so a package does not jump around the screen because one of its files
 *  finished. Tasks with no package name share one group, and it is named by
 *  the caller rather than left blank. */
export function groupByPackage(tasks: Task[]): Pkg[] {
  const out: Pkg[] = [];
  const byName = new Map<string, Pkg>();
  for (const t of tasks) {
    const name = t.package || '';
    let p = byName.get(name);
    if (!p) {
      p = { name, tasks: [], size: 0, loaded: 0 };
      byName.set(name, p);
      out.push(p);
    }
    p.tasks.push(t);
    p.size += t.size || 0;
    p.loaded += t.loaded || 0;
  }
  return out;
}

type Row = { kind: 'header'; pkg: Pkg } | { kind: 'task'; task: Task; index: number };

export default function PackageList({
  tasks,
  onStartPackage,
  empty,
}: {
  tasks: Task[];
  /** Only the collector passes this: a package there is a staged batch, and
   *  the badge is what promotes it. Undefined in the download tab, where the
   *  queue's own controls already decide what runs. */
  onStartPackage?: (pkg: Pkg) => void;
  empty: string;
}) {
  const { t } = useT();
  const { c, radii } = useAppearance();
  const packages = groupByPackage(tasks);

  const rows: Row[] = [];
  let n = 0;
  for (const pkg of packages) {
    rows.push({ kind: 'header', pkg });
    for (const task of pkg.tasks) rows.push({ kind: 'task', task, index: n++ });
  }

  return (
    <FlatList
      data={rows}
      keyExtractor={(r) => (r.kind === 'header' ? `p:${r.pkg.name}` : r.task.id)}
      contentContainerStyle={styles.list}
      renderItem={({ item }) => {
        if (item.kind === 'task') return <TaskRow task={item.task} index={item.index} />;
        const { pkg } = item;
        return (
          <View style={[styles.header, { backgroundColor: c.surface2, borderRadius: radii.control }]}>
            <View style={styles.headerText}>
              <Text style={[styles.headerName, { color: c.text }]} numberOfLines={1}>
                {pkg.name || t('packages.loose')}
              </Text>
              <Text style={[styles.headerLine, { color: c.textMuted }]} numberOfLines={1}>
                {`${pkg.tasks.length} ${t('instance.files')}${pkg.size > 0 ? ` · ${fmtBytes(pkg.size)}` : ''}`}
              </Text>
            </View>
            {onStartPackage && (
              <IconBadge
                symbol="▶"
                accent
                onPress={() => onStartPackage(pkg)}
                accessibilityLabel={t('packages.start')}
              />
            )}
          </View>
        );
      }}
      ListEmptyComponent={<Text style={[styles.empty, { color: c.textMuted }]}>{empty}</Text>}
    />
  );
}

// Colours and radii are applied inline from the resolved tokens, never baked
// in here: a stylesheet is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  list: { paddingHorizontal: 16, paddingBottom: 96, gap: 8 },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    paddingVertical: 10,
    paddingHorizontal: 12,
    marginTop: 6,
  },
  headerText: { flex: 1, minWidth: 0, gap: 2 },
  headerName: { fontSize: 15, fontWeight: '600' },
  headerLine: { fontSize: TYPE.caption },
  empty: { textAlign: 'center', marginTop: 40, fontSize: TYPE.body },
});
