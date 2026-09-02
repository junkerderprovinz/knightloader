import { useState } from 'react';
import { Alert, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import type { Task } from '../api/types';
import TaskRow from './TaskRow';
import DragList, { type DragRow } from './DragList';
import IconBadge, { Trash } from './IconBadge';
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
  /** Everything that belongs ABOVE the list and has to line up with it: the
   *  queue bar, the speed graph, the Downloads/Collector strip. They used to be
   *  siblings of this list with their own copy of its width and margins, which
   *  is how the strip ended up narrower than the cards and hard against the left
   *  edge (jdp, 2026-08-31: "Der download/Sammler selektor soll bündig mit den
   *  cards sien, jetzt ist er am rand links und er soll so breit sein wie die
   *  cards"). Inside the list's own content container they cannot disagree with
   *  it: same padding, same cap, same centring, by construction rather than by
   *  two numbers somebody keeps in step. */
  header?: React.ReactNode;
  /** Only the collector passes this: a package there is a staged batch, and
   *  the badge is what promotes it. Undefined in the download tab, where the
   *  queue's own controls already decide what runs. */
  onStartPackage?: (pkg: Pkg) => void;
  /** Both tabs pass this (jdp, 2026-08-31: "man soll ordner auch löschen
   *  können"). Confirmed here rather than at the call site, so every caller
   *  gets the same dialog and none of them can forget it. */
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

  /** Which packages are OPEN. Closed is the default (jdp, 2026-08-31: "link
   *  ordner sollen in der app standardmäßig zusammen geklappt sein und
   *  ausklappbar sein"), so the state records the exception rather than the
   *  rule: a package that arrives while the screen is open is closed like
   *  every other one, with nothing to initialise for it.
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

  const confirmDelete = (pkg: Pkg) => {
    Alert.alert(
      t('packages.deleteConfirmTitle'),
      t('packages.deleteConfirmMessage', { n: pkg.tasks.length }),
      [
        { text: t('settings.cancel'), style: 'cancel' },
        {
          text: t('packages.deleteConfirmButton'),
          style: 'destructive',
          onPress: () => onDeletePackage?.(pkg),
        },
      ],
    );
  };

  // One flat list of draggable rows. The BAND is what keeps a drag honest: a
  // package header may only move among other package headers, and a link only
  // within its own package. Without it a link could be dropped between two
  // packages, where the list has no way to render it and the server has no way
  // to store it.
  const dragRows: DragRow[] = rows.map((r) =>
    r.kind === 'header'
      ? { key: `p:${r.pkg.name}`, band: 'packages', render: (_ziehend, scharf) => renderHeader(r.pkg, scharf) }
      : { key: r.task.id, band: `pkg:${r.task.package || ''}`, render: () => <TaskRow task={r.task} index={r.index} /> },
  );

  /** A drop, turned into the flat task order the instance stores.
   *
   *  POST /api/tasks/reorder takes "one whole band of the wait queue in the
   *  exact order given, as a drag would" - the same call the web interface's own
   *  drag-and-drop makes - so both surfaces write the same shape and neither has
   *  a private idea of what an order is. Reordering PACKAGES is expressed the
   *  same way: the packages move, and the ids of their tasks are emitted in the
   *  new package order. */
  /** What the server will actually accept in one reorder.
   *
   *  Two rules, and both were learned the hard way (jdp, five rounds of "das
   *  drag and drop funktioniert nicht"):
   *
   *  - A finished or failed task is not in the wait queue, so naming one
   *    refuses the WHOLE request, and this list happily shows both.
   *  - Priority is what the server groups a band by. A list mixing two
   *    priorities is refused for spanning bands, and nothing on this screen
   *    shows a priority, so it would look like the drag simply did nothing.
   *
   *  Filtering here rather than letting the request fail is right because
   *  neither is a mistake anybody made: they are rows this list is meant to
   *  show and the queue is not meant to move. */
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
    // Within one package: that package's own tasks in the new order. Only that
    // package's ids travel - every other task in the band is left where it is,
    // which is exactly what a partial reorder now means to the server.
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
   * While it is on, the controls inside a row stop responding. A hold that
   * arms the drag lands ON one of them as often as not - these headers are
   * caption, start and bin edge to edge - and without this, letting go without
   * moving would arm the drag AND press whatever was under the finger. Which,
   * for the bin, means a confirmation dialog nobody asked for.
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
              {/* The speed belongs on the HEADER, not only on the rows inside:
                  closed by default would otherwise hide the one thing a running
                  folder has to say. */}
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
    <DragList
      rows={dragRows}
      onReorder={applyOrder}
      contentContainerStyle={styles.list}
      header={header}
      empty={<Text style={[styles.empty, { color: c.textMuted }]}>{empty}</Text>}
    />
  );
}

// Colours and radii are applied inline from the resolved tokens, never baked
// in here: a stylesheet is built once and cannot follow a theme change.
// One column stretched across a tablet is a card 900 points wide with its text
// at one edge and its badge at the other. A cap plus centring costs a phone
// nothing (640 is wider than every phone) and makes a tablet readable. The same
// helper the screens use, here so the header this list carries and the rows
// under it are measured by ONE rule.
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
  headerName: { fontSize: 15, fontWeight: '600', flexShrink: 1 },
  headerLine: { fontSize: TYPE.caption, marginStart: 20 },
  empty: { textAlign: 'center', marginTop: 40, fontSize: TYPE.body },
});
