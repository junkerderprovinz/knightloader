import { useState } from 'react';
import { ActivityIndicator, StyleSheet, Text, TextInput, TouchableOpacity, View } from 'react-native';
import { addLinks, ApiError } from '../api/client';
import type { Instance, ServerConnection } from '../api/types';
import { colors } from '../theme';

export default function AddDownloadScreen({
  conn,
  peer,
  onDone,
}: {
  conn: ServerConnection;
  peer?: Instance;
  onDone: () => void;
}) {
  const base = peer ? `/api/instances/${encodeURIComponent(peer.name)}` : '/api';
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async () => {
    const links = text
      .split('\n')
      .map((l) => l.trim())
      .filter(Boolean);
    if (links.length === 0) {
      setError('Mindestens einen Link einfügen.');
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await addLinks(conn, links, base);
      onDone();
    } catch (err) {
      setError(err instanceof ApiError ? `Server: ${err.message}` : 'Konnte Links nicht senden.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <View style={styles.container}>
      <Text style={styles.title}>Links hinzufügen{peer ? ` – ${peer.name}` : ''}</Text>
      <Text style={styles.hint}>Ein Link pro Zeile, genau wie im Paste-Feld der Weboberfläche.</Text>

      <TextInput
        style={styles.textArea}
        multiline
        placeholder="https://…"
        placeholderTextColor={colors.textMuted}
        value={text}
        onChangeText={setText}
        autoCapitalize="none"
        autoCorrect={false}
        textAlignVertical="top"
      />

      {error && <Text style={styles.error}>{error}</Text>}

      <View style={styles.actions}>
        <TouchableOpacity style={styles.secondaryButton} onPress={onDone}>
          <Text style={styles.secondaryButtonText}>Abbrechen</Text>
        </TouchableOpacity>
        <TouchableOpacity style={[styles.button, busy && styles.buttonDisabled]} onPress={submit} disabled={busy}>
          {busy ? <ActivityIndicator color={colors.text} /> : <Text style={styles.buttonText}>Hinzufügen</Text>}
        </TouchableOpacity>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: colors.background, padding: 24, paddingTop: 56 },
  title: { color: colors.text, fontSize: 20, fontWeight: '600', marginBottom: 8 },
  hint: { color: colors.textMuted, fontSize: 14, marginBottom: 16 },
  textArea: {
    flex: 1,
    backgroundColor: colors.surface,
    color: colors.text,
    borderRadius: 8,
    padding: 14,
    fontSize: 14,
    borderWidth: 1,
    borderColor: colors.border,
  },
  error: { color: colors.danger, marginTop: 12, fontSize: 14 },
  actions: { flexDirection: 'row', gap: 12, marginTop: 16 },
  button: {
    flex: 1,
    backgroundColor: colors.accent,
    borderRadius: 8,
    paddingVertical: 14,
    alignItems: 'center',
  },
  buttonDisabled: { opacity: 0.6 },
  buttonText: { color: colors.text, fontSize: 16, fontWeight: '600' },
  secondaryButton: {
    flex: 1,
    backgroundColor: colors.surface,
    borderRadius: 8,
    paddingVertical: 14,
    alignItems: 'center',
    borderWidth: 1,
    borderColor: colors.border,
  },
  secondaryButtonText: { color: colors.textMuted, fontSize: 16 },
});
