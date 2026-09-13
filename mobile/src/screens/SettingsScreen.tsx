import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Animated, Easing, Linking, Platform, ScrollView, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { useFocusEffect } from '@react-navigation/native';
import Constants from 'expo-constants';
import * as Clipboard from 'expo-clipboard';
import { useT } from '../i18n/I18nContext';
import { LANGUAGES, flagEmoji } from '../i18n/catalogue';
import { getLanguageOverride } from '../storage/languagePreference';
import { removeAllConnections } from '../storage/connections';
import { useAppearance } from '../theme/AppearanceContext';
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
 * IT IS STILL A CONSTANT HERE AND IT SHOULD NOT BE. The design language ships
 * reference/react/version.ts for exactly this, and its own comment describes
 * this file's previous state word for word: an app that keeps the number next
 * to its About card has re-created the problem, because the number and the
 * files it describes are then two places that have to be changed together by
 * hand. They were not: this said 1.6.0 while the language stood at 1.14.0,
 * eight releases back, and since the number on screen is a LINK to that
 * release, a stale one does not merely read wrong, it sends somebody to the
 * wrong page. Copying that file in beside the rest of the reference, and
 * importing from it, is the fix; until then this at least agrees with the web
 * UI's Help.tsx and the extension's options.js, which both read 1.14.0, and
 * with the latest published release (v1.14.0 - read off the release list, not
 * off a changelog, because an unreleased tag has nothing behind the link).
 */
const GLIMSTONE_VERSION = '1.14.0';

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
  const [override, setOverride] = useState<string | null>(null);
  const anyOverride = overridden.accent || overridden.shape || overridden.theme || overridden.rainbow;
  /** Which colour the picker is open on, or null. One piece of state for both
   *  rows: only one picker can be open, so only one of them can be the subject
   *  of it. */
  // `hex` rides along so the picker opens on the colour of the swatch that was
  // pressed. Reading the accent instead showed a DIFFERENT slot's colour as soon
  // as two of them differed, and the first drag then wrote that foreign colour
  // into this slot.
  const [picking, setPicking] = useState<
    { kind: 'accent'; slot: number; hex: string } | { kind: 'palette'; index: number } | null
  >(null);
  /** Why a palette edit did not reach the instance. Shown rather than
   *  swallowed: this is the one control on the page that goes over the wire. */
  const [paletteError, setPaletteError] = useState('');
  /** The colour a palette drag has arrived at, held until the picker closes -
   *  a ref and not state, because nothing renders from it and re-rendering the
   *  whole page on every frame of a drag would be the point of holding it. */
  const draft = useRef<string | null>(null);
  /**
   * The palette row shakes when the instance refuses a write.
   *
   * The standing rule for a failed action is that the control which was
   * pressed says so with a movement, and this page had no movement at all: a
   * palette edit that never reached the instance left a grey sentence under
   * the row and nothing else, so the press and a press that worked looked
   * identical for as long as it took to read. The relay screen has had the
   * gesture since it was asked for; the settings screen simply never got it.
   *
   * The geometry is the language's own, the same numbers the relay screen now
   * carries and the same ones the web UI and the extension animate: five
   * segments of 72ms, so 360ms, decaying +-4, -+4, +-2, -+2.
   *
   * It sits on the palette's control GROUP rather than on one swatch. Both
   * things that can fail here - editing a position, and resetting all eight -
   * are one write of one object to one instance, and they fail together for
   * the same reason; a nonce per control is for two buttons that do two
   * different things through one handler.
   *
   * THIS ROUTINE IS DUPLICATED from RelayConnectScreen and should not stay
   * that way: it belongs beside the other shared controls in components/glim.tsx,
   * so a third screen that needs it finds it instead of typing new numbers.
   */
  const paletteWackeln = useRef(new Animated.Value(0)).current;
  const paletteZittern = useCallback(() => {
    paletteWackeln.setValue(0);
    Animated.sequence(
      [1, -1, 0.5, -0.5, 0].map((zu) =>
        Animated.timing(paletteWackeln, {
          toValue: zu,
          duration: 72,
          easing: Easing.inOut(Easing.ease),
          useNativeDriver: true,
        }),
      ),
    ).start();
  }, [paletteWackeln]);
  /** True for a moment after the report reached the clipboard, so the button
   *  can say so in its own label.
   *
   *  A clipboard write is invisible, and this screen has no toast, no
   *  snackbar and no status line of its own (paletteError is the one message
   *  it owns, and it belongs to the colour rows). Without the label swap the
   *  button would look dead on every press, which is exactly how the old
   *  share sheet did NOT look: that one opened a whole system panel, so it
   *  carried its own confirmation. Copying has to bring its own. */
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

  /**
   * The question that does the warning, now that the button no longer tries to.
   *
   * `style: 'destructive'` is gone from the commit button, and it is the same
   * removal as the one on the button that opens this (GlimStone 1.12.0, and
   * 1.13.0 for the window itself: the confirmation stopped taking a tone at
   * all, because a lever that decides nothing still reads as a lever). On iOS
   * that flag paints the button red - exactly the colour the language took off
   * destructive controls - and on Android React Native ignores `style`
   * outright, so the one line was also drawing two different windows on the two
   * platforms.
   *
   * `style: 'cancel'` stays on the other one. That is placement and keyboard
   * behaviour, not colour: it tells the platform which button is the way out,
   * and the platform then puts it where its own users look for it. Which is
   * also why 1.14.0's right-goes-ahead rule does not reach in here - the order
   * of these two is the operating system's to decide, and a card that fought it
   * would be the odd window on the phone rather than the consistent one.
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
              again. He asked for it plainly and it is one word to reverse.

              Dimmed and NOT removed, unlike the palette row further down, and
              the two look alike enough to be worth separating: this row is
              reporting rather than refusing. It says the single accent is not
              in force right now, which is information about the accent itself,
              and the swatches still show which colour the app goes back to the
              moment the mode is switched off. The palette row's own reason for
              being dead is a decision one row above it, which is the case the
              absence rule exists for.

              The value sits on this inner container and not on the row, which
              is a rule and not a layout accident: opacity composites the whole
              subtree in React Native exactly as it does in CSS, so a child can
              never be less transparent than its parent. Put it on the row and
              an info bubble beside the label fades with the swatches - and the
              bubble is the one thing on a dimmed row that still has something
              to say. */}
          <View style={[styles.swatches, rainbow.on && styles.dimmed]} pointerEvents={rainbow.on ? 'none' : 'auto'}>
            {ACCENTS.map((a, i) => {
              // Each slot wears whatever it was last mixed to, and keeps it.
              //
              // It used to wear the live accent drawn over its nearest preset,
              // which meant the row could hold exactly one mixed colour: choose
              // any other swatch and the mixed one was simply gone (jdp,
              // 2026-09-01: "wenn man ein Farbfeld bearbeitet setzt es die farbe
              // wieder zurück, sobald man ein anderes farbfeld auswählt"). The
              // remembered colour lives in the override layer, so it survives
              // the app being closed as well as the next swatch being pressed.
              const shown = accentCustoms[String(i)] ?? a.hex;
              // WHICH slot is chosen is a STORED fact, not arithmetic on the
              // colour (jdp, 2026-09-02: "wenn ich zb. alle farbfelder rot
              // machen will geht das nicht. nicht alle farbfelder speichern dann
              // die farbe").
              //
              // Deriving the choice by nearest preset works only while every
              // swatch holds a different colour. Mix two of them to the same red
              // and both match: two swatches light up at once, and a press on
              // either opens the picker instead of choosing, so the row stops
              // behaving like a row. A choice is not recoverable from a value
              // once two values are equal.
              //
              // The arithmetic stays as the fallback for a fresh install, where
              // nobody has chosen anything yet and the nearest preset is the
              // right answer.
              const mine = accentSlotChosen !== undefined ? accentSlotChosen === i : i === accentSlot(accent);
              return (
                <Swatch
                  key={a.hex}
                  hex={shown}
                  label={shown.toLowerCase() !== a.hex.toLowerCase() ? shown.toUpperCase() : a.name}
                  selected={mine}
                  // One press chooses; a second press on the one already chosen
                  // opens the picker on it. The pairing is what lets every
                  // colour be editable without a ninth control beside the eight
                  // - identical to the extension's own row.
                  onPress={() =>
                    mine ? setPicking({ kind: 'accent', slot: i, hex: shown }) : chooseAccentSlot(i, shown)
                  }
                />
              );
            })}
            {/* Always rendered, never only once the accent has moved. A control
                that is sometimes there is a control nobody learns the position
                of, and the moment somebody goes looking for it is exactly the
                moment it is missing - they check whether a reset exists BEFORE
                deciding to experiment. Same rule, same place, as the extension's
                own row. */}
            <SwatchReset onPress={clearAccentCustoms} label={t('settings.accentReset')} />
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

            ABSENT while the mode is off, not dimmed, and this row is the case
            GlimStone 1.10.0 names by hand: a palette editor under a rainbow
            that is not running is eight swatches nobody can open beside a reset
            nobody can press. It used to be dimmed on the reasoning that eight
            colours which are not in use are still the answer to "which eight" -
            which is true and is not worth what it costs. The reason the row is
            dead sits one row up, and that is exactly where nobody looks once
            they have decided this row is the interesting one; reaching a
            control and getting nothing teaches less than its absence does.

            The MODE's own switch stays where it is, which is the other half of
            the same rule and is not a detail: a mode whose switch disappears
            when it is off is a mode nobody can turn back on. What goes is only
            the thing that has meaning underneath it.

            Deliberately NOT what goes with it: the accent row above, which is
            dimmed while the rainbow is ON. That one is REPORTING rather than
            refusing - it says the single accent is not in force at the moment,
            which is information about the accent itself - and it stays. */}
        {rainbow.on && (
          <View style={styles.axisRow}>
            <Text style={[styles.rowLabel, { color: c.text }]}>{t('settings.rainbowPalette')}</Text>
            {/* The one state left that is shown and not offered: the mode is on
                but there is no instance to write a palette to. That is not the
                sub-switch case - it reports something about the palette itself,
                which lives on the instance and has none here, rather than a
                decision made elsewhere on this page - so it is dimmed and inert
                rather than absent, and the swatches still say which eight
                colours the positions are wearing.

                The dimming sits on THIS container and never on the row: opacity
                composites a whole subtree in React Native exactly as it does in
                CSS, so a label with an info bubble beside it would fade with
                the swatches if the row carried the value. Nothing is faded here
                that is not actually inert. */}
            <Animated.View
              style={[
                styles.swatches,
                !onSetPalette && styles.dimmed,
                {
                  transform: [
                    { translateX: paletteWackeln.interpolate({ inputRange: [-1, 1], outputRange: [-4, 4] }) },
                  ],
                },
              ]}
              pointerEvents={onSetPalette ? 'auto' : 'none'}
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
              {/* Back to the eight the language ships with. `null` is the reset
                  the instance understands - it clears the stored list rather
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
        {/* Goes with the row it belongs to. "What else hangs off the mode goes
            with it" is part of the same rule, and a failure message left
            standing under a row that is no longer there is the clearest case of
            it: the sentence would be explaining a control nobody can see. */}
        {rainbow.on && paletteError !== '' && (
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
        {/* One button, and it COPIES (jdp, 2026-09-02: "Der Bericht teilen
            button soll bericht kopieren button heißen (mit glyph), in allen
            instanzen"). It used to hand the text to Share.share, which opens
            the system share sheet and then asks the person to pick a target
            app for a block of plain text they are about to paste into a GitHub
            issue or a mail anyway. The clipboard is that target, so the sheet
            was a step between the report and the place it was going. The
            extension has copied since it shipped; this makes the three
            surfaces agree on the verb, the label and the glyph.

            The About card below carries both routes to reporting; a third
            door here, differently shaped and hard-wired to one of them, made
            this card a second answer to a question that card already answers
            (jdp, 2026-08-31: "In der Probleme-card den Problem melden button
            weg. auch in der App"). What this card is FOR is the report: take
            it, then use whichever route you prefer.

            Paste, the clipboard glyph, and not a pair of offset sheets: this
            family already draws the clipboard for the relay screen's paste
            button, and a second, near-identical mark for the opposite
            direction would be two glyphs where the person only ever needs to
            recognise one idea, "this is about the clipboard". */}
        <View style={styles.buttonRow}>
          <GlimButton
            hue={0}
            label={copied ? t('settings.problemsCopied') : t('settings.problemsCopy')}
            icon={(ink) => <Paste color={ink} />}
            onPress={() => {
              // Confirm only once the write has actually landed, so the label
              // never claims a copy that did not happen.
              void Clipboard.setStringAsync(report)
                .then(() => {
                  setCopied(true);
                  if (copiedTimer.current) clearTimeout(copiedTimer.current);
                  copiedTimer.current = setTimeout(() => setCopied(false), 2000);
                })
                // Swallowed, and the label simply stays as it was: the report
                // sits selectable in the box right above this button, so a
                // failed clipboard write leaves the person exactly where an
                // error message would have sent them anyway. Left unhandled
                // this would be a red unhandled-rejection warning over the
                // screen, which is a worse answer than a button that did
                // nothing visible.
                .catch(() => undefined);
            }}
          />
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
        {/* Three sentences, each with the thing it asks for right under it
            (jdp, 2026-09-01). The order is his: what this is, then the coffee,
            then the way to report something. A sentence with its own button
            beneath it reads as one offer; three sentences over one row of
            buttons reads as a form. */}
        <Text style={[styles.aboutText, { color: c.textSub }]}>{t('settings.aboutCoffee')}</Text>
        <View style={styles.buttonRow}>
          <GlimButton
            hue={1}
            label={t('settings.aboutCoffeeButton')}
            icon={(ink) => <Coffee color={ink} />}
            onPress={() => Linking.openURL(COFFEE_URL)}
          />
        </View>
        {/* A blank line above this sentence, because it FOLLOWS controls.
            Without it the coffee button sat as close to this line as to the one
            it belongs to, so the eye pairs it with the wrong text and the card
            reads as one column rather than as two offers. The step goes over the
            SENTENCE and never under the button row: a card whose last row is a
            control would otherwise end in a gap, which reads as a missing row.
            The first sentence of the card gets none - there is nothing above it
            to be separated from. */}
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
            // A plain mailto, subject prefilled so a mail arrives already saying
            // which product it is about. No body: a prefilled body reads as a
            // form to fill in, and this is meant to be a message somebody
            // writes.
            onPress={() =>
              Linking.openURL(
                `mailto:${CONTACT_MAIL}?subject=${encodeURIComponent(`KnightLoader ${t('settings.aboutMailSubject')}`)}`,
              )
            }
          />
        </View>
        {/* Last line in the card, under the buttons (jdp, 2026-09-05), same as
            the web interface and the extension. It reads as a footer, which is
            what it is: each sentence above has its own button under it, and a
            build number sat between the body and the coffee line cut that
            pairing in half.

            Both numbers are LINKS to their own release page (jdp, 2026-08-31:
            "Die Versionsnummer (auch von Glimstone) soll immer auf deren
            release auf github zeigen ... Das soll immmer und überall gelten").
            A version answers "which build is this"; the question straight after
            it is always "and what changed". Which GlimStone that is stands in
            GLIMSTONE_VERSION at the top of this file and nowhere else - a
            number repeated in a comment is a number that goes stale on the day
            the constant moves.

            Built from the version, never a hand-kept list of links: that list
            is wrong the first time somebody forgets it.

            It follows controls, so it takes the same blank line the report
            sentence above does. A footer is still a line of text after a button
            row, and the pairing it would otherwise break is the mail button
            with the sentence that asked for it. */}
        <Text style={[styles.aboutVersions, styles.afterControls, { color: c.textMuted }]}>
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
      </NotchCard>

      <NotchCard title={t('settings.dangerZone')} hue={4}>
        {/* Quiet, and the same ink every other quiet control on the page takes.
            It carried the fail colour, on the reasoning that a surface with red
            INK (rather than a red outline, which this language has no line to
            draw) is how a destructive control says what it is. GlimStone 1.12.0
            settled that the other way: what warns is the QUESTION below, which
            names what is about to be gone, and a button that is red before the
            question is asked is saying it twice and weaker each time.

            The card's own notch still carries its rainbow position, so this row
            is not colourless - the heading above it is coloured like every other
            heading. What is gone is the status colour on the CONTROL. */}
        <GlimButton tone="quiet" label={t('settings.removeAllConnections')} onPress={confirmRemoveAll} />
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
        /* The colour of the swatch that was pressed, never the accent. Opening
           on the accent showed the colour of a DIFFERENT slot the moment two of
           them differed, and the first drag then wrote that foreign colour into
           this slot. */
        initial={picking?.kind === 'palette' ? (rainbow.palette[picking.index] ?? accent) : (picking?.hex ?? accent)}
        onPick={(hex) => {
          if (!picking) return;
          if (picking.kind === 'accent') {
            // Local and immediate: the accent is this app's own choice, so
            // there is nothing to wait for and the whole page follows the drag.
            // Written against the SLOT it was opened on, so the mixed colour is
            // still there after another swatch has been chosen.
            setAccentCustom(picking.slot, hex);
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
          void onSetPalette(next).catch((e: unknown) => {
            setPaletteError(e instanceof Error ? e.message : String(e));
            paletteZittern();
          });
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
  aboutText: { fontSize: TYPE.body, lineHeight: 20, marginBottom: 8 },
  /* The blank line over a sentence that follows controls.
   *
   * 10 is not a new number: it is the same step the button row above already
   * carries under itself, so the gap between a control row and the next
   * sentence doubles while the gap between a sentence and its OWN controls
   * stays as it was. Two to one is what makes the pair read as a pair - the
   * extension's About card reaches the same ratio with 20 against 8.
   *
   * It closes nothing by itself: the card's own padding is still the space
   * under the last line, which is why nothing here adds a bottom margin. */
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
