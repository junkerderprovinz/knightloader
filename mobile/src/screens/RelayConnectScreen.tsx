import { useCallback, useEffect, useRef, useState } from 'react';
import { Animated, Easing, FlatList, StyleSheet, Text, TextInput, View } from 'react-native';
import QRScanner from '../components/QRScanner';
import { closeRelayClient, relayClientFor, type RelaySibling } from '../api/relayClient';
import { DEFAULT_RELAY_URL, PhraseError, frameKeyFromPhrase, keyFromPhrase } from '../api/seedphrase';
import { toHex } from '../api/sha256';
import { relayIdentity } from '../storage/relayIdentity';
import { addConnection, listConnections, setActiveConnectionId } from '../storage/connections';
import type { RelayConnection, ServerConnection } from '../api/types';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import { GlimButton } from '../components/glim';
import IconBadge, { Back, Connect, Paste, Scan } from '../components/IconBadge';
import * as Clipboard from 'expo-clipboard';

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

export default function RelayConnectScreen({
  onConnected,
  onBack,
}: {
  onConnected: (conn: ServerConnection) => void;
  /** The way out (jdp, 2026-08-31: "die mit phrase verbinden seite hat keinen
   *  zurückbutton"). This screen is reached from the overview's "+" and from
   *  the empty state, and on Android the hardware back key worked while nothing
   *  on screen did - which is a way out only if you already know it is there.
   *  Every other screen in this app has the badge; this one was the exception
   *  nobody meant to make. */
  onBack: () => void;
}) {
  const { t } = useT();
  const { c, accent, radii } = useAppearance();
  const [phrase, setPhrase] = useState('');
  const [searching, setSearching] = useState(false);
  const [scanning, setScanning] = useState(false);
  const [error, setError] = useState<string | null>(null);

  /**
   * The refusal signal: the button shakes, and no sentence appears under it
   * (jdp, 2026-09-01: "wenn keine phrasse eingegeben wurde soll der button kurz
   * zittern, keine fehlermeldung in textform").
   *
   * It is the design language's standing rule for a failed action, and the
   * browser extension has followed it since its own round of this. The written
   * message has the property the rule objects to: it never clears itself, so a
   * refusal from ten minutes ago looks exactly as current as one from a second
   * ago. A shake is over when it is over.
   *
   * Kept for the failures a shake cannot express - a word that is not in the
   * list, a checksum that does not add up - because "which of your twelve words
   * is wrong" is information, and losing it to make the rule tidy would be the
   * rule eating the product. An EMPTY field carries no such information, and
   * that is the case he is describing.
   */
  const wackeln = useRef(new Animated.Value(0)).current;
  const zittern = useCallback(() => {
    wackeln.setValue(0);
    Animated.sequence(
      [1, -1, 0.6, -0.6, 0].map((zu) =>
        Animated.timing(wackeln, { toValue: zu, duration: 55, easing: Easing.linear, useNativeDriver: true }),
      ),
    ).start();
  }, [wackeln]);
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
  // of a socket that simply never finds anybody. That check earns its keep
  // twice over for a scan: a QR code decodes to whatever it decodes to, and
  // pointing the camera at some other code should say "that is not a phrase"
  // rather than dial a group nobody is in.
  //
  // `entered` exists for the scan path. The caller has just set state that
  // this render does not see yet, so it passes the scanned words in directly;
  // the button path passes nothing and reads state as before.
  const join = async (entered?: string) => {
    const words = (entered ?? phrase).trim();
    setError(null);
    // An empty field is the one refusal that carries no information beyond
    // "not yet", so it gets the shake and nothing else. Anything below this
    // line has something to SAY, and saying it is worth a line of text.
    if (words === '') {
      zittern();
      return;
    }
    let key: string;
    // Both keys come out of the phrase here, in the one place it exists, and
    // the words are then gone - the frame key is carried alongside the relay
    // key from this point on rather than re-derived, because there would be
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
      {/* Same badge, same place as every other screen: left of the heading. */}
      <View style={styles.topBar}>
        <IconBadge icon={<Back color={c.textSub} />} onPress={onBack} accessibilityLabel={t('settings.back')} />
        <Text style={[styles.title, { color: c.text }]}>{t('relay.title')}</Text>
      </View>
      {/* Heading, explanation, field, buttons - in that order (jdp, 2026-09-01:
          "infotext unter die überschrift, dann das eingabefeld, dann die
          buttons").

          It sat below the controls for one round, on my reading of his "info
          text weiter runter" as "out of the way of the field". He meant lower
          than where it was in the heading, not last: a screen whose whole job
          is one unfamiliar input explains it BEFORE asking for it, and an
          explanation you reach after the buttons is one you read after
          guessing. */}
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

      {/* Paste, because twelve words is the one input a phone keyboard is worst
          at and the phrase usually arrives in a message somebody already
          copied (jdp, 2026-09-01: "ein einfügen button fehlt"). Between the
          field and Connect, in the order the hands move: paste, then join.

          Clipboard.getStringAsync rather than the TextInput's own long-press
          menu: that menu exists, and finding it means knowing it is there. */}
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

      <Animated.View
        style={{
          transform: [{ translateX: wackeln.interpolate({ inputRange: [-1, 1], outputRange: [-7, 7] }) }],
        }}
      >
      <GlimButton
        hue={1}
        label={t('relay.joinButton')}
        icon={(ink) => <Connect color={ink} />}
        busy={searching}
        // Wrapped, not passed directly: onPress hands its handler the touch
        // event, which join() would now read as the scanned phrase. tsc
        // caught it the moment join grew that parameter - untyped, it would
        // have shipped as "the Connect button says your phrase is not twelve
        // words" with no clue why.
        onPress={() => void join()}
      />
      </Animated.View>

      {/* Scanning is the point of the QR the web UI has been showing all
          along: twelve words is exactly the input a phone keyboard is worst
          at, and the code was scannable by nothing until now (the scanner
          existed, wired only to the old direct-address screen). */}
      <GlimButton
        hue={2}
        label={t('relay.scanButton')}
        icon={(ink) => <Scan color={ink} />}
        onPress={() => {
          setError(null);
          setScanning(true);
        }}
      />

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
            <GlimButton hue={3} label={t('relay.saveAllButton', { count: sibs.length })} onPress={saveAll} />
          )}
        </>
      )}

      {/* A scanned phrase joins straight away rather than only filling the
          field: the code carries exactly the twelve words the button below
          would be pressed with, and stopping to ask for one more tap after
          somebody has already aimed a camera at it is a step with nothing in
          it. join() reads `phrase` from state, so the scanned value is put
          there first and passed explicitly - React has not re-rendered yet
          at this point, and joining off the stale state would use whatever
          was typed before the scan. */}
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

// Colours and radii are applied inline from the resolved tokens, never baked
// in here: a stylesheet is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  container: { flex: 1, padding: 24, paddingTop: 56 },
  topBar: { flexDirection: 'row', alignItems: 'center', gap: 12, marginBottom: 4 },
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
  // A row, not a block: every button on this screen carries a glyph beside its
  // label now (jdp, 2026-09-01: "alle buttons sollen einen glyph bekommen").
  // ONE button style for all three, and one gap between them (jdp,
  // 2026-09-01: "alle buttons gleicher abstand und alle farbig!").
  //
  // They were three: Paste at a fixed height of 44 on the plain surface, Join
  // at 14 of vertical padding in the accent, and Scan the same padding again
  // with a DRAWN BORDER around it. Three heights, three grounds, three
  // different margins - which reads as three kinds of control rather than
  // three things you can do on one screen.
  //
  // The border went for its own reason as well: this language separates
  // surfaces by shade and never by a line, so an outlined button was the one
  // element on the page breaking that rule. "Secondary" was the argument for
  // it, and it does not survive the screen actually having three equal ways
  // forward: paste the phrase, scan it, or type it and join.
  button: {
    flexDirection: 'row',
    gap: 8,
    paddingVertical: 14,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 10,
  },
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
