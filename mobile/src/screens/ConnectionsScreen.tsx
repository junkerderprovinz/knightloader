import { useCallback, useEffect, useState } from 'react';
import { FlatList, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { checkConnection } from '../api/client';
import { listConnections, removeConnection, setActiveConnectionId } from '../storage/connections';
import type { ServerConnection } from '../api/types';
import { colors } from '../theme';

type ConnStatus = 'checking' | 'online' | 'offline';

// The home screen once more than one server is saved: every KnightLoader
// this phone knows about directly, each with its own token. A tap makes one
// active and opens its Downloads screen; this screen itself never shows
// tasks, so it stays fast to scan even with several boxes on flaky Wi-Fi.
export default function ConnectionsScreen({
  onActivate,
  onAddPress,
}: {
  onActivate: (conn: ServerConnection) => void;
  onAddPress: () => void;
}) {
  const [connections, setConnections] = useState<ServerConnection[]>([]);
  const [status, setStatus] = useState<Record<string, ConnStatus>>({});
  const [loaded, setLoaded] = useState(false);

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

  const activate = async (conn: ServerConnection) => {
    await setActiveConnectionId(conn.id);
    onActivate(conn);
  };

  const remove = async (id: string) => {
    await removeConnection(id);
    await reload();
  };

  return (
    <View style={styles.container}>
      <View style={styles.topBar}>
        <Text style={styles.title}>KnightLoader</Text>
        <TouchableOpacity style={styles.addButton} onPress={onAddPress}>
          <Text style={styles.addButtonText}>+ Verbindung</Text>
        </TouchableOpacity>
      </View>

      <FlatList
        data={connections}
        keyExtractor={(c) => c.id}
        contentContainerStyle={styles.list}
        renderItem={({ item }) => {
          const s = status[item.id] ?? 'checking';
          return (
            <TouchableOpacity style={styles.row} onPress={() => activate(item)}>
              <View
                style={[
                  styles.dot,
                  { backgroundColor: s === 'online' ? colors.success : s === 'checking' ? colors.warning : colors.danger },
                ]}
              />
              <View style={styles.rowText}>
                <Text style={styles.rowName}>{item.name}</Text>
                <Text style={styles.rowUrl} numberOfLines={1}>
                  {item.baseUrl}
                </Text>
              </View>
              <TouchableOpacity style={styles.removeButton} onPress={() => remove(item.id)}>
                <Text style={styles.removeText}>Entfernen</Text>
              </TouchableOpacity>
            </TouchableOpacity>
          );
        }}
        ListEmptyComponent={
          loaded ? (
            <View style={styles.empty}>
              <Text style={styles.emptyText}>Noch keine Verbindung gespeichert.</Text>
              <TouchableOpacity style={styles.emptyButton} onPress={onAddPress}>
                <Text style={styles.emptyButtonText}>Erste Verbindung hinzufügen</Text>
              </TouchableOpacity>
            </View>
          ) : null
        }
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: colors.background },
  topBar: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    padding: 16,
    paddingTop: 56,
  },
  title: { color: colors.text, fontSize: 22, fontWeight: '700' },
  addButton: { backgroundColor: colors.accent, borderRadius: 8, paddingVertical: 8, paddingHorizontal: 14 },
  addButtonText: { color: colors.text, fontSize: 13, fontWeight: '600' },
  list: { paddingHorizontal: 16, paddingBottom: 32, gap: 8 },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: colors.surface,
    borderRadius: 8,
    padding: 14,
    gap: 12,
  },
  dot: { width: 10, height: 10, borderRadius: 5 },
  rowText: { flex: 1, minWidth: 0 },
  rowName: { color: colors.text, fontSize: 15, fontWeight: '600' },
  rowUrl: { color: colors.textMuted, fontSize: 12, marginTop: 2 },
  removeButton: { paddingHorizontal: 8, paddingVertical: 4 },
  removeText: { color: colors.danger, fontSize: 12 },
  empty: { alignItems: 'center', marginTop: 64, gap: 16, paddingHorizontal: 32 },
  emptyText: { color: colors.textMuted, fontSize: 14, textAlign: 'center' },
  emptyButton: { backgroundColor: colors.accent, borderRadius: 8, paddingVertical: 12, paddingHorizontal: 20 },
  emptyButtonText: { color: colors.text, fontSize: 14, fontWeight: '600' },
});
