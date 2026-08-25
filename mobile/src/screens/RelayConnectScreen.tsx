import { useCallback, useEffect, useRef, useState } from 'react';
import { ActivityIndicator, FlatList, StyleSheet, Text, TextInput, TouchableOpacity, View } from 'react-native';
import { closeRelayClient, connectURL, relayClientFor, type RelaySibling } from '../api/relayClient';
import { relayIdentity } from '../storage/relayIdentity';
import { addConnection, listConnections, setActiveConnectionId } from '../storage/connections';
import type { RelayConnection, ServerConnection } from '../api/types';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';

// The second way in, for the one case the direct one cannot cover: no
// instance is reachable from this network at all. Both ends dial out to a
// relay instead, so this screen asks for the relay's address and key, shows
// whatever instances are currently connected to it, and saves ONE of them as
// a connection.
//
// One instance per saved connection, deliberately, even though a relay key
// normally has several behind it: everywhere else in the app a connection is
// one KnightLoader you are looking at, and a relay connection that meant
// "several" would be the one entry in that list that behaved differently.
// Adding a second is this screen again, which by then already lists it.
//
// The relay KEY is a credential shared by every instance on it, so a saved
// relay connection is stored in the keychain exactly like a token is - see
// storage/connections.ts.
const MIN_KEY_LENGTH = 16; // relay.minKeyLength, server side

export default function RelayConnectScreen({ onConnected }: { onConnected: (conn: ServerConnection) => void }) {
  const { t } = useT();
  const { c, accent, accentContrast, radii } = useAppearance();
  const [url, setUrl] = useState('');
  const [key, setKey] = useState('');
  const [searching, setSearching] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sibs, setSibs] = useState<RelaySibling[]>([]);
  const [picked, setPicked] = useState<RelaySibling | null>(null);
  const [name, setName] = useState('');
  const [token, setToken] = useState('');
  // What the running client was opened with. State, not a ref: the instance
  // list below only renders once this is set, so the screen has to re-render
  // when it changes.
  const [live, setLive] = useState<{ url: string; key: string } | null>(null);
  // The ref mirrors it purely for the unmount cleanup, which must see the
  // latest value rather than the one captured when the effect was set up.
  const liveRef = useRef<{ url: string; key: string } | null>(null);
  const unsubscribe = useRef<(() => void) | null>(null);

  // A client opened just to look around must not outlive the screen unless
  // something was actually saved with it - otherwise backing out of a typo
  // leaves a socket retrying against a relay nobody uses.
  const releaseIfUnused = useCallback(async () => {
    unsubscribe.current?.();
    unsubscribe.current = null;
    const open = liveRef.current;
    if (!open) return;
    const saved = await listConnections();
    const stillUsed = saved.some((sc) => sc.kind === 'relay' && sc.relayUrl === open.url && sc.relayKey === open.key);
    if (!stillUsed) closeRelayClient(open.url, open.key);
    liveRef.current = null;
  }, []);

  useEffect(() => () => void releaseIfUnused(), [releaseIfUnused]);

  const search = async () => {
    setError(null);
    setPicked(null);
    const address = url.trim();
    const relayKey = key.trim();
    if (!address || !relayKey) {
      setError(t('relay.errorMissing'));
      return;
    }
    if (!connectURL(address)) {
      setError(t('relay.errorBadUrl'));
      return;
    }
    // Checked here rather than left to the relay: it answers a short key by
    // closing the socket with a policy violation, which would surface as a
    // generic "could not connect" and send someone hunting the wrong problem.
    if (relayKey.length < MIN_KEY_LENGTH) {
      setError(t('relay.errorKeyShort', { n: MIN_KEY_LENGTH }));
      return;
    }

    await releaseIfUnused();
    setSearching(true);
    setSibs([]);
    const client = relayClientFor({
      url: address,
      key: relayKey,
      selfId: await relayIdentity(),
      selfName: 'KnightLoader app',
    });
    // Subscribed rather than passed in as an option: this client may already
    // exist for a saved connection, in which case constructor options are
    // never applied - see relayClientFor.
    unsubscribe.current = client.subscribe(() => setSibs(client.siblings()));
    liveRef.current = { url: address, key: relayKey };
    setLive({ url: address, key: relayKey });
    setSibs(client.siblings());
    // Siblings arrive asynchronously and there is no "that is all of them"
    // frame - the list simply fills in. This only stops the spinner; the list
    // below keeps updating from onChange for as long as the screen is open.
    setTimeout(() => setSearching(false), 3000);
  };

  const pick = (s: RelaySibling) => {
    setPicked(s);
    setName(s.name || s.instanceId);
    setError(null);
  };

  const save = async () => {
    if (!picked || !live) return;
    const conn: RelayConnection = {
      kind: 'relay',
      id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
      name: name.trim() || picked.name || picked.instanceId,
      relayUrl: live.url,
      relayKey: live.key,
      instanceId: picked.instanceId,
      token: token.trim(),
    };
    await addConnection(conn);
    await setActiveConnectionId(conn.id);
    // Deliberately NOT released on the way out now: the saved connection is
    // about to use this very client.
    liveRef.current = null;
    onConnected(conn);
  };

  const inputStyle = {
    backgroundColor: c.surface,
    color: c.text,
    borderColor: c.border,
    borderRadius: radii.control,
  };

  return (
    <View style={[styles.container, { backgroundColor: c.bg }]}>
      <Text style={[styles.title, { color: c.text }]}>{t('relay.title')}</Text>
      <Text style={[styles.hint, { color: c.textMuted }]}>{t('relay.hint')}</Text>

      <Text style={[styles.label, { color: c.textMuted }]}>{t('relay.urlLabel')}</Text>
      <TextInput
        style={[styles.input, inputStyle]}
        placeholder="https://relay.example.com"
        placeholderTextColor={c.textMuted}
        value={url}
        onChangeText={setUrl}
        autoCapitalize="none"
        autoCorrect={false}
        keyboardType="url"
      />

      <Text style={[styles.label, { color: c.textMuted }]}>{t('relay.keyLabel')}</Text>
      <TextInput
        style={[styles.input, inputStyle]}
        placeholder={t('relay.keyPlaceholder')}
        placeholderTextColor={c.textMuted}
        value={key}
        onChangeText={setKey}
        autoCapitalize="none"
        autoCorrect={false}
        secureTextEntry
      />

      <TouchableOpacity
        style={[
          styles.button,
          { backgroundColor: accent, borderRadius: radii.control },
          searching && styles.buttonDisabled,
        ]}
        onPress={search}
        disabled={searching}
      >
        {searching ? (
          <ActivityIndicator color={accentContrast} />
        ) : (
          <Text style={[styles.buttonText, { color: accentContrast }]}>{t('relay.searchButton')}</Text>
        )}
      </TouchableOpacity>

      {error && <Text style={[styles.error, { color: c.statusFailSolid }]}>{error}</Text>}

      {live && (
        <>
          <Text style={[styles.sectionTitle, { color: c.textMuted }]}>{t('relay.instancesTitle')}</Text>
          <FlatList
            data={sibs}
            keyExtractor={(s) => s.instanceId}
            style={styles.list}
            renderItem={({ item }) => (
              <TouchableOpacity
                style={[
                  styles.row,
                  { backgroundColor: c.surface, borderRadius: radii.card },
                  picked?.instanceId === item.instanceId && { borderWidth: 1, borderColor: accent },
                ]}
                onPress={() => pick(item)}
              >
                <View style={styles.rowText}>
                  <Text style={[styles.rowName, { color: c.text }]}>{item.name || item.instanceId}</Text>
                  <Text style={[styles.rowSub, { color: c.textMuted }]} numberOfLines={1}>
                    {item.deployment}
                  </Text>
                </View>
                {picked?.instanceId === item.instanceId && <Text style={[styles.check, { color: accent }]}>✓</Text>}
              </TouchableOpacity>
            )}
            ListEmptyComponent={
              searching ? null : <Text style={[styles.empty, { color: c.textMuted }]}>{t('relay.noInstances')}</Text>
            }
          />
        </>
      )}

      {picked && (
        <View style={styles.saveCard}>
          <Text style={[styles.label, { color: c.textMuted }]}>{t('connect.nameLabel')}</Text>
          <TextInput
            style={[styles.input, inputStyle]}
            placeholder={t('connect.namePlaceholder')}
            placeholderTextColor={c.textMuted}
            value={name}
            onChangeText={setName}
            autoCapitalize="none"
          />
          <Text style={[styles.label, { color: c.textMuted }]}>{t('connect.tokenLabel')}</Text>
          <TextInput
            style={[styles.input, inputStyle]}
            placeholder={t('connect.tokenPlaceholder')}
            placeholderTextColor={c.textMuted}
            value={token}
            onChangeText={setToken}
            autoCapitalize="none"
            autoCorrect={false}
            secureTextEntry
          />
          <Text style={[styles.tokenHint, { color: c.textMuted }]}>{t('relay.tokenHint')}</Text>
          <TouchableOpacity
            style={[styles.button, { backgroundColor: accent, borderRadius: radii.control }]}
            onPress={save}
          >
            <Text style={[styles.buttonText, { color: accentContrast }]}>{t('relay.saveButton')}</Text>
          </TouchableOpacity>
        </View>
      )}
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens, never baked
// in here: a stylesheet is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  container: { flex: 1, padding: 24, paddingTop: 56 },
  title: { fontSize: 22, fontWeight: '600', marginBottom: 8 },
  hint: { fontSize: TYPE.body, marginBottom: 16, lineHeight: 20 },
  label: { fontSize: 13, marginBottom: 6, marginTop: 12 },
  input: {
    paddingHorizontal: 14,
    paddingVertical: 12,
    fontSize: 15,
    borderWidth: 1,
  },
  button: {
    paddingVertical: 14,
    alignItems: 'center',
    marginTop: 16,
  },
  buttonDisabled: { opacity: 0.6 },
  buttonText: { fontSize: 16, fontWeight: '600' },
  error: { marginTop: 12, fontSize: TYPE.body },
  sectionTitle: { fontSize: TYPE.dense, fontWeight: '600', textTransform: 'uppercase', letterSpacing: 0.5, marginTop: 20 },
  list: { flexGrow: 0, marginTop: 8 },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    padding: 14,
    marginBottom: 6,
  },
  rowText: { flex: 1, minWidth: 0 },
  rowName: { fontSize: 15, fontWeight: '600' },
  rowSub: { fontSize: TYPE.dense, marginTop: 2 },
  check: { fontSize: 15, fontWeight: '700' },
  empty: { fontSize: 13, textAlign: 'center', marginTop: 12 },
  saveCard: { marginTop: 8 },
  tokenHint: { fontSize: TYPE.dense, marginTop: 6, lineHeight: 16 },
});
