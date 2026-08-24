import { useCallback, useState } from 'react';
import { Alert, Linking, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { useFocusEffect } from '@react-navigation/native';
import Constants from 'expo-constants';
import { useT } from '../i18n/I18nContext';
import { LANGUAGES } from '../i18n/catalogue';
import { getLanguageOverride } from '../storage/languagePreference';
import { removeAllConnections } from '../storage/connections';
import { colors } from '../theme';

const GITHUB_URL = 'https://github.com/junkerderprovinz/knightloader/tree/main/mobile';

export default function SettingsScreen({
  onBack,
  onOpenLanguagePicker,
  onRemovedAllConnections,
}: {
  onBack: () => void;
  onOpenLanguagePicker: () => void;
  onRemovedAllConnections: () => void;
}) {
  const { t } = useT();
  const [override, setOverride] = useState<string | null>(null);

  // useFocusEffect, not a plain mount-only effect: this screen stays
  // mounted underneath LanguagePickerScreen while it's open, so coming BACK
  // from it (having just changed the override) needs a re-read on every
  // return to this screen, not only the first time it opens.
  useFocusEffect(
    useCallback(() => {
      getLanguageOverride().then(setOverride);
    }, [])
  );

  const currentLabel = override ? LANGUAGES.find((l) => l.code === override)?.label ?? override : t('settings.languageAutomatic');

  const confirmRemoveAll = () => {
    Alert.alert(t('settings.removeAllConfirmTitle'), t('settings.removeAllConfirmMessage'), [
      { text: t('settings.cancel'), style: 'cancel' },
      {
        text: t('settings.removeAllConfirmButton'),
        style: 'destructive',
        onPress: async () => {
          await removeAllConnections();
          onRemovedAllConnections();
        },
      },
    ]);
  };

  return (
    <View style={styles.container}>
      <View style={styles.topBar}>
        <TouchableOpacity onPress={onBack}>
          <Text style={styles.back}>‹</Text>
        </TouchableOpacity>
        <Text style={styles.title}>{t('settings.title')}</Text>
      </View>

      <View style={styles.section}>
        <Text style={styles.sectionTitle}>{t('settings.language')}</Text>
        <TouchableOpacity style={styles.row} onPress={onOpenLanguagePicker}>
          <Text style={styles.rowLabel}>{t('settings.language')}</Text>
          <Text style={styles.rowValue}>{currentLabel}</Text>
        </TouchableOpacity>
      </View>

      <View style={styles.section}>
        <Text style={styles.sectionTitle}>{t('settings.about')}</Text>
        <View style={styles.row}>
          <Text style={styles.rowLabel}>{t('settings.version', { version: Constants.expoConfig?.version ?? '—' })}</Text>
        </View>
        <TouchableOpacity style={styles.row} onPress={() => Linking.openURL(GITHUB_URL)}>
          <Text style={[styles.rowLabel, styles.link]}>{t('settings.githubLink')}</Text>
        </TouchableOpacity>
      </View>

      <View style={styles.section}>
        <Text style={styles.sectionTitle}>{t('settings.dangerZone')}</Text>
        <TouchableOpacity style={styles.dangerButton} onPress={confirmRemoveAll}>
          <Text style={styles.dangerButtonText}>{t('settings.removeAllConnections')}</Text>
        </TouchableOpacity>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: colors.background },
  topBar: { padding: 16, paddingTop: 56, flexDirection: 'row', alignItems: 'center', gap: 12 },
  back: { color: colors.textMuted, fontSize: 22 },
  title: { color: colors.text, fontSize: 20, fontWeight: '600' },
  section: { paddingHorizontal: 16, marginTop: 20, gap: 8 },
  sectionTitle: { color: colors.textMuted, fontSize: 12, fontWeight: '600', textTransform: 'uppercase', letterSpacing: 0.5 },
  row: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    backgroundColor: colors.surface,
    borderRadius: 8,
    paddingVertical: 14,
    paddingHorizontal: 16,
  },
  rowLabel: { color: colors.text, fontSize: 15 },
  rowValue: { color: colors.textMuted, fontSize: 14 },
  link: { color: colors.accent },
  dangerButton: {
    backgroundColor: colors.surface,
    borderRadius: 8,
    paddingVertical: 14,
    paddingHorizontal: 16,
    borderWidth: 1,
    borderColor: colors.danger,
    alignItems: 'center',
  },
  dangerButtonText: { color: colors.danger, fontSize: 15, fontWeight: '600' },
});
