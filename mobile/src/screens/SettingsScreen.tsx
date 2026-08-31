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
import { GlimRow, GlimToggle, NotchCard, Swatch, WellSelector } from '../components/glim';
import IconBadge from '../components/IconBadge';

const GITHUB_URL = 'https://github.com/junkerderprovinz/knightloader/tree/main/mobile';
const CONTACT_MAIL = 'hello@knightloader.app';
const REPORT_URL = 'https://github.com/junkerderprovinz/knightloader/issues/new?template=app.yml';

/**
 * Which GlimStone this screen implements. A plain constant, kept in step by
 * hand, because there is nothing to import it from: the design language is a
 * document plus a reference, not a package. The same constant lives in the web
 * UI's Settings.tsx and the extension's options.js, and the three are expected
 * to agree.
 */
const GLIMSTONE_VERSION = '1.7.0';

/** shapeOf reads the shape back out of the radii the context resolved.
 *
 *  The context deliberately exposes radii rather than the name behind them -
 *  a component should ask "how round is a card", not "which setting is on".
 *  This screen is the one place that needs the name, to mark the segment that
 *  is active, so it derives it here rather than widening the contract for
 *  every other caller. */
function shapeOf(radii: { card: number }): Shape {
  if (radii.card === 0) return 'square';
  return radii.card <= 8 ? 'soft' : 'round';
}

/**
 * The settings, drawn in the same language as the product they configure
 * (jdp, 2026-08-29: "In den einstellungen sehen die buttons und alles ganz
 * anders aus wie in KL selbst. Das soll auch in der App exakt gleich
 * aussehen."): notch-titled cards instead of grey captions, well selectors
 * instead of bordered chips, a real switch for following the instance, and no
 * drawn border anywhere on the page.
 */
export default function SettingsScreen({
  onBack,
  onOpenLanguagePicker,
  onRemovedAllConnections,
  onRefreshAppearance,
}: {
  onBack: () => void;
  onOpenLanguagePicker: () => void;
  onRemovedAllConnections: () => void;
  onRefreshAppearance?: () => void;
}) {
  const { t, lang } = useT();
  const {
    c,
    accent,
    accentInk,
    radii,
    dark,
    rainbow,
    overridden,
    setAccent,
    setShape,
    setTheme,
    setRainbow,
    followInstance,
    snapshotAsLocal,
  } = useAppearance();
  const [override, setOverride] = useState<string | null>(null);
  const anyOverride = overridden.accent || overridden.shape || overridden.theme || overridden.rainbow;

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
  // resolving one (GlimStone 1.4.0).
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

  return (
    <ScrollView style={{ backgroundColor: c.bg }} contentContainerStyle={styles.container}>
      <View style={styles.topBar}>
        {/* A real 44x44 target, not a bare chevron (jdp, 2026-08-30: "Der
            zurück button in den einstellungen ist schlecht bedienbar und kaum
            zu treffen"). It was a single "‹" glyph, so the touchable was the
            size of that character - about 12 by 22 points, against the 44 both
            platforms' own guidelines call the minimum. hitSlop is deliberately
            NOT the fix here: it would widen the target invisibly while the
            thing on screen stayed a hairline, and the complaint is that it is
            hard to HIT and hard to SEE. It is a proper square badge now, the
            same one the overview's own top bar uses. */}
        <IconBadge symbol="‹" onPress={onBack} accessibilityLabel={t('settings.back')} />
        <Text style={[styles.title, { color: c.text }]}>{t('settings.title')}</Text>
      </View>

      {/* Each card owns a rainbow position, 0-based in page order - the same
          equal-member set the web UI's settings cards form. Without the mode
          they all resolve to the single accent. */}
      <NotchCard title={t('settings.language')} hue={0}>
        <TouchableOpacity onPress={onOpenLanguagePicker}>
          <GlimRow
            label={t('settings.language')}
            control={
              <View style={styles.valueGroup}>
                <Text style={styles.flag}>{currentFlag}</Text>
                <Text style={[styles.value, { color: c.textMuted }]}>{currentLabel}</Text>
              </View>
            }
          />
        </TouchableOpacity>
      </NotchCard>

      <NotchCard title={t('settings.appearance')} hue={1}>
        {/* The switch, not a link that appears once something is overridden:
            following the instance is a STATE, and a state gets the same
            control every state in this family gets. Off snapshots the current
            look as local so nothing visibly jumps; on clears the local
            overrides AND refetches, because "übernehmen" that shows last
            week's colours is not übernehmen (jdp: "Einstellungen übernehmen
            funktionieren nicht"). */}
        <GlimRow
          label={t('settings.followInstance')}
          sub={anyOverride ? t('settings.appearanceOverridden') : t('settings.appearanceFollows')}
          control={
            <GlimToggle
              hue={0}
              value={!anyOverride}
              onChange={(follow) => {
                if (follow) {
                  followInstance();
                  onRefreshAppearance?.();
                } else {
                  snapshotAsLocal();
                }
              }}
            />
          }
        />

        <Text style={[styles.axisLabel, { color: c.textSub }]}>{t('settings.theme')}</Text>
        <WellSelector
          options={[
            { value: 'light', label: t('settings.theme.light') },
            { value: 'dark', label: t('settings.theme.dark') },
          ]}
          value={dark ? 'dark' : 'light'}
          onPick={(v) => setTheme(v)}
        />

        <Text style={[styles.axisLabel, { color: c.textSub }]}>{t('settings.corners')}</Text>
        <WellSelector
          options={SHAPES.map((s: Shape) => ({ value: s, label: t(`settings.corners.${s}`) }))}
          value={shapeOf(radii)}
          onPick={(v) => setShape(v)}
        />

        {/* Label left, swatches right, one row (jdp, 2026-08-30: "Akzentfarbe
            und die farbfelder sollen in eine zeile, text linksbündig,
            farbfelder rechtsbündig") - the same shape the web interface's own
            Farben card just took, and the same shape every GlimRow on this
            page already has. The caption used to sit on its own line above a
            left-aligned row, which read as a heading over a group rather than
            as one setting with its answer beside it. */}
        <View style={styles.axisRow}>
          {/* Normal row text, not the small uppercase-ish axis caption (jdp,
              2026-08-30: "Akzentfarbe Text normal formatieren"): once the label
              sits BESIDE its control rather than above a group, it is a row
              label, and every other row label on this page is 15px body text in
              the ordinary ink. */}
          <Text style={[styles.rowLabel, { color: c.text }]}>{t('settings.accent')}</Text>
          <View style={styles.swatches}>
            {ACCENTS.map((a) => (
              <Swatch
                key={a.hex}
                hex={a.hex}
                label={a.name}
                selected={accent.toLowerCase() === a.hex.toLowerCase()}
                onPress={() => setAccent(a.hex)}
              />
            ))}
          </View>
        </View>

        {/* A switch, not a read-only line (jdp, 2026-08-30: "Regenbogenmodus
            hat kein toggle und kann nicht aktiviert werden"). It used to say
            where the mode was set instead of setting it, on the grounds that
            the seed belongs to the instance - which is still true, and still
            enforced: only ON/OFF is local here, the palette and the seed come
            from the instance either way, so two clients never disagree about
            which colour a position is. hue={1} puts this switch second in
            this card's own set of switches, after "follow the instance".

            No caption of its own: the switch at the top of this card already
            says whether the look is following the instance or set here, and
            flipping this one flips that one - a second sentence saying the
            same thing per row is how a card stops being readable. */}
        <GlimRow
          label={t('settings.rainbow')}
          control={<GlimToggle hue={1} value={rainbow.on} onChange={(on) => setRainbow(on)} />}
        />
      </NotchCard>

      <NotchCard title={t('settings.problems')} hue={2}>
        <Text style={[styles.hint, { color: c.textMuted }]}>{t('settings.problemsHint')}</Text>
        <View style={[styles.report, { backgroundColor: c.surface2, borderRadius: radii.control }]}>
          <Text style={[styles.reportText, { color: c.textSub }]} selectable>
            {report}
          </Text>
        </View>
        <View style={styles.buttonRow}>
          <TouchableOpacity
            style={[styles.button, { backgroundColor: c.surface2, borderRadius: radii.control }]}
            onPress={() => Share.share({ message: report })}
          >
            <Text style={[styles.buttonText, { color: c.text }]}>{t('settings.problemsCopy')}</Text>
          </TouchableOpacity>
          <TouchableOpacity
            style={[styles.button, { backgroundColor: c.surface2, borderRadius: radii.control }]}
            onPress={() => Linking.openURL(`${REPORT_URL}&report=${encodeURIComponent(report)}`)}
          >
            <Text style={[styles.buttonText, { color: accentInk }]}>{t('settings.problemsReport')}</Text>
          </TouchableOpacity>
        </View>
      </NotchCard>

      {/* The About card carries the versions AND the way to report something
          (jdp, 2026-08-31: "darin sollen die versionsnummern stehen und ein
          text ... Dann soll da ein button sein der zu Github führt ... und ein
          Button der die email app öffnet").

          This is the one card in the family whose body is prose rather than an
          info bubble: it has no control to explain, the sentence IS the
          content. Written into GlimStone 1.7.0 as a named exception rather than
          left for somebody to trip over. */}
      <NotchCard title={t('settings.about')} hue={3}>
        <Text style={[styles.aboutText, { color: c.textSub }]}>{t('settings.aboutBody')}</Text>
        <Text style={[styles.aboutVersions, { color: c.textMuted }]}>
          {`${t('settings.aboutVersion')} ${Constants.expoConfig?.version ?? '—'} · GlimStone ${GLIMSTONE_VERSION}`}
        </Text>
        <View style={styles.buttonRow}>
          <TouchableOpacity
            style={[styles.button, { backgroundColor: c.surface2, borderRadius: radii.control }]}
            onPress={() => Linking.openURL(GITHUB_URL)}
          >
            <Text style={[styles.buttonText, { color: c.text }]}>{t('settings.aboutGithub')}</Text>
          </TouchableOpacity>
          <TouchableOpacity
            style={[styles.button, { backgroundColor: c.surface2, borderRadius: radii.control }]}
            // A plain mailto, subject prefilled so a mail arrives already saying
            // which product it is about. No body: a prefilled body reads as a
            // form to fill in, and this is meant to be a message somebody
            // writes.
            onPress={() =>
              Linking.openURL(
                `mailto:${CONTACT_MAIL}?subject=${encodeURIComponent(`KnightLoader ${t('settings.aboutMailSubject')}`)}`,
              )
            }
          >
            <Text style={[styles.buttonText, { color: c.text }]}>{t('settings.aboutMail')}</Text>
          </TouchableOpacity>
        </View>
      </NotchCard>

      <NotchCard title={t('settings.dangerZone')} hue={4}>
        {/* A surface with red INK, not a red outline: the fail colour carries
            the meaning, and this language draws no outlines. The confirmation
            dialog is where the actually destructive control lives. */}
        <TouchableOpacity
          style={[styles.button, { backgroundColor: c.surface2, borderRadius: radii.control }]}
          onPress={confirmRemoveAll}
        >
          <Text style={[styles.buttonText, { color: c.statusFailSolid }]}>{t('settings.removeAllConnections')}</Text>
        </TouchableOpacity>
      </NotchCard>

      {/* No version footer any more (jdp, 2026-08-31: "Die vversionsnummer
          sollen dann nicht nochmal unter den card im hintergrund angeziegt
          werden"). It said the same thing the About card above now says, in
          smaller type and outside every card - and page chrome reads as
          something nobody put there on purpose. GlimStone 1.7.0 replaces its
          own version-footer rule with the About card for the whole family. */}
    </ScrollView>
  );
}

// Colours and radii are applied inline from the resolved tokens, never baked
// in here: a stylesheet is built once and cannot follow a theme change.
// One column stretched across a tablet is a card 900 points wide with its
// text at one edge and its badge at the other. A cap plus centring costs a
// phone nothing (640 is wider than every phone) and makes a tablet readable.
const capped = { width: '100%' as const, maxWidth: 640, alignSelf: 'center' as const };

const styles = StyleSheet.create({
  container: { ...capped, paddingHorizontal: 16, paddingBottom: 32 },
  topBar: { paddingTop: 56, paddingBottom: 4, flexDirection: 'row', alignItems: 'center', gap: 12 },
  title: { fontSize: TYPE.heading, fontWeight: '600' },
  // Half-muted and centred: something you look for, not something that
  // competes for attention.
  aboutText: { fontSize: TYPE.body, lineHeight: 20, marginBottom: 6 },
  aboutVersions: { fontSize: TYPE.caption, marginBottom: 10 },
  valueGroup: { flexDirection: 'row', alignItems: 'center', gap: 8, flexShrink: 1 },
  flag: { fontSize: 17 },
  value: { fontSize: TYPE.body },
  hint: { fontSize: TYPE.caption, lineHeight: 16, marginBottom: 8 },
  axisLabel: { fontSize: TYPE.caption, marginTop: 12, marginBottom: 6, letterSpacing: 0.6 },
  // The inline variant drops the stacked spacing: in a row the label is beside
  // its control, not above it.
  axisRow: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 12, marginTop: 12, marginBottom: 2 },
  axisLabelInline: { marginTop: 0, marginBottom: 0, flexShrink: 0 },
  // The same shape GlimRow gives every other label on this page.
  rowLabel: { fontSize: 15, flexShrink: 0 },
  swatches: { flexDirection: 'row', flexWrap: 'wrap', gap: 6, alignItems: 'center', justifyContent: 'flex-end', flexShrink: 1 },
  report: { padding: 12, marginBottom: 10 },
  reportText: { fontSize: TYPE.caption, lineHeight: 17, fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace' },
  buttonRow: { flexDirection: 'row', gap: 8 },
  button: { paddingVertical: 11, paddingHorizontal: 16, alignItems: 'center', flexShrink: 1 },
  buttonText: { fontSize: TYPE.dense, fontWeight: '500' },
});
