import { useEffect, useState } from 'react';
import { FlatList, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { subscribeTasks } from '../api/client';
import { clearConnection } from '../storage/connection';
import type { ServerConnection, Task } from '../api/types';
import TaskRow from '../components/TaskRow';
import { colors } from '../theme';

export default function DownloadsScreen({
  conn,
  onAddPress,
  onDisconnect,
}: {
  conn: ServerConnection;
  onAddPress: () => void;
  onDisconnect: () => void;
}) {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    setConnected(false);
    const unsubscribe = subscribeTasks(
      conn,
      (snapshot) => {
        setConnected(true);
        setTasks(snapshot);
      },
      () => setConnected(false)
    );
    return unsubscribe;
  }, [conn]);

  const disconnect = async () => {
    await clearConnection();
    onDisconnect();
  };

  return (
    <View style={styles.container}>
      <View style={styles.topBar}>
        <View>
          <Text style={styles.title}>{conn.name}</Text>
          <Text style={[styles.connState, { color: connected ? colors.success : colors.warning }]}>
            {connected ? 'verbunden' : 'verbinde…'}
          </Text>
        </View>
        <TouchableOpacity onPress={disconnect}>
          <Text style={styles.disconnect}>Trennen</Text>
        </TouchableOpacity>
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
  title: { color: colors.text, fontSize: 20, fontWeight: '600' },
  connState: { fontSize: 12, marginTop: 2 },
  disconnect: { color: colors.textMuted, fontSize: 13 },
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
