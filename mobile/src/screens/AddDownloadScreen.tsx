import { useState } from 'react';
import { StyleSheet, Text, TextInput, TouchableOpacity, View } from 'react-native';
import { addLinks, ApiError } from '../api/client';
import type { Instance, ServerConnection } from '../api/types';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import { GlimButton } from '../components/glim';

export default function AddDownloadScreen({
  conn,
  peer,
  onDone,
}: {
  conn: ServerConnection;
  peer?: Instance;
  onDone: () => void;
}) {
  const { t } = useT();
  const { c, accent, radii } = useAppearance();
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
      setError(t('addDownload.errorEmpty'));
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await addLinks(conn, links, base);
      onDone();
    } catch (err) {
      setError(err instanceof ApiError ? t('addDownload.errorServer', { message: err.message }) : t('addDownload.errorGeneric'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <View style={[styles.container, { backgroundColor: c.bg }]}>
      <Text style={[styles.title, { color: c.text }]}>
        {peer ? t('addDownload.titlePeer', { name: peer.displayName ?? peer.name }) : t('addDownload.title')}
      </Text>
      <Text style={[styles.hint, { color: c.textMuted }]}>{t('addDownload.hint')}</Text>

      <TextInput
        style={[
          styles.textArea,
          { backgroundColor: c.surface, color: c.text, borderColor: c.border, borderRadius: radii.control },
        ]}
        multiline
        placeholder={t('addDownload.placeholder')}
        placeholderTextColor={c.textMuted}
        value={text}
        onChangeText={setText}
        autoCapitalize="none"
        autoCorrect={false}
        textAlignVertical="top"
      />

      {error && <Text style={[styles.error, { color: c.statusFailSolid }]}>{error}</Text>}

      <View style={styles.actions}>
        <TouchableOpacity
          style={[
            styles.secondaryButton,
            { backgroundColor: c.surface, borderColor: c.border, borderRadius: radii.control },
          ]}
          onPress={onDone}
        >
          <Text style={[styles.secondaryButtonText, { color: c.textMuted }]}>{t('addDownload.cancel')}</Text>
        </TouchableOpacity>
        <GlimButton hue={1} grow label={t('addDownload.button')} busy={busy} onPress={submit} />
      </View>
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens, never baked
// in here: a stylesheet is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  container: { flex: 1, padding: 24, paddingTop: 56 },
  title: { fontSize: TYPE.heading, fontWeight: '600', marginBottom: 8 },
  hint: { fontSize: TYPE.body, marginBottom: 16 },
  textArea: {
    flex: 1,
    padding: 14,
    fontSize: TYPE.body,
    borderWidth: 1,
  },
  error: { marginTop: 12, fontSize: TYPE.body },
  actions: { flexDirection: 'row', gap: 12, marginTop: 16 },
  button: {
    flex: 1,
    paddingVertical: 14,
    alignItems: 'center',
  },
  secondaryButton: {
    flex: 1,
    paddingVertical: 14,
    alignItems: 'center',
    borderWidth: 1,
  },
  secondaryButtonText: { fontSize: 16 },
});
