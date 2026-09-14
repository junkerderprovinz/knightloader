import type { ReactNode } from 'react';
import { ActivityIndicator, StyleSheet, Text, TouchableOpacity, View, type ViewStyle } from 'react-native';
import { useAppearance } from '../theme/AppearanceContext';
import { BTN_H_KEY, TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';

/**
 * The GlimStone controls, as React Native.
 *
 * This file exists because the settings screen was drawn with borders and
 * loose grey captions while the product it connects to draws notch badges,
 * well selectors and filled switches (jdp, 2026-08-29: "In den einstellungen
 * sehen die buttons und alles ganz anders aus wie in KL selbst. Das soll auch
 * in der App exakt gleich aussehen."). The shapes and numbers here are the
 * web implementation's, not approximations: notch 22px tall, 12px uppercase
 * label with 1.2 tracking, half overlapped over its card's top edge; the well
 * is a padded groove one surface deeper whose CHOSEN segment is the only
 * badge; the switch is a 36x20 pill track with a 16px knob.
 *
 * No borders anywhere, which is the rule the old screen broke most: GlimStone
 * separates surfaces by shade, never by drawn lines.
 */

/** One card with its notch title badge half over the top edge. `hue` is the
 *  card's rainbow position; without the mode it resolves to the accent, which
 *  is exactly how the web's SectionTitle behaves. */
export function NotchCard({ title, hue, children }: { title: string; hue?: number; children: ReactNode }) {
  const { c, accent, accentContrast, radii, hueAt, rainbow } = useAppearance();
  const { fill, ink } = restingFill(hue, {
    accent,
    accentContrast,
    hueAt,
    reactive: rainbow.on && rainbow.reactive,
    muted: c.textMuted,
    ground: c.bg,
  });
  return (
    <View style={[styles.cardWrap]}>
      <View style={[styles.card, { backgroundColor: c.surface, borderRadius: radii.card }]}>{children}</View>
      <View style={[styles.notch, { backgroundColor: fill, borderRadius: radii.pill }]}>
        <Text style={[styles.notchText, { color: ink }]} numberOfLines={1}>
          {title}
        </Text>
      </View>
    </View>
  );
}

/** The one horizontal selector: a groove one surface deeper, equal segments,
 *  and only the chosen segment is a badge. Never per-segment borders. */
export function WellSelector<T extends string>({
  options,
  value,
  onPick,
}: {
  options: { value: T; label: string }[];
  value: T;
  onPick: (v: T) => void;
}) {
  const { c, accent, accentContrast, radii, hueAt } = useAppearance();
  // Four segments do not fit a narrow phone at the three-segment geometry.
  //
  // The groove sizes itself to its content and each segment carries a floor of
  // 84 points, which is what keeps two or three of them even rather than each
  // one hugging its own word. Four of those is about 350 points before the
  // card's padding, and a 360-point phone has under 300 to give - so the last
  // segment would have been cut off by the card's edge. That state is reachable
  // on exactly one row (the motion picker, once the hidden fourth level has
  // been found), and a secret that arrives half off the screen reads as a bug
  // rather than as a find.
  //
  // The floor is dropped only in that case, so the rows that already fit are
  // untouched: this is a fix for a new state, not a re-layout of three existing
  // ones. Below the floor the segments share what the row has, and the labels
  // stay on one line.
  const eng = options.length > 3;
  return (
    <View style={[styles.well, { backgroundColor: c.surface2, borderRadius: radii.control }]}>
      {options.map((o, i) => {
        const on = o.value === value;
        // Each SEGMENT owns a palette position (jdp, 2026-09-01: "die design und
        // ecken selektoren sind nicht im rainbowmode in der app").
        //
        // The design language names a segmented control's segments as one of
        // the things that may own a position, for the same reason a tab strip
        // does: they are members of one set, all equal. NotchCard and
        // GlimToggle in this very file already read hueAt; this control was the
        // one that did not, so the Theme and Corners rows stayed flat gold on a
        // screen where every card around them had gone plural. Positions are
        // this control's OWN 0-based sequence, not the page's.
        const fill = (hueAt(i) ?? accent) as string;
        return (
          <TouchableOpacity
            key={o.value}
            onPress={() => onPick(o.value)}
            /* NO radius on the segment. The groove is the one shape here and
               the radius sits on it; a segment that rounds its own corners
               inside a rounded track reads as a key loose in a slot rather
               than as one control with N settled positions. All three
               surfaces of this product had independently given the segment
               its own radius, which is what a never-finished port looks like
               rather than a decision. */
            style={[styles.segment, eng && styles.segmentEng, on && { backgroundColor: fill }]}
          >
            {/* Computed against the fill it actually landed on, never the flat
                accent's contrast: a palette position can be far lighter or
                darker than the accent, and reusing accentContrast is how white
                text ends up on a pale mint segment. */}
            <Text
              numberOfLines={1}
              style={[styles.segmentText, { color: on ? contrastFor(fill, accentContrast) : c.textSub }]}
            >
              {o.label}
            </Text>
          </TouchableOpacity>
        );
      })}
    </View>
  );
}

/**
 * The switch: a track, a knob, filled when on. The same object the web UI and
 * the browser extension draw, so a person who flipped one there recognises
 * this one.
 *
 * Both radii come from the shape engine (jdp, 2026-08-30: "die toggles folgen
 * nicht der form"). They were hard 999s, which is the one number that ignores
 * the setting entirely: on Eckig every other control in the app squared off
 * and the switches stayed pills. The web's own Toggle reads
 * `var(--radius-pill)` for exactly this reason - "pill" is a token the shape
 * engine sets, not a synonym for "fully round".
 *
 * `hue` is this switch's position in a set of switches sharing one card, the
 * same 0-based sequence the cards themselves carry. Without it three switches
 * in one card all light up in the single accent and stop reading as three
 * distinct rows.
 */
export function GlimToggle({
  value,
  onChange,
  hue,
}: {
  value: boolean;
  onChange: (v: boolean) => void;
  hue?: number;
}) {
  const { c, accent, radii, hueAt } = useAppearance();
  const on = (hue !== undefined ? hueAt(hue) : undefined) ?? accent;
  return (
    <TouchableOpacity
      accessibilityRole="switch"
      accessibilityState={{ checked: value }}
      onPress={() => onChange(!value)}
      style={[styles.track, { borderRadius: radii.pill, backgroundColor: value ? on : c.surface3 }]}
    >
      {/* The knob is the page's own ground sitting on the track, not a fixed
          white: it reads dark in dark mode and light in light mode. */}
      <View
        style={[
          styles.knob,
          { borderRadius: radii.pill, backgroundColor: c.bg, alignSelf: value ? 'flex-end' : 'flex-start' },
        ]}
      />
    </TouchableOpacity>
  );
}

/** A row inside a card: label left, control flush right - the ToggleRow shape
 *  the whole family uses. */
export function GlimRow({ label, sub, control }: { label: string; sub?: string; control?: ReactNode }) {
  const { c } = useAppearance();
  return (
    <View style={styles.rowOuter}>
      <View style={styles.rowText}>
        <Text style={[styles.rowLabel, { color: c.text }]}>{label}</Text>
        {sub ? <Text style={[styles.rowSub, { color: c.textMuted }]}>{sub}</Text> : null}
      </View>
      {control}
    </View>
  );
}

/** A colour swatch. The current one is marked by a RING - an inset gap in the
 *  card colour, then the ink - drawn as nested views rather than a border,
 *  because a border is a line and this language has none. */
export function Swatch({ hex, selected, onPress, label }: { hex: string; selected: boolean; onPress: () => void; label: string }) {
  const { c, radii } = useAppearance();
  return (
    <TouchableOpacity accessibilityLabel={label} onPress={onPress} style={[styles.swatchRing, { borderRadius: radii.pill, backgroundColor: selected ? c.text : 'transparent' }]}>
      <View style={[styles.swatchGap, { borderRadius: radii.pill, backgroundColor: selected ? c.surface : 'transparent' }]}>
        <View style={[styles.swatchFill, { borderRadius: radii.pill, backgroundColor: hex }]} />
      </View>
    </TouchableOpacity>
  );
}

/**
 * The way back for a row of swatches: an icon, not a text link (rule 13), and
 * the same circle as the swatches it stands beside.
 *
 * Sized by the row like they are, so nine things across one line stay nine
 * equal things - the extension's own row has looked exactly like this since
 * GlimStone 1.6.0, and the app was the surface still missing it entirely.
 */
export function SwatchReset({ onPress, label }: { onPress: () => void; label: string }) {
  const { c, radii } = useAppearance();
  return (
    <TouchableOpacity accessibilityLabel={label} onPress={onPress} style={styles.swatchRing}>
      <View style={[styles.swatchGap, { borderRadius: radii.pill, backgroundColor: c.surface2 }]}>
        {/* A counter-clockwise arrow, drawn as an open ring with a head, in the
            same filled register as every other glyph. */}
        <View style={[styles.resetRing, { borderColor: c.textSub, borderRadius: radii.pill }]} />
        <View style={[styles.resetHead, { borderBottomColor: c.textSub }]} />
      </View>
    </TouchableOpacity>
  );
}

/** Ink on a fill, mirroring the web's luminance rule closely enough for the
 *  eight palette colours; the resolved accent contrast is the fallback for
 *  the plain-accent case. */
/**
 * The fill an element takes when it has no active state of its own, and the ink
 * that goes on it.
 *
 * Reactive is the reading of the rainbow that rests neutral and shows colour on
 * what is hovered AND on what is active. A phone has no hover, so for anything
 * carrying neither an active nor a checked state - a card's title badge, a
 * button - the resting half is the whole of it, and that is not a compromise:
 * somebody who picked the quiet mode picked the quiet mode.
 *
 * It matters that ONE switch reads the same way everywhere in one app. The
 * download list already rested neutral and coloured what was running, while the
 * settings screen beside it lit eight cards and every button in them, so the
 * same setting meant two different things two taps apart.
 *
 * The ink is the page's own GROUND rather than the computed black-or-white: the
 * muted grey sits near enough the middle that white on it is the weaker of the
 * two, which is why the web binds --accent-contrast to --carbon-bg for exactly
 * this state instead of letting the contrast function decide.
 *
 * WellSelector and GlimToggle do not go through here and must not: their fill
 * only ever appears on the chosen segment and the switched-on track, which IS
 * the active state the rule keeps coloured.
 */
function restingFill(
  hue: number | undefined,
  opts: {
    accent: string;
    accentContrast: string;
    hueAt: (i: number) => string | undefined;
    reactive: boolean;
    muted: string;
    ground: string;
  },
): { fill: string; ink: string } {
  if (opts.reactive) return { fill: opts.muted, ink: opts.ground };
  const fill = (hue !== undefined ? opts.hueAt(hue) : undefined) ?? opts.accent;
  return { fill, ink: contrastFor(fill, opts.accentContrast) };
}

function contrastFor(hex: string, fallback: string): string {
  const m = /^#([0-9a-f]{6})$/i.exec(hex);
  if (!m) return fallback;
  const n = parseInt(m[1], 16);
  const lin = (v: number) => {
    const x = v / 255;
    return x <= 0.04045 ? x / 12.92 : Math.pow((x + 0.055) / 1.055, 2.4);
  };
  const lum = 0.2126 * lin((n >> 16) & 255) + 0.7152 * lin((n >> 8) & 255) + 0.0722 * lin(n & 255);
  return lum > 0.55 ? '#161616' : '#FFFFFF';
}

const styles = StyleSheet.create({
  /* ONE height and ONE gap for every labelled button in the app.
   *
   * The gap is what jdp reported on the extension the same evening ("die
   * glyphen der buttons sind zu nah am text") and it was true here too: a row
   * with no gap sets the glyph against the first letter and the two read as one
   * smudge. 8 is the step this family uses between a control and its
   * neighbour. */
  button: {
    // The taller of the family's TWO heights, and not a third number of this
    // app's own. A labelled button here is reached with a thumb, so it takes
    // the step up rather than the one a text field measures - but it takes it
    // from the pair, because "44 because a phone" is how a product ends up
    // measuring one object three ways across three surfaces.
    minHeight: BTN_H_KEY,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    paddingHorizontal: 16,
    paddingVertical: 12,
  },
  buttonLabel: { fontSize: TYPE.body, fontWeight: '600', flexShrink: 1 },
  buttonOff: { opacity: 0.45 },
  /* 40, the house rhythm for stacked cards, and the number the whole family
   * shares.
   *
   * 24 is the value the language names as the CRAMPED one, and it is cramped
   * for precisely the reason these cards qualify: every one of them carries a
   * notch badge hanging over its own top edge, so a 24 gap leaves 13 points of
   * real daylight between one card's body and the next card's title. The same
   * number serves the gap above the first card, rather than a second number
   * invented for that one seam. */
  cardWrap: { marginTop: 40 },
  card: { paddingTop: 24, paddingBottom: 14, paddingHorizontal: 16, gap: 4 },
  notch: {
    position: 'absolute',
    /* Half over the card's top edge, expressed as a HALF and not as a pixel
     * offset: the percentage resolves against the badge's own rendered height,
     * so it stays centred on the edge whatever that height turns out to be. A
     * hard -11 is only correct while the badge is exactly 22 tall, and on a
     * phone it stops being 22 the moment somebody raises the system font size
     * - which is the one platform where that is a setting rather than a
     * hypothetical.
     *
     * `start`, not `left`, for the same family of reason: the app ships
     * Arabic, Hebrew and Persian, and a badge pinned to the physical left is a
     * badge that stays there while the card it titles mirrors. */
    top: 0,
    start: 16,
    transform: [{ translateY: '-50%' }],
    paddingVertical: 3,
    paddingHorizontal: 12,
    justifyContent: 'center',
    elevation: 3,
    shadowColor: '#000',
    shadowOpacity: 0.25,
    shadowRadius: 3,
    shadowOffset: { width: 0, height: 1 },
  },
  // The line height is stated rather than left to the platform, because it is
  // half of what makes the badge 22 points tall now that the height is not
  // written down: 16 of text between 3 and 3 of padding.
  notchText: { fontSize: TYPE.dense, lineHeight: 16, fontWeight: '500', textTransform: 'uppercase', letterSpacing: 1.2 },
  // maxWidth so the groove can never grow past the card it sits in: it sizes
  // itself to its content, and content that does not fit would otherwise run
  // under the card's edge rather than being made to share.
  well: { flexDirection: 'row', padding: 3, gap: 2, alignSelf: 'flex-start', maxWidth: '100%' },
  segment: { minWidth: 84, paddingVertical: 7, paddingHorizontal: 14, alignItems: 'center' },
  // Four or more: the floor goes and the segments share the row instead. See
  // WellSelector's own note for why this is a fix for one new state rather than
  // a re-layout of the rows that already fit.
  segmentEng: { minWidth: 0, paddingHorizontal: 10, flexShrink: 1 },
  segmentText: { fontSize: TYPE.dense, fontWeight: '500' },
  // No borderRadius here: it comes from the shape engine at render time, and
  // a value baked into the stylesheet cannot follow a setting.
  track: { width: 36, height: 20, padding: 2, justifyContent: 'center' },
  knob: { width: 16, height: 16 },
  rowOuter: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 8 },
  rowText: { flex: 1, minWidth: 0, gap: 2 },
  // Body, off the table. 15 is not a step of the scale, and a 15 sitting next
  // to a 14 is how a four-row table grows a fifth row nobody decided on.
  rowLabel: { fontSize: TYPE.body },
  rowSub: { fontSize: TYPE.caption, lineHeight: 16 },
  // Sized by the ROW, not by a number here (jdp, 2026-09-01: "Die farbfelder
  // bitte enger zusammen rücken, dass sie in einer zeile platz haben").
  //
  // Eight swatches at a fixed 32 plus a reset plus a label do not fit across a
  // phone, so the row wrapped - and picking a smaller fixed number just moves
  // the wrap to a narrower phone. `flex: 1` with a square aspect divides
  // whatever the row has between nine equal things, and `maxWidth` keeps them
  // from growing into saucers on a tablet. The ring and the gap follow as
  // percentages so the selection ring stays proportional at every size.
  swatchRing: { flex: 1, maxWidth: 32, aspectRatio: 1, alignItems: 'center', justifyContent: 'center' },
  swatchGap: { width: '88%', height: '88%', alignItems: 'center', justifyContent: 'center' },
  swatchFill: { width: '86%', height: '86%' },
  // The reset glyph: three quarters of a ring, plus a head on the open end.
  resetRing: { width: '58%', height: '58%', borderWidth: 1.5, borderRightColor: 'transparent' },
  statusBadge: { paddingHorizontal: 7, paddingVertical: 2, flexShrink: 0 },
  statusText: { fontSize: TYPE.caption, fontWeight: '600', letterSpacing: 0.2 },
  resetHead: {
    position: 'absolute',
    right: '18%',
    top: '20%',
    width: 0,
    height: 0,
    borderStyle: 'solid',
    borderLeftWidth: 2.4,
    borderRightWidth: 2.4,
    borderBottomWidth: 4,
    borderLeftColor: 'transparent',
    borderRightColor: 'transparent',
  },
});

/**
 * A status badge: the word, on a ground in that status's own colour.
 *
 * It replaces a 10-point coloured dot (jdp, 2026-09-01: "der statuspunkt soll
 * ein kleiner badge mit online/offlin text sein und auch dem status entsprechend
 * eingefärbt sein"). A dot asks the reader to know the colour code, and to
 * anybody who cannot tell this green from this red it says nothing at all -
 * which is the whole of what a status indicator is for. The word carries the
 * meaning and the colour carries the urgency, the pairing every other status in
 * this family already uses.
 *
 * The ground is the status colour at low opacity rather than the solid, and the
 * ink is the solid: a fully filled badge in the fail colour on a card reads as
 * an alarm, and "offline" on an instance somebody has not switched on is not an
 * alarm. The extension's own `.glim-status` follows the identical rule, so the
 * two surfaces draw one object.
 *
 * That ground is the `*Bg` TOKEN, not the solid with an alpha appended to its
 * hex string. The old `ink + '26'` landed on the right colour and was still the
 * wrong mechanism: a string concatenation cannot differ between the two themes
 * the way the palette does, nothing can search for it, and it quietly assumes
 * every status colour it is ever handed is a six-digit hex. The palette now
 * carries all three steps of every family, so this reads the middle one.
 */
export function StatusBadge({ status }: { status: 'checking' | 'online' | 'offline' }) {
  const { c, radii } = useAppearance();
  const { t } = useT();
  const ink =
    status === 'online' ? c.statusOkSolid : status === 'checking' ? c.statusWarnSolid : c.statusFailSolid;
  const ground =
    status === 'online' ? c.statusOkBg : status === 'checking' ? c.statusWarnBg : c.statusFailBg;
  const label =
    status === 'online' ? t('instance.online') : status === 'checking' ? t('instance.checking') : t('instance.offline');
  return (
    <View style={[styles.statusBadge, { backgroundColor: ground, borderRadius: radii.pill }]}>
      <Text style={[styles.statusText, { color: ink }]}>{label}</Text>
    </View>
  );
}

/**
 * Every labelled button in this app, and the reason it exists is that they
 * were not one thing before.
 *
 * jdp, 2026-09-01: "die ganzen buttons sind nicht in die farbmodi integriert"
 * and "BItte ALLE buttons in die farbengine aufnehmen! mit phrase verbinde
 * seite fehlt komplett". Both are the same defect seen twice. Buttons were
 * being dressed at each call site: some took `accent` directly, some took
 * `c.surface2`, and NONE of them asked for a rainbow hue - so with the mode on,
 * the cards and switches around them cycled through the palette and the buttons
 * sat there in one colour, which is exactly what "not in the colour modes"
 * looks like.
 *
 * A component rather than a shared style object, because a style object is
 * something the next new button can forget to import and a component is not.
 * Anything with a label goes through here; icon-only actions are IconBadge's.
 *
 * `hue` is this button's position in the palette, the same index NotchCard and
 * GlimToggle already take. Without the rainbow it resolves to the accent, so
 * one number serves both modes and nothing has to branch at the call site.
 *
 * `tone`:
 *   - "solid" (the default) fills with the hue and takes contrasting ink.
 *   - "quiet" keeps the neutral surface and takes ordinary text ink. It does
 *     NOT put the hue in the ink: jdp, 2026-09-02, "Die buttons sind falsch
 *     farbig, der button soll farbig sein, nicht der TExt". A coloured word on a
 *     grey ground reads as a link, not as a button, and the whole point of the
 *     colour engine is that the pressable THING carries the colour.
 *
 * THERE IS NO "danger", AND ITS ABSENCE IS THE GUARD.
 *
 * It was here, it was quiet with the fail colour in the ink, and the argument
 * beside it was that the status colours mean what they mean and a delete button
 * turning teal because it is third in a palette has stopped warning anybody.
 * GlimStone 1.12.0 answered it: a destructive button takes the colour its
 * siblings take, because what warns is the QUESTION. An irreversible action
 * opens a window that states the stakes in words and counts, and somebody who
 * has read that window and reached for the button has already been told. A
 * colour cannot say more than the sentence above it, and red on every delete in
 * an app teaches people to read past it by the third time they meet it - which
 * is the same argument the delete BADGE lost one element earlier, and those
 * badges have been neutral here since they were drawn. 1.13.0 then removed the
 * one remaining sanctioned exception rather than relocating it, so there is no
 * carve-out left for this to sit in.
 *
 * Deleted from the union rather than left unused, and that is the whole of the
 * guard: a variant that still exists comes back, because it can be argued for
 * convincingly at any ONE call site. A variant that does not exist is a type
 * error at every call site at once, which is the strongest check available on a
 * surface with no stylesheet to lint.
 *
 * On a phone this matters more than it does in a browser, not less: there is no
 * hover to soften a red control into a state somebody is merely passing over.
 * It is simply a red button, sitting there.
 *
 * The contrast is computed FROM THE FILL, through the same contrastFor this
 * file's other controls use, never from the flat accent: a palette position can
 * be far lighter or darker than the accent, and reusing accentContrast is how
 * white text ends up on a pale mint button.
 */
export function GlimButton({
  label,
  icon,
  onPress,
  hue,
  tone = 'solid',
  disabled,
  busy,
  grow,
  style,
}: {
  label: string;
  /** Given the resolved ink colour AND the ground it is standing on, so a
   *  glyph never has to guess either. The second one is what a composed glyph
   *  needs to draw a detail that would otherwise be a hole: React Native
   *  cannot cut a shape out of another, so a flap, a slot or a gear's centre
   *  has to be PAINTED, and painting it a guessed colour is a lie on the first
   *  surface it was not tested against. */
  icon?: (ink: string, ground: string) => ReactNode;
  onPress: () => void;
  hue?: number;
  tone?: 'solid' | 'quiet';
  disabled?: boolean;
  /** Replaces the icon with a spinner and blocks the press. */
  busy?: boolean;
  /** Share a row equally with its siblings. */
  grow?: boolean;
  style?: ViewStyle;
}) {
  const { c, accent, accentContrast, radii, hueAt, rainbow } = useAppearance();
  const { fill, ink: filledInk } = restingFill(hue, {
    accent,
    accentContrast,
    hueAt,
    reactive: rainbow.on && rainbow.reactive,
    muted: c.textMuted,
    ground: c.bg,
  });
  const ground = tone === 'solid' ? fill : c.surface2;
  // Two tones, two answers, and no third branch for a destructive one: a quiet
  // button takes the page's own ink whatever it is about to do.
  const ink = tone === 'solid' ? filledInk : c.text;
  return (
    <TouchableOpacity
      style={[
        styles.button,
        { backgroundColor: ground, borderRadius: radii.control },
        grow ? { flex: 1 } : null,
        disabled || busy ? styles.buttonOff : null,
        style,
      ]}
      onPress={onPress}
      disabled={disabled || busy}
      accessibilityRole="button"
      accessibilityState={{ disabled: !!(disabled || busy) }}
      accessibilityLabel={label}
    >
      {busy ? <ActivityIndicator color={ink} /> : icon?.(ink, ground)}
      <Text style={[styles.buttonLabel, { color: ink }]} numberOfLines={1}>
        {label}
      </Text>
    </TouchableOpacity>
  );
}
