import { useCallback, useEffect, useState } from 'react';
import { FlatList, Image, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { checkConnection, setQueueHalted } from '../api/client';
import { listConnections, setActiveConnectionId } from '../storage/connections';
import type { ServerConnection } from '../api/types';
import { useAppearance } from '../theme/AppearanceContext';
import { contentMax, useWide } from '../theme/layout';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import IconBadge, { Gear } from '../components/IconBadge';
import SpeedGraph from '../components/SpeedGraph';
import { aggregate, fetchInstanceStats, fmtBytes, type InstanceStats } from '../api/stats';

/**
 * The one line under an instance's name: the same four figures, in the same
 * order, as the browser extension's own instance card - state, file count,
 * bytes left, speed. The file count is always shown, zero included, so the row
 * cannot change height the moment a download appears.
 *
 * `undefined` means the numbers have not arrived yet, `null` means they were
 * asked for and did not come. Those are different facts and the line says so
 * rather than drawing zeroes for an instance that is not there.
 */
function statusLine(
  // The catalogue's own key union, not a loose `string`: a typo in a key here
  // would otherwise compile and show an empty line in 42 languages.
  t: ReturnType<typeof useT>['t'],
  s: InstanceStats | null | undefined,
  reach: ConnStatus,
): string {
  if (s === undefined) return reach === 'offline' ? t('instance.offline') : '…';
  if (s === null) return t('instance.offline');
  const parts: string[] = [];
  if (s.halted) parts.push(t('downloads.queueHalted'));
  else if (s.running > 0) parts.push(t('downloads.queueRunning'));
  parts.push(`${s.files} ${t('instance.files')}`);
  if (s.remaining > 0) parts.push(`${fmtBytes(s.remaining)} ${t('instance.left')}`);
  if (s.speed > 0) parts.push(`${fmtBytes(s.speed)}/s`);
  return parts.join(' · ');
}

// The app's own mark, beside the name it belongs to (jdp, 2026-08-30: "In der
// Übersicht soll links von der Überschrift auch das Logo sein").
//
// The adaptive icon's FOREGROUND layer, not icon.png (jdp, same day: "Das logo
// in der übersicht soll das ohne hintergrundkachel sein"): icon.png is the
// finished launcher tile with mark and ground baked together, so on a card it
// read as a little app icon pasted onto the page rather than as this product's
// own mark. The foreground layer is the mark alone, on nothing.
//
// require() and not a URI: a shipped asset, resolved by the bundler, so it is
// on screen at first paint with nothing to fetch.
const MARK = require('../../assets/android-icon-foreground.png');

type ConnStatus = 'checking' | 'online' | 'offline';

// The home screen, opened straight from a fresh install rather than the
// connect form: it lands you IN the app first, empty state included, with
// the connect screen and Settings only ever a badge tap away, not something
// forced on you before you have looked at anything (jdp, 2026-08-24: "es
// soll nicht sofort gleich der Verbindungsbildschirm kommen"). A tap on a
// row makes that connection active and opens its Downloads screen; this
// screen itself never shows tasks, so it stays fast to scan even with
// several boxes on flaky Wi-Fi.
export default function ConnectionsScreen({
  onActivate,
  onAddPress,
  onOpenSettings,
}: {
  onActivate: (conn: ServerConnection) => void;
  onAddPress: () => void;
  onOpenSettings: () => void;
}) {
  const { t } = useT();
  const { c, accent, accentContrast, radii } = useAppearance();
  const wide = useWide();
  const [connections, setConnections] = useState<ServerConnection[]>([]);
  const [status, setStatus] = useState<Record<string, ConnStatus>>({});
  const [loaded, setLoaded] = useState(false);
  const [stats, setStats] = useState<Record<string, InstanceStats | null>>({});
  const [why, setWhy] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  /** What the last start/stop actually did, when it did not work. Shown rather
   *  than swallowed: a control that reports nothing is indistinguishable from
   *  a control that does nothing. */
  const [queueError, setQueueError] = useState('');

  const reload = useCallback(async () => {
    const list = await listConnections();
    setConnections(list);
    setLoaded(true);
    list.forEach((conn) => {
      setStatus((s) => ({ ...s, [conn.id]: 'checking' }));
      checkConnection(conn)
        .then((auth) => setStatus((s) => ({ ...s, [conn.id]: auth.authenticated ? 'online' : 'offline' })))
        .catch(() => setStatus((s) => ({ ...s, [conn.id]: 'offline' })));
    });
  }, []);

  useEffect(() => {
    reload();
  }, [reload]);

  // The numbers behind every card and behind the summary above them. Polled
  // rather than streamed: a socket per instance, for a list you only glance at,
  // would be a lot of machinery for a five-second refresh. Two calls each - the
  // queue for "is it halted", /api/queue/counters for the figures.
  const load = useCallback(async () => {
    const list = await listConnections();
    const results = await Promise.all(list.map((conn) => fetchInstanceStats(conn)));
    setStats(Object.fromEntries(list.map((conn, i) => [conn.id, results[i].ok ? results[i].stats : null])));
    // The reason, kept rather than dropped. See stats.ts: swallowing these is
    // what made "the buttons have no effect" impossible to explain.
    setWhy(
      Object.fromEntries(
        list.map((conn, i) => [conn.id, results[i].ok ? '' : (results[i] as { reason: string }).reason]),
      ),
    );
  }, []);

  useEffect(() => {
    void load();
    const id = setInterval(() => void load(), 5000);
    return () => clearInterval(id);
  }, [load]);

  const alle = connections.map((conn) => stats[conn.id] ?? null);
  const gesamt = aggregate(alle);

  // One button for the whole group, and it follows the group's own state
  // rather than carrying two controls that are wrong half the time (jdp:
  // "evtl mit play und stop button auf der card sie sich automatisch an den
  // zustand anpassen"). Halted everywhere means the offer is "start"; anything
  // else means "stop".
  const toggleAll = async () => {
    setBusy(true);
    setQueueError('');
    try {
      const list = await listConnections();
      // Settled, not all: one unreachable instance must not cancel the others,
      // and every failure is COLLECTED rather than swallowed. The first cut
      // wrote `.catch(() => {})` here, which is how a button that was failing
      // every time looked exactly like a button that did nothing.
      const results = await Promise.allSettled(list.map((conn) => setQueueHalted(conn, !gesamt.halted)));
      const failed = results.filter((r) => r.status === 'rejected') as PromiseRejectedResult[];
      if (failed.length > 0) {
        const reason = failed[0].reason;
        setQueueError(`${failed.length}/${list.length}: ${reason instanceof Error ? reason.message : String(reason)}`);
      }
      await load();
    } catch (e) {
      setQueueError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const activate = async (conn: ServerConnection) => {
    await setActiveConnectionId(conn.id);
    onActivate(conn);
  };


  return (
    <View style={[styles.container, { backgroundColor: c.bg }]}>
      <View style={styles.topBar}>
        <View style={styles.brand}>
          <Image source={MARK} style={styles.mark} resizeMode="contain" />
          <Text style={[styles.title, { color: c.text }]}>KnightLoader</Text>
        </View>
        <View style={styles.badgeRow}>
          <IconBadge symbol="+" accent onPress={onAddPress} accessibilityLabel={t('connections.addButton')} />
          <IconBadge
            icon={<Gear color={c.textSub} hole={c.surface2} />}
            onPress={onOpenSettings}
            accessibilityLabel={t('settings.title')}
          />
        </View>
      </View>

      <FlatList
        data={connections}
        // The summary, the failure line and the graph travel as the list's own
        // HEADER rather than as siblings above it (jdp, 2026-08-31: "in der
        // Übesicht soll die alle instanzen card genauso groß sein wie die
        // instanzen card"). As a sibling the card carried its own copy of the
        // list's width cap plus a horizontal margin, and `width: '100%'` with a
        // margin does not shrink the way padding inside a container does - so it
        // came out wider than the cards it summarises. Inside the content
        // container there is no second copy to disagree.
        //
        // On a tablet the header spans BOTH columns, which is right: it is the
        // group's own line, not a third card competing for a slot.
        ListHeaderComponent={
          connections.length > 0 ? (
            <View style={styles.header}>
              <View style={[styles.summary, { backgroundColor: c.surface, borderRadius: radii.card }]}>
                <View style={styles.summaryText}>
                  <Text style={[styles.summaryTitle, { color: c.text }]}>{t('overview.title')}</Text>
                  <Text style={[styles.summaryLine, { color: c.textMuted }]} numberOfLines={1}>
                    {[
                      t('overview.online', { n: gesamt.online, total: gesamt.total }),
                      `${gesamt.files} ${t('instance.files')}`,
                      gesamt.remaining > 0 ? `${fmtBytes(gesamt.remaining)} ${t('instance.left')}` : null,
                      gesamt.speed > 0 ? `${fmtBytes(gesamt.speed)}/s` : null,
                    ]
                      .filter(Boolean)
                      .join(' · ')}
                  </Text>
                </View>
                {/* Only offered when at least one instance answered: a start
                    button over a group that is entirely unreachable promises
                    something it cannot do. */}
                {gesamt.online > 0 && (
                  <IconBadge
                    symbol={gesamt.halted ? '▶' : '■'}
                    accent={gesamt.halted}
                    onPress={busy ? () => {} : toggleAll}
                    accessibilityLabel={t(gesamt.halted ? 'downloads.start' : 'downloads.stop')}
                  />
                )}
              </View>

              {/* Why the last start/stop did not take. One line, in the fail
                  colour, and only when there is something to say. */}
              {queueError !== '' && (
                <Text style={[styles.queueError, { color: c.statusFailSolid }]} numberOfLines={2}>
                  {queueError}
                </Text>
              )}

              {/* The graph belongs to the summary, not to a card: it is the
                  group's speed. Mounted only while something is moving. */}
              {gesamt.speed > 0 && (
                <View style={styles.summaryGraph}>
                  <SpeedGraph speed={gesamt.speed} />
                </View>
              )}
            </View>
          ) : null
        }
        // Two columns on a tablet, one on a phone. key is tied to the count
        // because FlatList cannot change numColumns on an existing list - it
        // throws rather than re-laying out - so the list is remounted on a
        // rotation instead, which is cheap for a handful of cards and is the
        // documented way to do this.
        key={wide ? 'zwei' : 'eins'}
        numColumns={wide ? 2 : 1}
        columnWrapperStyle={wide ? styles.columns : undefined}
        keyExtractor={(conn) => conn.id}
        contentContainerStyle={[styles.list, { maxWidth: contentMax(wide) }]}
        renderItem={({ item }) => {
          const s = status[item.id] ?? 'checking';
          return (
            <TouchableOpacity
              style={[styles.row, { backgroundColor: c.surface, borderRadius: radii.card }]}
              onPress={() => activate(item)}
            >
              <View
                style={[
                  styles.dot,
                  {
                    borderRadius: radii.pill,
                    backgroundColor:
                      s === 'online' ? c.statusOkSolid : s === 'checking' ? c.statusWarnSolid : c.statusFailSolid,
                  },
                ]}
              />
              <View style={styles.rowText}>
                <Text style={[styles.rowName, { color: c.text }]}>{item.name}</Text>
                {/* What the instance is DOING, not where the connection goes
                    (jdp, 2026-08-30: "über relay text soll nicht dort stehen.
                    dort sollen die gleichen infos in der card stehen wie in
                    den cards in der browsererweiterung"). The relay address
                    answered a question nobody was asking - it is the same for
                    every card in the list, so it distinguished nothing while
                    taking the one line that could have. Same four figures and
                    the same order as the extension's own card. */}
                <Text style={[styles.rowUrl, { color: c.textMuted }]} numberOfLines={1}>
                  {stats[item.id] === null && why[item.id] ? why[item.id] : statusLine(t, stats[item.id], s)}
                </Text>
              </View>
              {/* No delete here any more (jdp, 2026-08-30: "der löschenbutton
                  soll nur in der instanz drinnen zu sehen sein, nicht auf der
                  card"). A bin sitting on every row of a list you tap to open
                  is a mis-tap waiting to happen, and it competed with the only
                  action the row actually has. It lives inside the instance
                  now, where the thing being removed is what you are looking
                  at - see DownloadsScreen. */}
            </TouchableOpacity>
          );
        }}
        ListEmptyComponent={
          loaded ? (
            <View style={styles.empty}>
              <Text style={[styles.emptyText, { color: c.textMuted }]}>{t('connections.empty')}</Text>
              <TouchableOpacity
                style={[styles.emptyButton, { backgroundColor: accent, borderRadius: radii.control }]}
                onPress={onAddPress}
              >
                <Text style={[styles.emptyButtonText, { color: accentContrast }]}>{t('connections.emptyButton')}</Text>
              </TouchableOpacity>
            </View>
          ) : null
        }
      />
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens, never baked
// in here: a stylesheet is built once and cannot follow a theme change.
// One column stretched across a tablet is a card 900 points wide with its
// text at one edge and its badge at the other. A cap plus centring costs a
// phone nothing (640 is wider than every phone) and makes a tablet readable.
const capped = { width: '100%' as const, maxWidth: 640, alignSelf: 'center' as const };

const styles = StyleSheet.create({
  container: { flex: 1 },
  topBar: { ...capped,
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    padding: 16,
    paddingTop: 56,
  },
  // The list's header block. No width, no cap, no horizontal margin: it lives
  // inside the list's own content container, which already carries all three.
  // Every one of those it used to repeat was a chance to disagree, and it did.
  header: { marginBottom: 8 },
  summary: {
    // Same box as a row below it (jdp: "Übersichtscard soll genau so groß sein
    // wie die instanzencards"): same padding, same radius, same width - the
    // last one now by construction rather than by matching numbers.
    padding: 14,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
  },
  summaryText: { flex: 1, minWidth: 0, gap: 2 },
  summaryTitle: { fontSize: 15, fontWeight: '600' },
  summaryLine: { fontSize: TYPE.dense },
  queueError: { marginTop: 8, fontSize: TYPE.caption },
  summaryGraph: { marginTop: 10 },
  brand: { flexDirection: 'row', alignItems: 'center', gap: 10, flexShrink: 1, minWidth: 0 },
  // 32, so the mark reads as a mark beside a 22px title rather than as a
  // second heading. The asset is square, so both sides are set.
  // Drawn larger than the old tile: an adaptive icon's foreground layer carries
  // the safe-zone padding every launcher crops into, so at 32 the mark itself
  // would come out noticeably smaller than the tile it replaces.
  mark: { width: 44, height: 44, marginLeft: -6 },
  title: { fontSize: 22, fontWeight: '700' },
  badgeRow: { flexDirection: 'row', gap: 10 },
  list: { ...capped, paddingHorizontal: 16, paddingBottom: 32, gap: 8 },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    padding: 14,
    gap: 12,
    // flex only bites inside a column wrapper, where two siblings share a
    // line; in a one-column list it is a no-op.
    flex: 1,
  },
  columns: { gap: 8 },
  dot: { width: 10, height: 10 },
  rowText: { flex: 1, minWidth: 0 },
  rowName: { fontSize: 15, fontWeight: '600' },
  rowUrl: { fontSize: TYPE.dense, marginTop: 2 },
  empty: { alignItems: 'center', marginTop: 64, gap: 16, paddingHorizontal: 32 },
  emptyText: { fontSize: TYPE.body, textAlign: 'center' },
  emptyButton: { paddingVertical: 12, paddingHorizontal: 20 },
  emptyButtonText: { fontSize: TYPE.body, fontWeight: '600' },
});
