import { useState } from 'react';
import { ActivityIndicator, StyleSheet, Text, TextInput, TouchableOpacity, View } from 'react-native';
import { checkConnection } from '../api/client';
import { scanLocalNetwork, type Found } from '../api/discover';
import { addConnection, setActiveConnectionId } from '../storage/connections';
import type { ServerConnection } from '../api/types';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import QRScanner from '../components/QRScanner';

// The direct way in: an address this phone can actually reach, plus a token
// if that instance has a password. The scan button reads the plain
// remote-access QR, which encodes a bare address (routes_remote.go's own
// remoteAccessInfo, renderQR(addr)).
//
// It used to have to tell that QR apart from a pairing-code QR, which encoded
// base64 JSON and needed decoding before the address could be lifted out of
// it. Pairing is gone, so there is one kind of QR here again and the scanned
// text is the address. The OTHER way in - twelve words, every instance at
// once, no address and no token - is RelayConnectScreen, one tap away.
export default function ConnectScreen({
  onConnected,
  onUseRelay,
}: {
  onConnected: (conn: ServerConnection) => void;
  onUseRelay: () => void;
}) {
  const { t } = useT();
  const { c, accent, accentContrast, radii } = useAppearance();
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [token, setToken] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [scanning, setScanning] = useState(false);
  // The LAN sweep (api/discover.ts). null means it has not been run; an empty
  // array means it ran and found nothing, and those two say different things
  // to somebody looking at this screen.
  const [found, setFound] = useState<Found[] | null>(null);
  const [finding, setFinding] = useState(false);

  const findOnNetwork = async () => {
    setFinding(true);
    setError(null);
    setNotice(null);
    try {
      const hits = await scanLocalNetwork();
      setFound(hits);
      // One hit needs no list: fill it in, say so, and leave the token as the
      // only thing left to do.
      if (hits.length === 1) {
        setUrl(hits[0].url);
        setNotice(t('connect.foundOne'));
      }
    } finally {
      setFinding(false);
    }
  };

  const normalizedUrl = url.trim().replace(/\/+$/, '');

  const connect = async () => {
    setError(null);
    if (!normalizedUrl || !token.trim()) {
      setError(t('connect.errorMissing'));
      return;
    }
    const conn: ServerConnection = {
      id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
      baseUrl: normalizedUrl,
      token: token.trim(),
      name: name.trim() || normalizedUrl,
    };
    setBusy(true);
    try {
      const auth = await checkConnection(conn);
      if (!auth.authenticated) {
        setError(t('connect.errorTokenRejected'));
        return;
      }
      await addConnection(conn);
      await setActiveConnectionId(conn.id);
      onConnected(conn);
    } catch (err) {
      setError(err instanceof Error ? err.message : t('connect.errorTokenRejected'));
    } finally {
      setBusy(false);
    }
  };

  const inputStyle = {
    backgroundColor: c.surface,
    color: c.text,
    borderColor: c.border,
    borderRadius: radii.control,
  };

  return (
    <View style={[styles.container, { backgroundColor: c.bg }]}>
      <Text style={[styles.title, { color: c.text }]}>{t('connect.title')}</Text>
      <Text style={[styles.hint, { color: c.textMuted }]}>{t('connect.hint')}</Text>

      <Text style={[styles.label, { color: c.textMuted }]}>{t('connect.nameLabel')}</Text>
      <TextInput
        style={[styles.input, inputStyle]}
        placeholder={t('connect.namePlaceholder')}
        placeholderTextColor={c.textMuted}
        value={name}
        onChangeText={setName}
        autoCapitalize="none"
      />

      <Text style={[styles.label, { color: c.textMuted }]}>{t('connect.addressLabel')}</Text>
      <View style={styles.inputRow}>
        <TextInput
          style={[styles.input, inputStyle, styles.inputFlex]}
          placeholder="https://192.168.10.10:1234"
          placeholderTextColor={c.textMuted}
          value={url}
          onChangeText={(v) => {
            setUrl(v);
            setNotice(null);
          }}
          autoCapitalize="none"
          autoCorrect={false}
          keyboardType="url"
        />
        <TouchableOpacity
          style={[
            styles.scanButton,
            { backgroundColor: c.surface, borderColor: c.border, borderRadius: radii.control },
          ]}
          onPress={() => setScanning(true)}
        >
          <Text style={[styles.scanButtonText, { color: accent }]}>{t('connect.qrButton')}</Text>
        </TouchableOpacity>
      </View>

      {/* Sits directly under the address field because that is the field it
          fills in - the whole point is that nobody has to know the address. */}
      <TouchableOpacity style={styles.findLink} onPress={findOnNetwork} disabled={finding}>
        {finding ? (
          <Text style={[styles.findLinkText, { color: accent }]}>{t('connect.finding')}</Text>
        ) : (
          <Text style={[styles.findLinkText, { color: accent }]}>{t('connect.findButton')}</Text>
        )}
      </TouchableOpacity>

      {found?.length === 0 && <Text style={[styles.hintSmall, { color: c.textMuted }]}>{t('connect.foundNone')}</Text>}
      {found && found.length > 1 && (
        <View>
          <Text style={[styles.hintSmall, { color: c.textMuted }]}>
            {t('connect.foundMany', { n: String(found.length) })}
          </Text>
          {found.map((f) => (
            <TouchableOpacity
              key={f.url}
              style={[
                styles.foundRow,
                { backgroundColor: c.surface, borderColor: c.border, borderRadius: radii.card },
              ]}
              onPress={() => {
                setUrl(f.url);
                setFound(null);
                setNotice(t('connect.foundOne'));
              }}
            >
              <Text style={[styles.foundUrl, { color: c.text }]}>{f.url}</Text>
              <Text style={[styles.foundMeta, { color: c.textMuted }]}>{f.version}</Text>
            </TouchableOpacity>
          ))}
        </View>
      )}

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

      {notice && <Text style={[styles.notice, { color: c.statusOkSolid }]}>{notice}</Text>}
      {error && <Text style={[styles.error, { color: c.statusFailSolid }]}>{error}</Text>}

      <TouchableOpacity
        style={[styles.button, { backgroundColor: accent, borderRadius: radii.control }, busy && styles.buttonDisabled]}
        onPress={connect}
        disabled={busy}
      >
        {busy ? (
          <ActivityIndicator color={accentContrast} />
        ) : (
          <Text style={[styles.buttonText, { color: accentContrast }]}>{t('connect.connectButton')}</Text>
        )}
      </TouchableOpacity>

      {/* The relay is the fallback, not an equal first choice: it only helps
          when nothing here can reach the instance directly, and it costs a
          shared key. So it sits below as a way out of a dead end rather than
          as a second button someone has to choose between up front. */}
      <TouchableOpacity style={styles.relayLink} onPress={onUseRelay}>
        <Text style={[styles.relayLinkText, { color: accent }]}>{t('connect.relayLink')}</Text>
      </TouchableOpacity>

      <QRScanner
        visible={scanning}
        hint={t('connect.qrHintAddress')}
        onScanned={(data) => {
          setScanning(false);
          setUrl(data.trim());
          setNotice(null);
        }}
        onClose={() => setScanning(false)}
      />
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens, never baked
// in here: a stylesheet is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  container: { flex: 1, padding: 24, justifyContent: 'center' },
  title: { fontSize: 22, fontWeight: '600', marginBottom: 8 },
  hint: { fontSize: TYPE.body, marginBottom: 24, lineHeight: 20 },
  label: { fontSize: 13, marginBottom: 6, marginTop: 12 },
  input: {
    paddingHorizontal: 14,
    paddingVertical: 12,
    fontSize: 15,
    borderWidth: 1,
  },
  inputRow: { flexDirection: 'row', gap: 8, alignItems: 'stretch' },
  inputFlex: { flex: 1 },
  scanButton: {
    borderWidth: 1,
    paddingHorizontal: 16,
    alignItems: 'center',
    justifyContent: 'center',
  },
  scanButtonText: { fontSize: 13, fontWeight: '700' },
  findLink: { marginTop: 10, alignSelf: 'flex-start' },
  findLinkText: { fontSize: 13 },
  hintSmall: { fontSize: TYPE.dense, marginTop: 8, lineHeight: 17 },
  foundRow: {
    borderWidth: 1,
    paddingHorizontal: 14,
    paddingVertical: 10,
    marginTop: 8,
  },
  foundUrl: { fontSize: TYPE.body },
  foundMeta: { fontSize: TYPE.dense, marginTop: 2 },
  notice: { marginTop: 16, fontSize: 13, lineHeight: 18 },
  error: { marginTop: 16, fontSize: TYPE.body },
  button: {
    paddingVertical: 14,
    alignItems: 'center',
    marginTop: 24,
  },
  buttonDisabled: { opacity: 0.6 },
  buttonText: { fontSize: 16, fontWeight: '600' },
  relayLink: { marginTop: 18, alignItems: 'center' },
  relayLinkText: { fontSize: 13 },
});
