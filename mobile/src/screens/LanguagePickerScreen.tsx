import { useEffect, useState } from 'react';
import { FlatList, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { useT } from '../i18n/I18nContext';
import { LANGUAGES, flagEmoji } from '../i18n/catalogue';
import { getLanguageOverride } from '../storage/languagePreference';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';

export default function LanguagePickerScreen({ onBack }: { onBack: () => void }) {
  const { t, lang, setLanguage } = useT();
  const { c, accentInk, radii } = useAppearance();
  const [override, setOverride] = useState<string | null>(null);

  useEffect(() => {
    getLanguageOverride().then(setOverride);
  }, []);

  // No "Automatic" row (jdp, 2026-08-28, now rule in GlimStone 1.4.0: "Im
  // Sprachen-dropdown soll nicht stehen Automatisch. Es soll einfach die
  // Sprache auswählen die im Browser eingestellt ist").
  //
  // The row existed and even carried the resolved flag, which made it look
  // like the thoughtful version. It still could not answer the question
  // somebody opens this screen to ask - WHICH language am I reading - because
  // the answer sat on a row labelled after the act of resolving rather than
  // after the language. `lang` already holds the resolved code, so selecting
  // that row says it outright and the extra entry buys nothing.
  const rows = LANGUAGES.map((l) => ({ code: l.code, label: l.label, flag: flagEmoji(l.flag) }));

  const pick = (code: string | null) => {
    setLanguage(code);
    onBack();
  };

  return (
    <View style={[styles.container, { backgroundColor: c.bg }]}>
      <View style={styles.topBar}>
        <TouchableOpacity onPress={onBack}>
          <Text style={[styles.back, { color: c.textMuted }]}>‹ {t('settings.title')}</Text>
        </TouchableOpacity>
        <Text style={[styles.title, { color: c.text }]}>{t('settings.language')}</Text>
      </View>

      <FlatList
        data={rows}
        keyExtractor={(r) => r.code}
        contentContainerStyle={styles.list}
        renderItem={({ item }) => {
          // Marked against the RESOLVED language, not against the stored
          // override: with the "Automatic" row gone, a screen where nothing is
          // ticked until somebody picks would leave the one question this list
          // answers unanswered on first open.
          const isSelected = (override ?? lang) === item.code;
          return (
            <TouchableOpacity
              style={[
                styles.row,
                // The chosen row is a deeper fill with accent ink, never an
                // outline: this language separates by shade, and a ring here
                // was its one drawn border on the whole screen.
                { backgroundColor: isSelected ? c.surface2 : c.surface, borderRadius: radii.card },
              ]}
              onPress={() => pick(item.code)}
            >
              <Text style={styles.flag}>{item.flag}</Text>
              <Text style={[styles.rowLabel, { color: c.text }]}>{item.label}</Text>
              {isSelected && <Text style={[styles.check, { color: accentInk }]}>✓</Text>}
            </TouchableOpacity>
          );
        }}
      />
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens, never baked
// in here: a stylesheet is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  container: { flex: 1 },
  topBar: { padding: 16, paddingTop: 56, gap: 4 },
  // Off the scale in theme/tokens.ts, like every other size on this screen:
  // 13 and 15 are rungs between Dense and Body that the table does not have.
  back: { fontSize: TYPE.dense },
  title: { fontSize: TYPE.heading, fontWeight: '600' },
  list: { paddingHorizontal: 16, paddingBottom: 32 },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    paddingVertical: 14,
    paddingHorizontal: 16,
    marginBottom: 6,
  },
  // A fixed width keeps every label starting on the same column even though
  // emoji flags do not all measure identically across platform fonts. The size
  // is an icon metric next to that width, not a role on the type scale, which
  // is why it stays a number while the heading beside it does not.
  flag: { fontSize: 20, width: 30 },
  rowLabel: { fontSize: TYPE.body, flex: 1 },
  // The tick rides the row's own text size rather than a number of its own: it
  // sits on one line with the label and reads as part of it.
  check: { fontSize: TYPE.body, fontWeight: '700' },
});
