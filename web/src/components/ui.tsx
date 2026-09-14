// Primitives of the GlimStone design language. Everything is expressed through the
// shared tokens in index.css, so a sibling app inherits the look by adopting
// that file — see the comment block there.
import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import type { ButtonHTMLAttributes, CSSProperties, InputHTMLAttributes, ReactNode, RefObject } from 'react';
import { hueVars, rainbowAt } from '../lib/appearance';
import { useNavLabels } from '../lib/navLabels';
import { useDialogMute, type DialogId } from '../lib/dialogmute';
import { useT } from '../lib/i18n';
import { IconClose, IconEye, IconEyeOff } from '../lib/icons';
import { openColorPickerPopover } from '../lib/colorPicker';

/**
 * THERE IS NO 'danger', AND ITS ABSENCE IS THE GUARD.
 *
 * GlimStone 1.12.0 took status-red off destructive controls, and 1.13.0 removed
 * the one sanctioned exception rather than relocating it. What warns is the
 * QUESTION: an irreversible action opens a window that states the stakes in
 * words and counts, and somebody who has read that and reached for the button
 * has already been told. A colour cannot say more than the sentence above it,
 * and red on every delete teaches people to read past it by the third time.
 *
 * Deleting the variant from the union rather than leaving it unused is what
 * makes tsc the check. A variant that still exists comes back, because it can
 * be argued for convincingly at any single call site; one that does not exist
 * is a compile error at all of them at once. That is the strongest guard
 * available here and it costs nothing to run.
 */
type ButtonKind = 'primary' | 'secondary' | 'ghost';

/**
 * HOVER IS A TOKEN, AND ON A FILLED ACCENT CONTROL IT IS AN OPACITY STEP.
 *
 * `hover:brightness-110` stood on the accent fill here, and it is the mistake
 * rule 21 names by hand: a single brightness value can only move one way,
 * while the two themes need opposite directions. A step away from the surface
 * is LIGHTER on the dark ground and DARKER on the light one, so brightening an
 * accent fill under a light theme walks the control back towards the page it
 * is supposed to stand out from. Invisible in the markup, obvious on screen,
 * and only in one of the modes - which is how it survived this long.
 *
 * The surface ramp has a token for every tier and the other two kinds take
 * them: no fill of its own hovers to `--carbon-hover`, a surface2 fill to
 * `--carbon-surface3` (web/check-hover-ramp.mjs is the guard on that pair).
 * An accent fill is not ON that ramp and so has no tier above it; it takes the
 * step GlimStone's own button takes for its accent tone instead, which is
 * opacity. Opacity reads the same in both themes because it moves the fill
 * towards whatever is behind it rather than towards one fixed end of the grey
 * scale.
 */
const kindClass: Record<ButtonKind, string> = {
  primary: 'bg-accent text-accentContrast hover:opacity-90',
  secondary: 'bg-carbon-surface2 text-carbon-text hover:bg-carbon-surface3',
  ghost: 'text-carbon-textSub hover:bg-carbon-hover hover:text-carbon-text',
};

/**
 * THE TWO HEIGHTS, AND THERE IS NO THIRD (GlimStone rule 19).
 *
 * `--btn-h` (2rem) is what an ordinary text field measures, so a button
 * standing in a row of fields matches it instead of standing proud of it;
 * `--btn-h-key` (2.5rem, `.glim-btn-key`) is the one step up, for the button a
 * surface exists FOR - the control that creates the thing the page lists, or
 * one whose press is hard to undo. Both numbers live in index.css and nowhere
 * else, which is why nothing in this file writes a height of its own any more:
 * this app had 36px buttons, 32px badges and a 48px transport row, and three
 * heights read as a ladder somebody has to pick a rung from.
 *
 * Every square CONTROL in here therefore measures `--btn-h` on both axes, and
 * the field below measures it too (see `inputClass`). A key control standing in
 * a field row has to be CENTRED against its neighbours rather than top-aligned,
 * which is the call site's `items-center`, not something this component can do
 * for it.
 *
 * The square that was still short is gone with the component that carried it.
 * This paragraph used to say `Swatch` was 28px, that the four hand-built squares
 * in pages/settings/Look.tsx were deliberately matched to it, and that it would
 * move when they moved, in one edit. They have moved, to --btn-h, and the edit
 * turned out not to reach this file at all: nothing imports Swatch or SwatchRow
 * any more, because that page built its own. See the note where they used to be.
 */
const BTN_H = 'h-[var(--btn-h)]';
const BTN_SQUARE = 'h-[var(--btn-h)] w-[var(--btn-h)]';

/**
 * RULE 13, THE MEASUREMENT: A GLYPH ALONE IN A SQUARE IS HALF ITS BOX.
 *
 * 16px in a 32px control, 20px in a 40px one - not the 20px a glyph takes
 * BESIDE WORDS, where the mark and 14px text have to read as one control. In a
 * square there are no words to match, so the only proportion left is how much
 * of the frame the ink fills, and 62% of it was reported in as many words:
 * "die glyphen sind zu gross und wirken klobig".
 *
 * The size is set HERE, by the component that draws the square, rather than at
 * the call sites - which is the whole point of this block. Handed to the call
 * sites it drifted to four different answers across 79 badges (49 at 16px, 27
 * at 14, two at 15 and one at the icon set's own 22px base), and 14px in a 32px
 * tile is as wrong in the other direction as 22px is in this one. A number
 * passed per call site comes back; a number owned by the component cannot.
 *
 * `[&>svg]` beats the `width`/`height` written on the glyph itself, and that is
 * load-bearing rather than lucky: those two are SVG presentation attributes,
 * which lose to any CSS rule at all. So a call site that still passes 14 gets
 * the right size anyway, and the leftover numbers are tidy-up rather than
 * breakage.
 */
const GLYPH_16 = '[&>svg]:h-4 [&>svg]:w-4';
const GLYPH_20 = '[&>svg]:h-5 [&>svg]:w-5';
/** The mark grows with the box: the key control's step matches the height's. */
const GLYPH_22 = '[&>svg]:h-[1.375rem] [&>svg]:w-[1.375rem]';

function glyphSize(shape: 'square' | 'besideWords', height: 'btn' | 'key'): string {
  if (shape === 'square') return height === 'key' ? GLYPH_20 : GLYPH_16;
  return height === 'key' ? GLYPH_22 : GLYPH_20;
}

/**
 * hue overrides `kind`'s own colour entirely, the same relationship
 * NeutralSwitch's hue prop has to its neutral fill: the button becomes an
 * accent-filled control (`bg-accent`/`text-accentContrast`, same as `kind:
 * "primary"`), and `.glim-hue` then redefines those two custom properties to
 * this button's own position colour under rainbow mode - see index.css's own
 * comment on `.glim-hue` for why no separate hued style rules are needed:
 * anything already painted with `var(--accent)` picks the position colour up
 * for free. Inert when rainbow mode is off, same as everywhere else `hue` is
 * used - the single global accent applies exactly as `kind: "primary"` would.
 */
export function Button({
  kind = 'primary',
  icon,
  children,
  className = '',
  hue,
  labelled,
  keyControl = false,
  title,
  ...rest
}: {
  kind?: ButtonKind;
  icon?: ReactNode;
  hue?: number;
  /**
   * Opts a glyph-only button into the Beschriftung setting - see IconBadge's
   * own `labelled` for the full reasoning. Used by the head card's transport
   * buttons, which have a title and no children.
   */
  labelled?: boolean;
  /**
   * The SECOND height (`--btn-h-key`, 2.5rem), and the only other one there is.
   *
   * It belongs to the button a surface exists FOR: the control that creates the
   * thing the page lists (Add account, New category, Add feed), or one whose
   * press is hard to undo. Everything else stays at `--btn-h`, because that is
   * what the fields beside it measure - raising the ordinary height to make one
   * control bigger puts every button in the app out of line with every field to
   * solve a problem two controls have.
   *
   * Two, and not three: the transport row's own 48px was argued for on its own
   * merits at its own call site, which is exactly how a house ends up with a
   * ladder. That exemption is withdrawn - 48px becomes this height, since a
   * key control is what those three buttons are.
   *
   * A key control standing in a row of fields has to be centred against them,
   * so the row it sits in carries `items-center`. This component cannot do that
   * for its own parent.
   */
  keyControl?: boolean;
} & ButtonHTMLAttributes<HTMLButtonElement>) {
  const labelMode = useNavLabels();
  // Only ever fills in for a button that HAS no children of its own: a
  // labelled button already says what it does, and appending its own tooltip
  // to it would say it twice.
  const fallback =
    labelled && !children && title && (labelMode === 'text' || labelMode === 'both') ? title : undefined;
  const body = children ?? fallback;
  const hideIcon = labelled && labelMode === 'text' && !!fallback;
  const iconOnly = !!icon && !body;
  const hued = hue !== undefined;
  // ONE CONTROL, ONE TOOLTIP MECHANISM - the same move IconBadge below already
  // made, for the same reason and now in the same place. `title` used to travel
  // on through `rest` to the DOM, so a button showed the operating system's own
  // unstyled box at the pointer while the badge beside it showed the house
  // bubble at the trigger: two mechanisms, one row, and the difference reads as
  // a rendering fault. Pulled out of the props here, it never reaches the
  // element and there is only the one bubble left.
  const tip = useTooltip<HTMLButtonElement>(title);
  // role/tabIndex dropped for the reason spelled out at IconBadge's own copy of
  // this line: a <button> already has both, and the "note" role would tell a
  // screen reader this is a description rather than a control.
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <button
        className={`inline-flex items-center justify-center gap-2 rounded-[var(--radius-control)] text-sm font-medium
          transition duration-150 select-none disabled:opacity-35 disabled:pointer-events-none
          motion-safe:active:scale-[.98] ${keyControl ? 'glim-btn-key' : BTN_H}
          ${iconOnly ? (keyControl ? 'w-[var(--btn-h-key)] px-0' : 'w-[var(--btn-h)] px-0') : 'px-3.5'}
          ${glyphSize(iconOnly ? 'square' : 'besideWords', keyControl ? 'key' : 'btn')}
          ${hued ? 'glim-hue bg-accent text-accentContrast hover:opacity-90' : kindClass[kind]} ${className}`}
        style={hued ? (hueVars(rainbowAt(hue)) as CSSProperties) : undefined}
        // A glyph-only button drew its accessible name from the `title`
        // attribute, and that attribute is gone now - so the name is stated.
        // Before the spread, never after, so a call site that passes its own
        // aria-label still wins.
        aria-label={iconOnly && title ? title : undefined}
        {...(title ? tipHoverProps : undefined)}
        {...rest}
      >
        {!hideIcon && icon}
        {body}
      </button>
      {title && tip.node}
    </>
  );
}

/**
 * ONE COLOURING, AND NO LEVER BESIDE IT.
 *
 * A bin badge takes the colour its siblings take, for the same reason
 * ButtonKind has no 'danger' above. This one had a second failure on top of
 * the rule, worth keeping because it explains why the variant was never right
 * here: every delete badge already passes `hue`, so the tile was in the colour
 * engine, and `kind="danger"` painted over it. Under rainbow that produced a
 * hue-tinted tile with a red glyph in it, because .glim-tint-badge sets only
 * the box-shadow and left text-statusFail standing.
 *
 * What stood here until now was the SWITCH that used to choose between them: a
 * union with one member, a record with one entry, and a `kind` prop no call
 * site in the app passed. That is the shape 1.13.0 went after - a lever that
 * decides nothing is deleted rather than hollowed out, because from the outside
 * it looks like a choice somebody forgot to make, and it is an invitation to
 * add the second member back one convincing call site at a time. The reasoning
 * above is worth keeping; the switch is not, so it is a constant now.
 */
const iconBadgeClass = 'bg-carbon-surface2 text-carbon-textSub hover:bg-carbon-surface3 hover:text-carbon-text';

/**
 * IconTile is IconBadge's inert twin: the same square, the same `--btn-h`
 * footprint, the same hue wash - but a `<span>`, because it marks a row
 * rather than doing anything when pressed.
 *
 * It exists so a decorative glyph can still be a badge. A bare glyph beside
 * a list row reads as an unfinished control (jdp, 2026-08-27, on the API
 * token list: "Angelegte API Token haben ein ganz komisches icon. Können
 * wir das ein schönes icon nehmen und es in ein quadratischen badge
 * einpflegen?"), while an IconBadge there would be a button that ignores
 * clicks - worse than either. Same size token as every other square badge
 * in the app, deliberately: they share one size regardless of role.
 */
export function IconTile({
  icon,
  hue,
  className = '',
}: {
  icon: ReactNode;
  hue?: number;
  className?: string;
}) {
  const hued = hue !== undefined;
  return (
    <span
      aria-hidden
      className={`flex ${BTN_SQUARE} shrink-0 items-center justify-center rounded-[var(--radius-control)]
        ${GLYPH_16} bg-carbon-surface2 text-carbon-textSub ${hued ? 'glim-tint-badge' : ''} ${className}`}
      style={hued ? (hueVars(rainbowAt(hue)) as CSSProperties) : undefined}
    >
      {icon}
    </span>
  );
}

/**
 * LabelBadge is the text-carrying member of the same family: one line of
 * label, optionally an InfoBubble, at the shared badge height.
 *
 * Two colourings, and which one applies is a question about the content and
 * not about taste. `hue` puts it in the rainbow engine like every other
 * badge (jdp, 2026-08-27: "der 'wie funktioniert das' badge in die
 * farbengine aufnehmen"). `tone` paints it in a status colour instead, for
 * a badge whose whole job is to report a state - a connection that is up or
 * down is green or red in every colour scheme, and running that through the
 * rainbow would make the palette decide what "connected" looks like.
 *
 * The status variant carries no separate dot (jdp, same message: "Der
 * Verbunden badge soll keinen runden punkt haben sondern selbst entweder
 * grün oder rot eingefärbt sein") - the badge IS the indicator, and a dot
 * inside a coloured badge says the same thing twice.
 */
export function LabelBadge({
  label,
  tip,
  hue,
  tone,
  onClick,
}: {
  label: string;
  tip?: ReactNode;
  hue?: number;
  tone?: 'ok' | 'fail';
  onClick?: () => void;
}) {
  const hued = hue !== undefined && !tone;
  const toneClass =
    tone === 'ok'
      ? 'bg-statusOkBg text-statusOk'
      : tone === 'fail'
        ? 'bg-statusFailBg text-statusFail'
        : 'bg-carbon-surface2 text-carbon-textSub';
  const Tag = onClick ? 'button' : 'span';
  return (
    <Tag
      {...(onClick ? { type: 'button' as const, onClick } : {})}
      // The hover is an opacity step and not a rung of the surface ramp, and
      // this badge is the case that cannot take a rung: it wears three
      // different fills (the neutral surface2, a status wash, a hue wash) and
      // one hover has to answer for all three. Only the neutral one has a tier
      // above it, the status pair would lose the state it exists to report, and
      // the hue wash is painted as an inset box-shadow - a background utility
      // sits UNDER that and changes nothing at all. `hover:brightness-110` was
      // the previous answer, and it is the value rule 21 rules out by name: one
      // direction, two themes that need opposite ones.
      className={`inline-flex ${BTN_H} shrink-0 items-center gap-1.5 rounded-[var(--radius-control)] px-3
        text-[11px] font-medium transition duration-150
        ${hued ? 'glim-tint-badge bg-carbon-surface2 text-carbon-textSub' : toneClass}
        ${onClick ? 'motion-safe:active:scale-[.98] hover:opacity-80' : ''}`}
      style={hued ? (hueVars(rainbowAt(hue)) as CSSProperties) : undefined}
    >
      <span className="whitespace-nowrap">{label}</span>
      {tip && <InfoBubble tip={tip} label={label} />}
    </Tag>
  );
}

/**
 * A small square colour tile around one glyph - the shape a cluster of
 * icon-only actions (a row's hover controls, a header's small utility
 * buttons) reads as, distinct from `Button`'s own icon-only mode, which
 * stays transparent until hovered and reads as bare floating glyphs rather
 * than a control (jdp, on Rules.tsx's row actions specifically: "die icons
 * die bei mouseover auf die regel erscheinen sind nicht im Glimstone. das
 * sollen farbige quadratischen badges mit icon sein"). The square is
 * `--btn-h` on both axes, which is the same 32px the sibling apps' own icon
 * badges measure (BombVault's Settings.tsx) and the same number every other
 * control in this file now reads - the token, not a repeated `h-8 w-8`.
 *
 * `hue` opts a badge into the rainbow palette the same way Button and
 * SectionTitle already do (jdp: "Bitte alle quadratischen badges in die
 * farbengine aufnehmen. Die icons sollen keine farbe haben sondern nur der
 * badge selbst") — and WHICH class carries it depends on whether this badge
 * has a state of its own to read `--accent` through:
 *
 *   no `active`  `.glim-tint-badge`, the stronger at-rest wash index.css's own
 *                doc comment describes for exactly this case. A one-shot
 *                action badge is a compact, isolated square that never
 *                becomes anything, so without a wash it would show no colour
 *                at all.
 *   `active`     `.glim-hue` ALONE, unconditionally, and no tint class of any
 *                kind ("an icon-only TOGGLE badge carries ONLY `.glim-hue`").
 *                At rest it shows nothing; pressed it fills solid with its
 *                own position colour, because `.glim-hue` has already
 *                rebound `--accent` for the subtree. That is the whole
 *                reason the class has to be unconditional rather than added
 *                while checked - added late there would be no `--accent` for
 *                the fill to resolve at the moment it is needed. The wash was
 *                tried on this exact control twice and rejected both times
 *                ("die iconbadges sollen nicht eingefärbt sein und erst wenn
 *                man sie klickt. diese halb abgedunkelt eingefärbt sein
 *                gefällt mir nicht").
 *
 * Deliberately NOT `.glim-hue-icon` in either case: that class colours the
 * glyph itself, which is the one thing this request asks to keep neutral —
 * only the tile takes the hue, the icon stays `currentColor` from
 * `iconBadgeClass` regardless.
 */
export function IconBadge({
  icon,
  hue,
  active,
  labelled,
  quiet,
  className = '',
  style,
  title,
  ...rest
}: {
  icon: ReactNode;
  hue?: number;
  /**
   * Marks a TOGGLE badge (a filter switching on/off, not a one-shot action)
   * as currently engaged — added for Collector.tsx's "Nicht prüfbar" /
   * "Ungeprüft" filters, jdp 2026-08-25: moved off ListToolbar's text-chip
   * strip into this same badge row. Left `undefined` by every other call
   * site in the app (a one-shot action has no pressed state to report), so
   * `aria-pressed` is only ever rendered where a caller opts in — an action
   * button staying a plain button, not silently becoming a toggle button
   * for assistive tech everywhere else this component is already used.
   *
   * ENGAGED IS A FULL FILL, NOT A HALO, and the halo that stood here is the
   * reason this prop also decides the hue class above. A ring in the tile's
   * own current colour was argued for on the grounds that a fill would cost
   * the tile its own hue - it does not: `.glim-hue` rebinds `--accent` for
   * this element, so `bg-accent` on the pressed state IS the position colour
   * and nothing is lost. What the halo actually produced was the idle look the
   * language rules out by name, a 50%-wash tile at rest with a ring on top of
   * it, and it took two rounds of rejection before the wash came off. Pressed
   * fills (`bg-accent`/`text-accentContrast`, plus `.glim-active` so reactive
   * mode keeps the colour on the one badge that is on), idle is the plain
   * neutral tile every other badge rests as.
   */
  active?: boolean;
  /**
   * Opts this badge into the Beschriftung setting (lib/navLabels.ts), the same
   * one the sidebar and the settings rail already follow.
   *
   * The setting existed and reached exactly two places, which is why it read as
   * a sidebar option rather than as a rule about the app (jdp, 2026-09-07: "die
   * beschriftungsengine ist nicht vollständig. Das fehlen zwei kategoriern",
   * and, asked which: the head bar, the list toolbars and the collector's own
   * buttons). Those three now opt in.
   *
   * IT IS OPT-IN, AND THAT IS A DEVIATION THIS FILE NO LONGER DEFENDS.
   *
   * The argument that stood here was that a badge in a table row cannot grow
   * into a labelled button without setting the width of its column, so a row
   * action and a toolbar action are not the same thing. Rule 13 answers it
   * directly, and the answer is the one this app is the example for: what a
   * row action's SHAPE decides is the shape, not whether the words appear, and
   * "from outside, a documented exemption and a control that simply ignores the
   * setting look identical". It is measurable here - Downloads.tsx has sixteen
   * of these and every one is `labelled`, while the seven in the rows directly
   * underneath it (TaskList.tsx) are not, so with Beschriftung on "text and
   * glyph" one page labels its toolbar and not its rows.
   *
   * So the prop is on its way out rather than argued for: the end state is no
   * prop at all and every badge with a title answering the setting. It is not
   * deleted in this pass because deleting it is not a component change - 45
   * call sites currently rely on staying silent, and the row layouts under them
   * have to be able to hold three verbs per line before the words arrive.
   * Whoever does that pass removes this prop in the same move; nothing here
   * should be read as a reason to keep it.
   *
   * The label is `title`, which every one of these already carries as its
   * tooltip: one string per control, not a second one that can disagree with it.
   */
  labelled?: boolean;
  /**
   * No tile until somebody reaches for it. For a badge sitting ON A LIST ROW,
   * and nowhere else.
   *
   * The house rule is a filled square with a glyph, and it stays the rule: a
   * toolbar, a card header and a settings row all get the tile. A dense list is
   * where that turns against itself, because six of these on every row over
   * forty rows read as a wall of boxes rather than as the row's own actions.
   * The behaviour itself is not new - the reactive rainbow has always drawn
   * these badges this way - this only stops it depending on which colour mode
   * somebody happens to run. See .glim-badge-quiet.
   */
  quiet?: boolean;
} & ButtonHTMLAttributes<HTMLButtonElement>) {
  const hued = hue !== undefined;
  // A badge that REPORTS a checked state is a toggle, and a toggle is coloured
  // by its own state rather than by a wash - see this component's doc comment.
  // `active !== undefined` rather than `active`, because the two kinds have to
  // stay the same badge in both of its states: keyed on the truthy value, an
  // idle filter would wear the one-shot action's wash and only stop wearing it
  // once pressed, which is the half-darkened at-rest look this rule removed.
  const toggle = active !== undefined;
  // Always called (Rules of Hooks); what it returns only matters where a
  // caller opted in and gave a title to show.
  const labelMode = useNavLabels();
  const showText = labelled && !!title && (labelMode === 'text' || labelMode === 'both');
  // The glyph is only ever dropped where WORDS ARRIVE IN ITS PLACE, which is
  // the mirror of the rule the label engine already has. `!labelled ||
  // labelMode !== 'text'` said something subtly different: a badge that opted
  // in, carried no title and met the text mode lost its glyph and gained
  // nothing, and rendered as an empty box with its accessible name intact -
  // nothing throws, nothing goes red, and the mode that does it is the plainest
  // of the four. A glyph with no label beside it is not decoration to be
  // stripped; it is the label, drawn.
  const showIcon = !(labelMode === 'text' && showText);
  // A GlimStone bubble (useTooltip, InfoBubble's own sibling) rather than
  // the native `title` attribute every call site here used to pass
  // straight through to the DOM (jdp, 2026-08-26: "Alle hoover infobubbles
  // sind nicht im Glimstone format!") - fixed once, at the root, so the
  // dozens of existing IconBadge call sites across the app pick it up
  // without themselves changing: they already pass `title`, which now
  // becomes the bubble's own content instead of the browser's unstyled
  // tooltip box. The hook always runs (Rules of Hooks), but its trigger
  // props and its portal node are only wired up while there is a real
  // title to show - a badge with none stays exactly as inert as before.
  const tip = useTooltip<HTMLButtonElement>(title);
  // role/tabIndex stripped back out: triggerProps was built for InfoBubble's
  // own plain, otherwise-inert <div>, which needs both to become reachable
  // and nameable at all. A button already has a real role and is already
  // focusable - keeping triggerProps' "note" role here would tell a screen
  // reader this is no longer a button, only a description, silently taking
  // away every one of these badges' own click semantics.
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <button
        type="button"
        aria-pressed={active}
        // A badge showing text is no longer square: it keeps its height and
        // takes the width its words need. The fixed width is therefore
        // conditional, and the padding only appears with the text it is there
        // to hold. The glyph follows the same fork - 16px alone in the square,
        // 20px once there are words beside it to read as one control with.
        className={`flex ${BTN_H} shrink-0 items-center justify-center gap-1.5 rounded-[var(--radius-control)]
          transition duration-150 select-none disabled:opacity-35 disabled:pointer-events-none
          motion-safe:active:scale-[.98]
          ${showText ? `px-2.5 text-xs font-medium ${GLYPH_20}` : `w-[var(--btn-h)] ${GLYPH_16}`}
          ${hued ? (toggle ? 'glim-hue' : 'glim-tint-badge') : ''} ${quiet ? 'glim-badge-quiet' : ''}
          ${toggle && active ? 'glim-active bg-accent text-accentContrast hover:opacity-90' : iconBadgeClass} ${className}`}
        style={hued ? { ...(hueVars(rainbowAt(hue)) as CSSProperties), ...style } : style}
        // The name a glyph-only badge would otherwise not have: `title` is
        // pulled out of the props for the bubble and never reaches the DOM, so
        // it cannot act as the accessible name the way a native tooltip does.
        // Stated before the spread, so a call site's own aria-label still wins.
        aria-label={!showText && title ? title : undefined}
        {...(title ? tipHoverProps : undefined)}
        {...rest}
      >
        {showIcon && icon}
        {showText && <span className="whitespace-nowrap">{title}</span>}
      </button>
      {title && tip.node}
    </>
  );
}

// One caption, so a Field and a FieldGroup cannot drift apart: they are the same
// row of words with the same (i) beside it, and the only difference between them
// is which element wraps the control underneath.
const FIELD_SHELL = 'flex flex-col gap-1.5';
// The caption sits BESIDE the control instead of above it - opt-in (jdp:
// "Entpacken nach und Eingabefeld soll in einer Zeile sein", "[Wenn eine
// Datei schon da ist] soll ein horizontaler Selektor werden, in die gleiche
// Zeile wie der Text"), for a control that reads fine on one line rather
// than every Field/FieldGroup by default - most captions are long enough,
// or their control wide enough, that stacking is still the right call.
const FIELD_SHELL_ROW = 'flex flex-wrap items-center gap-3';

function Caption({ label, hint }: { label: string; hint?: string }) {
  return (
    // data-glim-label is the settings search's anchor, and it is here rather
    // than at the call sites because this one span is every Field and every
    // FieldGroup in the app - roughly 190 rows tagged by one line instead of 190
    // hand-written ids that would be wrong the first time somebody copied a row.
    // The value is the TRANSLATED caption, which is what the search resolves and
    // compares against; see pages/settings/jump.ts, which reads the property and
    // never builds a `[data-glim-label="…"]` selector out of it, because a
    // caption may contain a quote in any of 42 languages.
    <span data-glim-label={label} className="flex shrink-0 items-center text-xs text-carbon-textSub">
      {label}
      {hint && <InfoBubble tip={hint} />}
    </span>
  );
}

/**
 * Field pairs a label with ONE control. The explanation, when there is one, is
 * not printed under the control: it lives behind the (i) beside the label.
 *
 * A settings page whose every row carries two lines of grey prose is a page
 * nobody reads twice — the explanation is needed once and then costs vertical
 * space forever. Behind the bubble it is still one hover away, and still
 * reachable by keyboard and by screen reader.
 *
 * ONE control, and the word is load-bearing. This is a `<label>`, and a
 * `<label>` hands its clicks and its name to the first labelable thing inside
 * it — which is what makes clicking the word "Password" focus the password box,
 * and which is a trap the moment the control is a *set* of controls. Measured on
 * the live instance: the corner picker sat in a Field, so clicking the caption
 * "Corners" set the whole app back to round corners, and the first tab
 * announced itself as "Corners Applies to cards, buttons, tabs…" instead of
 * "Round". Nothing was broken about the tabs; they were simply inside the wrong
 * element, which no test can see. Use FieldGroup for a row of swatches, a tab
 * strip, a pair of buttons — anything where "the first control" is not the
 * answer.
 */
export function Field({
  label,
  hint,
  layout = 'stack',
  children,
}: {
  label: string;
  hint?: string;
  /** `'row'` puts the caption and the control on one line instead of
   *  stacking them - the control gets `flex-1` so it still fills the line
   *  the way it always filled the full width when stacked. */
  layout?: 'stack' | 'row';
  children: ReactNode;
}) {
  return (
    <label className={layout === 'row' ? FIELD_SHELL_ROW : FIELD_SHELL}>
      <Caption label={label} hint={hint} />
      {layout === 'row' ? <span className="min-w-0 flex-1">{children}</span> : children}
    </label>
  );
}

/**
 * FieldGroup is Field's caption over a SET of controls — identical to look at,
 * and deliberately not a `<label>`.
 *
 * It adds no `role` and no `aria-labelledby` of its own, because the things that
 * go in it already name themselves: `Tabs` puts its `label` on the tablist and
 * `SwatchRow` is a `role="group"` with the same. A second group around them
 * would announce the caption twice and give two names to one idea — the same
 * mistake in the accessibility tree that a nested card is in the layout.
 */
export function FieldGroup({
  label,
  hint,
  layout = 'stack',
  children,
}: {
  label: string;
  hint?: string;
  /** `'row'` puts the caption beside the control set instead of above it -
   *  unlike Field's own row mode, the children stay their own natural
   *  width rather than stretching (a tab strip or swatch row is meant to
   *  hug its content, the way the Look page's Akzentfarbe row already
   *  does, not fill whatever's left on the line). */
  layout?: 'stack' | 'row';
  children: ReactNode;
}) {
  return (
    <div className={layout === 'row' ? FIELD_SHELL_ROW : FIELD_SHELL}>
      <Caption label={label} hint={hint} />
      {children}
    </div>
  );
}

/**
 * InfoBubble is the one way GlimStone explains something in place: a neutral
 * (i) that opens a bubble on hover or focus.
 *
 * The bubble is rendered into <body> rather than next to the icon. Anchored
 * locally it is at the mercy of every scroll container, card and table it sits
 * inside — one `overflow: hidden` anywhere above and the explanation is a
 * sliver. At body level it is clipped by nothing, and the position is measured
 * from the icon each time it opens.
 *
 * The icon is deliberately never the accent colour: it is furniture, and the
 * accent means activity.
 */
export function InfoBubble({
  tip,
  label,
  className = '',
  onColor = false,
}: {
  tip: ReactNode;
  /**
   * The accessible name for the trigger. Optional because most callers pass
   * a plain sentence as `tip`, which doubles as its own label - only a
   * caller whose `tip` is structured content (e.g. a stacked ordered list of
   * steps) needs to supply this separately, since that content is no longer
   * a single readable string.
   */
  label?: string;
  className?: string;
  /**
   * True when this bubble sits on a filled, coloured surface (a card
   * title's own notch badge, whose fill can be the flat accent OR any
   * rainbow position) rather than the page's own neutral ground - the
   * fixed muted-grey trigger can read as nearly invisible against some of
   * those fills, or blend into others entirely. Reads `currentColor`
   * instead, so it always inherits whatever contrast-ink colour the
   * surface it sits on already resolved for its own text (jdp: "die
   * Infobubbles auf den Cardtitelbadges müssen ihre Farbe je nach
   * Badgefarbe flippen, damit sie immer gut sichtbar sind").
   */
  onColor?: boolean;
}) {
  const [shown, setShown] = useState(false);
  const [at, setAt] = useState<{ left: number; top: number } | null>(null);
  const ref = useRef<HTMLSpanElement>(null);
  const bubble = useRef<HTMLSpanElement>(null);

  // The same measure-then-place pass useTooltip's own `place()` below makes,
  // out of the one shared function so the two cannot drift: both call sites of
  // this bubble in this app went through the identical clip bug once already.
  // A layout effect rather than the mouse handler, because the size being
  // measured is the bubble's REAL rendered one, which does not exist until it
  // has been put in the document - and it has to be read before the browser
  // paints, or the bubble is visibly in the wrong place for one frame.
  useLayoutEffect(() => {
    if (!shown) {
      setAt(null);
      return;
    }
    const r = ref.current?.getBoundingClientRect();
    const el = bubble.current;
    if (!r || !el) return;
    setAt(placeBubble(r, el.offsetWidth, el.offsetHeight));
  }, [shown]);

  // Escape closes it, because a bubble opened by keyboard has to be closable by
  // keyboard without moving focus somewhere else first. A pointerdown anywhere
  // closes it too: a press means somebody is acting rather than reading.
  useEffect(() => {
    if (!shown) return;
    const close = () => setShown(false);
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && close();
    const onScroll = () => close(); // a measured position goes stale the moment the page moves
    window.addEventListener('keydown', onKey);
    window.addEventListener('scroll', onScroll, true);
    document.addEventListener('pointerdown', close, true);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('scroll', onScroll, true);
      document.removeEventListener('pointerdown', close, true);
    };
  }, [shown]);

  return (
    <>
      <span
        ref={ref}
        role="note"
        tabIndex={0}
        aria-label={label ?? (typeof tip === 'string' ? tip : undefined)}
        onMouseEnter={() => setShown(true)}
        onMouseLeave={() => setShown(false)}
        onFocus={() => setShown(true)}
        onBlur={() => setShown(false)}
        className={`glim-info ms-1.5 inline-flex h-[15px] w-[15px] shrink-0 cursor-help items-center
          justify-center rounded-[var(--radius-pill)] align-middle transition-opacity ${
            onColor
              ? 'text-current opacity-80 hover:opacity-100 focus-visible:opacity-100'
              : 'text-carbon-textMuted hover:text-carbon-textSub focus-visible:text-carbon-textSub'
          } ${className}`}
      >
        <svg viewBox="0 0 16 16" width={15} height={15} aria-hidden focusable="false">
          <circle cx="8" cy="8" r="7.1" fill="none" stroke="currentColor" strokeWidth="1.2" />
          <circle cx="8" cy="4.7" r="1.05" fill="currentColor" />
          <rect x="7.05" y="6.8" width="1.9" height="5" rx=".95" fill="currentColor" />
        </svg>
      </span>
      {shown &&
        createPortal(
          <span
            ref={bubble}
            role="tooltip"
            dir="auto"
            className="glim-bubble glim-fade"
            // Hidden, not unrendered, for the one frame between mounting and
            // being measured: `visibility` still lays the bubble out, which is
            // what there is to measure, and it keeps the unplaced first frame
            // off the screen instead of flashing it at the top-left corner.
            style={{
              left: at?.left ?? 0,
              top: at?.top ?? 0,
              visibility: at ? undefined : 'hidden',
              maxWidth: TOOLTIP_MAX_WIDTH,
            }}
          >
            {tip}
          </span>,
          document.body,
        )}
    </>
  );
}

/**
 * .glim-bubble's own 280px, widened for the one caller that needs it:
 * InfoBubble's tip is one sentence, a row tooltip (below) is several
 * labelled fields stacked, and the extra 40px keeps a host name or a short
 * path off a second line without asking every bubble in the app to widen
 * with it - it is passed as an inline style, which wins over the class.
 */
const TOOLTIP_MAX_WIDTH = 320;

/**
 * WHERE A BUBBLE GOES. Never even partly clipped by the viewport, and that is
 * a requirement rather than a preference: a bubble rendering one pixel past an
 * edge has failed at the one thing it is for.
 *
 * The two numbers it is given are the bubble's REAL rendered width and height,
 * measured after it is in the document. Both call sites used to pass a
 * constant instead - a flat 320px "tall enough for the biggest tooltip this
 * build opens" - and the constant is the defect the rule names by hand: the
 * height depends on how many lines the tip wraps to, so a guess is wrong in
 * both directions at once. Too small and a long tip hangs off the bottom; too
 * large and a short one flips above a trigger that had room below it all along.
 *
 * Clamp, THEN flip, with an 8px margin, ported from the shared engine
 * (glimstone/reference/tooltip.ts's own `show()`) pixel for pixel rather than
 * re-derived. `left` is the bubble's CENTRE, because `.glim-bubble` carries
 * `transform: translateX(-50%)` - and `width: max-content` on that same class
 * is what makes measuring legitimate at all: without it a `position: fixed`
 * box with only `left` set resizes itself in response to the very `left` this
 * computes.
 *
 * Flips ABOVE only when opening below would clip the bottom edge AND there is
 * genuinely room up there. The `else` branch this replaces flipped
 * unconditionally, so a trigger near the top of a short window traded a
 * clipped bottom for a clipped top.
 */
function placeBubble(r: DOMRect, w: number, h: number): { left: number; top: number } {
  const vw = document.documentElement.clientWidth || window.innerWidth;
  const vh = document.documentElement.clientHeight || window.innerHeight;
  const cx = r.left + r.width / 2;
  const left = Math.max(8 + w / 2, Math.min(vw - 8 - w / 2, cx));
  const above = r.bottom + 8 + h > vh && r.top - 8 - h >= 0;
  return { left, top: above ? r.top - 8 - h : r.bottom + 8 };
}

/** How long a hover has to hold still before the bubble opens. */
const TOOLTIP_DELAY_MS = 400;

export interface TooltipHandle<T extends HTMLElement> {
  /** Spread onto the element the tooltip is ABOUT. */
  triggerProps: {
    // RefObject<T | null>, matching what useRef<T>(null) actually returns
    // under React 19's types - they stopped pretending a ref initialised to
    // null holds a T before it is attached.
    ref: RefObject<T | null>;
    tabIndex: number;
    role: string;
    onMouseEnter: () => void;
    onMouseLeave: () => void;
    onFocus: () => void;
    onBlur: () => void;
    'aria-describedby': string | undefined;
  };
  /** Render once, anywhere in the tree - it portals to <body> on its own, and is null while closed. */
  node: ReactNode;
}

/**
 * useTooltip is InfoBubble's sibling for the other half of "explain this
 * further". InfoBubble is a dedicated (i) glyph built to be hovered, so a
 * settings form can afford to open it the instant the pointer arrives and
 * say one sentence. What this hook attaches to is content that is already on
 * screen for its own reason - a file name in a table row - and a pointer
 * crosses dozens of rows a second while scrolling or just reading down the
 * list, so opening on every one of them for the eye-blink before it moves on
 * is noise, not help. It therefore opens after a short hold rather than on
 * arrival, takes a whole panel of content rather than one string, and picks
 * which side of the trigger to open on rather than always landing below - a
 * row can be anywhere in a tall scrolling list, where an info bubble is
 * reliably near the top of a short settings page. That set of differences is
 * the reason this is a second primitive and not InfoBubble reused: none of
 * them can be expressed by passing InfoBubble a different prop.
 *
 * What it keeps from InfoBubble on purpose, because the same failure would
 * only repeat itself otherwise: rendered through a portal into <body>, so a
 * table's own `overflow-x-auto` cannot clip it the way it would clip
 * anything positioned inside the scrolling table itself; Escape, a scroll and
 * a pointerdown all close it, because a position measured off the trigger goes
 * stale the moment the page moves under it and because a press means somebody
 * is acting rather than reading.
 */
export function useTooltip<T extends HTMLElement = HTMLElement>(content: ReactNode): TooltipHandle<T> {
  const id = useId();
  const ref = useRef<T>(null);
  const bubble = useRef<HTMLSpanElement>(null);
  const [shown, setShown] = useState(false);
  const [at, setAt] = useState<{ left: number; top: number } | null>(null);
  const openTimer = useRef<number | undefined>(undefined);

  // Measured, then placed - see placeBubble above for why the constant this
  // used to guess the height with could not be right in both directions at
  // once. The measurement has to happen in a layout effect: the bubble has no
  // rendered size until it is in the document, and reading it any later than
  // this would show one painted frame in the wrong place.
  useLayoutEffect(() => {
    if (!shown) {
      setAt(null);
      return;
    }
    const r = ref.current?.getBoundingClientRect();
    const el = bubble.current;
    if (!r || !el) return;
    setAt(placeBubble(r, el.offsetWidth, el.offsetHeight));
  }, [shown]);

  const open = useCallback(() => {
    window.clearTimeout(openTimer.current);
    openTimer.current = window.setTimeout(() => setShown(true), TOOLTIP_DELAY_MS);
  }, []);

  const show = useCallback(() => {
    window.clearTimeout(openTimer.current);
    setShown(true);
  }, []);

  const close = useCallback(() => {
    window.clearTimeout(openTimer.current);
    setShown(false);
  }, []);

  // Unmounting mid-hold must not fire the timer into a row that is gone - the
  // table repaints on every websocket tick, so a row under the pointer
  // disappearing before its own open timer fires is routine, not an edge case.
  useEffect(() => () => window.clearTimeout(openTimer.current), []);

  useEffect(() => {
    if (!shown) return;
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && close();
    const onScroll = () => close(); // a measured position goes stale the moment the page moves
    window.addEventListener('keydown', onKey);
    window.addEventListener('scroll', onScroll, true);
    // A press means the person is acting, not reading - the same third
    // dismissal the shared engine has, and the one both bubbles here were
    // missing, so a click on the trigger left its own tip standing over it.
    document.addEventListener('pointerdown', close, true);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('scroll', onScroll, true);
      document.removeEventListener('pointerdown', close, true);
    };
  }, [shown, close]);

  return {
    triggerProps: {
      ref,
      tabIndex: 0,
      // Matches InfoBubble's own role - a plain tabbable <div> with neither
      // has no accessible role at all, and reads to a screen reader as a
      // mystery stop. Unlike InfoBubble's bare glyph this trigger already
      // wraps its own visible text (a row's name/URL), which is what
      // supplies the accessible name here - InfoBubble has none of its own
      // and needs an explicit aria-label instead.
      role: 'note',
      onMouseEnter: open,
      onMouseLeave: close,
      // Focus opens at once rather than after the hold: a keyboard user has
      // already arrived on purpose, and the delay exists only to filter a
      // POINTER passing through on its way somewhere else.
      onFocus: show,
      onBlur: close,
      'aria-describedby': shown ? id : undefined,
    },
    node:
      shown && content
        ? createPortal(
            <span
              ref={bubble}
              id={id}
              role="tooltip"
              dir="auto"
              className="glim-bubble glim-fade"
              // Laid out but invisible until it has been measured and placed -
              // see InfoBubble's own copy of this for why `visibility` is the
              // one that can be measured.
              style={{
                left: at?.left ?? 0,
                top: at?.top ?? 0,
                visibility: at ? undefined : 'hidden',
                maxWidth: TOOLTIP_MAX_WIDTH,
              }}
            >
              {content}
            </span>,
            document.body,
          )
        : null,
  };
}

/**
 * A segmented control picks exactly one of a few (the download filter, the
 * corner picker). The chosen segment is FILLED with the accent — the same
 * treatment as the active nav item, so "this is the one that is on" reads
 * identically everywhere instead of being a surface tint here and a rail there.
 *
 * `Tabs` in components/Tabs.tsx is built from these three strings rather than
 * from lookalikes of them, so a tab, a filter chip and a segment cannot drift
 * apart: there is one treatment, and it is defined here.
 */
export const segBase = 'rounded-[var(--radius-control)] font-medium transition-colors';
export const segOn = 'bg-accent text-accentContrast';
// bg-carbon-surface2 at rest, not transparent: the previous "transparent at
// rest" call here was matching a BombVault test container that was itself
// running a stale build (see KnightLoader's own changelog, "der
// BV-Testcontainer läuft eine ÄLTERE Version als das Repo/GlimStone-Doku").
// GlimStone's design-language.md now states this explicitly ("Every tab is
// a badge, not just the selected one... an early build read this rule as
// bare-until-selected... the resulting strip looked unfinished"), and
// BombVault's own current source carries the fix. jdp, on KnightLoader
// specifically: "auch im nicht ausgewählten zustand sollen sie als badges
// erkennbar sein, siehe BV."
export const segOff = 'bg-carbon-surface2 text-carbon-textMuted hover:bg-carbon-surface3 hover:text-carbon-text';

/**
 * hueStyle is how anything that is one member of a set claims a palette
 * position: the element carries `glim-hue` and gets these inline properties.
 *
 * It exists so the class and the properties are never separated — `.glim-hue`
 * on an element with no `--item-hue` under it resolves the accent to nothing at
 * all. Pass the item's index in its list; positions come from position, never
 * from a hash of an id (see the design language). When rainbow is off the
 * properties are inert, so a component may set them unconditionally.
 *
 * It reads the live palette during render, which means the component calling it
 * must also subscribe with `useRainbow()` — otherwise it keeps whatever colours
 * were current when it last rendered for some other reason, and editing a
 * swatch appears to do nothing until the page is touched. `Tabs` does this for
 * its callers; a component hueing its own rows does it itself.
 */
export function hueStyle(index: number | undefined): CSSProperties {
  if (index === undefined) return {};
  return hueVars(rainbowAt(index)) as CSSProperties;
}

/**
 * Swatch and SwatchRow USED TO BE HERE, and they are gone rather than resized.
 *
 * They were the shared colour square: a preset with a halo on the chosen one, a
 * row that kept whatever ended it. pages/settings/Look.tsx has since built its
 * own - a pill with the badge's height, because two rows of circles in one card
 * have to be one size or they read as two different kinds of thing - and
 * nothing imported these two any longer.
 *
 * An audit found them short at 28px against the house number, and resizing them
 * would have been the wrong repair: what was actually wrong is that a component
 * nobody uses was still being measured against a rule, and the comment above it
 * still described a row it no longer belonged to. This file has spent this round
 * deleting levers that decide nothing rather than gutting them, for the reason
 * that a thing which still exists comes back. Same answer here.
 *
 * If a second page ever needs a colour square, Look.tsx's is the one that
 * matches the rule, and lifting it here is a smaller job than reviving this was.
 */

// `py-1.5`, and the one and a half is the whole reason rule 19 can hold: a
// 14px line box is 20px tall, plus 12px of padding, which is the 2rem
// `--btn-h` names. The field measured 36px here and every button beside it
// now measures 32, and a four-pixel difference between neighbours is the
// thing GlimStone has the bug report for. The number stays written as padding
// rather than as a height because TextArea shares this string and a textarea
// that cannot grow is a worse defect than the one being fixed.
const inputClass =
  'w-full rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-1.5 text-sm text-carbon-text ' +
  'placeholder:text-carbon-textMuted outline-none transition-shadow ' +
  'focus:shadow-[0_0_0_2px_var(--focus-ring)]';

// className is pulled out and merged rather than left in `props`: JSX spread
// applies later props last, so `<input className={inputClass} {...props} />`
// let a caller's own className silently REPLACE the base look (padding,
// background, focus ring) instead of extending it - the one existing caller
// that passed one only ever added a width constraint, so the difference
// never showed, but it made every base style invisible to any prop that
// isn't width.
export function TextInput({ className = '', ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={`${inputClass} ${className}`} {...props} />;
}

export function TextArea(props: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className={`${inputClass} resize-y`} {...props} />;
}

/**
 * A stepper is part of the field, not a control beside it (GlimStone 1.6.0): no
 * ground of its own, only the ink changing on hover, and greyed out at its own
 * end of the range. A hover background here would re-import the exact property
 * that got the native spinner removed in the first place, one layer down.
 */
const STEPPER =
  'flex h-3.5 w-4 items-center justify-center text-carbon-textMuted transition-colors ' +
  'hover:text-carbon-text disabled:opacity-35 disabled:hover:text-carbon-textMuted';

/**
 * Solid triangles with rounded corners, not two-stroke chevrons (GlimStone
 * 1.6.0). An icon set made of filled shapes gets one outlined mark and it reads
 * as borrowed from somewhere else, at exactly the size where that is hardest to
 * miss. The rounding is a matched stroke plus stroke-linejoin rather than arcs
 * in the path: three corners, three radii, and the shape stays one triangle
 * anybody can read. The base path is inset by the stroke's half-width, so the
 * painted result lands where the sharp version did instead of growing.
 */
function Stepper({ up = false }: { up?: boolean }) {
  return (
    <svg viewBox="0 0 10 6" width={10} height={6} aria-hidden>
      <path
        d={up ? 'M5 1.7 L8.6 4.6 L1.4 4.6 Z' : 'M5 4.3 L1.4 1.4 L8.6 1.4 Z'}
        fill="currentColor"
        stroke="currentColor"
        strokeWidth="1.4"
        strokeLinejoin="round"
      />
    </svg>
  );
}

/**
 * The browser's own up/down spinner paints as one native widget under
 * `color-scheme: dark` (see :root in index.css) - a themed rounded box
 * behind the arrows that `background-color` on `::-webkit-inner-spin-button`
 * cannot strip, because Chromium renders that widget as a single image, not
 * a styleable box plus glyphs (jdp: "diese hoch und runterzähler haben einen
 * kleinen dunklen hintergrund ... bitte den entfernen"). The native spinner
 * is hidden outright (glim-num-hide-spin in index.css) and replaced with our
 * own two arrows in the same muted ink as everything else in this file, so
 * "no visible box" actually means no box, not a differently-coloured one.
 *
 * Once the native widget is gone, so is the wheel it answered, and the wheel is
 * the way people reach for a value they are dialling in rather than typing. It
 * is put back here rather than at a call site: one field in the whole app had
 * it (QueueBar's rate limit, written correctly and by hand), and 48 did not.
 */
export function NumberInput({
  value,
  onValue,
  min,
  max,
  step = 1,
  className = '',
  ...rest
}: {
  value: number;
  onValue: (n: number) => void;
  min?: number;
  max?: number;
  step?: number;
  className?: string;
} & Omit<InputHTMLAttributes<HTMLInputElement>, 'value' | 'onChange' | 'className'>) {
  const field = useRef<HTMLInputElement>(null);

  /**
   * THE ARROWS ARE THE FIELD'S OWN stepUp()/stepDown(), NOT ARITHMETIC BESIDE
   * THEM.
   *
   * What stood here was `onValue(clamp(value + step))` with a clamp() of its
   * own, which laid out `min` and `max` a second time - so the range existed
   * twice, in the attributes the field already carries and in a function next
   * to them, and the two could only ever agree by being edited together. The
   * browser's own stepper reads the attributes, snaps to the step grid from
   * the field's own base, and stops at both ends, which is the same behaviour
   * the arrow keys and the wheel already give; the copy could only ever
   * approximate it.
   *
   * The value is read back off the field rather than recomputed, so the
   * caller's onValue receives exactly what the field now holds - the same
   * number typing into it would have produced.
   */
  function nudge(dir: 'up' | 'down') {
    const el = field.current;
    if (!el) return;
    if (dir === 'up') el.stepUp();
    else el.stepDown();
    if (Number.isNaN(el.valueAsNumber)) return;
    onValue(el.valueAsNumber);
  }
  // The handler is attached once and reads the current props through this box,
  // rather than being torn down and rebuilt on every keystroke. A number field
  // re-renders on each character typed into it.
  const live = useRef({ value, step, min, max, onValue, disabled: rest.disabled, readOnly: rest.readOnly });
  useEffect(() => {
    live.current = { value, step, min, max, onValue, disabled: rest.disabled, readOnly: rest.readOnly };
  });

  /**
   * The wheel steps the value, but ONLY while the field has focus.
   *
   * That condition is the design and not a caution. A field that answers the
   * wheel on hover alone edits whatever a pointer happened to pass over on its
   * way down the page, which is exactly why browsers took the behaviour off the
   * native widget; requiring focus makes it the same deliberate gesture the
   * arrow keys already need. Up is more, matching the upper arrow and the up
   * key, and only the SIGN of the delta is read because a trackpad reports
   * fractions.
   *
   * A real listener with `{ passive: false }`, never React's `onWheel`: React
   * registers that one passive at the root, so `preventDefault` inside it does
   * nothing but log a warning - and without it the page scrolls the field out
   * from under the pointer while the number is still changing.
   */
  useEffect(() => {
    const el = field.current;
    if (!el) return;
    function onWheel(e: WheelEvent) {
      const s = live.current;
      if (s.disabled || s.readOnly) return;
      if (document.activeElement !== el) return;
      if (e.deltaY === 0) return;
      e.preventDefault();
      const next = s.value + (e.deltaY < 0 ? s.step : -s.step);
      const clamped = s.min !== undefined && next < s.min ? s.min : s.max !== undefined && next > s.max ? s.max : next;
      if (clamped !== s.value) s.onValue(clamped);
    }
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
  }, []);

  return (
    <span className="relative inline-block w-full">
      <input
        ref={field}
        type="number"
        className={`${inputClass} glim-num glim-num-hide-spin pr-7 ${className}`}
        value={value}
        min={min}
        max={max}
        step={step}
        onChange={(e) => onValue(Number(e.target.value))}
        {...rest}
      />
      <span className="absolute inset-y-0 right-1.5 flex flex-col justify-center gap-0.5">
        <button
          type="button"
          tabIndex={-1}
          aria-hidden
          disabled={max !== undefined && value >= max}
          onClick={() => nudge('up')}
          className={STEPPER}
        >
          <Stepper up />
        </button>
        <button
          type="button"
          tabIndex={-1}
          aria-hidden
          disabled={min !== undefined && value <= min}
          onClick={() => nudge('down')}
          className={STEPPER}
        >
          <Stepper />
        </button>
      </span>
    </span>
  );
}

/**
 * A password field with a reveal-eye toggle (jdp, 2026-08-26: "Im
 * passwort-eingabefeld fehlt das reveal auge um das passwort anschauen zu
 * können") - no such toggle existed anywhere in this codebase before this
 * was added. Shares NumberInput's own `relative` wrapper + `absolute`
 * overlay-button shape just above rather than reinventing it.
 *
 * `showLabel`/`hideLabel` are caller-supplied rather than resolved via
 * useT() in here, the same reason InfoBubble's own `label` prop is
 * caller-supplied: this file has no i18n dependency of its own, and every
 * caller already has the translated strings (`common.showPassword`/
 * `common.hidePassword`) at hand.
 *
 * `autoComplete` is required, not defaulted to "off" - see this component's
 * history: an early version hardcoded "off", which suppresses a password
 * manager's save/fill/strength prompts. Callers pass the real semantic
 * token their field needs ("current-password", "new-password", ...).
 */
export function PasswordInput({
  value,
  onChange,
  autoComplete,
  showLabel,
  hideLabel,
  autoFocus,
}: {
  value: string;
  onChange: (v: string) => void;
  autoComplete: InputHTMLAttributes<HTMLInputElement>['autoComplete'];
  showLabel: string;
  hideLabel: string;
  autoFocus?: boolean;
}) {
  const [reveal, setReveal] = useState(false);
  const label = reveal ? hideLabel : showLabel;
  // The house bubble, not the `title` attribute this button used to carry:
  // every other control in this file explains itself through useTooltip now,
  // and the one left showing the operating system's own box - at the pointer,
  // in a font no rule here reaches - is the one that reads as a fault.
  const tip = useTooltip<HTMLButtonElement>(label);
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <div className="relative">
      <TextInput
        type={reveal ? 'text' : 'password'}
        autoComplete={autoComplete}
        autoFocus={autoFocus}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        // Room for the reveal button, which is the field's own height wide:
        // the box is square, so its 16px glyph is half of it the way rule 13
        // asks - a 36px box around the same mark was not.
        className="pr-8"
      />
      {tip.node}
      <button
        type="button"
        // Stops the click from also refocusing/moving the caret through a
        // <label>-wrapped Field the way a plain click would - the toggle is
        // its own control, not a second way to focus the field.
        onMouseDown={(e) => e.preventDefault()}
        onClick={() => setReveal((r) => !r)}
        aria-label={label}
        {...tipHoverProps}
        className="absolute inset-y-0 right-0 flex w-[var(--btn-h)] items-center justify-center text-carbon-textMuted transition-colors hover:text-carbon-text"
      >
        {reveal ? <IconEyeOff width={16} height={16} /> : <IconEye width={16} height={16} />}
      </button>
    </div>
  );
}

/**
 * A switch with its words beside it.
 *
 * `label` is normally rendered. Set `hideLabel` where the heading above the
 * switch already says the same thing — the words then survive only as the
 * accessible name, so a screen reader still announces something other than
 * "switch", while the eye is not told twice. Never drop the label itself: a
 * bare switch with no name at all is a control nobody can describe.
 */
export function Toggle({
  checked,
  onChange,
  label,
  hideLabel = false,
  hue,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label: string;
  hideLabel?: boolean;
  /**
   * This switch's position in a list of switches sharing one card, the same
   * 0-based sequence SectionTitle/Tabs already carry (jdp, repeatedly:
   * "Alle Toggles, buttons, badges, selektoren immer in die Farbmodi
   * aufnehmen") - `bg-accent` on the "on" state was a flat, unconditional
   * fill with no rainbow position at all, so a card with three toggles on
   * showed one solid colour for all three instead of reading as three
   * distinct rows. Omit it for a lone switch with no siblings needing to be
   * told apart - the SectionTitle rule ("omit for the only one of its kind
   * on the page") applies the same way here.
   */
  hue?: number;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={hideLabel ? label : undefined}
      onClick={() => onChange(!checked)}
      className={`${hue !== undefined ? 'glim-hue' : ''} flex items-center gap-3 text-left text-sm text-carbon-text select-none`}
      style={hue !== undefined ? (hueVars(rainbowAt(hue)) as CSSProperties) : undefined}
    >
      <span
        className={`relative h-5 w-9 shrink-0 rounded-[var(--radius-pill)] transition-colors ${
          checked ? 'bg-accent' : 'bg-carbon-surface3'
        }`}
      >
        {/* left-0 is load-bearing: without it the knob starts from its static
            position, which a button's inherited text-align centres — the knob
            then slides out past the pill. Tailwind v4 also animates the
            `translate` property here, not `transform`. `bg-carbon-background`
            (not a fixed white) is the "opposite ground" trick — the knob is
            the page's own background sitting on the accent-coloured track,
            so it reads dark in dark mode and light in light mode instead of
            a permanent white dot (jdp: "Die Toggle Punkte sollen im Darkmode
            schwarz sein"). Matches BombVault's own Toggle.tsx thumb, the
            reference this pattern is ported from. */}
        <span
          className={`absolute left-0 top-0.5 h-4 w-4 rounded-[var(--radius-pill)] bg-carbon-background shadow-sm transition-[translate] duration-150 ${
            checked ? 'translate-x-4' : 'translate-x-0.5'
          }`}
        />
      </span>
      {!hideLabel && <span>{label}</span>}
    </button>
  );
}

/**
 * ToggleRow is a Toggle used on its own as a whole card row: the caption
 * flush left, the switch flush right on the SAME line - never the bare
 * `Toggle` (track-then-label, both glued together at the left edge) and
 * never a `FieldGroup` with the caption on its own line above a lone
 * hideLabel switch below it. Both of those read as "shifted" once you
 * compare them to every other left-started row on the same card (jdp:
 * "Die Toggles sollen immer rechts in der Card sein (systemweit! Merken!)
 * alles andere soll links bündig anfangen, ist jetzt teilweise nach
 * rechts verschoben") - found live on the Archive tab, but the same two
 * wrong shapes were repeated across most of the settings pages, so this
 * is the one place the row gets built now.
 */
export function ToggleRow({
  label,
  hint,
  checked,
  onChange,
  disabled = false,
  hue,
}: {
  label: string;
  hint?: string;
  checked: boolean;
  onChange: (v: boolean) => void;
  /**
   * Dims the label and the switch and blocks interaction.
   *
   * FOR A CONTROL THAT REPORTS ITS OWN STATE, AND NOT FOR A SUB-SWITCH.
   * A switch that is dead because a switch one row up is off is ABSENT, never
   * dimmed: it is something somebody can see, read and reach for that answers
   * nothing, and the reason it is dead sits in exactly the place nobody looks
   * once they have decided this row is the interesting one. Render it
   * conditionally instead. The bullet this replaces said the opposite ("a
   * control that vanishes teaches nobody what the mode can do"), and it was
   * reported often enough to be worth the reversal ("dieser abgeschaltet
   * toggle soll weg, das hab ich schon oft angesprochen").
   *
   * What is left for this prop is the row whose own state is the message: busy
   * while a request is in flight, or a setting this build genuinely cannot
   * offer. There the dimming reports something, and the (i) beside it is the
   * only thing that can say what.
   */
  disabled?: boolean;
  /** Passed straight through to the underlying Toggle - see its own doc
   *  comment. Omit for a lone switch with nothing beside it to distinguish. */
  hue?: number;
}) {
  return (
    <div className={`flex items-center justify-between gap-4 ${disabled ? 'pointer-events-none' : ''}`}>
      {/* THE DIMMING IS ON THE PARTS, NEVER ON THE ROW, and this is a trap
          rather than a preference: `opacity` composites a whole subtree, so a
          child cannot be less transparent than its parent. With the row
          carrying it, the (i) inside it rendered at 40% too - the one element
          that has to stay readable while everything around it recedes became
          the one element nobody could read, in precisely the state it exists
          for. The class moved onto the label word and the switch; the trigger
          between them stays at full strength.
          pointer-events is the same shape and was already solved that way:
          pointer-events-auto re-opens hit-testing for just this span, undoing
          the row-wide pointer-events-none above it - CSS pointer-events isn't a
          one-way lock, a descendant can switch itself back on. The Toggle is a
          sibling, still under the row's block, so the switch stays
          un-clickable either way. */}
      {/* data-glim-label for the same reason Caption above carries it: a
          ToggleRow draws its own caption inline rather than through Caption, so
          without this line every switch in the settings tree would be the one
          shape the search could find but never scroll to. */}
      <span data-glim-label={label} className="pointer-events-auto flex items-center gap-1.5 text-sm text-carbon-text">
        <span className={disabled ? 'opacity-40' : ''}>{label}</span>
        {hint && <InfoBubble tip={hint} />}
      </span>
      <span className={`flex ${disabled ? 'opacity-40' : ''}`}>
        <Toggle hideLabel label={label} checked={checked} onChange={onChange} hue={hue} />
      </span>
    </div>
  );
}

// Card is the one raised surface. Never nest it inside another Card.
export function Card({
  children,
  className = '',
  hover = false,
  padding = 'normal',
  hue,
}: {
  children: ReactNode;
  className?: string;
  hover?: boolean;
  /**
   * This card's place in the palette, and the reason it moved here from
   * SectionTitle (GlimStone 1.4.0: "the position belongs on the CONTAINER,
   * not on the one visible element inside it").
   *
   * `[data-rainbow] .glim-hue` rebinds --accent for a whole subtree, so a
   * card that owns its position hands it to its badge, its buttons, its
   * switch tracks and its focus ring at once. With the position on the badge
   * instead, only the badge was ever coloured, and everything else in the
   * card went on wearing the single accent while the mode was on - which is
   * how this codebase ended up with a hand-written rule extending the badge's
   * colour to a card hover. That rule was the missing container rule, written
   * out one case at a time.
   */
  hue?: number;
  /**
   * 'none' drops the default p-5 outright rather than relying on a caller's
   * own `className="... p-0"` to beat it - it never can. Tailwind's compiled
   * stylesheet orders same-property utilities by their own scale value
   * (`.p-0` before `.p-5`, confirmed in dist/assets/index.css: p-0 at byte
   * offset 21089, p-5 at 21332), not by where they appear in a className
   * string, and CSS resolves a same-specificity tie in favour of whichever
   * rule comes LAST in the stylesheet - so p-5 always won regardless of
   * which order a caller wrote them in. That silently kept every
   * `<Card className="p-0">` at the full 20px padding underneath, which is
   * exactly what broke Shortcuts.tsx's per-group SectionTitle badges: the
   * badge has no left/right of its own (only `top`), so its horizontal
   * position falls out of normal flow (the CSS "static position" rule) -
   * doubling the padding (Card's own unremoved p-5, THEN the group's inner
   * `p-5 pb-0` wrapper on top of it) pushed every group's badge 20px right
   * of where the page-header Card's badge sits, since that one Card never
   * attempted this override and so never doubled up (jdp, 2026-08-24:
   * "alle cardtitelbadges sind zu weit rechts außer der der ersten card").
   * Advanced.tsx and Diagnostics.tsx carried the identical latent bug from
   * the identical `className="p-0"` pattern, just not visibly misaligned
   * since neither page puts a correctly-padded SectionTitle beside the
   * doubled one to compare against.
   */
  padding?: 'normal' | 'none';
}) {
  return (
    <div
      // The trailing space lives INSIDE the string, never after the
      // interpolation: an expression glued to the class before it fuses into
      // one nonsense name and silently drops both.
      className={`glim-card ${hue !== undefined ? 'glim-hue ' : ''}${padding === 'normal' ? 'p-5' : ''} ${
        hover ? 'transition-transform duration-150 motion-safe:hover:-translate-y-0.5' : ''
      } ${className}`}
      style={hue !== undefined ? (hueVars(rainbowAt(hue)) as CSSProperties) : undefined}
    >
      {children}
    </div>
  );
}

/**
 * PageHeader opens a page with the one line the navigation cannot carry.
 *
 * The title is deliberately NOT rendered: the sidebar entry for this page is
 * already highlighted and already says the same word, and repeating it costs a
 * whole heading of vertical space to tell the reader something they just
 * clicked. It stays in the props because it is the page's accessible name.
 */
export function PageHeader({
  title,
  subtitle,
  right,
}: {
  title: string;
  subtitle?: string;
  right?: ReactNode;
}) {
  // WITH NOTHING VISIBLE IN IT, THE WHOLE HEADER LEAVES THE LAYOUT.
  //
  // The title is sr-only by the rule above, so on a page that passes no
  // subtitle and no `right` this header renders an empty row - and an empty row
  // is not free. Every page that uses it sits in a `flex flex-col gap-6`, and a
  // flex child with no content still earns its 24px gap, which is why the
  // download list appeared to have a tall blank strip above it where nothing
  // was drawn (jdp: "im downloadtab ist die kopfzeile viel zu hoch").
  //
  // sr-only rather than `hidden`, and that is the whole trick: the heading has
  // to stay readable, it IS the page's accessible name. sr-only positions the
  // element absolutely, and an absolutely positioned child is not a flex item -
  // so it takes no row and earns no gap, while still being announced.
  const bare = !subtitle && !right;
  if (bare) {
    return (
      <header className="sr-only">
        <h1>{title}</h1>
      </header>
    );
  }

  return (
    <header className="flex items-center gap-4">
      <div className="min-w-0">
        <h1 className="sr-only">{title}</h1>
        {subtitle && <p className="text-carbon-textSub text-sm">{subtitle}</p>}
      </div>
      <span className="flex-1" />
      {right}
    </header>
  );
}

// One treatment for "there is nothing here", used everywhere so an empty app
// never looks broken — and, where it makes sense, offers the way out.
//
// `nested` swaps the raised .glim-card surface for the quieter .glim-well one
// - this component is called BOTH as a whole-page/whole-tab replacement (a
// bare glim-card is correct there, nothing else on screen to nest inside of)
// AND from inside an existing Card as that card's own "nothing here yet"
// state (Dashboard's Recent list, Scripts' script list, the hoster-login and
// debrid sections, an onboarding step's own Modal) - the second case was
// rendering a real .glim-card, with its own drop shadow and the SAME surface
// colour as its parent, inside another .glim-card, which index.css's own
// comment on .glim-card explicitly forbids ("never nest it") and which read
// as a visually unrelated floating box rather than a quiet inset section
// (jdp, live: "die cards in den cards haben einen schlagschatten und sind
// nicht heller eingefärbt... die sind optisch total anders").
export function EmptyState({
  icon,
  title,
  hint,
  action,
  nested,
}: {
  icon?: ReactNode;
  title: string;
  hint?: string;
  action?: ReactNode;
  nested?: boolean;
}) {
  return (
    <div className={`${nested ? 'glim-well' : 'glim-card'} flex flex-col items-center gap-2 p-10 text-center`}>
      {icon && <div className="text-carbon-textMuted/60">{icon}</div>}
      <div className="text-sm text-carbon-textSub">{title}</div>
      {hint && <div className="text-[11px] text-carbon-textMuted">{hint}</div>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}

/**
 * UnavailableNotice - what a card shows WHERE A CONTROL WOULD BE, when the
 * environment does not permit the thing the control would do (GlimStone
 * 1.15.0).
 *
 * There are three answers to a control that cannot act, and only one of them is
 * this component. The test question decides which:
 *
 *   - the grey state says something about the THING the control touches
 *     -> dim the control, it is reporting;
 *   - it says something about a decision taken elsewhere on the page
 *     -> leave the control out, no prose owed;
 *   - the ENVIRONMENT does not permit the thing at all
 *     -> leave the control out AND write the reason. That is this.
 *
 * Passkeys are the case that produced it: WebAuthn binds a credential to a
 * domain name, so a browser refuses the whole exchange on a bare IP address and
 * again on a certificate it does not trust, which is the DEFAULT state of a
 * self-hosted app opened over its LAN address. Offering a button there means
 * the browser answers with an error nobody can act on. Saying it first means
 * somebody reads one sentence and either fixes their setup or stops looking.
 *
 * Three things it deliberately does not do:
 *
 *   - it does not translate. Every string is a prop, including the title;
 *   - it does not know what a passkey is. It takes a reason and renders it;
 *   - it does not take the SERVER's sentence. The caller passes UI copy in the
 *     reader's own language and the server's verdict travels as a boolean.
 *     Promoting a diagnostic to be the paragraph that explains a feature is the
 *     specific mistake this shape exists to prevent, and
 *     web/check-passkey-reason.mjs is what keeps it prevented.
 *
 * Why a paragraph and not an info bubble, when every other explanation in this
 * app is a bubble: a bubble hangs off a control, and the entire point here is
 * that there is no control to hang one off.
 */
export function UnavailableNotice({
  title,
  reason,
  action,
}: {
  /** One line saying what is unavailable. Already translated. */
  title: string;
  /** Why, and ideally what would change it. Already translated. */
  reason: ReactNode;
  /**
   * Optional. Something the reader CAN do from here - never the control that
   * was refused, since offering that again is the button-that-fails this
   * component exists to remove.
   */
  action?: ReactNode;
}) {
  return (
    <div className="rounded-[var(--radius-card)] bg-statusWarnBgSoft px-4 py-3">
      <p className="text-sm font-medium text-carbon-text">{title}</p>
      <p className="mt-1 text-sm text-carbon-textSub">{reason}</p>
      {action && <div className="mt-3">{action}</div>}
    </div>
  );
}

// A quiet placeholder while a page's data is still on the wire. `nested`: see
// EmptyState's own doc comment just above - the same whole-page-vs-inside-a-
// card split applies here.
export function LoadingCard({ label, nested }: { label: string; nested?: boolean }) {
  return (
    <div className={`${nested ? 'glim-well' : 'glim-card'} p-10 text-center text-sm text-carbon-textMuted`}>{label}</div>
  );
}

// A fault state that says what went wrong and offers a way to recover.
// `nested`: see EmptyState's own doc comment above.
export function ErrorCard({
  message,
  retry,
  retryLabel,
  nested,
}: {
  message: string;
  retry?: () => void;
  retryLabel?: string;
  nested?: boolean;
}) {
  return (
    <div className={`${nested ? 'glim-well' : 'glim-card'} flex flex-col items-center gap-3 p-10 text-center`}>
      <div className="text-sm text-statusFail">{message}</div>
      {retry && (
        <Button kind="secondary" onClick={retry}>
          {retryLabel}
        </Button>
      )}
    </div>
  );
}

// SectionTitle labels a group of content as a "notch" badge: a small filled
// pill that sits HALF OVER the card's own top edge (absolute, `top-0` plus a
// self-relative `-translate-y-1/2`, a shadow lifting it off the surface)
// rather than as a plain heading inside the card's normal content flow. Two
// prior passes on this component got it wrong in opposite directions - first
// an accent-soft wash sitting inline (matching GlimStone's repo/docs, never
// actually live anywhere), then plain bare text (matching a DIFFERENT, older
// BombVault instance that turned out not to be the real reference container
// at all, per jdp: "Nein das ist falsch! Hier ist der Testcontainer
// erreichbar..."). This is the real container's own markup: solid
// `bg-accent`/`text-accentContrast` fill, 12px/500/1.2px-tracking uppercase,
// `rounded-[var(--radius-pill)]`, `absolute top-0 z-10
// shadow-[var(--elevation)]`. Requires its Card ancestor to be
// `position: relative` (`.glim-card` carries that now) so the badge
// anchors to the CARD's own box, not wherever it would otherwise fall in
// normal flow.
//
// Which means: the badge anchors to the NEAREST positioned ancestor, and a
// Card is the nearest one by default. A second SectionTitle in the same Card
// therefore lands on top of the first - same corner, same offset, the earlier
// one painted over and gone. Where a card is genuinely divided into titled
// sections, give each section's own wrapper `relative`; the badge then
// straddles that section's divider the way a card's badge straddles the card
// edge. Access.tsx's remote-access card is the worked example.
//
// `hint` renders INSIDE the filled badge itself (a child of the same
// span), not as a sibling beside it - confirmed from the live container's
// own DOM, and matching jdp's own words two rounds ago ("die infobubble
// der Ecken in den Titelbadge").
//
// `hue` opts the badge into a rainbow position exactly like Sidebar.tsx's
// own nav items and Tabs.tsx's own segments already do (`glim-hue` +
// `hueVars(rainbowAt(hue))`) - confirmed live: every card's own title on
// the real container carries its own sequential `--item-hue`, gated by
// the SAME `[data-rainbow]` mechanism as everywhere else (a hue is always
// assigned, only actually painted once rainbow mode is on). Omit it for a
// card that is the only one of its kind on the page - rule: "anything
// that is the only one of its kind keeps the single accent."
//
// `right` still exists, still spaced off with its own gap after the row,
// for the rare call site that means a real far-right header action (Add,
// Refresh) rather than an explanatory hint.
export function SectionTitle({
  children,
  hint,
  hue,
  right,
  id,
}: {
  children: ReactNode;
  hint?: string;
  hue?: number;
  right?: ReactNode;
  /**
   * Names the heading element so a window can point `aria-labelledby` at it.
   * A window's title is a badge like every other heading in the house (rule
   * 15), and Modal below is the one caller that also has to say WHICH element
   * is its accessible name - pointing at the real heading rather than copying
   * the words into an aria-label that could later disagree with them.
   */
  id?: string;
}) {
  return (
    <div className="flex items-center gap-3">
      <h2 id={id} className="flex items-center">
        <span
          // The position lives on the Card now (GlimStone 1.4.0), so this
          // badge carries only its marker class and inherits --item-hue* from
          // there. `.glim-hue` is NOT added unconditionally: the class and the
          // properties must travel together, and six cards in this app have no
          // position at all - a badge wearing the class without them resolves
          // --accent to nothing and disappears. index.css addresses the badge
          // through `.glim-card.glim-hue .glim-section-badge` instead, which is
          // true exactly when there is something to inherit.
          //
          // `hue` still exists for a title with no hued card above it; passing
          // it sets the vars here, so the class it then adds has them.
          //
          // THE HALF-OVERLAP IS SELF-RELATIVE, NEVER A FIXED PIXEL OFFSET.
          // `top-0` plus `-translate-y-1/2` resolves against the badge's OWN
          // rendered height, so it re-centres on the card's edge whether the
          // badge is one line or has wrapped to two. The pair it replaces
          // (`-top-[11px]` beside a hard-wired `h-[22px]`) was only ever
          // correct because the two numbers were written next to each other:
          // the 11 is half the 22, so the badge could not re-centre itself and
          // either number moving alone put it off the edge. The height is
          // padding plus line box now (3.5px + 15px + 3.5px = the same 22),
          // which is what lets it grow. `whitespace-nowrap` went with them: in
          // 42 locales a long translated card title has to be able to take a
          // second line rather than run off the side of its own card.
          className={`${hue !== undefined ? 'glim-hue ' : ''}glim-section-badge absolute top-0 z-10 inline-flex -translate-y-1/2
            items-center gap-1 rounded-[var(--radius-pill)] bg-accent px-3 py-[3.5px] text-[12px]
            font-medium uppercase leading-[15px] tracking-[1.2px] text-accentContrast shadow-[var(--elevation)]`}
          style={hue !== undefined ? (hueVars(rainbowAt(hue)) as CSSProperties) : undefined}
        >
          {children}
          {hint && <InfoBubble tip={hint} onColor />}
        </span>
      </h2>
      {right && (
        <>
          <span className="flex-1" />
          {right}
        </>
      )}
    </div>
  );
}

// Modal is the one overlay treatment: a dimmed page and a single raised panel.
// Escape and a click on the backdrop both close it, so it never traps anyone.
export function Modal({
  title,
  onClose,
  closeLabel,
  children,
  footer,
  mute,
}: {
  title: string;
  onClose: () => void;
  /**
   * The corner X, and the window only has one WHEN THIS IS GIVEN.
   *
   * It used to be drawn unconditionally, and seventeen of this app's
   * twenty-seven windows also carry a Cancel button in their footer - so those
   * seventeen offered one answer twice, once in the place a window's close
   * button lives. That reads as a choice between two things rather than as the
   * same thing said twice, which is how it was reported ("der obere stehen
   * lassen button weg"). A window whose footer already says how to leave
   * passes nothing here; a window with no such button passes the label, and the
   * string is the caller's because this file does not own anyone's wording.
   *
   * Escape and a click on the scrim close the window either way, so nothing is
   * ever trapped by leaving this out.
   */
  closeLabel?: string;
  children: ReactNode;
  /**
   * The answers, built by the caller: this window has no confirm/cancel pair of
   * its own, and it is not getting one just to carry a name from the language.
   * GlimStone's `confirmGlyph` exists because its dialog resolves the cancel
   * button's glyph from a translation key and cannot resolve the confirm
   * button's - the confirming verb changes with the action - so the confirm
   * button ended up as bare words beside a cancel button with a mark. Nothing
   * here resolves a glyph from a key, so that lopsidedness cannot arise by
   * itself; it can only be written by hand, and the rule that prevents it is
   * that a footer is all glyphs or none. Two of three buttons carrying one is
   * the shape to look for.
   */
  footer?: ReactNode;
  /**
   * Turns this dialog into one somebody can silence - see lib/dialogmute.ts.
   * The switch writes the preference the moment it is flipped rather than on
   * confirm: it is a preference about this dialog, not part of the action, and
   * a person who flips it and then cancels still meant it.
   */
  mute?: DialogId;
}) {
  const { t } = useT();
  const dialogs = useDialogMute();
  const titleId = useId();
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div
      // glim-modal-backdrop, not a number typed here. GlimStone 1.11.0 made the
      // scrim a token because it was a value the rule described and nothing
      // held: asked to change it, somebody has to find every place it was
      // typed. This one was .50 while the language asks for .65 on a dark
      // ground and .55 on a light one, and at .50 the card in front and the
      // page behind sit close enough in value that the eye keeps reading the
      // page, which is the one thing a scrim exists to stop.
      className="glim-modal-backdrop fixed inset-0 z-50 grid place-items-center p-6"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      {/* glim-modal-card is the window's ARRIVAL, and it is a class rather than
          anything typed here: index.css's motion engine carries the keyframe and
          the three intensity steps, so the rise settles with every other
          animation in the app and stops with them under reduced motion. The
          scrim's own fade rides on .glim-modal-backdrop above. Two windows in
          one app must not arrive in two different ways. */}
      <div
        className="glim-card glim-modal-card w-full max-w-md p-5 flex flex-col gap-5"
        role="dialog"
        aria-modal="true"
        // The heading IS the window's name, so it is pointed at rather than
        // copied into an aria-label that could disagree with it later.
        aria-labelledby={titleId}
      >
        {/* A WINDOW IS A WINDOW: same surface, same radius, same elevation,
            TITLE AS A BADGE. The heading was a bare `text-sm font-semibold`
            line inside the window while every card in the same app wore the
            notch badge, so one surface had two heading treatments - and the
            badge was already built, three functions up, and already used
            everywhere else. SectionTitle itself, not a copy of its markup:
            a second span with the same classes is the drift this file exists
            to prevent.
            No size on the close glyph: a glyph-only button is a square at
            --btn-h and its mark is half that box, which Button now decides for
            every one of them at once. */}
        <SectionTitle
          id={titleId}
          right={closeLabel ? <Button kind="ghost" icon={<IconClose />} onClick={onClose} title={closeLabel} /> : undefined}
        >
          {title}
        </SectionTitle>
        {children}
        {/* Above the buttons, not among them: it decides whether this window
            appears again, which is a different kind of thing from the two
            answers it is asking for right now. */}
        {mute && (
          <ToggleRow
            hue={0}
            label={t('dialog.dontShowAgain')}
            hint={t('dialog.dontShowAgainHint')}
            checked={dialogs.isMuted(mute)}
            onChange={(v) => dialogs.setMuted(mute, v)}
          />
        )}
        {/* justify-end, so the pair ends the line rather than starting it.
            1.14.0 puts the advancing button on the RIGHT because that is where
            the hand already is, and half of that is only true if the row itself
            reaches the edge: in a max-w-md window a left-started footer leaves
            the advancing button sitting in the middle. Twenty-one footers, and
            eleven of them opened with a `<span className="flex-1" />` to get
            here by hand while eight did not - one file had it both ways in two
            windows of the same kind. The window owns the alignment now; a
            leftover spacer is harmless, and anything that belongs on the LEFT
            of the pair (a counter, an error line) has to be written before it
            rather than after. */}
        {footer && <div className="flex items-center justify-end gap-3">{footer}</div>}
      </div>
    </div>
  );
}
