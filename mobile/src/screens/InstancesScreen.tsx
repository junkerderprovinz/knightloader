import { useCallback, useEffect, useState } from 'react';
import { FlatList, StyleSheet, Text, TextInput, TouchableOpacity, View } from 'react-native';
import { addInstance, ApiError, fetchInstances, redeemPairingCode, removeInstance } from '../api/client';
import type { Instance, ServerConnection } from '../api/types';
import { colors } from '../theme';
import { useT } from '../i18n/I18nContext';
import QRScanner from '../components/QRScanner';

// The peers the CONNECTED server itself knows about (GET /api/instances) -
// this app's equivalent of the web UI's Instances.tsx. Adding one either
// takes name+address by hand (POST /api/instances) or a pairing code from
// that peer's own Access tab, pasted or scanned: that code already carries
// name+address+a one-time token and registers both directions in one call
// (routes_pairing.go), so scanning it here is a genuine one-scan add - see
// ConnectScreen's own doc comment on why adding a NEW direct connection to
// THIS app can't do that yet.
export default function InstancesScreen({
  conn,
  onOpenInstance,
}: {
  conn: ServerConnection;
  onOpenInstance: (peer: Instance) => void;
}) {
  const { t } = useT();
  const [peers, setPeers] = useState<Instance[]>([]);
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [addError, setAddError] = useState('');
  const [code, setCode] = useState('');
  const [pairing, setPairing] = useState(false);
  const [pairError, setPairError] = useState('');
  const [pairOk, setPairOk] = useState('');
  const [scanning, setScanning] = useState(false);

  const reload = useCallback(() => {
    fetchInstances(conn).then(setPeers).catch(() => {});
  }, [conn]);

  useEffect(() => {
    reload();
  }, [reload]);

  const onAdd = async () => {
    setAddError('');
    try {
      const r = await addInstance(conn, name.trim(), url.trim());
      if (!r.online) setAddError(t('instances.addOfflineWarning'));
      setName('');
      setUrl('');
      reload();
    } catch (err) {
      setAddError(err instanceof ApiError ? err.message : t('instances.addError'));
    }
  };

  const redeem = async (rawCode: string) => {
    setPairError('');
    setPairOk('');
    setPairing(true);
    try {
      const r = await redeemPairingCode(conn, rawCode.trim());
      setPairOk(t('instances.pairSuccess', { name: r.name }) + (r.online ? '' : t('instances.pairSuccessOffline')));
      setCode('');
      reload();
    } catch (err) {
      setPairError(err instanceof ApiError ? err.message : t('instances.pairError'));
    } finally {
      setPairing(false);
    }
  };

  const remove = async (peerName: string) => {
    await removeInstance(conn, peerName);
    reload();
  };

  return (
    <View style={styles.container}>
      <View style={styles.topBar}>
        <Text style={styles.title}>{t('instances.title')}</Text>
        <Text style={styles.subtitle}>{t('instances.subtitle', { name: conn.name })}</Text>
      </View>

      <FlatList
        data={peers}
        keyExtractor={(p) => p.name}
        contentContainerStyle={styles.list}
        renderItem={({ item }) => (
          <TouchableOpacity style={styles.row} onPress={() => onOpenInstance(item)}>
            <View style={styles.rowText}>
              <Text style={styles.rowName}>{item.displayName ?? item.name}</Text>
              <Text style={styles.rowUrl} numberOfLines={1}>
                {item.relayId ? t('instances.viaRelay') : item.url}
              </Text>
            </View>
            {/* No remove for a relay peer: it is synthesised per request from
                whoever is connected to the relay right now
                (federation.Manager.reachable) and is never in the stored list,
                so removing it deleted nothing while still answering 204 - the
                row simply came back on the next reload. It goes away by
                disconnecting it or clearing the relay config, not from here. */}
            {!item.relayId && (
              <TouchableOpacity style={styles.removeButton} onPress={() => remove(item.name)}>
                <Text style={styles.removeText}>{t('instances.remove')}</Text>
              </TouchableOpacity>
            )}
          </TouchableOpacity>
        )}
        ListEmptyComponent={<Text style={styles.empty}>{t('instances.empty')}</Text>}
      />

      <View style={styles.card}>
        <Text style={styles.cardTitle}>{t('instances.manualTitle')}</Text>
        <TextInput
          style={styles.input}
          placeholder={t('instances.namePlaceholder')}
          placeholderTextColor={colors.textMuted}
          value={name}
          onChangeText={setName}
          autoCapitalize="none"
        />
        <TextInput
          style={styles.input}
          placeholder={t('instances.urlPlaceholder')}
          placeholderTextColor={colors.textMuted}
          value={url}
          onChangeText={setUrl}
          autoCapitalize="none"
          autoCorrect={false}
          keyboardType="url"
        />
        <TouchableOpacity style={styles.button} onPress={onAdd} disabled={!name.trim() || !url.trim()}>
          <Text style={styles.buttonText}>{t('instances.addButton')}</Text>
        </TouchableOpacity>
        {addError && <Text style={styles.error}>{addError}</Text>}
      </View>

      <View style={styles.card}>
        <Text style={styles.cardTitle}>{t('instances.pairingTitle')}</Text>
        <Text style={styles.cardHint}>{t('instances.pairingHint')}</Text>
        <View style={styles.inputRow}>
          <TextInput
            style={[styles.input, styles.inputFlex]}
            placeholder={t('instances.codePlaceholder')}
            placeholderTextColor={colors.textMuted}
            value={code}
            onChangeText={setCode}
            autoCapitalize="none"
            autoCorrect={false}
          />
          <TouchableOpacity style={styles.scanButton} onPress={() => setScanning(true)}>
            <Text style={styles.scanButtonText}>{t('connect.qrButton')}</Text>
          </TouchableOpacity>
        </View>
        <TouchableOpacity
          style={[styles.button, (pairing || !code.trim()) && styles.buttonDisabled]}
          onPress={() => void redeem(code)}
          disabled={pairing || !code.trim()}
        >
          <Text style={styles.buttonText}>{pairing ? t('instances.redeeming') : t('instances.redeemButton')}</Text>
        </TouchableOpacity>
        {pairError && <Text style={styles.error}>{pairError}</Text>}
        {pairOk && <Text style={styles.success}>{pairOk}</Text>}
      </View>

      <QRScanner
        visible={scanning}
        hint={t('instances.scanHint')}
        onScanned={(data) => {
          setScanning(false);
          void redeem(data);
        }}
        onClose={() => setScanning(false)}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: colors.background },
  topBar: { padding: 16, paddingTop: 56 },
  title: { color: colors.text, fontSize: 20, fontWeight: '600' },
  subtitle: { color: colors.textMuted, fontSize: 13, marginTop: 2 },
  list: { paddingHorizontal: 16, gap: 8 },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: colors.surface,
    borderRadius: 8,
    padding: 14,
    gap: 12,
  },
  rowText: { flex: 1, minWidth: 0 },
  rowName: { color: colors.text, fontSize: 15, fontWeight: '600' },
  rowUrl: { color: colors.textMuted, fontSize: 12, marginTop: 2 },
  removeButton: { paddingHorizontal: 8, paddingVertical: 4 },
  removeText: { color: colors.danger, fontSize: 12 },
  empty: { color: colors.textMuted, textAlign: 'center', marginTop: 16 },
  card: { backgroundColor: colors.surface, borderRadius: 8, padding: 16, margin: 16, marginTop: 8, gap: 10 },
  cardTitle: { color: colors.text, fontSize: 14, fontWeight: '600' },
  cardHint: { color: colors.textMuted, fontSize: 12, lineHeight: 17 },
  input: {
    backgroundColor: colors.surfaceRaised,
    color: colors.text,
    borderRadius: 8,
    paddingHorizontal: 12,
    paddingVertical: 10,
    fontSize: 14,
    borderWidth: 1,
    borderColor: colors.border,
  },
  inputRow: { flexDirection: 'row', gap: 8, alignItems: 'stretch' },
  inputFlex: { flex: 1 },
  scanButton: {
    backgroundColor: colors.surfaceRaised,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: colors.border,
    paddingHorizontal: 16,
    alignItems: 'center',
    justifyContent: 'center',
  },
  scanButtonText: { color: colors.accent, fontSize: 13, fontWeight: '700' },
  button: { backgroundColor: colors.accent, borderRadius: 8, paddingVertical: 12, alignItems: 'center' },
  buttonDisabled: { opacity: 0.6 },
  buttonText: { color: colors.text, fontSize: 14, fontWeight: '600' },
  error: { color: colors.danger, fontSize: 12 },
  success: { color: colors.success, fontSize: 12 },
});
