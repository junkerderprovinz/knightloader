import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Animated, Linking, Platform, ScrollView, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { useFocusEffect } from '@react-navigation/native';
import Constants from 'expo-constants';
import * as Clipboard from 'expo-clipboard';
import { useT } from '../i18n/I18nContext';
import { LANGUAGES, flagEmoji } from '../i18n/catalogue';
import { getLanguageOverride } from '../storage/languagePreference';
import { removeAllConnections } from '../storage/connections';
import { useAppearance } from '../theme/AppearanceContext';
import { useMotion, useShake } from '../theme/MotionContext';
import { MOTION_LEVELS, stormTap, type Motion } from '../theme/motion';
import { ACCENTS, SHAPES, accentSlot, type Shape } from '../theme/appearance';
import { TYPE } from '../theme/tokens';
import { GlimButton, GlimRow, GlimToggle, NotchCard, Swatch, SwatchReset, WellSelector } from '../components/glim';
import IconBadge, { Back, Coffee, Github, Mail, Paste } from '../components/IconBadge';
import ColorPicker from '../components/ColorPicker';

const GITHUB_URL = 'https://github.com/junkerderprovinz/knightloader';
const REPO_URL = GITHUB_URL;
const GLIMSTONE_URL = 'https://github.com/junkerderprovinz/glimstone';
const CONTACT_MAIL = 'hello@halleluja.design';
// From .github/FUNDING.yml, so there is one place that knows the handle.
const COFFEE_URL = 'https://buymeacoffee.com/junkerderprovinz';

/**
 * Which GlimStone this screen implements.
 *
 * A constant here, which it should not be: the design language ships
 * reference/react/version.ts for this, and a number kept beside the About card
 * has to be moved by hand with every lift. The number is also a link to that
 * release, so a stale one sends somebody to the wrong page. Copying that file
 * in beside the rest of the reference and importing from it is the fix.
 */
const GLIMSTONE_VERSION = '1.17.0';

/** shapeOf reads the shape back out of the radii the context resolved.
 *
 *  The context exposes radii rather than the name behind them, so a component
 *  asks how round a card is rather than which setting is on. This screen is the
 *  one place that needs the name, to mark the active segment, so it derives it
 *  here rather than widening the contract for every other caller. */
function shapeOf(radii: { card: number }): Shape {
  if (radii.card === 0) return 'square';
  return radii.card <= 8 ? 'soft' : 'round';
}

/**
 * The settings, drawn in the same language as the product they configure:
 * notch-titled cards instead of grey captions, well selectors instead of
 * bordered chips, a real switch for following the instance, and no drawn border
 * anywhere on the page.
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
   *  there is no connection to write to, and that absence removes the row
   *  rather than dimming it: a control with nothing behind it is furniture
   *  (GlimStone 1.16.0), and a capability the environment withholds owes the
   *  reader a sentence in its place (1.15.0). */
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
    accentCustoms,
    accentSlotChosen,
    chooseAccentSlot,
    setAccentCustom,
    clearAccentCustoms,
    setShape,
    setTheme,
    setRainbow,
    followInstance,
    snapshotAsLocal,
  } = useAppearance();
  const { chosen: motion, reduced: motionReduced, setMotion } = useMotion();
  const [override, setOverride] = useState<string | null>(null);
  const anyOverride = overridden.accent || overridden.shape || overridden.theme || overridden.rainbow;

  /**
   * The hidden fourth motion level (GlimStone 1.17.0; theme/motion.ts's
   * stormTap carries the rule).
   *
   * `stormFound` is a fact about this screen, so it is state and never storage:
   * leave the settings with something else selected and the segment is gone
   * until somebody makes the gesture again. The chosen value goes to
   * AsyncStorage like every other one, so a storm survives the app being closed
   * without putting a fourth entry in anybody's picker.
   *
   * It turns true whenever the level is in force, which measures "found"
   * rather than reading it. Opening the settings on a stored storm and then
   * picking the quiet level would otherwise make the segment vanish under the
   * finger mid-screen, and the rule asks for it to go once somebody has chosen
   * something else and left.
   *
   * An effect rather than a lazy useState initialiser, because the stored level
   * is read out of AsyncStorage asynchronously and at first render this value
   * is still the default.
   */
  const [stormFound, setStormFound] = useState(false);
  useEffect(() => {
    if (motion === 'storm') setStormFound(true);
  }, [motion]);
  // A ref rather than state: five taps are counting, not rendering, and any tap
  // that is not on the top level resets the count.
  const stormTaps = useRef({ taps: 0 });

  /**
   * What the picker offers: MOTION_LEVELS, the list without the hidden level.
   * `storm` joins it while it has just been found or while it is the value in
   * force, because a picker that hid the value it is showing would lie about
   * the interface.
   */
  const motionOptions = (stormFound || motion === 'storm' ? [...MOTION_LEVELS, 'storm' as Motion] : MOTION_LEVELS).map(
    (m) => ({ value: m, label: t(`settings.motion.${m}`) }),
  );
  /** Which colour the picker is open on, or null. One piece of state for both
   *  rows: only one picker can be open, so only one of them can be the subject
   *  of it. */
  // `hex` rides along so the picker opens on the colour of the swatch that was
  // pressed. Reading the accent instead shows another slot's colour as soon as
  // two of them differ, and the first drag then writes that colour into this
  // slot.
  const [picking, setPicking] = useState<
    { kind: 'accent'; slot: number; hex: string } | { kind: 'palette'; index: number } | null
  >(null);
  /** Why a palette edit did not reach the instance. Shown rather than
   *  swallowed: this is the one control on the page that goes over the wire. */
  const [paletteError, setPaletteError] = useState('');
  /** The colour a palette drag has arrived at, held until the picker closes. A
   *  ref rather than state, because nothing renders from it and re-rendering
   *  the whole page on every frame of a drag is what holding it avoids. */
  const draft = useRef<string | null>(null);
  /**
   * The palette row shakes when the instance refuses a write, because a failed
   * action is said by the control that was pressed. A grey sentence under the
   * row leaves a refused press and a press that worked looking identical for as
   * long as it takes to read.
   *
   * It sits on the palette's control group rather than on one swatch: editing a
   * position and resetting all eight are one write of one object to one
   * instance, and they fail together for the same reason.
   *
   * The routine lives in theme/MotionContext's useShake, since a shake spends
   * the motion table, and the numbers follow the chosen level: at the quietest
   * one there is no travel and the group dips in opacity instead.
   */
  const { style: paletteZitterStil, shake: paletteZittern } = useShake();
  /** True for a moment after the report reached the clipboard, so the button
   *  can say so in its own label.
   *
   *  A clipboard write is invisible, and this screen has no toast, snackbar or
   *  status line of its own; paletteError is the one message it owns and it
   *  belongs to the colour rows. Without the label swap the button looks dead
   *  on every press. */
  const [copied, setCopied] = useState(false);
  const copiedTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  // The timer has to die with the screen: its callback calls setCopied, and a
  // press followed straight away by a back tap would otherwise land that call
  // on a component that is gone.
  useEffect(
    () => () => {
      if (copiedTimer.current) clearTimeout(copiedTimer.current);
    },
    [],
  );

  // What a bug report needs and nothing more. No address and no token: an
  // address is somebody's home network and a token is a credential, and both
  // would be pasted into a public issue by anybody who trusted this button.
  const report = [
    `app:      ${Constants.expoConfig?.version ?? '?'} (versionCode ${Constants.expoConfig?.android?.versionCode ?? '?'})`,
    `platform: ${Platform.OS} ${Platform.Version}`,
    `language: ${lang}`,
    `look:     theme=${dark ? 'dark' : 'light'}${overridden.theme ? '' : ' (device)'} accent=${anyOverride ? 'local' : 'instance'} rainbow=${rainbow.on ? 'on' : 'off'}`,
  ].join(String.fromCharCode(10));

  // useFocusEffect rather than a mount-only effect: this screen stays mounted
  // underneath LanguagePickerScreen, so coming back from it with a changed
  // override needs a re-read on every return.
  useFocusEffect(
    useCallback(() => {
      getLanguageOverride().then(setOverride);
    }, [])
  );

  // Named after the language actually in effect, never after the act of
  // resolving one (GlimStone 1.4.0).
  const currentLabel = LANGUAGES.find((l) => l.code === (override ?? lang))?.label ?? (override ?? lang);
  const currentFlag = flagEmoji(LANGUAGES.find((l) => l.code === lang)?.flag ?? '');

  /**
   * The question does the warning, so the commit button carries no tone
   * (GlimStone 1.12.0, and 1.13.0 for the window itself). On iOS
   * `style: 'destructive'` paints it the red the language took off destructive
   * controls, and Android ignores `style` outright, so one line would draw two
   * different windows.
   *
   * `style: 'cancel'` stays on the other button. That is placement and keyboard
   * behaviour rather than colour: it tells the platform which button is the way
   * out, and the platform puts it where its own users look for it, which is why
   * 1.14.0's right-goes-ahead rule has nothing to decide here.
   */
  const confirmRemoveAll = () => {
    Alert.alert(t('settings.removeAllConfirmTitle'), t('settings.removeAllConfirmMessage'), [
      { text: t('settings.cancel'), style: 'cancel' },
      {
        text: t('settings.removeAllConfirmButton'),
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
        {/* A square badge, the one the overview's top bar uses, rather than a
            bare chevron: a single glyph makes the touchable about 12 by 22
            points against the 44 both platforms' guidelines call the minimum.
            hitSlop is not the fix, since it widens the target invisibly while
            the thing on screen stays a hairline. */}
        <IconBadge icon={<Back color={c.textSub} />} onPress={onBack} accessibilityLabel={t('settings.back')} />
        <Text style={[styles.title, { color: c.text }]}>{t('settings.title')}</Text>
      </View>

      {/* Each card owns a rainbow position, 0-based in page order, the same
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
        {/* A switch rather than a link that appears once something is
            overridden: following the instance is a state, and a state gets the
            control every state in this family gets. Off snapshots the current
            look as local so nothing jumps; on clears the local overrides and
            refetches, since adopting the instance's look while showing last
            week's colours is not adopting it. */}
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

        {/* Label left, swatches right, one row, the shape the web interface's
            Farben card takes and the shape every GlimRow on this page has. A
            caption on its own line above a left-aligned row reads as a heading
            over a group rather than as one setting with its answer beside it. */}
        <View style={styles.axisRow}>
          {/* Normal row text rather than the small axis caption: a label beside
              its control is a row label, and every other row label on this page
              is body text in the ordinary ink. */}
          <Text style={[styles.rowLabel, { color: rainbow.on ? c.textMuted : c.text }]}>{t('settings.accent')}</Text>
          {/* The row rainbow mode takes over, and the case GlimStone 1.16.0
              answers: does the control still do anything?

              The rainbow recolours only what is one member of a set, cards,
              rows and the segments of a selector, because those are the things
              that ask hueAt for a position. Anything that is the only one of
              its kind reads `accent` straight: DownloadsScreen's floating
              action button, SpeedGraph's curve, the Add screen and the QR
              scanner's frame all still paint the picked colour while the mode
              is on. The value is still doing work, so removing the row would
              hide a setting that is in effect.

              So it dims and stays pressable. Making it inert as well would take
              away the ability to change the colour of the controls it still
              paints without switching the whole mode off first. The dimming
              carries the signal and the sentence under the row carries the
              reason. pointerEvents is also worse here than on the web: on
              Android it takes the subtree out of TalkBack's reach along with
              the finger's.

              If the rainbow is ever made total, with every control taking its
              colour from a position, this value stops acting and the absence
              rule takes over.

              The dimming sits on the circles and never on the row: opacity
              composites the whole subtree in React Native as it does in CSS, so
              on the row the label would fade with the swatches. */}
          <View style={[styles.swatches, rainbow.on && styles.dimmed]}>
            {ACCENTS.map((a, i) => {
              // Each slot wears whatever it was last mixed to and keeps it. The
              // live accent drawn over its nearest preset would let the row
              // hold one mixed colour at a time. The remembered colour lives in
              // the override layer, so it survives the app being closed as well
              // as the next swatch being pressed.
              const shown = accentCustoms[String(i)] ?? a.hex;
              // Which slot is chosen is a stored fact rather than arithmetic on
              // the colour. Nearest preset works only while every swatch holds
              // a different colour: mix two of them to the same red and both
              // match, so two swatches light up and a press on either opens the
              // picker instead of choosing.
              //
              // The arithmetic stays as the fallback for a fresh install, where
              // nobody has chosen anything yet.
              const mine = accentSlotChosen !== undefined ? accentSlotChosen === i : i === accentSlot(accent);
              return (
                <Swatch
                  key={a.hex}
                  hex={shown}
                  label={shown.toLowerCase() !== a.hex.toLowerCase() ? shown.toUpperCase() : a.name}
                  selected={mine}
                  // One press chooses; a second press on the one already chosen
                  // opens the picker on it. The pairing is what lets every
                  // colour be editable without a ninth control beside the
                  // eight, as in the extension's own row.
                  onPress={() =>
                    mine ? setPicking({ kind: 'accent', slot: i, hex: shown }) : chooseAccentSlot(i, shown)
                  }
                />
              );
            })}
            {/* Always rendered rather than only once the accent has moved. A
                control that is sometimes there is one nobody learns the
                position of, and somebody checks whether a reset exists before
                deciding to experiment. Same rule and same place as the
                extension's own row. */}
            <SwatchReset onPress={clearAccentCustoms} label={t('settings.accentReset')} />
          </View>
        </View>

        {/* The other half of 1.16.0's answer: dim it and say who is in charge.
            A row that goes pale with no explanation reads as broken.

            A line rather than a bubble, because the app has no info-bubble
            component and the Probleme card explains itself the same way. The
            sentence is the web UI's own accentRainbowOwns in all forty-two
            languages, so the same state reads the same way in a browser.

            It sits outside the dimmed container, which is why the dim is on the
            circles: the one element that still has something to say must not
            fade with the ones that have gone quiet. */}
        {rainbow.on && (
          <Text style={[styles.hint, styles.afterControls, { color: c.textMuted }]}>{t('settings.accentRainbowOwns')}</Text>
        )}

        {/* A switch rather than a read-only line. Only on and off are local:
            the palette and the seed come from the instance either way, so two
            clients never disagree about which colour a position is. hue={1}
            puts this switch second in this card's set, after "follow the
            instance".

            No caption of its own, because the switch at the top of this card
            already says whether the look is following the instance or set here,
            and flipping this one flips that one. */}
        <GlimRow
          label={t('settings.rainbow')}
          control={<GlimToggle hue={1} value={rainbow.on} onChange={(on) => setRainbow(on)} />}
        />

        {/* The mode is on and there is no instance to write a palette to.
            GlimStone 1.16.0's test: a grey state says something about the thing
            the control touches (dim it), about a decision taken elsewhere on
            the page (leave it out), or about the environment not permitting the
            thing at all, and only that third case is left out and owes prose.

            This is the third. The palette lives on the instance and there is
            none, so nothing a finger does here reaches anything. The eight
            colours in force are still worth knowing, which is what the sentence
            says instead of a row that lies about being editable.

            The mode's own switch stays: a mode whose switch disappears when it
            cannot be configured is a mode nobody can turn back on. */}
        {rainbow.on && !onSetPalette && (
          <Text style={[styles.hint, styles.afterControls, { color: c.textMuted }]}>{t('settings.rainbowPaletteNoInstance')}</Text>
        )}

        {/* The eight colours the mode hands out by position, shown only where a
            press can land. They belong to the instance rather than to this
            phone, and editing one writes it back (POST /api/appearance),
            because colours are handed out by position and a palette kept
            locally would make the same card teal in a browser and pink here.

            Absent while the mode is off rather than dimmed (GlimStone 1.10.0):
            a palette editor under a rainbow that is not running is eight
            swatches nobody can open beside a reset nobody can press, with the
            reason one row up, where nobody looks.

            The accent row above does not go with it. That one stays pressable
            while the rainbow is on, because it is still in force on everything
            that owns no position. */}
        {rainbow.on && onSetPalette && (
          <View style={styles.axisRow}>
            <Text style={[styles.rowLabel, { color: c.text }]}>{t('settings.rainbowPalette')}</Text>
            {/* Nothing here is dimmed or inert: the row exists only in the
                state where every swatch on it works. The container carries the
                refusal shake, which belongs to the group rather than to one
                swatch, since editing a position and resetting all eight are one
                write of one object to one instance. */}
            <Animated.View style={[styles.swatches, paletteZitterStil]}>
              {rainbow.palette.map((hex, i) => (
                <Swatch
                  key={i}
                  hex={hex}
                  // Every position is editable and none is selected: all eight
                  // are in force at once, so a press can only mean "change this
                  // one".
                  selected={false}
                  label={t('settings.rainbowPalettePosition', { position: i + 1 })}
                  onPress={() => setPicking({ kind: 'palette', index: i })}
                />
              ))}
              {/* Back to the eight the language ships with. `null` is the reset
                  the instance understands: it clears the stored list rather
                  than writing the defaults as if somebody had chosen them. */}
              <SwatchReset
                onPress={() => {
                  setPaletteError('');
                  if (onSetPalette) {
                    void onSetPalette(null).catch((e: unknown) => {
                      setPaletteError(e instanceof Error ? e.message : String(e));
                      paletteZittern();
                    });
                  }
                }}
                label={t('settings.accentReset')}
              />
            </Animated.View>
          </View>
        )}
        {/* Goes with the row it belongs to: a failure message left standing
            under a row that is no longer there explains a control nobody can
            see. */}
        {rainbow.on && onSetPalette && paletteError !== '' && (
          <Text style={[styles.hint, { color: c.statusFailSolid }]}>{paletteError}</Text>
        )}
      </NotchCard>

      {/* Motion gets a card of its own rather than a fourth axis inside
          Appearance. GlimStone 1.15.0's test for that split is whether somebody
          can want one and not the other, and somebody who wants a quieter
          interface has said nothing about colour. The web UI's Look page is
          shaped the same way.

          hue={5} rather than 2, although the card sits second in reading order:
          the four cards below carry fixed positions in this page's 0-based
          sequence, and renumbering them to place one card would re-colour four. */}
      <NotchCard title={t('settings.motion')} hue={5}>
        {/* What the three levels do, above the control rather than below it,
            where the Probleme card puts its own sentence, so the extra step
            above a sentence that follows controls does not apply. A paragraph
            rather than a bubble, since this app has no bubble component.

            The sentence names three levels while the picker sometimes shows
            four: a hidden level gets no entry in the text that explains the
            visible ones. */}
        <Text style={[styles.hint, { color: c.textMuted }]}>{t('settings.motionHint')}</Text>
        <WellSelector
          options={motionOptions}
          value={motion}
          onPick={(v) => {
            // The gesture first, because it is a press on the segment that is
            // already active, the one press a picker would otherwise swallow as
            // a no-op. The count lives in a ref: five taps are counting rather
            // than rendering.
            const gefunden = stormTap(stormTaps.current, v, motion);
            setMotion(gefunden ?? v);
          }}
        />

        {/* The one thing the phone can say here that a browser cannot. Without
            it, somebody whose system is set to reduce motion picks the
            liveliest level, sees nothing change and reports a bug, which is the
            failure "say who is in charge" exists to prevent, arriving from the
            operating system instead of from another row.

            The picker is neither dimmed nor removed while this is true. It is
            not the environment-refusal case: the value is stored, it is real,
            and it takes effect again the moment the system setting changes, so
            a control that vanished here would hide a preference that is still
            somebody's. */}
        {motionReduced && (
          <Text style={[styles.hint, styles.afterControls, { color: c.textMuted }]}>{t('settings.motionReduced')}</Text>
        )}
      </NotchCard>

      <NotchCard title={t('settings.problems')} hue={2}>
        <Text style={[styles.hint, { color: c.textMuted }]}>{t('settings.problemsHint')}</Text>
        <View style={[styles.report, { backgroundColor: c.surface2, borderRadius: radii.control }]}>
          <Text style={[styles.reportText, { color: c.textSub }]} selectable>
            {report}
          </Text>
        </View>
        {/* One button, and it copies. Share.share opens the system share sheet
            and asks the person to pick a target app for a block of plain text
            they are about to paste into a GitHub issue or a mail anyway; the
            clipboard is that target. The extension has copied since it shipped,
            so the three surfaces agree on the verb, the label and the glyph.

            The About card below carries both routes to reporting, so a third
            door here hard-wired to one of them would be a second answer to a
            question that card answers. What this card is for is the report.

            Paste, the clipboard glyph, rather than a pair of offset sheets:
            this family already draws the clipboard for the relay screen's paste
            button, and a near-identical mark for the opposite direction would
            be two glyphs for one idea. */}
        <View style={styles.buttonRow}>
          <GlimButton
            hue={0}
            label={copied ? t('settings.problemsCopied') : t('settings.problemsCopy')}
            icon={(ink) => <Paste color={ink} />}
            onPress={() => {
              // Confirm only once the write has landed, so the label never
              // claims a copy that did not happen.
              void Clipboard.setStringAsync(report)
                .then(() => {
                  setCopied(true);
                  if (copiedTimer.current) clearTimeout(copiedTimer.current);
                  copiedTimer.current = setTimeout(() => setCopied(false), 2000);
                })
                // Swallowed, and the label stays as it was: the report sits
                // selectable in the box above this button, so a failed
                // clipboard write leaves the person where an error message
                // would have sent them. Left unhandled it would be a red
                // unhandled-rejection warning over the screen.
                .catch(() => undefined);
            }}
          />
        </View>
      </NotchCard>

      {/* The About card carries the versions and the ways to report something.
          It is the one card in the family whose body is prose rather than an
          info bubble, because it has no control to explain and the sentence is
          the content; GlimStone 1.7.0 names it as an exception. */}
      <NotchCard title={t('settings.about')} hue={3}>
        <Text style={[styles.aboutText, { color: c.textSub }]}>{t('settings.aboutBody')}</Text>
        {/* Three sentences, each with the thing it asks for under it: what this
            is, then the coffee, then the way to report something. A sentence
            with its own button beneath it reads as one offer, while three
            sentences over one row of buttons read as a form. */}
        <Text style={[styles.aboutText, { color: c.textSub }]}>{t('settings.aboutCoffee')}</Text>
        <View style={styles.buttonRow}>
          <GlimButton
            hue={1}
            label={t('settings.aboutCoffeeButton')}
            icon={(ink) => <Coffee color={ink} />}
            onPress={() => Linking.openURL(COFFEE_URL)}
          />
        </View>
        {/* A blank line above this sentence, because it follows controls.
            Without it the coffee button sits as close to this line as to the
            one it belongs to, so the eye pairs it with the wrong text. The step
            goes over the sentence and never under the button row, or a card
            whose last row is a control ends in a gap that reads as a missing
            row. The first sentence of the card gets none. */}
        <Text style={[styles.aboutText, styles.afterControls, { color: c.textSub }]}>
          {t('settings.aboutReport')}
        </Text>
        <View style={styles.buttonRow}>
          <GlimButton
            hue={2}
            grow
            label={t('settings.aboutGithub')}
            icon={(ink) => <Github color={ink} />}
            onPress={() => Linking.openURL(GITHUB_URL)}
          />
          <GlimButton
            hue={3}
            grow
            label={t('settings.aboutMail')}
            icon={(ink) => <Mail color={ink} />}
            // A plain mailto with the subject prefilled, so a mail arrives
            // saying which product it is about. No body, which would read as a
            // form to fill in rather than a message somebody writes.
            onPress={() =>
              Linking.openURL(
                `mailto:${CONTACT_MAIL}?subject=${encodeURIComponent(`KnightLoader ${t('settings.aboutMailSubject')}`)}`,
              )
            }
          />
        </View>
        {/* Last line in the card, under the buttons, as in the web interface
            and the extension. Each sentence above has its own button under it,
            and a build number between the body and the coffee line would cut
            that pairing in half.

            Both numbers link to their own release page, because the question
            straight after "which build is this" is "and what changed". The
            links are built from the version rather than from a hand-kept list,
            which is wrong the first time somebody forgets it.

            It follows controls, so it takes the same blank line the report
            sentence above does. */}
        <Text style={[styles.aboutVersions, styles.afterControls, { color: c.textMuted }]}>
          {`${t('settings.aboutVersion')} `}
          <Text
            style={{ color: accentInk }}
            onPress={() => Linking.openURL(`${REPO_URL}/releases/tag/mobile/v${Constants.expoConfig?.version ?? ''}`)}
          >
            {Constants.expoConfig?.version ?? '-'}
          </Text>
          {' · GlimStone '}
          <Text
            style={{ color: accentInk }}
            onPress={() => Linking.openURL(`${GLIMSTONE_URL}/releases/tag/v${GLIMSTONE_VERSION}`)}
          >
            {GLIMSTONE_VERSION}
          </Text>
        </Text>
      </NotchCard>

      <NotchCard title={t('settings.dangerZone')} hue={4}>
        {/* Quiet, in the ink every other quiet control on the page takes.
            GlimStone 1.12.0 keeps the status colour off a destructive control:
            what warns is the question below, which names what is about to be
            gone, and a button that is red before the question is asked says it
            twice and weaker each time. The card's notch still carries its
            rainbow position, so the heading above is coloured like every
            other. */}
        <GlimButton tone="quiet" label={t('settings.removeAllConnections')} onPress={confirmRemoveAll} />
      </NotchCard>

      {/* No version footer: it would say what the About card above says, in
          smaller type outside every card, and page chrome reads as something
          nobody put there. GlimStone 1.7.0 replaces its version-footer rule
          with the About card for the whole family. */}

      {/* One picker for both colour rows. Mounted here rather than inside
          either row: a Modal is a whole-screen thing, and hanging it off a row
          would make its lifetime depend on that row still being rendered. */}
      <ColorPicker
        visible={picking !== null}
        /* The colour of the swatch that was pressed, never the accent. Opening
           on the accent shows another slot's colour as soon as two of them
           differ, and the first drag then writes that colour into this slot. */
        initial={picking?.kind === 'palette' ? (rainbow.palette[picking.index] ?? accent) : (picking?.hex ?? accent)}
        onPick={(hex) => {
          if (!picking) return;
          if (picking.kind === 'accent') {
            // Local and immediate: the accent is this app's own choice, so
            // there is nothing to wait for and the whole page follows the drag.
            // Written against the slot it was opened on, so the mixed colour is
            // still there after another swatch has been chosen.
            setAccentCustom(picking.slot, hex);
            return;
          }
          // A palette position goes to the instance, so it is held rather than
          // sent on every frame of a drag, which would be one request per
          // pixel. The picker shows the colour live in its own preview and the
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
          void onSetPalette(next).catch((e: unknown) => {
            setPaletteError(e instanceof Error ? e.message : String(e));
            paletteZittern();
          });
        }}
      />
    </ScrollView>
  );
}

// Colours and radii are applied inline from the resolved tokens rather than
// baked in here: a stylesheet is built once and cannot follow a theme change.
//
// One column stretched across a tablet is a card 900 points wide with its text
// at one edge and its badge at the other. A cap plus centring costs a phone
// nothing, since 640 is wider than every phone, and makes a tablet readable.
const capped = { width: '100%' as const, maxWidth: 640, alignSelf: 'center' as const };

const styles = StyleSheet.create({
  container: { ...capped, paddingHorizontal: 16, paddingBottom: 32 },
  topBar: { paddingTop: 56, paddingBottom: 4, flexDirection: 'row', alignItems: 'center', gap: 12 },
  title: { fontSize: TYPE.heading, fontWeight: '600' },
  // Half-muted and centred: something to look for rather than something that
  // competes for attention.
  aboutText: { fontSize: TYPE.body, lineHeight: 20, marginBottom: 8 },
  /* The blank line over a sentence that follows controls.
   *
   * 10 is the step the button row above already carries under itself, so the
   * gap between a control row and the next sentence doubles while the gap
   * between a sentence and its own controls stays. Two to one is what makes
   * the pair read as a pair; the extension's About card reaches the same ratio
   * with 20 against 8.
   *
   * The card's own padding is still the space under the last line, which is why
   * nothing here adds a bottom margin. */
  afterControls: { marginTop: 10 },
  // Tabular numerals, which the About card's own rule asks for by name: two
  // version numbers on one line, each of them a number that changes with every
  // release, so proportional digits make the middle dot wander between builds.
  aboutVersions: { fontSize: TYPE.caption, fontVariant: ['tabular-nums'] },
  valueGroup: { flexDirection: 'row', alignItems: 'center', gap: 8, flexShrink: 1 },
  flag: { fontSize: 17 },
  value: { fontSize: TYPE.body },
  hint: { fontSize: TYPE.caption, lineHeight: 16, marginBottom: 8 },
  axisLabel: { fontSize: TYPE.caption, marginTop: 12, marginBottom: 6, letterSpacing: 0.6 },
  // The inline variant drops the stacked spacing: in a row the label is beside
  // its control, not above it.
  axisRow: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 12, marginTop: 12, marginBottom: 2 },
  axisLabelInline: { marginTop: 0, marginBottom: 0, flexShrink: 0 },
  // The same size GlimRow's own label takes, off the scale in theme/tokens.ts.
  // Both were 15, a rung between Dense and Body the table does not have.
  rowLabel: { fontSize: TYPE.body, flexShrink: 0 },
  // One line, always. No wrapping: the children divide what the row has (see
  // Swatch's own note), so nine of them fit whatever the phone is. `flex: 1`
  // here rather than flexShrink, because the row has to CLAIM the space left
  // over by the label instead of only agreeing to give some back.
  /* 2, not 4 (jdp, 2026-09-01: "die farbfelder können näher zusammen"). Nine
     circles and eight gaps share whatever the label leaves of one line, so the
     gap is the only number that decides whether they read as one row of colours
     or as nine separate controls. Halving it also hands each circle two more
     points of its own, which is where the size actually went. */
  swatches: { flexDirection: 'row', gap: 2, alignItems: 'center', justifyContent: 'flex-end', flex: 1 },
  // A row that is shown and not offered, for the one reason that still earns
  // it: the control is REPORTING something about its own subject - the accent
  // is not in force under the rainbow, the palette has no instance to be
  // written to - rather than refusing because of a switch one row up. That
  // second case is absent now and not dimmed, so this style no longer has the
  // job it was written for. It stands on a CONTROL GROUP and never on a row
  // that also carries a label, because opacity composites the subtree.
  dimmed: { opacity: 0.4 },
  report: { padding: 12, marginBottom: 10 },
  reportText: { fontSize: TYPE.caption, lineHeight: 17, fontFamily: Platform.OS === 'ios' ? 'Menlo' : 'monospace' },
  buttonRow: { flexDirection: 'row', gap: 8, marginBottom: 10 },
  // A row, so the glyph and the label sit together rather than stacking.
  button: { flexDirection: 'row', gap: 8, paddingVertical: 11, paddingHorizontal: 16, alignItems: 'center', justifyContent: 'center', flexShrink: 1 },
});
