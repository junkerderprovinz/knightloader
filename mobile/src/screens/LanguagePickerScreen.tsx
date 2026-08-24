import { useEffect, useState } from 'react';
import { FlatList, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { useT } from '../i18n/I18nContext';
import { LANGUAGES } from '../i18n/catalogue';
import { getLanguageOverride } from '../storage/languagePreference';
import { colors } from '../theme';

export default function LanguagePickerScreen({ onBack }: { onBack: () => void }) {
  const { t, setLanguage } = useT();
  const [override, setOverride] = useState<string | null>(null);

  useEffect(() => {
    getLanguageOverride().then(setOverride);
  }, []);

  const rows: { code: string | null; label: string }[] = [
    { code: null, label: t('settings.languageAutomatic') },
    ...LANGUAGES.map((l) => ({ code: l.code as string | null, label: l.label })),
  ];

  const pick = (code: string | null) => {
    setLanguage(code);
    onBack();
  };

  return (
    <View style={styles.container}>
      <View style={styles.topBar}>
        <TouchableOpacity onPress={onBack}>
          <Text style={styles.back}>‹ {t('settings.title')}</Text>
        </TouchableOpacity>
        <Text style={styles.title}>{t('settings.language')}</Text>
      </View>

      <FlatList
        data={rows}
        keyExtractor={(r) => r.code ?? 'auto'}
        contentContainerStyle={styles.list}
        renderItem={({ item }) => {
          const isSelected = override === item.code;
          return (
            <TouchableOpacity style={[styles.row, isSelected && styles.rowSelected]} onPress={() => pick(item.code)}>
              <Text style={styles.rowLabel}>{item.label}</Text>
              {isSelected && <Text style={styles.check}>✓</Text>}
            </TouchableOpacity>
          );
        }}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: colors.background },
  topBar: { padding: 16, paddingTop: 56, gap: 4 },
  back: { color: colors.textMuted, fontSize: 13 },
  title: { color: colors.text, fontSize: 20, fontWeight: '600' },
  list: { paddingHorizontal: 16, paddingBottom: 32 },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    paddingVertical: 14,
    paddingHorizontal: 16,
    backgroundColor: colors.surface,
    borderRadius: 8,
    marginBottom: 6,
  },
  rowSelected: { borderWidth: 1, borderColor: colors.accent },
  rowLabel: { color: colors.text, fontSize: 15, flex: 1 },
  check: { color: colors.accent, fontSize: 15, fontWeight: '700' },
});
