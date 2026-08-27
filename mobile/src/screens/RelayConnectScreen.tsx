import { useCallback, useEffect, useRef, useState } from 'react';
import { ActivityIndicator, FlatList, StyleSheet, Text, TextInput, TouchableOpacity, View } from 'react-native';
import { closeRelayClient, relayClientFor, type RelaySibling } from '../api/relayClient';
import { DEFAULT_RELAY_URL, PhraseError, frameKeyFromPhrase, keyFromPhrase } from '../api/seedphrase';
import { toHex } from '../api/sha256';
import { relayIdentity } from '../storage/relayIdentity';
import { addConnection, listConnections, setActiveConnectionId } from '../storage/connections';
import type { RelayConnection, ServerConnection } from '../api/types';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';

// Joining the group, which is the whole of connecting this app now: twelve
// words, and every instance the person runs appears.
//
// It replaces a screen that asked for a relay address, a relay key and then a
// per-instance API token, and saved exactly ONE instance per visit. All three
// of those were things to go and look up somewhere else, which is precisely
// what the phrase exists to abolish - and the one-at-a-time saving meant
// three instances were three trips through the same form.
//
// The phone is a full group member, not a client of one instance. It derives
// the same key its siblings derive (api/seedphrase.ts, a port of
// internal/seedphrase held to the Go side's own vectors), dials the same
// relay, and is authenticated by that: the server accepts a relay-delivered
// request from anything presenting the group key, so there is no token to
// enter any more. See relayProxyHandler's own comment for what that admits
// and what it does not.
//
// The phrase is a group credential, so a saved connection holds it in the
// keychain exactly as a token was - see storage/connections.ts.

// How long to keep the spinner up. Siblings arrive asynchronously and no
// frame says "that is all of them", so this is a pause for the list to fill,
// not a timeout: it keeps updating afterwards for as long as the screen is open.
const SETTLE_MS = 3000;

export default function RelayConnectScreen({ onConnected }: { onConnected: (conn: ServerConnection) => void }) {
  const { t } = useT();
  const { c, accent, accentContrast, radii } = useAppearance();
  const [phrase, setPhrase] = useState('');
  const [searching, setSearching] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sibs, setSibs] = useState<RelaySibling[]>([]);
  // What the running client was opened with. State, not a ref: the instance
  // list below only renders once this is set, so the screen has to re-render
  // when it changes.
  // frameKey rides along as hex because that is the form it is saved in - see
  // types.ts's relayFrameKey for why it is stored rather than re-derived.
  const [live, setLive] = useState<{ url: string; key: string; frameKey: string } | null>(null);
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

  // The checksum catches a mistyped or swapped word here, before anything is
  // dialled, so the failure reads as "word 3 is not one of the words" instead
  // of a socket that simply never finds anybody.
  const join = async () => {
    setError(null);
    let key: string;
    // Both keys come out of the phrase here, in the one place it exists, and
    // the words are then gone - the frame key is carried alongside the relay
    // key from this point on rather than re-derived, because there would be
    // nothing left to re-derive it from. See types.ts's relayFrameKey.
    let frameKey: Uint8Array;
    try {
      key = keyFromPhrase(phrase);
      frameKey = frameKeyFromPhrase(phrase);
    } catch (e) {
      setError(
        e instanceof PhraseError
          ? e.problem.reason === 'unknown_word'
            ? t('phrase.errUnknownWord', { position: e.problem.position, word: e.problem.word })
            : e.problem.reason === 'word_count'
              ? t('phrase.errWordCount', { count: e.problem.count, need: 12 })
              : t('phrase.errChecksum')
          : String(e),
      );
      return;
    }

    await releaseIfUnused();
    setSearching(true);
    setSibs([]);
    const client = relayClientFor({
      url: DEFAULT_RELAY_URL,
      key,
      frameKey,
      selfId: await relayIdentity(),
      selfName: 'KnightLoader app',
    });
    // Subscribed rather than passed in as an option: this client may already
    // exist for a saved connection, in which case constructor options are
    // never applied - see relayClientFor.
    unsubscribe.current = client.subscribe(() => setSibs(client.siblings()));
    liveRef.current = { url: DEFAULT_RELAY_URL, key };
    setLive({ url: DEFAULT_RELAY_URL, key, frameKey: toHex(frameKey) });
    setSibs(client.siblings());
    setTimeout(() => setSearching(false), SETTLE_MS);
  };

  // Every instance at once, which is what "join the group" means. Picking one
  // and coming back for the next was the old shape, and it made a person do
  // the same form once per machine they own.
  const saveAll = async () => {
    if (!live || sibs.length === 0) return;
    const existing = await listConnections();
    let first: RelayConnection | null = null;
    for (const s of sibs) {
      // Re-joining with the same phrase must not double every row. An
      // instance is the same instance if the group and its id match; its name
      // is a label it may have changed since.
      const already = existing.find(
        (e) => e.kind === 'relay' && e.relayUrl === live.url && e.relayKey === live.key && e.instanceId === s.instanceId,
      );
      if (already) {
        first = first ?? (already as RelayConnection);
        continue;
      }
      const conn: RelayConnection = {
        kind: 'relay',
        id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        name: s.name || s.instanceId,
        relayUrl: live.url,
        relayKey: live.key,
        relayFrameKey: live.frameKey,
        instanceId: s.instanceId,
        // No token: being on this relay under this key IS the credential now.
        token: '',
      };
      await addConnection(conn);
      first = first ?? conn;
    }
    if (!first) return;
    await setActiveConnectionId(first.id);
    // Deliberately NOT released on the way out now: the saved connections are
    // about to use this very client.
    liveRef.current = null;
    onConnected(first);
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

      <Text style={[styles.label, { color: c.textMuted }]}>{t('relay.phraseLabel')}</Text>
      <TextInput
        style={[styles.input, styles.phraseInput, inputStyle]}
        placeholder={t('relay.phrasePlaceholder')}
        placeholderTextColor={c.textMuted}
        value={phrase}
        onChangeText={setPhrase}
        autoCapitalize="none"
        autoCorrect={false}
        autoComplete="off"
        multiline
      />

      <TouchableOpacity
        style={[
          styles.button,
          { backgroundColor: accent, borderRadius: radii.control },
          searching && styles.buttonDisabled,
        ]}
        onPress={join}
        disabled={searching}
      >
        {searching ? (
          <ActivityIndicator color={accentContrast} />
        ) : (
          <Text style={[styles.buttonText, { color: accentContrast }]}>{t('relay.joinButton')}</Text>
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
              <View style={[styles.row, { backgroundColor: c.surface, borderRadius: radii.card }]}>
                <View style={styles.rowText}>
                  <Text style={[styles.rowName, { color: c.text }]}>{item.name || item.instanceId}</Text>
                  <Text style={[styles.rowSub, { color: c.textMuted }]} numberOfLines={1}>
                    {item.deployment}
                  </Text>
                </View>
              </View>
            )}
            ListEmptyComponent={
              searching ? null : <Text style={[styles.empty, { color: c.textMuted }]}>{t('relay.noInstances')}</Text>
            }
          />
          {sibs.length > 0 && (
            <TouchableOpacity
              style={[styles.button, { backgroundColor: accent, borderRadius: radii.control }]}
              onPress={saveAll}
            >
              <Text style={[styles.buttonText, { color: accentContrast }]}>
                {t('relay.saveAllButton', { count: sibs.length })}
              </Text>
            </TouchableOpacity>
          )}
        </>
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
  // Twelve words do not fit on one phone line, and a field that scrolls
  // sideways while somebody checks their typing is a field that hides the
  // typo they are looking for.
  phraseInput: { minHeight: 76, textAlignVertical: 'top' },
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
  empty: { fontSize: 13, textAlign: 'center', marginTop: 12 },
});
