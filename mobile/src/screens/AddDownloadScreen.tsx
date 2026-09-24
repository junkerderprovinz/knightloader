import { useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { addLinks, ApiError } from '../api/client';
import type { Instance, ServerConnection } from '../api/types';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import { GlimButton } from '../components/glim';
import { Cross, Plus } from '../components/IconBadge';
import { InfoTip } from '../components/InfoTip';
import { Text, TextInput } from '../components/Text';

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
  const { c, radii } = useAppearance();
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
      {/* How the field wants its links is an explanation, so it hangs off an
          (i) beside the title rather than standing under it as a paragraph
          (GlimStone rule 8). */}
      <View style={styles.titleRow}>
        <Text style={[styles.title, { color: c.text }]}>
          {peer ? t('addDownload.titlePeer', { name: peer.displayName ?? peer.name }) : t('addDownload.title')}
        </Text>
        <InfoTip text={t('addDownload.hint')} />
      </View>

      {/* Filled and borderless, as every field in the family is: a surface is
          told apart by its shade, never by a drawn line. */}
      <TextInput
        style={[styles.textArea, { backgroundColor: c.surface, color: c.text, borderRadius: radii.control }]}
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

      {/* The way back first and the one that goes ahead at the end, both the
          shared button with its glyph. Cancel is quiet: a page has one accent
          button, and it is the one that adds. */}
      <View style={styles.actions}>
        <GlimButton tone="quiet" grow label={t('addDownload.cancel')} icon={(ink) => <Cross color={ink} />} onPress={onDone} />
        <GlimButton
          hue={1}
          grow
          label={t('addDownload.button')}
          icon={(ink) => <Plus color={ink} />}
          busy={busy}
          onPress={submit}
        />
      </View>
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens rather than
// baked in here: a stylesheet is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  container: { flex: 1, padding: 24, paddingTop: 56 },
  titleRow: { flexDirection: 'row', alignItems: 'center', gap: 8, marginBottom: 16 },
  title: { fontSize: TYPE.heading, fontWeight: '600', flexShrink: 1 },
  textArea: {
    flex: 1,
    padding: 14,
    fontSize: TYPE.body,
  },
  error: { marginTop: 12, fontSize: TYPE.body },
  actions: { flexDirection: 'row', gap: 12, marginTop: 16 },
});
