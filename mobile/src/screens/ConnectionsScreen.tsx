import { useCallback, useEffect, useState } from 'react';
import { FlatList, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { checkConnection } from '../api/client';
import { listConnections, removeConnection, setActiveConnectionId } from '../storage/connections';
import type { ServerConnection } from '../api/types';
import { colors } from '../theme';
import { useT } from '../i18n/I18nContext';
import IconBadge from '../components/IconBadge';

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
        <View style={styles.badgeRow}>
          <IconBadge symbol="+" accent onPress={onAddPress} accessibilityLabel={t('connections.addButton')} />
          <IconBadge symbol="⚙" onPress={onOpenSettings} accessibilityLabel={t('settings.title')} />
        </View>
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
                <Text style={styles.removeText}>{t('connections.remove')}</Text>
              </TouchableOpacity>
            </TouchableOpacity>
          );
        }}
        ListEmptyComponent={
          loaded ? (
            <View style={styles.empty}>
              <Text style={styles.emptyText}>{t('connections.empty')}</Text>
              <TouchableOpacity style={styles.emptyButton} onPress={onAddPress}>
                <Text style={styles.emptyButtonText}>{t('connections.emptyButton')}</Text>
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
  badgeRow: { flexDirection: 'row', gap: 10 },
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
