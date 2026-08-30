import { useEffect, useState } from 'react';
import { ActivityIndicator, FlatList, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { fetchQueue, liveTasks, setQueueHalted } from '../api/client';
import type { Instance, QueueState, ServerConnection, Task } from '../api/types';
import TaskRow from '../components/TaskRow';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import IconBadge, { Gear, Trash } from '../components/IconBadge';
import SpeedGraph from '../components/SpeedGraph';
import { fmtBytes } from '../api/stats';

// peer, when set, means this screen is showing a FEDERATION PEER of conn
// rather than conn's own queue: base becomes the proxy prefix
// (/api/instances/{name}, see internal/api/routes_federation.go).
//
// Whether the task list then streams or polls is liveTasks' decision, not
// this screen's - a federation peer and a relay connection both forward
// plain REST calls with no socket to attach to, and only api/client.ts knows
// which of those is in play.
export default function DownloadsScreen({
  conn,
  peer,
  onAddPress,
  onSwitchConnection,
  onOpenSettings,
  onBackToOwn,
  onRemoveConnection,
}: {
  conn: ServerConnection;
  peer?: Instance;
  onAddPress: () => void;
  onSwitchConnection: () => void;
  onOpenSettings: () => void;
  onBackToOwn?: () => void;
  /** Undefined for a federation peer: a peer is not a saved connection, so
   *  there is nothing here to remove. */
  onRemoveConnection?: () => void;
}) {
  const { t } = useT();
  const { c, accent, accentInk, accentContrast, radii } = useAppearance();
  const base = peer ? `/api/instances/${encodeURIComponent(peer.name)}` : '/api';
  const [tasks, setTasks] = useState<Task[]>([]);
  const [connected, setConnected] = useState(false);
  const [queue, setQueue] = useState<QueueState | null>(null);
  const [queueBusy, setQueueBusy] = useState(false);
  // Summed from the task list this screen already streams: no second request,
  // and no second truth about the same number.
  const speed = tasks.reduce((n, t) => n + (t.speed || 0), 0);

  useEffect(() => {
    setConnected(false);
    setTasks([]);
    const onSnapshot = (snapshot: Task[]) => {
      setConnected(true);
      setTasks(snapshot);
    };
    const onError = () => setConnected(false);
    return liveTasks(conn, base, onSnapshot, onError);
  }, [conn, base, peer]);

  useEffect(() => {
    let alive = true;
    setQueue(null);
    const load = () => fetchQueue(conn, base).then((q) => alive && setQueue(q)).catch(() => {});
    load();
    const iv = setInterval(load, 5000);
    return () => {
      alive = false;
      clearInterval(iv);
    };
  }, [conn, base]);

  const toggleHalted = async (nextHalted: boolean) => {
    setQueueBusy(true);
    try {
      setQueue(await setQueueHalted(conn, nextHalted, base));
    } finally {
      setQueueBusy(false);
    }
  };

  return (
    <View style={[styles.container, { backgroundColor: c.bg }]}>
      <View style={styles.topBar}>
        <View style={styles.topBarLeft}>
          {peer && onBackToOwn ? (
            <TouchableOpacity onPress={onBackToOwn}>
              <Text style={[styles.back, { color: c.textMuted }]}>‹ {conn.name}</Text>
            </TouchableOpacity>
          ) : null}
          <Text style={[styles.title, { color: c.text }]}>{peer ? (peer.displayName ?? peer.name) : conn.name}</Text>
          {/* Only while it is NOT connected (jdp, 2026-08-30: "der
              verbundentext soll weg"). "verbunden" is the ordinary case, so it
              said nothing on the screen it occupied - a label that is true
              almost always is a label nobody reads. Still connecting, or
              dropped, is worth saying, so that half stays. */}
          {!connected && (
            <Text style={[styles.connState, { color: c.statusWarnSolid }]}>
              {t('downloads.connecting')}
            </Text>
          )}
        </View>
        <View style={styles.topBarRight}>
          {/* One way back, not two (jdp, 2026-08-30: "Wenn man in einer
              instanz ist soll oben der button 'Instanzen' weg" / "Der button
              Wechseln soll 'Übersicht' heißen"). Both used to lead to a list
              of instances, and the Übersicht - the screen this app opens on -
              is that list: every member of the group is a connection there.
              What the removed link led to was the federation-peer view, which
              still carried the name-and-address form the phrase replaced; see
              App.tsx for what went with it. */}
          {/* A real button, not a text link (jdp, 2026-08-30: "Übersicht
              soll auch ein button sein"): it stands in a row of square badges,
              and a bare word among them reads as a caption that happens to be
              tappable. Text rather than a glyph, because "Übersicht" has no
              symbol anybody would recognise. */}
          {!peer && (
            <TouchableOpacity
              style={[styles.linkButton, { backgroundColor: c.surface2, borderRadius: radii.control }]}
              onPress={onSwitchConnection}
            >
              <Text style={[styles.link, { color: c.text }]}>{t('downloads.overviewLink')}</Text>
            </TouchableOpacity>
          )}
          {/* Removing THIS connection belongs here, on the thing being removed
              (jdp, 2026-08-30: "der löschenbutton soll nur in der instanz
              drinnen zu sehen sein, nicht auf der card"). On the overview it
              sat on every row of a list you tap to open, which is a mis-tap
              waiting to happen. */}
          {!peer && onRemoveConnection && (
            <IconBadge
              icon={<Trash color={c.textSub} />}
              onPress={onRemoveConnection}
              accessibilityLabel={t('connections.remove')}
            />
          )}
          {!peer && <IconBadge
            icon={<Gear color={c.textSub} hole={c.surface2} />}
            onPress={onOpenSettings}
            accessibilityLabel={t('settings.title')}
          />}
        </View>
      </View>

      <View style={[styles.queueBar, { backgroundColor: c.surface, borderRadius: radii.card }]}>
        <Text style={[styles.queueLabel, { color: c.textMuted }]}>
          {queue ? (queue.halted ? t('downloads.queueHalted') : t('downloads.queueRunning')) : '—'}
          {queue && queue.running > 0 ? ` · ${t('downloads.queueActive', { n: queue.running })}` : ''}
          {speed > 0 ? ` · ${fmtBytes(speed)}/s` : ''}
        </Text>
        {/* Two square badges, not a switch (jdp, 2026-08-30: "in der instanz
            drinnen soll man die download starten und stoppen können"). A
            switch says "on/off" about a thing whose verbs are start and stop,
            and it is the same control the summary card above the overview
            uses - one badge whose offer follows the state, so it can never
            offer the thing that is already true. */}
        {queueBusy ? (
          <ActivityIndicator color={accentInk} size="small" />
        ) : (
          <IconBadge
            symbol={queue?.halted ? '▶' : '■'}
            accent={queue?.halted === true}
            onPress={() => queue && toggleHalted(!queue.halted)}
            accessibilityLabel={t(queue?.halted ? 'downloads.start' : 'downloads.stop')}
          />
        )}
      </View>

      {/* Only while something is actually moving (jdp: "ein downloadgraph soll
          die geschwindigkeit anzeigen wenn der download läuft"). An idle graph
          is a row of nothing that still costs the height of a graph. */}
      {speed > 0 && (
        <View style={styles.graph}>
          <SpeedGraph speed={speed} />
        </View>
      )}

      <FlatList
        data={tasks}
        keyExtractor={(t) => t.id}
        renderItem={({ item, index }) => <TaskRow task={item} index={index} />}
        contentContainerStyle={styles.list}
        ListEmptyComponent={
          <Text style={[styles.empty, { color: c.textMuted }]}>
            {connected ? t('downloads.empty') : t('downloads.emptyConnecting')}
          </Text>
        }
      />

      <TouchableOpacity
        style={[styles.fab, { backgroundColor: accent, borderRadius: radii.pill }]}
        onPress={onAddPress}
      >
        <Text style={[styles.fabText, { color: accentContrast }]}>+</Text>
      </TouchableOpacity>
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens, never baked
// in here: a stylesheet is built once and cannot follow a theme change.
// One column stretched across a tablet is a card 900 points wide with its
// text at one edge and its badge at the other. A cap plus centring costs a
// phone nothing (640 is wider than every phone) and makes a tablet readable.
const wide = { width: '100%' as const, maxWidth: 640, alignSelf: 'center' as const };

const styles = StyleSheet.create({
  container: { flex: 1 },
  topBar: { ...wide,
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'flex-start',
    padding: 16,
    paddingTop: 56,
  },
  topBarLeft: { minWidth: 0, flexShrink: 1 },
  topBarRight: { flexDirection: 'row', gap: 16, alignItems: 'center' },
  back: { fontSize: 13, marginBottom: 4 },
  title: { fontSize: TYPE.heading, fontWeight: '600' },
  connState: { fontSize: TYPE.dense, marginTop: 2 },
  link: { fontSize: 13, fontWeight: '600' },
  queueBar: { ...wide,
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginHorizontal: 16,
    marginBottom: 12,
    paddingHorizontal: 14,
    paddingVertical: 10,
  },
  queueLabel: { fontSize: 13 },
  graph: { ...wide, marginHorizontal: 16, marginBottom: 10 },
  // The same height as the badges beside it, so the row reads as one set.
  linkButton: { height: 36, paddingHorizontal: 12, alignItems: 'center', justifyContent: 'center' },
  list: { ...wide, paddingHorizontal: 16, paddingBottom: 96 },
  empty: { textAlign: 'center', marginTop: 48 },
  fab: {
    position: 'absolute',
    right: 20,
    bottom: 32,
    width: 56,
    height: 56,
    alignItems: 'center',
    justifyContent: 'center',
    elevation: 4,
  },
  fabText: { fontSize: 28, lineHeight: 30, fontWeight: '400' },
});
