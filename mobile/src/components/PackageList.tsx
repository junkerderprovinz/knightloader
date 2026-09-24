import { useState } from 'react';
import { StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import type { Task } from '../api/types';
import TaskRow from './TaskRow';
import DragList, { type DragRow } from './DragList';
import IconBadge, { Folder, Trash } from './IconBadge';
import { ConfirmDialog } from './ConfirmDialog';
import { useAppearance } from '../theme/AppearanceContext';
import { NUM, TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import { fmtBytes } from '../api/stats';

/**
 * The task list, grouped into the packages the instance already put it in.
 *
 * A container is one thing somebody added, usually a dozen or a hundred files,
 * and a flat list of them is a wall that says nothing about what was added,
 * which is why the web interface and JDownloader both group by package.
 *
 * One flattened array rather than a SectionList: the header and its rows are
 * the same virtualised list either way, and flattening keeps one FlatList with
 * one keyExtractor instead of a second component's worth of section plumbing.
 */
export interface Pkg {
  name: string;
  tasks: Task[];
  size: number;
  loaded: number;
  speed: number;
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
      p = { name, tasks: [], size: 0, loaded: 0, speed: 0 };
      byName.set(name, p);
      out.push(p);
    }
    p.tasks.push(t);
    p.size += t.size || 0;
    p.loaded += t.loaded || 0;
    p.speed += t.speed || 0;
  }
  return out;
}

type Row = { kind: 'header'; pkg: Pkg } | { kind: 'task'; task: Task; index: number };

export default function PackageList({
  tasks,
  onStartPackage,
  onDeletePackage,
  onReorder,
  empty,
  header,
}: {
  tasks: Task[];
  /** Everything that belongs above the list and has to line up with it: the
   *  queue bar, the speed graph, the Downloads/Collector strip. As siblings of
   *  this list they would carry their own copy of its width and margins; inside
   *  its content container they get the same padding, cap and centring by
   *  construction. */
  header?: React.ReactNode;
  /** Only the collector passes this: a package there is a staged batch and the
   *  badge is what promotes it. Undefined in the download tab, where the
   *  queue's own controls decide what runs. */
  onStartPackage?: (pkg: Pkg) => void;
  /** Both tabs pass this. Confirmed here rather than at the call site, so every
   *  caller gets the same dialog and none of them can forget it. */
  onDeletePackage?: (pkg: Pkg) => void;
  /** The flat task order after a drag, ready for POST /api/tasks/reorder.
   *  Undefined leaves the list un-draggable, which is what the collector tab
   *  wants: nothing there is in the wait queue yet, so there is no order to
   *  write. */
  onReorder?: (ids: string[]) => void;
  empty: string;
}) {
  const { t } = useT();
  const { c, radii } = useAppearance();
  const packages = groupByPackage(tasks);

  /** Which packages are open. Closed is the default, so the state records the
   *  exception rather than the rule and a package that arrives while the screen
   *  is open needs nothing initialised for it.
   *
   *  Keyed by package name, which is what the instance groups by, so a folder
   *  stays open across the five-second refresh that replaces every Task object
   *  in the list. */
  const [open, setOpen] = useState<Record<string, boolean>>({});

  const rows: Row[] = [];
  let n = 0;
  for (const pkg of packages) {
    rows.push({ kind: 'header', pkg });
    if (open[pkg.name]) for (const task of pkg.tasks) rows.push({ kind: 'task', task, index: n++ });
  }

  /**
   * The package the removal window is asking about, or null. The question is
   * what warns, and it is the only thing that does: the message names the
   * count, so the commit button needs no colour (GlimStone 1.12.0).
   */
  const [confirming, setConfirming] = useState<Pkg | null>(null);
  const confirmDelete = (pkg: Pkg) => setConfirming(pkg);

  // One flat list of draggable rows. The band is what keeps a drag honest: a
  // package header moves among package headers and a link within its own
  // package. Without it a link could be dropped between two packages, where the
  // list cannot render it and the server cannot store it.
  const dragRows: DragRow[] = rows.map((r) =>
    r.kind === 'header'
      ? { key: `p:${r.pkg.name}`, band: 'packages', render: (_ziehend, scharf) => renderHeader(r.pkg, scharf) }
      : { key: r.task.id, band: `pkg:${r.task.package || ''}`, render: () => <TaskRow task={r.task} index={r.index} /> },
  );

  /** What the server accepts in one reorder.
   *
   *  POST /api/tasks/reorder takes one whole band of the wait queue in the
   *  order given, the same call the web interface's drag-and-drop makes, so
   *  both surfaces write the same shape. Reordering packages is expressed the
   *  same way: the packages move and the ids of their tasks are emitted in the
   *  new package order.
   *
   *  Two rows have to be filtered out first. A finished or failed task is not
   *  in the wait queue, so naming one refuses the whole request, and this list
   *  shows both. Priority is what the server groups a band by, so a list mixing
   *  two priorities is refused for spanning bands; nothing on this screen shows
   *  a priority, so the drag would look as if it had done nothing. */
  const sortierbar = (t: Task) => t.status !== 'done' && t.status !== 'error';
  const bandVon = (t: Task) => t.priority ?? 0;

  const applyOrder = (keys: string[], band: string) => {
    if (!onReorder) return;
    if (band === 'packages') {
      const nachName = new Map(packages.map((p) => [`p:${p.name}`, p]));
      const neu = keys.map((k) => nachName.get(k)).filter((p): p is Pkg => !!p);
      const alle = neu.flatMap((p) => p.tasks).filter(sortierbar);
      // One priority only: the dragged rows' own. Everything else in the list
      // belongs to another band and is left to a drag made inside it.
      const gezogen = nachName.get(keys[0]);
      const prio = gezogen ? bandVon(gezogen.tasks[0]) : 0;
      const ids = alle.filter((x) => bandVon(x) === prio).map((x) => x.id);
      if (ids.length > 0) onReorder(ids);
      return;
    }
    // Within one package: that package's own tasks in the new order. Only its
    // ids travel, and every other task in the band is left where it is, which
    // is what a partial reorder means to the server.
    const name = band.slice('pkg:'.length);
    const pkg = packages.find((p) => p.name === name);
    if (!pkg) return;
    const nachId = new Map(pkg.tasks.map((x) => [x.id, x]));
    const geordnet = keys.map((k) => nachId.get(k)).filter((x): x is Task => !!x && sortierbar(x));
    const prio = geordnet.length > 0 ? bandVon(geordnet[0]) : 0;
    const ids = geordnet.filter((x) => bandVon(x) === prio).map((x) => x.id);
    if (ids.length > 0) onReorder(ids);
  };

  /**
   * `scharf` is the list's reorder mode, handed down by DragList.
   *
   * While it is on, the controls inside a row stop responding. A hold that arms
   * the drag lands on one of them as often as not, since these headers are
   * caption, start and bin edge to edge, and letting go without moving would
   * otherwise arm the drag and press whatever was under the finger, which for
   * the bin means a confirmation dialog nobody asked for.
   */
  const renderHeader = (pkg: Pkg, scharf: boolean) => {
        const auf = open[pkg.name] === true;
        return (
          <View style={[styles.header, { backgroundColor: c.surface2, borderRadius: radii.control }]}>
            {/* The whole caption is the hit target, not the chevron: a folder
                you open by hitting a 12-point glyph is a folder you miss. */}
            <TouchableOpacity
              style={styles.headerText}
              disabled={scharf}
              onPress={() => setOpen((o) => ({ ...o, [pkg.name]: !auf }))}
              accessibilityRole="button"
              accessibilityState={{ expanded: auf }}
              accessibilityLabel={t(auf ? 'packages.collapse' : 'packages.expand')}
            >
              <View style={styles.headerTop}>
                {/* Rotated rather than two glyphs: one character, one meaning,
                    and the direction says which way it goes. */}
                <Text style={[styles.chevron, { color: c.textSub }, auf && styles.chevronOpen]}>›</Text>
                <Text style={[styles.headerName, { color: c.text }]} numberOfLines={1}>
                  {pkg.name || t('packages.loose')}
                </Text>
              </View>
              {/* The speed goes on the header as well as on the rows inside,
                  or closed by default hides the one thing a running folder has
                  to say. */}
              <Text style={[styles.headerLine, { color: c.textMuted }]} numberOfLines={1}>
                {[
                  `${pkg.tasks.length} ${t('instance.files')}`,
                  pkg.size > 0 ? fmtBytes(pkg.size) : null,
                  pkg.speed > 0 ? `${fmtBytes(pkg.speed)}/s` : null,
                ]
                  .filter(Boolean)
                  .join(' · ')}
              </Text>
            </TouchableOpacity>
            {onStartPackage && (
              <IconBadge
                symbol="▶"
                accent
                onPress={() => scharf || onStartPackage(pkg)}
                accessibilityLabel={t('packages.start')}
              />
            )}
            {onDeletePackage && (
              <IconBadge
                icon={<Trash color={c.textSub} />}
                onPress={() => scharf || confirmDelete(pkg)}
                accessibilityLabel={t('packages.delete')}
              />
            )}
          </View>
        );
  };

  return (
    <>
      <DragList
        rows={dragRows}
        onReorder={applyOrder}
        contentContainerStyle={styles.list}
        header={header}
        /**
         * A list with nothing in it gets a card, a muted glyph at reduced
         * opacity and a muted title rather than blank space. One sentence
         * floating in the middle of an empty screen reads as a screen that
         * failed to load, and this is the first thing a new install shows
         * anybody.
         *
         * The sentence is the caller's, because empty means nothing queued in
         * one tab and nothing collected in the other, while the shape is the
         * same.
         */
        empty={
          <View style={[styles.empty, { backgroundColor: c.surface, borderRadius: radii.card }]}>
            <View style={styles.emptyGlyph}>
              <Folder color={c.textMuted} size={44} />
            </View>
            <Text style={[styles.emptyText, { color: c.textMuted }]}>{empty}</Text>
          </View>
        }
      />
      <ConfirmDialog
        visible={confirming !== null}
        title={t('packages.deleteConfirmTitle')}
        message={t('packages.deleteConfirmMessage', { n: confirming?.tasks.length ?? 0 })}
        cancelLabel={t('settings.cancel')}
        confirmLabel={t('packages.deleteConfirmButton')}
        confirmIcon={(ink) => <Trash color={ink} />}
        onCancel={() => setConfirming(null)}
        onConfirm={() => {
          const pkg = confirming;
          setConfirming(null);
          if (pkg) onDeletePackage?.(pkg);
        }}
      />
    </>
  );
}

// Colours and radii are applied inline from the resolved tokens rather than
// baked in here: a stylesheet is built once and cannot follow a theme change.
//
// One column stretched across a tablet is a card 900 points wide with its text
// at one edge and its badge at the other. A cap plus centring costs a phone
// nothing, since 640 is wider than every phone, and makes a tablet readable.
// The same helper the screens use, so the header this list carries and the rows
// under it are measured by one rule.
const capped = { width: '100%' as const, maxWidth: 640, alignSelf: 'center' as const };

const styles = StyleSheet.create({
  list: { ...capped, paddingHorizontal: 16, paddingBottom: 96, gap: 8 },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    paddingVertical: 10,
    paddingHorizontal: 12,
    marginTop: 6,
  },
  headerText: { flex: 1, minWidth: 0, gap: 2 },
  headerTop: { flexDirection: 'row', alignItems: 'center', gap: 8, minWidth: 0 },
  chevron: { fontSize: 17, lineHeight: 20, width: 12, textAlign: 'center' },
  chevronOpen: { transform: [{ rotate: '90deg' }] },
  // Body, off the table. 15 is not a step of the scale, and one 15 beside the
  // 14s around it is how a four-step scale grows a fifth step nobody chose.
  headerName: { fontSize: TYPE.body, fontWeight: '600', flexShrink: 1 },
  // Tabular figures: a file count, a size and a live speed, in a line repeated
  // once per folder down the screen.
  headerLine: { fontSize: TYPE.caption, marginStart: 20, ...NUM },
  empty: { marginTop: 40, paddingVertical: 32, paddingHorizontal: 24, alignItems: 'center', gap: 12 },
  // The reduced opacity sits on the glyph rather than on the card, so the words
  // under it stay at full strength: opacity applies to a whole subtree and a
  // child cannot be less transparent than its parent.
  emptyGlyph: { opacity: 0.45 },
  emptyText: { textAlign: 'center', fontSize: TYPE.body },
});
