import { useCallback, useState } from 'react';
import { Alert, Linking, Platform, ScrollView, Share, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { useFocusEffect } from '@react-navigation/native';
import Constants from 'expo-constants';
import { useT } from '../i18n/I18nContext';
import { LANGUAGES, flagEmoji } from '../i18n/catalogue';
import { getLanguageOverride } from '../storage/languagePreference';
import { removeAllConnections } from '../storage/connections';
import { useAppearance } from '../theme/AppearanceContext';
import { ACCENTS, SHAPES, type Shape } from '../theme/appearance';
import { TYPE } from '../theme/tokens';

const GITHUB_URL = 'https://github.com/junkerderprovinz/knightloader/tree/main/mobile';
const REPORT_URL = 'https://github.com/junkerderprovinz/knightloader/issues/new?template=app.yml';

/** shapeOf reads the shape back out of the radii the context resolved.
 *
 *  The context deliberately exposes radii rather than the name behind them -
 *  a component should ask "how round is a card", not "which setting is on".
 *  The settings screen is the one place that needs the name, to mark the chip
 *  that is active, so it derives it here rather than widening the contract for
 *  every other caller. */
function shapeOf(radii: { card: number }): Shape {
  if (radii.card === 0) return 'square';
  return radii.card <= 8 ? 'soft' : 'round';
}

export default function SettingsScreen({
  onBack,
  onOpenLanguagePicker,
  onRemovedAllConnections,
}: {
  onBack: () => void;
  onOpenLanguagePicker: () => void;
  onRemovedAllConnections: () => void;
}) {
  const { t, lang } = useT();
  const {
    c,
    accent,
    accentContrast,
    radii,
    dark,
    rainbow,
    overridden,
    setAccent,
    setShape,
    setTheme,
    followInstance,
  } = useAppearance();
  const [override, setOverride] = useState<string | null>(null);
  const anyOverride = overridden.accent || overridden.shape || overridden.theme;

  // What a bug report actually needs, and nothing more. No address, no token:
  // an address is somebody's home network, and a token is a credential - both
  // would be pasted into a public issue by anybody who trusted this button.
  const report = [
    `app:      ${Constants.expoConfig?.version ?? '?'} (versionCode ${Constants.expoConfig?.android?.versionCode ?? '?'})`,
    `platform: ${Platform.OS} ${Platform.Version}`,
    `language: ${lang}`,
    `look:     theme=${dark ? 'dark' : 'light'}${overridden.theme ? '' : ' (device)'} accent=${anyOverride ? 'local' : 'instance'} rainbow=${rainbow.on ? 'on' : 'off'}`,
  ].join(String.fromCharCode(10));

  // useFocusEffect, not a plain mount-only effect: this screen stays
  // mounted underneath LanguagePickerScreen while it's open, so coming BACK
  // from it (having just changed the override) needs a re-read on every
  // return to this screen, not only the first time it opens.
  useFocusEffect(
    useCallback(() => {
      getLanguageOverride().then(setOverride);
    }, [])
  );

  // Named after the language actually in effect, never after the act of
  // resolving one: the "Automatic" label is gone from the picker and from
  // here, for the same reason (GlimStone 1.4.0).
  const currentLabel = LANGUAGES.find((l) => l.code === (override ?? lang))?.label ?? (override ?? lang);
  const currentFlag = flagEmoji(LANGUAGES.find((l) => l.code === lang)?.flag ?? '');

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

  const rowStyle = { backgroundColor: c.surface, borderRadius: radii.card };

  return (
    <View style={[styles.container, { backgroundColor: c.bg }]}>
      <View style={styles.topBar}>
        <TouchableOpacity onPress={onBack}>
          <Text style={[styles.back, { color: c.textMuted }]}>‹</Text>
        </TouchableOpacity>
        <Text style={[styles.title, { color: c.text }]}>{t('settings.title')}</Text>
      </View>

      <View style={styles.section}>
        <Text style={[styles.sectionTitle, { color: c.textMuted }]}>{t('settings.language')}</Text>
        <TouchableOpacity style={[styles.row, rowStyle]} onPress={onOpenLanguagePicker}>
          <Text style={[styles.rowLabel, { color: c.text }]}>{t('settings.language')}</Text>
          <View style={styles.rowValueGroup}>
            <Text style={styles.rowFlag}>{currentFlag}</Text>
            <Text style={[styles.rowValue, { color: c.textMuted }]}>{currentLabel}</Text>
          </View>
        </TouchableOpacity>
      </View>

      <View style={styles.section}>
        <Text style={[styles.sectionTitle, { color: c.textMuted }]}>{t('settings.appearance')}</Text>
        {/* The instance leads and this overrides it, so the hint says which
            state you are in rather than leaving somebody to guess why a colour
            they never picked is on screen. */}
        <Text style={[styles.hint, { color: c.textMuted }]}>
          {anyOverride ? t('settings.appearanceOverridden') : t('settings.appearanceFollows')}
        </Text>

        {/* Two chips, and the device's own mode is the one already marked
            (jdp, 2026-08-29, now rule in GlimStone 1.4.0: "es soll nur hell und
            dunkel zur auswahl geben und es soll automatisch der im system
            eingestelle modus standardmässig ausgewählt werden"). The third chip
            named the act of resolving rather than a result, so the control
            could not answer the only question anyone asks it: which of the two
            am I looking at. `dark` already holds the resolved answer, so the
            chip that matches it is marked whether or not anything is stored. */}
        <Text style={[styles.axisLabel, { color: c.textSub }]}>{t('settings.theme')}</Text>
        <View style={styles.chips}>
          {(['light', 'dark'] as const).map((k) => {
            const on = dark === (k === 'dark');
            return (
              <TouchableOpacity
                key={k}
                onPress={() => setTheme(k)}
                style={[
                  styles.chip,
                  { borderRadius: radii.control, backgroundColor: on ? accent : c.surface, borderColor: c.border },
                ]}
              >
                <Text style={[styles.chipText, { color: on ? accentContrast : c.text }]}>{t(`settings.theme.${k}`)}</Text>
              </TouchableOpacity>
            );
          })}
        </View>

        <Text style={[styles.axisLabel, { color: c.textSub }]}>{t('settings.corners')}</Text>
        <View style={styles.chips}>
          {SHAPES.map((s: Shape) => (
            <TouchableOpacity
              key={s}
              onPress={() => setShape(s)}
              style={[
                styles.chip,
                {
                  borderRadius: radii.control,
                  backgroundColor: overridden.shape && shapeOf(radii) === s ? accent : c.surface,
                  borderColor: c.border,
                },
              ]}
            >
              <Text
                style={[
                  styles.chipText,
                  { color: overridden.shape && shapeOf(radii) === s ? accentContrast : c.text },
                ]}
              >
                {t(`settings.corners.${s}`)}
              </Text>
            </TouchableOpacity>
          ))}
        </View>

        <Text style={[styles.axisLabel, { color: c.textSub }]}>{t('settings.accent')}</Text>
        <View style={styles.chips}>
          {ACCENTS.map((a) => (
            <TouchableOpacity
              key={a.hex}
              onPress={() => setAccent(a.hex)}
              accessibilityLabel={a.name}
              style={[
                styles.swatch,
                {
                  backgroundColor: a.hex,
                  borderRadius: radii.pill,
                  // The current colour is marked by a ring rather than a tick:
                  // a glyph on a swatch has to be legible on all five, which
                  // means computing an ink colour for a decoration.
                  borderColor: accent.toLowerCase() === a.hex.toLowerCase() ? c.text : 'transparent',
                },
              ]}
            />
          ))}
        </View>

        {anyOverride && (
          <TouchableOpacity
            style={[styles.row, rowStyle]}
            onPress={followInstance}
          >
            <Text style={[styles.rowLabel, { color: accent }]}>{t('settings.followInstance')}</Text>
          </TouchableOpacity>
        )}

        {/* Rainbow is shown, never set: its seed belongs to the instance so
            that two clients of one server cannot disagree about the colour of
            a download. Saying where it is changed is more use than a switch
            that would create exactly that disagreement. */}
        <View style={[styles.row, rowStyle]}>
          <Text style={[styles.rowLabel, { color: c.text }]}>{t('settings.rainbow')}</Text>
          <Text style={[styles.rowValue, { color: c.textMuted }]}>
            {rainbow.on ? t('settings.rainbowOnFromInstance') : t('settings.rainbowOff')}
          </Text>
        </View>
      </View>

      <View style={styles.section}>
        <Text style={[styles.sectionTitle, { color: c.textMuted }]}>{t('settings.problems')}</Text>
        <Text style={[styles.hint, { color: c.textMuted }]}>{t('settings.problemsHint')}</Text>
        <View style={[styles.report, { backgroundColor: c.surface2, borderRadius: radii.control }]}>
          <Text style={[styles.reportText, { color: c.textSub }]} selectable>
            {report}
          </Text>
        </View>
        <View style={styles.chips}>
          <TouchableOpacity
            style={[styles.chip, { borderRadius: radii.control, backgroundColor: c.surface, borderColor: c.border }]}
            onPress={() => Share.share({ message: report })}
          >
            <Text style={[styles.chipText, { color: c.text }]}>{t('settings.problemsCopy')}</Text>
          </TouchableOpacity>
          <TouchableOpacity
            style={[styles.chip, { borderRadius: radii.control, backgroundColor: c.surface, borderColor: c.border }]}
            onPress={() => Linking.openURL(`${REPORT_URL}&report=${encodeURIComponent(report)}`)}
          >
            <Text style={[styles.chipText, { color: accent }]}>{t('settings.problemsReport')}</Text>
          </TouchableOpacity>
        </View>
      </View>

      <View style={styles.section}>
        <Text style={[styles.sectionTitle, { color: c.textMuted }]}>{t('settings.about')}</Text>
        <View style={[styles.row, rowStyle]}>
          <Text style={[styles.rowLabel, { color: c.text }]}>{t('settings.version', { version: Constants.expoConfig?.version ?? '—' })}</Text>
        </View>
        <TouchableOpacity style={[styles.row, rowStyle]} onPress={() => Linking.openURL(GITHUB_URL)}>
          <Text style={[styles.rowLabel, { color: accent }]}>{t('settings.githubLink')}</Text>
        </TouchableOpacity>
      </View>

      <View style={styles.section}>
        <Text style={[styles.sectionTitle, { color: c.textMuted }]}>{t('settings.dangerZone')}</Text>
        <TouchableOpacity
          style={[
            styles.dangerButton,
            { backgroundColor: c.surface, borderColor: c.statusFailSolid, borderRadius: radii.control },
          ]}
          onPress={confirmRemoveAll}
        >
          <Text style={[styles.dangerButtonText, { color: c.statusFailSolid }]}>{t('settings.removeAllConnections')}</Text>
        </TouchableOpacity>
      </View>
    </View>
  );
}

// Colours and radii are applied inline from the resolved tokens, never baked
// in here: a stylesheet is built once and cannot follow a theme change.
const styles = StyleSheet.create({
  container: { flex: 1 },
  topBar: { padding: 16, paddingTop: 56, flexDirection: 'row', alignItems: 'center', gap: 12 },
  back: { fontSize: 22 },
  title: { fontSize: TYPE.heading, fontWeight: '600' },
  section: { paddingHorizontal: 16, marginTop: 20, gap: 8 },
  sectionTitle: { fontSize: TYPE.dense, fontWeight: '600', textTransform: 'uppercase', letterSpacing: 0.5 },
  row: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: 14,
    paddingHorizontal: 16,
  },
  rowLabel: { fontSize: 15 },
  rowValueGroup: { flexDirection: 'row', alignItems: 'center', gap: 8, flexShrink: 1 },
  rowFlag: { fontSize: 17 },
  rowValue: { fontSize: TYPE.body },
  dangerButton: {
    paddingVertical: 14,
    paddingHorizontal: 16,
    borderWidth: 1,
    alignItems: 'center',
  },
  // Layout only. Colours and radii are applied inline from the resolved
  // tokens - see the note above this block.
  hint: { fontSize: TYPE.caption, lineHeight: 16, marginBottom: 10 },
  axisLabel: { fontSize: TYPE.caption, marginTop: 12, marginBottom: 6, letterSpacing: 0.6 },
  chips: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, alignItems: 'center' },
  chip: { paddingHorizontal: 14, paddingVertical: 8, borderWidth: 1 },
  chipText: { fontSize: TYPE.dense, fontWeight: '500' },
  swatch: { width: 28, height: 28, borderWidth: 2 },
  report: { padding: 12, marginBottom: 10 },
  reportText: { fontSize: TYPE.caption, lineHeight: 17, fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace' },
  dangerButtonText: { fontSize: 15, fontWeight: '600' },
});
