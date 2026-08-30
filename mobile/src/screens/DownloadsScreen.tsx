import { useEffect, useState } from 'react';
import { ActivityIndicator, FlatList, StyleSheet, Switch, Text, TouchableOpacity, View } from 'react-native';
import { fetchQueue, liveTasks, setQueueHalted } from '../api/client';
import type { Instance, QueueState, ServerConnection, Task } from '../api/types';
import TaskRow from '../components/TaskRow';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import IconBadge from '../components/IconBadge';

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
}: {
  conn: ServerConnection;
  peer?: Instance;
  onAddPress: () => void;
  onSwitchConnection: () => void;
  onOpenSettings: () => void;
  onBackToOwn?: () => void;
}) {
  const { t } = useT();
  const { c, accent, accentInk, accentContrast, radii } = useAppearance();
  const base = peer ? `/api/instances/${encodeURIComponent(peer.name)}` : '/api';
  const [tasks, setTasks] = useState<Task[]>([]);
  const [connected, setConnected] = useState(false);
  const [queue, setQueue] = useState<QueueState | null>(null);
  const [queueBusy, setQueueBusy] = useState(false);

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
          <Text style={[styles.connState, { color: connected ? c.statusOkSolid : c.statusWarnSolid }]}>
            {connected ? t('downloads.connected') : t('downloads.connecting')}
          </Text>
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
          {!peer && (
            <TouchableOpacity onPress={onSwitchConnection}>
              <Text style={[styles.link, { color: accentInk }]}>{t('downloads.overviewLink')}</Text>
            </TouchableOpacity>
          )}
          {!peer && <IconBadge symbol="⚙" onPress={onOpenSettings} accessibilityLabel={t('settings.title')} />}
        </View>
      </View>

      <View style={[styles.queueBar, { backgroundColor: c.surface, borderRadius: radii.card }]}>
        <Text style={[styles.queueLabel, { color: c.textMuted }]}>
          {queue ? (queue.halted ? t('downloads.queueHalted') : t('downloads.queueRunning')) : '—'}
          {queue && queue.running > 0 ? ` · ${t('downloads.queueActive', { n: queue.running })}` : ''}
        </Text>
        {queueBusy ? (
          <ActivityIndicator color={accentInk} size="small" />
        ) : (
          <Switch
            value={!!queue && !queue.halted}
            onValueChange={(on) => toggleHalted(!on)}
            disabled={!queue}
            trackColor={{ false: c.border, true: accent }}
            thumbColor={c.text}
          />
        )}
      </View>

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
const styles = StyleSheet.create({
  container: { flex: 1 },
  topBar: {
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
  queueBar: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginHorizontal: 16,
    marginBottom: 12,
    paddingHorizontal: 14,
    paddingVertical: 10,
  },
  queueLabel: { fontSize: 13 },
  list: { paddingHorizontal: 16, paddingBottom: 96 },
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
