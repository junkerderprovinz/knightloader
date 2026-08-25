import { useCallback, useEffect, useState } from 'react';
import { FlatList, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { checkConnection } from '../api/client';
import { listConnections, removeConnection, setActiveConnectionId } from '../storage/connections';
import { isRelayConnection, type ServerConnection } from '../api/types';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
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
  const { c, accent, accentContrast, radii } = useAppearance();
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
    <View style={[styles.container, { backgroundColor: c.bg }]}>
      <View style={styles.topBar}>
        <Text style={[styles.title, { color: c.text }]}>KnightLoader</Text>
        <View style={styles.badgeRow}>
          <IconBadge symbol="+" accent onPress={onAddPress} accessibilityLabel={t('connections.addButton')} />
          <IconBadge symbol="⚙" onPress={onOpenSettings} accessibilityLabel={t('settings.title')} />
        </View>
      </View>

      <FlatList
        data={connections}
        keyExtractor={(conn) => conn.id}
        contentContainerStyle={styles.list}
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
                <Text style={[styles.rowUrl, { color: c.textMuted }]} numberOfLines={1}>
                  {isRelayConnection(item) ? t('connections.viaRelay', { relay: item.relayUrl }) : item.baseUrl}
                </Text>
              </View>
              <TouchableOpacity style={styles.removeButton} onPress={() => remove(item.id)}>
                <Text style={[styles.removeText, { color: c.statusFailSolid }]}>{t('connections.remove')}</Text>
              </TouchableOpacity>
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
const styles = StyleSheet.create({
  container: { flex: 1 },
  topBar: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    padding: 16,
    paddingTop: 56,
  },
  title: { fontSize: 22, fontWeight: '700' },
  badgeRow: { flexDirection: 'row', gap: 10 },
  list: { paddingHorizontal: 16, paddingBottom: 32, gap: 8 },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    padding: 14,
    gap: 12,
  },
  dot: { width: 10, height: 10 },
  rowText: { flex: 1, minWidth: 0 },
  rowName: { fontSize: 15, fontWeight: '600' },
  rowUrl: { fontSize: TYPE.dense, marginTop: 2 },
  removeButton: { paddingHorizontal: 8, paddingVertical: 4 },
  removeText: { fontSize: TYPE.dense },
  empty: { alignItems: 'center', marginTop: 64, gap: 16, paddingHorizontal: 32 },
  emptyText: { fontSize: TYPE.body, textAlign: 'center' },
  emptyButton: { paddingVertical: 12, paddingHorizontal: 20 },
  emptyButtonText: { fontSize: TYPE.body, fontWeight: '600' },
});
