import type { ReactNode } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, TouchableOpacity, View, type ViewStyle } from 'react-native';
import { useAppearance } from '../theme/AppearanceContext';
import { BRAND, BTN_H_KEY, TYPE, inkFor, type Brand } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';
import { InfoTip } from './InfoTip';
import { Text } from './Text';

/**
 * The GlimStone controls, as React Native.
 *
 * The shapes and numbers are the web implementation's rather than
 * approximations: a notch 22px tall with a 12px uppercase label at 1.2
 * tracking, half overlapped over its card's top edge; a well that is a padded
 * groove one surface deeper whose chosen segment is the only badge; a switch
 * that is a 36x20 pill track with a 16px knob.
 *
 * No borders anywhere: GlimStone separates surfaces by shade, never by drawn
 * lines.
 */

/** One card with its notch title badge half over the top edge. `hue` is the
 *  card's rainbow position; without the mode it resolves to the accent, as the
 *  web's SectionTitle does. `info` explains the card through an (i) in the
 *  notch, in the notch's own ink, as the extension's section badges carry it. */
export function NotchCard({
  title,
  hue,
  info,
  style,
  children,
}: {
  title: string;
  hue?: number;
  info?: string;
  /** For a card that is not one of a stack, such as a floating window, which
   *  owes nothing to the page's rhythm above it. */
  style?: ViewStyle;
  children: ReactNode;
}) {
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
    <View style={[styles.cardWrap, style]}>
      <View style={[styles.card, { backgroundColor: c.surface, borderRadius: radii.card }]}>{children}</View>
      <View style={[styles.notch, { backgroundColor: fill, borderRadius: radii.pill }]}>
        <Text style={[styles.notchText, { color: ink }]} numberOfLines={1}>
          {title}
        </Text>
        {info ? <InfoTip text={info} color={ink} size={14} /> : null}
      </View>
    </View>
  );
}

/** The one horizontal selector: a groove one surface deeper, equal segments,
 *  and only the chosen segment is a badge. No per-segment borders. */
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
  // 84 points, which keeps two or three of them even rather than each hugging
  // its own word. Four of those is about 350 points before the card's padding,
  // and a 360-point phone has under 300 to give, so the last segment would run
  // past the card's edge. Only the motion picker reaches that state, once the
  // hidden fourth level has been found.
  //
  // The floor is dropped in that case alone, so the rows that already fit are
  // untouched. Below the floor the segments share what the row has and the
  // labels stay on one line.
  const eng = options.length > 3;
  return (
    <View style={[styles.well, { backgroundColor: c.surface2, borderRadius: radii.control }]}>
      {options.map((o, i) => {
        const on = o.value === value;
        // Each segment owns a palette position. The design language names a
        // segmented control's segments as one of the things that may own one,
        // for the reason a tab strip does: they are equal members of one set.
        // The positions are this control's own 0-based sequence, not the
        // page's.
        const fill = (hueAt(i) ?? accent) as string;
        return (
          <TouchableOpacity
            key={o.value}
            onPress={() => onPick(o.value)}
            /* No radius on the segment. The groove is the one shape here and
               the radius sits on it; a segment that rounds its own corners
               inside a rounded track reads as a key loose in a slot rather
               than as one control with several settled positions. */
            style={[styles.segment, eng && styles.segmentEng, on && { backgroundColor: fill }]}
          >
            {/* Computed against the fill it landed on rather than the flat
                accent's contrast: a palette position can be far lighter or
                darker than the accent, and reusing accentContrast puts white
                text on a pale mint segment. */}
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
 * Both radii come from the shape engine. A hard 999 ignores the setting, so on
 * the square shape every other control squares off and the switches stay pills.
 * The web's Toggle reads `var(--radius-pill)` for the same reason: "pill" is a
 * token the shape engine sets, not a synonym for fully round.
 *
 * `hue` is this switch's position in a set of switches sharing one card, the
 * same 0-based sequence the cards carry. Without it three switches in one card
 * all light up in the single accent and stop reading as three rows.
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
      {/* The knob is the page's own ground sitting on the track rather than a
          fixed white, so it reads dark in dark mode and light in light. */}
      <View
        style={[
          styles.knob,
          { borderRadius: radii.pill, backgroundColor: c.bg, alignSelf: value ? 'flex-end' : 'flex-start' },
        ]}
      />
    </TouchableOpacity>
  );
}

/** A row inside a card: label left, control flush right, the ToggleRow shape
 *  the whole family uses. `sub` states what is in force; `info` explains the
 *  setting through an (i) beside the label. */
export function GlimRow({
  label,
  sub,
  info,
  control,
}: {
  label: string;
  sub?: string;
  info?: string;
  control?: ReactNode;
}) {
  const { c } = useAppearance();
  return (
    <View style={styles.rowOuter}>
      <View style={styles.rowText}>
        <View style={styles.rowLabelLine}>
          <Text style={[styles.rowLabel, { color: c.text }]}>{label}</Text>
          {info ? <InfoTip text={info} /> : null}
        </View>
        {sub ? <Text style={[styles.rowSub, { color: c.textMuted }]}>{sub}</Text> : null}
      </View>
      {control}
    </View>
  );
}

/**
 * What stands in for a control the environment does not permit: a title and
 * the reason, on the quiet end of the warn family (GlimStone's
 * UnavailableNotice). No control at all rather than one that cannot answer,
 * and a paragraph rather than a bubble, because there is no control left to
 * hang a bubble on.
 */
export function UnavailableNotice({ title, reason }: { title: string; reason: string }) {
  const { c, radii } = useAppearance();
  return (
    <View style={[styles.notice, { backgroundColor: c.statusWarnBgSoft, borderRadius: radii.card }]}>
      <Text style={[styles.noticeTitle, { color: c.text }]}>{title}</Text>
      <Text style={[styles.noticeReason, { color: c.textSub }]}>{reason}</Text>
    </View>
  );
}

/** A colour swatch. The current one is marked by a ring, an inset gap in the
 *  card colour and then the ink, drawn as nested views rather than a border,
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
 * The way back for a row of swatches: an icon rather than a text link (rule
 * 13), and the same circle as the swatches it stands beside.
 *
 * Sized by the row as they are, so nine things across one line stay nine equal
 * things, which is how the extension's row has looked since GlimStone 1.6.0.
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

/**
 * The fill an element takes when it has no active state of its own, and the ink
 * that goes on it.
 *
 * Reactive is the reading of the rainbow that rests neutral and shows colour on
 * what is hovered and on what is active. A phone has no hover, so for anything
 * carrying neither an active nor a checked state, a card's title badge or a
 * button, the resting half is the whole of it.
 *
 * One switch has to read the same way everywhere in one app: the download list
 * rests neutral and colours what is running, so a settings screen that lit
 * eight cards and every button in them would make the same setting mean two
 * things two taps apart.
 *
 * The ink is the page's own ground rather than the computed black or white,
 * because the muted grey sits near enough the middle that white on it is the
 * weaker of the two. The web binds --accent-contrast to --carbon-bg for this
 * state instead of letting the contrast function decide.
 *
 * WellSelector and GlimToggle do not go through here: their fill appears on the
 * chosen segment and the switched-on track, which is the active state the rule
 * keeps coloured.
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

/** Ink on a fill, mirroring the web's luminance rule closely enough for the
 *  eight palette colours. The resolved accent contrast is the fallback for the
 *  plain-accent case. */
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
  /* One height and one gap for every labelled button in the app. A row with no
   * gap sets the glyph against the first letter and the two read as one smudge;
   * 8 is the step this family uses between a control and its neighbour. */
  button: {
    // The taller of the family's two heights rather than a third number of this
    // app's own. A labelled button here is reached with a thumb, so it takes
    // the step up, and it takes it from the pair.
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
   * shares. The language calls 24 the cramped value, and it is cramped for the
   * reason these cards qualify: each carries a notch badge hanging over its own
   * top edge, so a 24 gap leaves 13 points of daylight between one card's body
   * and the next card's title. The same number serves the gap above the first
   * card. */
  cardWrap: { marginTop: 40 },
  card: { paddingTop: 24, paddingBottom: 14, paddingHorizontal: 16, gap: 4 },
  notch: {
    position: 'absolute',
    /* Half over the card's top edge, as a percentage rather than a pixel
     * offset: it resolves against the badge's own rendered height, so it stays
     * centred on the edge whatever that height turns out to be. A hard -11 is
     * correct only while the badge is exactly 22 tall, which it stops being the
     * moment somebody raises the system font size.
     *
     * `start` rather than `left`, because the app ships Arabic, Hebrew and
     * Persian and a badge pinned to the physical left stays there while the
     * card it titles mirrors. */
    top: 0,
    start: 16,
    transform: [{ translateY: '-50%' }],
    paddingVertical: 3,
    paddingHorizontal: 12,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    elevation: 3,
    shadowColor: '#000',
    shadowOpacity: 0.25,
    shadowRadius: 3,
    shadowOffset: { width: 0, height: 1 },
  },
  // The line height is stated rather than left to the platform, because it is
  // half of what makes the badge 22 points tall: 16 of text between 3 and 3 of
  // padding.
  notchText: { fontSize: TYPE.dense, lineHeight: 16, fontWeight: '500', textTransform: 'uppercase', letterSpacing: 1.2 },
  // maxWidth so the groove cannot grow past the card it sits in: it sizes
  // itself to its content, and content that does not fit would run under the
  // card's edge rather than being made to share.
  well: { flexDirection: 'row', padding: 3, gap: 2, alignSelf: 'flex-start', maxWidth: '100%' },
  segment: { minWidth: 84, paddingVertical: 7, paddingHorizontal: 14, alignItems: 'center' },
  // Four or more: the floor goes and the segments share the row instead. See
  // WellSelector above.
  segmentEng: { minWidth: 0, paddingHorizontal: 10, flexShrink: 1 },
  segmentText: { fontSize: TYPE.dense, fontWeight: '500' },
  // No borderRadius here: it comes from the shape engine at render time, and a
  // value baked into the stylesheet cannot follow a setting.
  track: { width: 36, height: 20, padding: 2, justifyContent: 'center' },
  knob: { width: 16, height: 16 },
  rowOuter: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 8 },
  rowText: { flex: 1, minWidth: 0, gap: 2 },
  // The (i) sits beside the words, spaced like the extension's row label.
  rowLabelLine: { flexDirection: 'row', alignItems: 'center', gap: 6, flexShrink: 1 },
  notice: { paddingVertical: 12, paddingHorizontal: 16, marginTop: 8, gap: 4 },
  noticeTitle: { fontSize: TYPE.body, fontWeight: '500' },
  noticeReason: { fontSize: TYPE.dense, lineHeight: 18 },
  // Body, off the table. 15 is not a step of the scale, and a 15 sitting next
  // to a 14 is how a four-row table grows a fifth row nobody decided on. It
  // shrinks and wraps rather than pushing the (i) off the row.
  rowLabel: { fontSize: TYPE.body, flexShrink: 1 },
  rowSub: { fontSize: TYPE.caption, lineHeight: 16 },
  // Sized by the row rather than by a number here. Eight swatches at a fixed 32
  // plus a reset plus a label do not fit across a phone, and a smaller fixed
  // number only moves the wrap to a narrower one. `flex: 1` with a square
  // aspect divides whatever the row has between nine equal things, and
  // `maxWidth` keeps them from growing into saucers on a tablet. The ring and
  // the gap follow as percentages so the selection ring stays proportional.
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
 * A coloured dot asks the reader to know the code, and says nothing to anybody
 * who cannot tell this green from this red. The word carries the meaning and
 * the colour carries the urgency, the pairing every other status in this family
 * uses.
 *
 * The ground is the status colour at low opacity and the ink is the solid: a
 * fully filled badge in the fail colour reads as an alarm, and "offline" on an
 * instance somebody has not switched on is not one. The extension's
 * `.glim-status` follows the same rule, so the two surfaces draw one object.
 *
 * The ground comes from the `*Bg` token rather than the solid with an alpha
 * appended to its hex string. A concatenation cannot differ between the two
 * themes the way the palette does, nothing can search for it, and it assumes
 * every status colour it is handed is a six-digit hex.
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
 * Every labelled button in this app.
 *
 * Buttons dressed at each call site take `accent` here and `c.surface2` there
 * and ask for no rainbow hue at all, so with the mode on the cards and switches
 * around them cycle through the palette while the buttons sit in one colour.
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
 *   - "quiet" keeps the neutral surface and takes ordinary text ink, without
 *     putting the hue in the ink. A coloured word on a grey ground reads as a
 *     link rather than a button, and the colour engine is about the pressable
 *     thing carrying the colour.
 *
 * There is no "danger" tone, and its absence is the guard. GlimStone 1.12.0
 * gives a destructive button the colour its siblings take, because what warns
 * is the question: an irreversible action opens a window that states the stakes
 * in words and counts, and somebody who has read that window has been told. Red
 * on every delete in an app teaches people to read past it by the third time
 * they meet it, and 1.13.0 removed the last sanctioned exception. On a phone
 * there is not even a hover to soften a red control into a state somebody is
 * passing over.
 *
 * Absent from the union rather than left unused: a variant that exists comes
 * back, because it can be argued for at any one call site, while a variant that
 * does not is a type error at every call site at once.
 *
 * The contrast is computed from the fill through the same contrastFor this
 * file's other controls use, never from the flat accent: a palette position can
 * be far lighter or darker than the accent, and reusing accentContrast puts
 * white text on a pale mint button.
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
  /** Given the resolved ink colour and the ground it stands on, so a glyph
   *  never has to guess either. A composed glyph needs the ground to draw a
   *  detail that would otherwise be a hole: React Native cannot cut a shape out
   *  of another, so a flap, a slot or a gear's centre is painted, and a guessed
   *  colour there is wrong on the first surface it was not tested against. */
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

/**
 * A button that goes to a brand, for the About card: a neutral ground, the
 * words in the page's ink and the brand's mark in its colour (GlimStone's
 * `.glim-brand-btn`).
 *
 * At rest the mark takes the brand's adjusted colour from the palette, since
 * the published one fails 3:1 on this ground in one of the two themes. The
 * true colour is spent as the fill while the button is pressed, the one ground
 * it was drawn for, with words and mark flipping to that fill's measured ink.
 * The web does this on hover, which a phone does not have.
 *
 * `house` is the exception that proves the rule: the button that reaches the
 * app's own authors takes the accent, and with it the card's rainbow position,
 * which a vendor's mark may never do. `hue` is that position.
 */
export function BrandButton({
  brand,
  label,
  icon,
  onPress,
  hue,
}: {
  brand: Brand | 'house';
  label: string;
  /** Given the mark's colour and the ground it stands on, as GlimButton's is,
   *  so a painted detail such as the envelope's flap matches the ground. */
  icon: (ink: string, ground: string) => ReactNode;
  onPress: () => void;
  hue?: number;
}) {
  const { c, dark, accent, accentContrast, radii, hueAt, rainbow } = useAppearance();
  let rest: string;
  let fill: string;
  let ink: string;
  if (brand === 'house') {
    ({ fill, ink } = restingFill(hue, {
      accent,
      accentContrast,
      hueAt,
      reactive: rainbow.on && rainbow.reactive,
      muted: c.textMuted,
      ground: c.bg,
    }));
    // The accent used as ink on this ground, as accentInk is for the plain
    // accent: darkened on the light theme, as given on the dark one.
    rest = dark ? fill : inkFor(fill);
  } else {
    ({ fill, ink } = BRAND[brand]);
    rest = { coffee: c.brandCoffee, bitcoin: c.brandBitcoin, paypal: c.brandPaypal, github: c.brandGithub }[brand];
  }
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={label}
      style={({ pressed }) => [styles.button, { backgroundColor: pressed ? fill : c.surface2, borderRadius: radii.control }]}
    >
      {({ pressed }) => (
        <>
          {icon(pressed ? ink : rest, pressed ? fill : c.surface2)}
          <Text style={[styles.buttonLabel, { color: pressed ? ink : c.text }]} numberOfLines={1}>
            {label}
          </Text>
        </>
      )}
    </Pressable>
  );
}
