import { useState } from 'react';
import { ActivityIndicator, StyleSheet, Text, TextInput, TouchableOpacity, View } from 'react-native';
import { checkConnection } from '../api/client';
import { decodePairingCode } from '../api/pairing';
import { addConnection, setActiveConnectionId } from '../storage/connections';
import type { ServerConnection } from '../api/types';
import { colors } from '../theme';
import { useT } from '../i18n/I18nContext';
import QRScanner from '../components/QRScanner';

// The Access tab carries two DIFFERENT QR codes, and this screen's scan
// button has to tell them apart rather than assume: the plain
// remote-access QR encodes just a bare address (routes_remote.go's own
// remoteAccessInfo, renderQR(addr)), while the pairing-code QR
// (routes_pairing.go) encodes base64 JSON carrying name+address+a one-time
// token. Feeding the SECOND one's raw text straight into the address field,
// as an earlier version of this screen did, put the pairing code itself
// where the address belongs and left name/token untouched - decodePairingCode
// (api/pairing.ts) is what tells the two apart. Either way the token itself
// still needs pasting by hand: even a decoded pairing offer's token is a
// short-lived federation handshake secret (routes_pairing.go's own doc
// comment), not a bearer API token - a one-scan flow for THAT needs the
// server to grow a QR that carries one, not built yet, see mobile/README.md.
export default function ConnectScreen({
  onConnected,
  onUseRelay,
}: {
  onConnected: (conn: ServerConnection) => void;
  onUseRelay: () => void;
}) {
  const { t } = useT();
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [token, setToken] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [scanning, setScanning] = useState(false);

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

  return (
    <View style={styles.container}>
      <Text style={styles.title}>{t('connect.title')}</Text>
      <Text style={styles.hint}>{t('connect.hint')}</Text>

      <Text style={styles.label}>{t('connect.nameLabel')}</Text>
      <TextInput
        style={styles.input}
        placeholder={t('connect.namePlaceholder')}
        placeholderTextColor={colors.textMuted}
        value={name}
        onChangeText={setName}
        autoCapitalize="none"
      />

      <Text style={styles.label}>{t('connect.addressLabel')}</Text>
      <View style={styles.inputRow}>
        <TextInput
          style={[styles.input, styles.inputFlex]}
          placeholder="https://192.168.10.10:1234"
          placeholderTextColor={colors.textMuted}
          value={url}
          onChangeText={(v) => {
            setUrl(v);
            setNotice(null);
          }}
          autoCapitalize="none"
          autoCorrect={false}
          keyboardType="url"
        />
        <TouchableOpacity style={styles.scanButton} onPress={() => setScanning(true)}>
          <Text style={styles.scanButtonText}>{t('connect.qrButton')}</Text>
        </TouchableOpacity>
      </View>

      <Text style={styles.label}>{t('connect.tokenLabel')}</Text>
      <TextInput
        style={styles.input}
        placeholder={t('connect.tokenPlaceholder')}
        placeholderTextColor={colors.textMuted}
        value={token}
        onChangeText={setToken}
        autoCapitalize="none"
        autoCorrect={false}
        secureTextEntry
      />

      {notice && <Text style={styles.notice}>{notice}</Text>}
      {error && <Text style={styles.error}>{error}</Text>}

      <TouchableOpacity style={[styles.button, busy && styles.buttonDisabled]} onPress={connect} disabled={busy}>
        {busy ? <ActivityIndicator color={colors.text} /> : <Text style={styles.buttonText}>{t('connect.connectButton')}</Text>}
      </TouchableOpacity>

      {/* The relay is the fallback, not an equal first choice: it only helps
          when nothing here can reach the instance directly, and it costs a
          shared key. So it sits below as a way out of a dead end rather than
          as a second button someone has to choose between up front. */}
      <TouchableOpacity style={styles.relayLink} onPress={onUseRelay}>
        <Text style={styles.relayLinkText}>{t('connect.relayLink')}</Text>
      </TouchableOpacity>

      <QRScanner
        visible={scanning}
        hint={t('connect.qrHintAddress')}
        onScanned={(data) => {
          setScanning(false);
          const offer = decodePairingCode(data);
          if (offer) {
            setUrl(offer.url.trim());
            if (offer.name) setName(offer.name.trim());
            setNotice(t('connect.qrAutofillNotice'));
          } else {
            setUrl(data.trim());
            setNotice(null);
          }
        }}
        onClose={() => setScanning(false)}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: colors.background, padding: 24, justifyContent: 'center' },
  title: { color: colors.text, fontSize: 22, fontWeight: '600', marginBottom: 8 },
  hint: { color: colors.textMuted, fontSize: 14, marginBottom: 24, lineHeight: 20 },
  label: { color: colors.textMuted, fontSize: 13, marginBottom: 6, marginTop: 12 },
  input: {
    backgroundColor: colors.surface,
    color: colors.text,
    borderRadius: 8,
    paddingHorizontal: 14,
    paddingVertical: 12,
    fontSize: 15,
    borderWidth: 1,
    borderColor: colors.border,
  },
  inputRow: { flexDirection: 'row', gap: 8, alignItems: 'stretch' },
  inputFlex: { flex: 1 },
  scanButton: {
    backgroundColor: colors.surface,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: colors.border,
    paddingHorizontal: 16,
    alignItems: 'center',
    justifyContent: 'center',
  },
  scanButtonText: { color: colors.accent, fontSize: 13, fontWeight: '700' },
  notice: { color: colors.success, marginTop: 16, fontSize: 13, lineHeight: 18 },
  error: { color: colors.danger, marginTop: 16, fontSize: 14 },
  button: {
    backgroundColor: colors.accent,
    borderRadius: 8,
    paddingVertical: 14,
    alignItems: 'center',
    marginTop: 24,
  },
  buttonDisabled: { opacity: 0.6 },
  buttonText: { color: colors.text, fontSize: 16, fontWeight: '600' },
  relayLink: { marginTop: 18, alignItems: 'center' },
  relayLinkText: { color: colors.accent, fontSize: 13 },
});
