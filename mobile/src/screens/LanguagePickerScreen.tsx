import { useEffect, useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { useT } from '../i18n/I18nContext';
import { LANGUAGES, flagEmoji } from '../i18n/catalogue';
import { getLanguageOverride } from '../storage/languagePreference';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { Text } from '../components/Text';
import { CardButton } from '../components/glim';
import IconBadge, { Back, Check } from '../components/IconBadge';
import { Arrive, MovingList } from '../components/Moving';

export default function LanguagePickerScreen({ onBack }: { onBack: () => void }) {
  const { t, lang, setLanguage } = useT();
  const { c, accentInk, corners } = useAppearance();
  const [override, setOverride] = useState<string | null>(null);

  useEffect(() => {
    getLanguageOverride().then(setOverride);
  }, []);

  // No "Automatic" row (GlimStone 1.4.0). Somebody opens this screen to ask
  // which language they are reading, and a row labelled after the act of
  // resolving one does not answer it. `lang` holds the resolved code, so
  // selecting that row says it outright.
  const rows = LANGUAGES.map((l) => ({ code: l.code, label: l.label, flag: flagEmoji(l.flag) }));

  const pick = (code: string | null) => {
    setLanguage(code);
    onBack();
  };

  return (
    <View style={[styles.container, { backgroundColor: c.bg }]}>
      {/* The settings page's own top bar, so the way back sits where it sat
          one step before. */}
      <View style={styles.topBar}>
        <IconBadge icon={<Back color={c.textSub} />} onPress={onBack} accessibilityLabel={t('settings.back')} />
        <Text style={[styles.title, { color: c.text }]}>{t('settings.language')}</Text>
      </View>

      <MovingList
        data={rows}
        keyExtractor={(r) => r.code}
        contentContainerStyle={styles.list}
        renderItem={({ item }) => {
          // Marked against the resolved language rather than the stored
          // override: without the "Automatic" row, a screen where nothing is
          // ticked until somebody picks leaves the question this list answers
          // open on first sight.
          const isSelected = (override ?? lang) === item.code;
          return (
            <Arrive>
              <CardButton
                style={[
                  styles.row,
                  // The chosen row is a deeper fill with accent ink rather than
                  // an outline: this language separates by shade, and a ring
                  // here would be the one drawn border on the screen.
                  { backgroundColor: isSelected ? c.surface2 : c.surface, ...corners.card },
                ]}
                onPress={() => pick(item.code)}
              >
                <Text style={styles.flag}>{item.flag}</Text>
                <Text style={[styles.rowLabel, { color: c.text }]}>{item.label}</Text>
                {isSelected && <Check color={accentInk} size={18} />}
              </CardButton>
            </Arrive>
          );
        }}
      />
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens rather than
// baked in here: a stylesheet is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  container: { flex: 1 },
  topBar: { paddingHorizontal: 16, paddingTop: 56, paddingBottom: 16, flexDirection: 'row', alignItems: 'center', gap: 12 },
  title: { fontSize: TYPE.heading, fontWeight: '600' },
  list: { paddingHorizontal: 16, paddingBottom: 32 },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    minHeight: 52,
    paddingHorizontal: 16,
    marginBottom: 8,
  },
  // A fixed width keeps every label starting on the same column, since emoji
  // flags do not all measure the same across platform fonts. The size is an
  // icon metric next to that width rather than a role on the type scale, which
  // is why it stays a number while the heading beside it does not.
  flag: { fontSize: 20, width: 30 },
  rowLabel: { fontSize: TYPE.body, flex: 1 },
});
