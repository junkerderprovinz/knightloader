import { useEffect, useState } from 'react';
import { ActivityIndicator, FlatList, StyleSheet, Switch, Text, TouchableOpacity, View } from 'react-native';
import { fetchQueue, pollTasks, setQueueHalted, subscribeTasks } from '../api/client';
import type { Instance, QueueState, ServerConnection, Task } from '../api/types';
import TaskRow from '../components/TaskRow';
import { colors } from '../theme';

// peer, when set, means this screen is showing a FEDERATION PEER of conn
// rather than conn's own queue: base becomes the proxy prefix
// (/api/instances/{name}, see internal/api/routes_federation.go) and the
// live feed switches from the WebSocket subscription to polling, because
// the proxy only forwards plain REST calls, not a WebSocket upgrade - the
// web UI's own InstanceCard.tsx polls a peer's tasks for exactly this
// reason.
export default function DownloadsScreen({
  conn,
  peer,
  onAddPress,
  onSwitchConnection,
  onOpenInstances,
  onBackToOwn,
}: {
  conn: ServerConnection;
  peer?: Instance;
  onAddPress: () => void;
  onSwitchConnection: () => void;
  onOpenInstances: () => void;
  onBackToOwn?: () => void;
}) {
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
    const unsubscribe = peer ? pollTasks(conn, base, onSnapshot, onError) : subscribeTasks(conn, onSnapshot, onError);
    return unsubscribe;
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
    <View style={styles.container}>
      <View style={styles.topBar}>
        <View style={styles.topBarLeft}>
          {peer && onBackToOwn ? (
            <TouchableOpacity onPress={onBackToOwn}>
              <Text style={styles.back}>‹ {conn.name}</Text>
            </TouchableOpacity>
          ) : null}
          <Text style={styles.title}>{peer ? peer.name : conn.name}</Text>
          <Text style={[styles.connState, { color: connected ? colors.success : colors.warning }]}>
            {connected ? 'verbunden' : 'verbinde…'}
          </Text>
        </View>
        <View style={styles.topBarRight}>
          {!peer && (
            <TouchableOpacity onPress={onOpenInstances}>
              <Text style={styles.link}>Instanzen</Text>
            </TouchableOpacity>
          )}
          {!peer && (
            <TouchableOpacity onPress={onSwitchConnection}>
              <Text style={styles.link}>Wechseln</Text>
            </TouchableOpacity>
          )}
        </View>
      </View>

      <View style={styles.queueBar}>
        <Text style={styles.queueLabel}>
          {queue ? (queue.halted ? 'Angehalten' : 'Läuft') : '—'}
          {queue && queue.running > 0 ? ` · ${queue.running} aktiv` : ''}
        </Text>
        {queueBusy ? (
          <ActivityIndicator color={colors.accent} size="small" />
        ) : (
          <Switch
            value={!!queue && !queue.halted}
            onValueChange={(on) => toggleHalted(!on)}
            disabled={!queue}
            trackColor={{ false: colors.border, true: colors.accent }}
            thumbColor={colors.text}
          />
        )}
      </View>

      <FlatList
        data={tasks}
        keyExtractor={(t) => t.id}
        renderItem={({ item }) => <TaskRow task={item} />}
        contentContainerStyle={styles.list}
        ListEmptyComponent={
          <Text style={styles.empty}>{connected ? 'Keine Downloads.' : 'Verbinde mit dem Server…'}</Text>
        }
      />

      <TouchableOpacity style={styles.fab} onPress={onAddPress}>
        <Text style={styles.fabText}>+</Text>
      </TouchableOpacity>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: colors.background },
  topBar: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'flex-start',
    padding: 16,
    paddingTop: 56,
  },
  topBarLeft: { minWidth: 0, flexShrink: 1 },
  topBarRight: { flexDirection: 'row', gap: 16, alignItems: 'center' },
  back: { color: colors.textMuted, fontSize: 13, marginBottom: 4 },
  title: { color: colors.text, fontSize: 20, fontWeight: '600' },
  connState: { fontSize: 12, marginTop: 2 },
  link: { color: colors.accent, fontSize: 13, fontWeight: '600' },
  queueBar: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginHorizontal: 16,
    marginBottom: 12,
    backgroundColor: colors.surface,
    borderRadius: 8,
    paddingHorizontal: 14,
    paddingVertical: 10,
  },
  queueLabel: { color: colors.textMuted, fontSize: 13 },
  list: { paddingHorizontal: 16, paddingBottom: 96 },
  empty: { color: colors.textMuted, textAlign: 'center', marginTop: 48 },
  fab: {
    position: 'absolute',
    right: 20,
    bottom: 32,
    width: 56,
    height: 56,
    borderRadius: 28,
    backgroundColor: colors.accent,
    alignItems: 'center',
    justifyContent: 'center',
    elevation: 4,
  },
  fabText: { color: colors.text, fontSize: 28, lineHeight: 30, fontWeight: '400' },
});
