import { useState } from 'react';
import { ActivityIndicator, StyleSheet, Text, TextInput, TouchableOpacity, View } from 'react-native';
import { checkConnection } from '../api/client';
import { addConnection, setActiveConnectionId } from '../storage/connections';
import type { ServerConnection } from '../api/types';
import { colors } from '../theme';
import QRScanner from '../components/QRScanner';

// Onboarding is still address + pasted token, not a single scan: the QR on
// the Access tab today only encodes the address (routes_remote.go's own
// remoteAccessInfo, renderQR(addr) - a bare URL string, no token in it), the
// same one this screen's scan button reads to fill the address field. A
// one-scan flow needs the server to grow a QR that also carries a fresh
// token - not built yet, see mobile/README.md. Adding a PEER to an already
// connected instance is a genuine one-scan flow already, on
// InstancesScreen, because that QR (the pairing-code one) already carries
// both.
export default function ConnectScreen({ onConnected }: { onConnected: (conn: ServerConnection) => void }) {
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [token, setToken] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [scanning, setScanning] = useState(false);

  const normalizedUrl = url.trim().replace(/\/+$/, '');

  const connect = async () => {
    setError(null);
    if (!normalizedUrl || !token.trim()) {
      setError('Server-Adresse und Token angeben.');
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
        setError('Verbindung ging durch, aber der Token wurde nicht akzeptiert.');
        return;
      }
      await addConnection(conn);
      await setActiveConnectionId(conn.id);
      onConnected(conn);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Verbindung fehlgeschlagen.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <View style={styles.container}>
      <Text style={styles.title}>Mit KnightLoader verbinden</Text>
      <Text style={styles.hint}>
        Im Access-Tab der KnightLoader-Weboberfläche einen Token erzeugen und hier einfügen. Die Adresse lässt sich
        über den QR-Code auf derselben Seite einscannen.
      </Text>

      <Text style={styles.label}>Name (optional)</Text>
      <TextInput
        style={styles.input}
        placeholder="z. B. Bottich"
        placeholderTextColor={colors.textMuted}
        value={name}
        onChangeText={setName}
        autoCapitalize="none"
      />

      <Text style={styles.label}>Server-Adresse</Text>
      <View style={styles.inputRow}>
        <TextInput
          style={[styles.input, styles.inputFlex]}
          placeholder="https://192.168.10.10:1234"
          placeholderTextColor={colors.textMuted}
          value={url}
          onChangeText={setUrl}
          autoCapitalize="none"
          autoCorrect={false}
          keyboardType="url"
        />
        <TouchableOpacity style={styles.scanButton} onPress={() => setScanning(true)}>
          <Text style={styles.scanButtonText}>QR</Text>
        </TouchableOpacity>
      </View>

      <Text style={styles.label}>Token</Text>
      <TextInput
        style={styles.input}
        placeholder="einfügen"
        placeholderTextColor={colors.textMuted}
        value={token}
        onChangeText={setToken}
        autoCapitalize="none"
        autoCorrect={false}
        secureTextEntry
      />

      {error && <Text style={styles.error}>{error}</Text>}

      <TouchableOpacity style={[styles.button, busy && styles.buttonDisabled]} onPress={connect} disabled={busy}>
        {busy ? <ActivityIndicator color={colors.text} /> : <Text style={styles.buttonText}>Verbinden</Text>}
      </TouchableOpacity>

      <QRScanner
        visible={scanning}
        hint="QR-Code aus dem Access-Tab scannen"
        onScanned={(data) => {
          setScanning(false);
          setUrl(data.trim());
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
});
