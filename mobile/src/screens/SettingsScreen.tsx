import { useCallback, useRef, useState } from 'react';
import { Alert, Linking, Platform, ScrollView, Share, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { useFocusEffect } from '@react-navigation/native';
import Constants from 'expo-constants';
import { useT } from '../i18n/I18nContext';
import { LANGUAGES, flagEmoji } from '../i18n/catalogue';
import { getLanguageOverride } from '../storage/languagePreference';
import { removeAllConnections } from '../storage/connections';
import { useAppearance } from '../theme/AppearanceContext';
import { ACCENTS, SHAPES, accentSlot, type Shape } from '../theme/appearance';
import { TYPE } from '../theme/tokens';
import { GlimRow, GlimToggle, NotchCard, Swatch, WellSelector } from '../components/glim';
import IconBadge, { Back } from '../components/IconBadge';
import ColorPicker from '../components/ColorPicker';

const GITHUB_URL = 'https://github.com/junkerderprovinz/knightloader';
const REPO_URL = GITHUB_URL;
const GLIMSTONE_URL = 'https://github.com/junkerderprovinz/glimstone';
const CONTACT_MAIL = 'hello@knightloader.app';

/**
 * Which GlimStone this screen implements. A plain constant, kept in step by
 * hand, because there is nothing to import it from: the design language is a
 * document plus a reference, not a package. The same constant lives in the web
 * UI's Settings.tsx and the extension's options.js, and the three are expected
 * to agree.
 */
const GLIMSTONE_VERSION = '1.6.0';

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
  onSetPalette,
}: {
  onBack: () => void;
  onOpenLanguagePicker: () => void;
  onRemovedAllConnections: () => void;
  onRefreshAppearance?: () => void;
  /** Write the rainbow palette back to the connected instance. Absent when
   *  there is no connection to write to, which is what makes the row inert
   *  rather than a button that fails. */
  onSetPalette?: (palette: string[] | null) => Promise<void>;
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
  /** Which colour the picker is open on, or null. One piece of state for both
   *  rows: only one picker can be open, so only one of them can be the subject
   *  of it. */
  const [picking, setPicking] = useState<{ kind: 'accent' } | { kind: 'palette'; index: number } | null>(null);
  /** Why a palette edit did not reach the instance. Shown rather than
   *  swallowed: this is the one control on the page that goes over the wire. */
  const [paletteError, setPaletteError] = useState('');
  /** The colour a palette drag has arrived at, held until the picker closes -
   *  a ref and not state, because nothing renders from it and re-rendering the
   *  whole page on every frame of a drag would be the point of holding it. */
  const draft = useRef<string | null>(null);

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
        <IconBadge icon={<Back color={c.textSub} />} onPress={onBack} accessibilityLabel={t('settings.back')} />
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
          <Text style={[styles.rowLabel, { color: rainbow.on ? c.textMuted : c.text }]}>{t('settings.accent')}</Text>
          {/* Dimmed and inert while the rainbow is on (jdp, 2026-09-01: "Wenn
              man den regenbogenmodus aktiviert soll man die akzentfarben nicht
              wählen können, mach ja kein sinn").

              Worth being straight about what this costs, because it is not
              nothing: the rainbow replaces the accent only for things that are
              one member of a SET - cards, rows, tabs. Anything that is the only
              one of its kind keeps the single accent, so the button at the
              bottom of Add, the floating action button and the speed curve are
              still painted with it while the mode is on. Locking the row means
              those keep whatever accent was last chosen until the mode goes off
              again. He asked for it plainly and it is one word to reverse. */}
          <View style={[styles.swatches, rainbow.on && styles.dimmed]} pointerEvents={rainbow.on ? 'none' : 'auto'}>
            {ACCENTS.map((a, i) => {
              // The slot in force wears the LIVE colour, so an accent mixed in
              // the picker is visible in the row rather than only in the app
              // around it. Same rule, same nearest-preset arithmetic as the
              // extension's accentSlot and the web's.
              const mine = i === accentSlot(accent);
              const shown = mine ? accent : a.hex;
              return (
                <Swatch
                  key={a.hex}
                  hex={shown}
                  label={mine && shown.toLowerCase() !== a.hex.toLowerCase() ? shown.toUpperCase() : a.name}
                  selected={mine}
                  // One press chooses; a second press on the one already chosen
                  // opens the picker on it. The pairing is what lets every
                  // colour be editable without a ninth control beside the eight
                  // - identical to the extension's own row.
                  onPress={() => (mine ? setPicking({ kind: 'accent' }) : setAccent(a.hex))}
                />
              );
            })}
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

        {/* The eight colours the mode hands out by position (jdp, 2026-09-01:
            "wo sind die farbfelder für den regenbogenmodus?"). They were on the
            web UI's Look page and in the extension's options and nowhere here,
            so the accent row above was the only colour control on the screen -
            which is most of why turning the rainbow on and then reaching for
            the accent looked like the thing to do.

            They belong to the INSTANCE, not to this phone, and editing one
            writes it back there (POST /api/appearance). That is deliberate:
            colours are handed out by POSITION, so a palette kept locally would
            make the same card teal in a browser and pink here. It is also why
            this row is the one place in this card that needs a connection.

            Same treatment as the accent row - label left, swatches right,
            dimmed while the mode is off, since eight colours that are not
            being used are worth showing and not worth pressing. */}
        <View style={styles.axisRow}>
          <Text style={[styles.rowLabel, { color: rainbow.on ? c.text : c.textMuted }]}>
            {t('settings.rainbowPalette')}
          </Text>
          <View
            style={[styles.swatches, !rainbow.on && styles.dimmed]}
            pointerEvents={rainbow.on && onSetPalette ? 'auto' : 'none'}
          >
            {rainbow.palette.map((hex, i) => (
              <Swatch
                key={i}
                hex={hex}
                // Every position is editable and none of them is "selected":
                // all eight are in force at once, so a press here can only
                // mean "change this one".
                selected={false}
                label={t('settings.rainbowPalettePosition', { position: i + 1 })}
                onPress={() => setPicking({ kind: 'palette', index: i })}
              />
            ))}
          </View>
        </View>
        {paletteError !== '' && (
          <Text style={[styles.hint, { color: c.statusFailSolid }]}>{paletteError}</Text>
        )}
      </NotchCard>

      <NotchCard title={t('settings.problems')} hue={2}>
        <Text style={[styles.hint, { color: c.textMuted }]}>{t('settings.problemsHint')}</Text>
        <View style={[styles.report, { backgroundColor: c.surface2, borderRadius: radii.control }]}>
          <Text style={[styles.reportText, { color: c.textSub }]} selectable>
            {report}
          </Text>
        </View>
        {/* Share only (jdp, 2026-08-31: "In der Probleme-card den Problem
            melden button weg. auch in der App"). The About card below carries
            both routes to reporting; a third door here, differently shaped and
            hard-wired to one of them, made this card a second answer to a
            question that card already answers. What this card is FOR is the
            report: take it, then use whichever route you prefer. */}
        <View style={styles.buttonRow}>
          <TouchableOpacity
            style={[styles.button, { backgroundColor: c.surface2, borderRadius: radii.control }]}
            onPress={() => Share.share({ message: report })}
          >
            <Text style={[styles.buttonText, { color: c.text }]}>{t('settings.problemsCopy')}</Text>
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
        {/* Both numbers are LINKS to their own release page (jdp, 2026-08-31:
            "Die Versionsnummer (auch von Glimstone) soll immer auf deren
            release auf github zeigen ... Das soll immmer und überall gelten").
            A version answers "which build is this"; the question straight after
            it is always "and what changed". Now GlimStone 1.6.0 for the family.

            Built from the version, never a hand-kept list of links: that list
            is wrong the first time somebody forgets it. */}
        <Text style={[styles.aboutVersions, { color: c.textMuted }]}>
          {`${t('settings.aboutVersion')} `}
          <Text
            style={{ color: accentInk }}
            onPress={() => Linking.openURL(`${REPO_URL}/releases/tag/mobile/v${Constants.expoConfig?.version ?? ''}`)}
          >
            {Constants.expoConfig?.version ?? '—'}
          </Text>
          {' · GlimStone '}
          <Text
            style={{ color: accentInk }}
            onPress={() => Linking.openURL(`${GLIMSTONE_URL}/releases/tag/v${GLIMSTONE_VERSION}`)}
          >
            {GLIMSTONE_VERSION}
          </Text>
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

      {/* One picker for both colour rows. Mounted here rather than inside
          either row: a Modal is a whole-screen thing, and hanging it off a row
          would make its lifetime depend on that row still being rendered. */}
      <ColorPicker
        visible={picking !== null}
        initial={picking?.kind === 'palette' ? (rainbow.palette[picking.index] ?? accent) : accent}
        onPick={(hex) => {
          if (!picking) return;
          if (picking.kind === 'accent') {
            // Local and immediate: the accent is this app's own choice, so
            // there is nothing to wait for and the whole page follows the drag.
            setAccent(hex);
            return;
          }
          // A palette position goes to the INSTANCE, so it is held rather than
          // sent on every frame of a drag - that would be one request per
          // pixel. The picker shows the colour live in its own preview; the
          // write happens once, when it closes.
          draft.current = hex;
        }}
        onClose={() => {
          const open = picking;
          const hex = draft.current;
          draft.current = null;
          setPicking(null);
          if (!open || open.kind !== 'palette' || !onSetPalette || !hex) return;
          const next = rainbow.palette.slice();
          next[open.index] = hex;
          setPaletteError('');
          void onSetPalette(next).catch((e: unknown) =>
            setPaletteError(e instanceof Error ? e.message : String(e)),
          );
        }}
      />
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
  // A row that is shown and not offered. Dimmed rather than hidden: eight
  // colours that are not in use are still the answer to "which eight", and a
  // control that disappears teaches nobody why.
  dimmed: { opacity: 0.4 },
  report: { padding: 12, marginBottom: 10 },
  reportText: { fontSize: TYPE.caption, lineHeight: 17, fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace' },
  buttonRow: { flexDirection: 'row', gap: 8 },
  button: { paddingVertical: 11, paddingHorizontal: 16, alignItems: 'center', flexShrink: 1 },
  buttonText: { fontSize: TYPE.dense, fontWeight: '500' },
});
