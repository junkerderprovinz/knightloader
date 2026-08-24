import { useState } from 'react';
import { ActivityIndicator, StyleSheet, Text, TextInput, TouchableOpacity, View } from 'react-native';
import { checkConnection } from '../api/client';
import { saveConnection } from '../storage/connection';
import type { ServerConnection } from '../api/types';
import { colors } from '../theme';

// Onboarding is manual paste for v1: generate a token on the Access tab of
// the KnightLoader web UI (POST /api/tokens under the hood), copy the
// address + secret in here. A QR-scan flow can follow once the server side
// grows a matching QR payload - see mobile/README.md.
export default function ConnectScreen({ onConnected }: { onConnected: (conn: ServerConnection) => void }) {
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [token, setToken] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const normalizedUrl = url.trim().replace(/\/+$/, '');

  const connect = async () => {
    setError(null);
    if (!normalizedUrl || !token.trim()) {
      setError('Server-Adresse und Token angeben.');
      return;
    }
    const conn: ServerConnection = {
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
      await saveConnection(conn);
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
        Im Access-Tab der KnightLoader-Weboberfläche einen Token erzeugen und hier einfügen.
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
      <TextInput
        style={styles.input}
        placeholder="https://192.168.10.10:1234"
        placeholderTextColor={colors.textMuted}
        value={url}
        onChangeText={setUrl}
        autoCapitalize="none"
        autoCorrect={false}
        keyboardType="url"
      />

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
