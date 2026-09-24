import { useCallback, useEffect, useRef, useState } from 'react';
import { Animated, FlatList, StyleSheet, View } from 'react-native';
import QRScanner from '../components/QRScanner';
import { closeRelayClient, relayClientFor, type RelaySibling } from '../api/relayClient';
import { DEFAULT_RELAY_URL, PhraseError, frameKeyFromPhrase, keyFromPhrase } from '../api/seedphrase';
import { toHex } from '../api/sha256';
import { relayIdentity } from '../storage/relayIdentity';
import { addConnection, listConnections, setActiveConnectionId } from '../storage/connections';
import type { RelayConnection, ServerConnection } from '../api/types';
import { useAppearance } from '../theme/AppearanceContext';
import { useShake } from '../theme/MotionContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import { GlimButton } from '../components/glim';
import IconBadge, { Back, Connect, Paste, Scan, boxForInk } from '../components/IconBadge';
import { InfoTip } from '../components/InfoTip';
import * as Clipboard from 'expo-clipboard';
import { Text, TextInput } from '../components/Text';

// Joining the group, which is the whole of connecting this app now: twelve
// words, and every instance the person runs appears.
//
// It replaces a screen that asked for a relay address, a relay key and a
// per-instance API token and saved one instance per visit, so three instances
// were three trips through the same form.
//
// The phone is a full group member rather than a client of one instance. It
// derives the same key its siblings derive (api/seedphrase.ts, a port of
// internal/seedphrase held to the Go side's own vectors), dials the same relay,
// and is authenticated by that: the server accepts a relay-delivered request
// from anything presenting the group key, so there is no token to enter. See
// relayProxyHandler for what that admits and what it does not.
//
// The phrase is a group credential, so a saved connection holds it in the
// keychain exactly as a token was - see storage/connections.ts.

// How long to keep the spinner up. Siblings arrive asynchronously and no
// frame says "that is all of them", so this is a pause for the list to fill,
// not a timeout: it keeps updating afterwards for as long as the screen is open.
const SETTLE_MS = 3000;

export default function RelayConnectScreen({
  onConnected,
  onBack,
}: {
  onConnected: (conn: ServerConnection) => void;
  /** The way out. This screen is reached from the overview's "+" and from the
   *  empty state, and the Android hardware back key is a way out only for
   *  somebody who already knows it is there. Every other screen in this app has
   *  the badge. */
  onBack: () => void;
}) {
  const { t } = useT();
  const { c, accent, radii } = useAppearance();
  const [phrase, setPhrase] = useState('');
  const [searching, setSearching] = useState(false);
  const [scanning, setScanning] = useState(false);
  const [error, setError] = useState<string | null>(null);

  /**
   * The refusal signal: the button shakes and no sentence appears under it.
   *
   * That is the design language's rule for a failed action. A written message
   * never clears itself, so a refusal from ten minutes ago looks as current as
   * one from a second ago, while a shake is over when it is over.
   *
   * A sentence is kept for the failures a shake cannot express, a word that is
   * not in the list or a checksum that does not add up, because which of the
   * twelve words is wrong is information. An empty field carries none.
   *
   * The geometry is the house's: `glim-shake` is a translateX oscillation
   * decaying +-4, -+4, +-2, -+2 over 360ms, and the web UI (index.css) and the
   * extension (glimstone.css) both carry it. The routine lives in
   * theme/MotionContext's useShake, so there is one copy and the travel comes
   * off the motion level: at the quietest one the button dips in opacity
   * instead, because an invisible shake carries no refusal.
   */
  const { style: zitterStil, shake: zittern } = useShake();
  const [sibs, setSibs] = useState<RelaySibling[]>([]);
  // What the running client was opened with. State rather than a ref, because
  // the instance list below renders only once this is set.
  // frameKey rides along as hex because that is the form it is saved in - see
  // types.ts's relayFrameKey for why it is stored rather than re-derived.
  const [live, setLive] = useState<{ url: string; key: string; frameKey: string } | null>(null);
  // The ref mirrors it purely for the unmount cleanup, which must see the
  // latest value rather than the one captured when the effect was set up.
  const liveRef = useRef<{ url: string; key: string } | null>(null);
  const unsubscribe = useRef<(() => void) | null>(null);

  // A client opened to look around must not outlive the screen unless something
  // was saved with it, or backing out of a typo leaves a socket retrying
  // against a relay nobody uses.
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

  // The checksum catches a mistyped or swapped word before anything is dialled,
  // so the failure reads as "word 3 is not one of the words" rather than as a
  // socket that never finds anybody. It earns its keep twice over for a scan,
  // where a QR code decodes to whatever it decodes to and pointing the camera
  // at some other code should say that it is not a phrase.
  //
  // `entered` exists for the scan path. The caller has just set state this
  // render does not see yet, so it passes the scanned words in directly; the
  // button path passes nothing and reads state.
  const join = async (entered?: string) => {
    const words = (entered ?? phrase).trim();
    setError(null);
    // An empty field is the one refusal that carries no information beyond
    // "not yet", so it gets the shake and nothing else. Everything below has
    // something to say, which is worth a line of text.
    if (words === '') {
      zittern();
      return;
    }
    let key: string;
    // Both keys come out of the phrase here, in the one place it exists, and
    // the words are then gone. The frame key travels alongside the relay key
    // from this point rather than being re-derived, since there would be
    // nothing left to re-derive it from. See types.ts's relayFrameKey.
    let frameKey: Uint8Array;
    try {
      key = keyFromPhrase(words);
      frameKey = frameKeyFromPhrase(words);
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
    // exist for a saved connection, where constructor options are never
    // applied. See relayClientFor.

    unsubscribe.current = client.subscribe(() => setSibs(client.siblings()));
    liveRef.current = { url: DEFAULT_RELAY_URL, key };
    setLive({ url: DEFAULT_RELAY_URL, key, frameKey: toHex(frameKey) });
    setSibs(client.siblings());
    setTimeout(() => setSearching(false), SETTLE_MS);
  };

  // Every instance at once, which is what joining the group means. Picking one
  // and coming back for the next makes a person fill the same form once per
  // machine they own.
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
        // No token: being on this relay under this key is the credential.
        token: '',
      };
      await addConnection(conn);
      first = first ?? conn;
    }
    if (!first) return;
    await setActiveConnectionId(first.id);
    // Not released on the way out: the saved connections are about to use this
    // client.
    liveRef.current = null;
    onConnected(first);
  };

  // Filled and borderless, as every field in the family is: a surface is told
  // apart by its shade, never by a drawn line.
  const inputStyle = {
    backgroundColor: c.surface,
    color: c.text,
    borderRadius: radii.control,
  };

  return (
    <View style={[styles.container, { backgroundColor: c.bg }]}>
      {/* Same badge, same place as every other screen: left of the heading.
          What the phrase is and where to find it hangs off an (i) on the
          heading, ahead of the field it explains, rather than standing as a
          paragraph over it (GlimStone rule 8). */}
      <View style={styles.topBar}>
        <IconBadge icon={<Back color={c.textSub} />} onPress={onBack} accessibilityLabel={t('settings.back')} />
        <Text style={[styles.title, { color: c.text }]}>{t('relay.title')}</Text>
        <InfoTip text={t('relay.hint')} />
      </View>
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

      {/* Paste, because twelve words is the input a phone keyboard is worst at
          and the phrase usually arrives in a message somebody already copied.
          Between the field and Connect, in the order the hands move.

          Clipboard.getStringAsync rather than the TextInput's long-press menu,
          which exists and has to be known about to be found.

          One stack, and the stack owns the spacing: a component must not carry
          an outside margin, so the gap belongs to whatever stacks them. */}
      <View style={styles.buttonStack}>
      <GlimButton
        hue={0}
        label={t('relay.pasteButton')}
        icon={(ink) => <Paste color={ink} />}
        disabled={searching}
        onPress={async () => {
          setError(null);
          const text = await Clipboard.getStringAsync();
          if (text.trim()) setPhrase(text.trim());
        }}
      />

      <Animated.View style={zitterStil}>
      <GlimButton
        hue={1}
        label={t('relay.joinButton')}
        icon={(ink) => <Connect color={ink} />}
        busy={searching}
        // Wrapped rather than passed directly: onPress hands its handler the
        // touch event, which join() would read as the scanned phrase.
        onPress={() => void join()}
      />
      </Animated.View>

      {/* Scanning is what the QR the web UI shows is for: twelve words is the
          input a phone keyboard is worst at. */}
      <GlimButton
        hue={2}
        label={t('relay.scanButton')}
        icon={(ink) => <Scan color={ink} />}
        onPress={() => {
          setError(null);
          setScanning(true);
        }}
      />
      </View>

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
            /* The same empty state every other list in the app draws: a card, a
               muted glyph at reduced opacity, a muted line. A bare sentence on
               the page ground would be a third shape for one situation, and a
               list with nothing in it is the moment the page should still look
               like the page.

               The glyph's size is the role's own number, 26 points of ink as an
               empty state takes in the web UI, converted through boxForInk
               because a drawn glyph fills less than the box it is handed. */
            ListEmptyComponent={
              searching ? null : (
                <View style={[styles.empty, { backgroundColor: c.surface, borderRadius: radii.card }]}>
                  <View style={styles.emptyIcon}>
                    <Connect color={c.textMuted} size={boxForInk(26)} />
                  </View>
                  <Text style={[styles.emptyText, { color: c.textMuted }]}>{t('relay.noInstances')}</Text>
                </View>
              )
            }
          />
          {sibs.length > 0 && (
            <GlimButton hue={3} label={t('relay.saveAllButton', { count: sibs.length })} onPress={saveAll} />
          )}
        </>
      )}

      {/* A scanned phrase joins straight away rather than only filling the
          field: the code carries the twelve words the button below would be
          pressed with. join() reads `phrase` from state, so the scanned value
          is put there first and passed explicitly, since React has not
          re-rendered and joining off the stale state would use whatever was
          typed before the scan. */}
      <QRScanner
        visible={scanning}
        hint={t('relay.qrHintPhrase')}
        onScanned={(data) => {
          setScanning(false);
          const scanned = data.trim();
          setPhrase(scanned);
          void join(scanned);
        }}
        onClose={() => setScanning(false)}
      />
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens rather than
// baked in here: a stylesheet is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  container: { flex: 1, padding: 24, paddingTop: 56 },
  topBar: { flexDirection: 'row', alignItems: 'center', gap: 12, marginBottom: 4 },
  // Every size on this screen comes off the scale in theme/tokens.ts: this
  // screen's title is the same object as Downloads' and Settings' and has to
  // measure the same.
  title: { fontSize: TYPE.heading, fontWeight: '600', flexShrink: 1 },
  label: { fontSize: TYPE.dense, marginBottom: 6, marginTop: 12 },
  input: {
    paddingHorizontal: 14,
    paddingVertical: 12,
    fontSize: TYPE.body,
  },
  // Twelve words do not fit on one phone line, and a field that scrolls
  // sideways while somebody checks their typing hides the typo they are looking
  // for.
  phraseInput: { minHeight: 76, textAlignVertical: 'top' },
  // A row rather than a block: every button on this screen carries a glyph
  // beside its label, with one button style for all three and one gap between
  // them. Three heights, three grounds and three margins read as three kinds of
  // control rather than as three things to do on one screen, and the screen has
  // three equal ways forward: paste the phrase, scan it, or type it and join.
  //
  // No outlined button either, since this language separates surfaces by shade
  // and never by a line.
  buttonStack: { gap: 12, marginTop: 16 },
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
  rowName: { fontSize: TYPE.body, fontWeight: '600' },
  rowSub: { fontSize: TYPE.dense, marginTop: 2 },
  // The empty state's own card rather than a loose line of text.
  empty: { alignItems: 'center', gap: 10, paddingVertical: 24, paddingHorizontal: 24, marginTop: 8 },
  emptyIcon: { opacity: 0.5 },
  emptyText: { fontSize: TYPE.body, textAlign: 'center' },
});
