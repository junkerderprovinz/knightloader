// Primitives of the GlimStone design language. Everything is expressed through
// the shared tokens in index.css, so a sibling app inherits the look by adopting
// that file.
import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import type { ButtonHTMLAttributes, CSSProperties, InputHTMLAttributes, ReactNode, RefObject } from 'react';
import { hueVars, rainbowAt } from '../lib/appearance';
// Every component in this file that paints a palette position calls
// useRainbow(), so it renders again when the palette moves under it.
import { useRainbow } from '../lib/useRainbow';
import { useNavLabels } from '../lib/navLabels';
import { useDialogMute, type DialogId } from '../lib/dialogmute';
import { useT } from '../lib/i18n';
import { IconEye, IconEyeOff } from '../lib/icons';
import { openColorPickerPopover } from '../lib/colorPicker';

/**
 * There is no 'danger' kind. What warns is the question: an irreversible action
 * opens a window that states the stakes in words and counts, and red on every
 * delete teaches people to read past it by the third time. The variant is
 * deleted from the union rather than left unused, so tsc is the guard.
 */
type ButtonKind = 'primary' | 'secondary' | 'ghost';

/**
 * An accent fill hovers by opacity, and the other two kinds take the next tier
 * of the surface ramp (web/check-hover-ramp.mjs guards the surface2/surface3
 * pair). A brightness step can only move one way while the two themes need
 * opposite directions, and an accent fill is not on the ramp, so it has no tier
 * above it to step to.
 */
const kindClass: Record<ButtonKind, string> = {
  primary: 'bg-accent text-accentContrast hover:opacity-90',
  secondary: 'bg-carbon-surface2 text-carbon-text hover:bg-carbon-surface3',
  ghost: 'text-carbon-textSub hover:bg-carbon-hover hover:text-carbon-text',
};

/**
 * The two button heights, and there is no third (GlimStone rule 19). `--btn-h`
 * (2rem) is what a text field measures, so a button in a row of fields matches
 * it; `--btn-h-key` (2.5rem, `.glim-btn-key`) is the one step up. Both live in
 * index.css, so nothing here writes a height of its own.
 */
const BTN_H = 'h-[var(--btn-h)]';
const BTN_SQUARE = 'h-[var(--btn-h)] w-[var(--btn-h)]';

/**
 * A glyph alone in a square is half its box (rule 13), and 20px beside words,
 * where the mark and 14px text have to read as one control. `[&>svg]` beats the
 * width and height written on the glyph itself, which are SVG presentation
 * attributes and lose to any CSS rule, so a call site passing its own number
 * still gets the right size.
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
 * hue overrides `kind`'s colour: the button becomes an accent-filled control
 * and `.glim-hue` rebinds `--accent` to this button's position colour under
 * rainbow mode. Inert when rainbow mode is off.
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
   * Opts a glyph-only button into the Beschriftung setting; see IconBadge's own
   * `labelled`. Used by the head card's transport buttons, which have a title
   * and no children.
   */
  labelled?: boolean;
  /**
   * The second height (`--btn-h-key`), for the control that creates the thing
   * the page lists or one whose press is hard to undo. A key control in a row
   * of fields is centred by that row's `items-center`, which this component
   * cannot set for its parent.
   */
  keyControl?: boolean;
} & ButtonHTMLAttributes<HTMLButtonElement>) {
  useRainbow();
  const labelMode = useNavLabels();
  // Only fills in for a button that has no children of its own: a labelled
  // button already says what it does.
  const fallback =
    labelled && !children && title && (labelMode === 'text' || labelMode === 'both') ? title : undefined;
  const body = children ?? fallback;
  const hideIcon = labelled && labelMode === 'text' && !!fallback;
  const iconOnly = !!icon && !body;
  const hued = hue !== undefined;
  // One control, one tooltip mechanism: `title` is pulled out of the props so
  // it never reaches the element, and the house bubble is the only one left.
  // Passed through, it showed the operating system's own box at the pointer
  // while the badge beside it showed the bubble at the trigger.
  const tip = useTooltip<HTMLButtonElement>(title, !!rest.disabled);
  // A <button> already has a role and a tab stop, and the "note" role would
  // tell a screen reader this is a description rather than a control.
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
        // `title` never reaches the DOM, so a glyph-only button states its name
        // here. Before the spread, so a call site's own aria-label still wins.
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
 * A bin badge takes the colour its siblings take, for the same reason
 * ButtonKind has no 'danger'. Every delete badge passes `hue`, so the tile is
 * already in the colour engine, and a status-red variant painted over it:
 * .glim-tint-badge sets only the box-shadow, leaving a red glyph in a
 * hue-tinted tile.
 */
const iconBadgeClass = 'bg-carbon-surface2 text-carbon-textSub hover:bg-carbon-surface3 hover:text-carbon-text';

/**
 * IconTile is IconBadge's inert twin: the same square, the same `--btn-h`
 * footprint, the same hue wash, but a `<span>`, because it marks a row rather
 * than doing anything when pressed. A bare glyph beside a list row reads as an
 * unfinished control, while an IconBadge there would be a button that ignores
 * clicks. Every square badge in the app shares one size regardless of role.
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
  useRainbow();
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
 * LabelBadge is the text-carrying member of the same family: one line of label,
 * optionally an InfoBubble, at the shared badge height. `hue` puts it in the
 * rainbow engine; `tone` paints it in a status colour instead, because running
 * "up or down" through the rainbow would let the palette decide what connected
 * looks like. The status variant carries no dot, being the indicator itself.
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
  useRainbow();
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
      // An opacity step rather than a rung of the surface ramp: this badge
      // wears three fills and one hover has to answer for all of them. Only the
      // neutral one has a tier above it, the status pair would lose the state
      // it reports, and the hue wash is an inset box-shadow a background
      // utility sits under.
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
 * A small square colour tile around one glyph, the shape a cluster of icon-only
 * actions reads as, against `Button`'s icon-only mode, which stays transparent
 * until hovered. A one-shot badge takes `.glim-tint-badge`, the at-rest wash,
 * or it would show no colour at all; a toggle takes `.glim-hue` alone and
 * unconditionally, so the class is there before the pressed fill needs an
 * `--accent` to resolve. Never `.glim-hue-icon`: only the tile takes the hue.
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
   * Marks a toggle badge as engaged. Left `undefined` by every other call site,
   * so `aria-pressed` is only rendered where a caller opts in and an action
   * button does not become a toggle button for assistive tech. Engaged is a
   * full fill, not a halo: `.glim-hue` has rebound `--accent`, so `bg-accent`
   * is the position colour and the tile loses no hue.
   */
  active?: boolean;
  /**
   * Opts this badge into the Beschriftung setting (lib/navLabels.ts), the same
   * one the sidebar and the settings rail follow. Opt-in is on its way out, not
   * a rule: from outside, a documented exemption and a control that ignores the
   * setting look identical. The end state is no prop at all, which first needs
   * the row layouts under the 45 call sites to hold three verbs per line. The
   * label is `title`, so no second string can disagree with it.
   */
  labelled?: boolean;
  /**
   * No tile until somebody reaches for it, for a badge sitting on a list row
   * and nowhere else: six filled squares on every row over forty rows read as a
   * wall of boxes rather than as the row's own actions. See .glim-badge-quiet.
   */
  quiet?: boolean;
} & ButtonHTMLAttributes<HTMLButtonElement>) {
  useRainbow();
  const hued = hue !== undefined;
  // Keyed on `active !== undefined` and not on the value, so an idle filter
  // does not wear the one-shot action's wash until it is first pressed.
  const toggle = active !== undefined;
  const labelMode = useNavLabels();
  const showText = labelled && !!title && (labelMode === 'text' || labelMode === 'both');
  // The glyph is only dropped where words arrive in its place. A badge that
  // opted in and carries no title would otherwise render as an empty box with
  // its accessible name intact.
  const showIcon = !(labelMode === 'text' && showText);
  // The house bubble rather than the native `title`, fixed at the root so the
  // call sites pick it up without changing.
  const tip = useTooltip<HTMLButtonElement>(title, !!rest.disabled);
  // triggerProps was built for InfoBubble's otherwise-inert <div>, which needs
  // a role and a tab stop to be reachable at all. A button has both already,
  // and the "note" role would take these badges' click semantics away from a
  // screen reader.
  const { role: _tipRole, tabIndex: _tipTabIndex, ...tipHoverProps } = tip.triggerProps;
  return (
    <>
      <button
        type="button"
        aria-pressed={active}
        // A badge showing text is no longer square: it keeps its height and
        // takes the width its words need, so the width, the padding and the
        // glyph size all fork on that.
        className={`flex ${BTN_H} shrink-0 items-center justify-center gap-1.5 rounded-[var(--radius-control)]
          transition duration-150 select-none disabled:opacity-35 disabled:pointer-events-none
          motion-safe:active:scale-[.98]
          ${showText ? `px-2.5 text-xs font-medium ${GLYPH_20}` : `w-[var(--btn-h)] ${GLYPH_16}`}
          ${hued ? (toggle ? 'glim-hue' : 'glim-tint-badge') : ''} ${quiet ? 'glim-badge-quiet' : ''}
          ${toggle && active ? 'glim-active bg-accent text-accentContrast hover:opacity-90' : iconBadgeClass} ${className}`}
        style={hued ? { ...(hueVars(rainbowAt(hue)) as CSSProperties), ...style } : style}
        // `title` is pulled out for the bubble and never reaches the DOM, so it
        // cannot act as the accessible name the way a native tooltip does.
        // Before the spread, so a call site's own aria-label still wins.
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

// One caption, so a Field and a FieldGroup cannot drift apart: the same row of
// words with the same (i) beside it, and only the wrapping element differs.
const FIELD_SHELL = 'flex flex-col gap-1.5';
// The caption beside the control instead of above it. Opt-in, for a control
// that reads fine on one line; most captions are long enough, or their control
// wide enough, that stacking is still the right call.
const FIELD_SHELL_ROW = 'flex flex-wrap items-center gap-3';

function Caption({ label, hint }: { label: string; hint?: string }) {
  return (
    // data-glim-label is the settings search's anchor, here rather than at the
    // call sites because this one span is every Field and FieldGroup in the
    // app. The value is the translated caption, which is what the search
    // compares against; pages/settings/jump.ts reads the property rather than
    // building a `[data-glim-label="…"]` selector out of it, because a caption
    // may contain a quote in any of 42 languages.
    <span data-glim-label={label} className="flex shrink-0 items-center text-xs text-carbon-textSub">
      {label}
      {hint && <InfoBubble tip={hint} />}
    </span>
  );
}

/**
 * Field pairs a label with one control. The explanation lives behind the (i)
 * beside the label rather than under the control, where a settings page whose
 * every row carries two lines of grey prose pays that space forever.
 *
 * One control, and the word is load-bearing: a `<label>` hands its clicks and
 * its name to the first labelable thing inside it, so a caption over a corner
 * picker sets the app back to round corners. Use FieldGroup for a row of
 * swatches, a tab strip or a pair of buttons.
 */
export function Field({
  label,
  hint,
  layout = 'stack',
  children,
}: {
  label: string;
  hint?: string;
  /** `'row'` puts the caption and the control on one line instead of stacking
   *  them; the control gets `flex-1` so it still fills the line. */
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
 * FieldGroup is Field's caption over a set of controls: identical to look at,
 * and not a `<label>`.
 *
 * It adds no `role` and no `aria-labelledby` of its own, because the things
 * that go in it already name themselves. `Tabs` puts its `label` on the tablist
 * and `SwatchRow` is a `role="group"` with the same, so a second group around
 * them would announce the caption twice.
 */
export function FieldGroup({
  label,
  hint,
  layout = 'stack',
  children,
}: {
  label: string;
  hint?: string;
  /** `'row'` puts the caption beside the control set instead of above it.
   *  Unlike Field's row mode the children keep their natural width: a tab strip
   *  or a swatch row hugs its content rather than filling the line. */
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
 * The bubble is rendered into <body> rather than next to the icon, where one
 * `overflow: hidden` on any scroll container, card or table above it leaves a
 * sliver. The position is measured from the icon each time it opens. The icon
 * is never the accent colour: it is furniture, and the accent means activity.
 */
export function InfoBubble({
  tip,
  label,
  className = '',
  onColor = false,
}: {
  tip: ReactNode;
  /**
   * The accessible name for the trigger. Optional because most callers pass a
   * plain sentence as `tip`, which doubles as its own label; one whose `tip` is
   * structured content has to supply this.
   */
  label?: string;
  className?: string;
  /**
   * True when this bubble sits on a filled, coloured surface rather than the
   * page's neutral ground, where the fixed muted grey can be nearly invisible.
   * The trigger then takes `currentColor` and inherits whatever contrast ink
   * the surface resolved for its own text.
   */
  onColor?: boolean;
}) {
  const [shown, setShown] = useState(false);
  const [at, setAt] = useState<{ left: number; top: number } | null>(null);
  const ref = useRef<HTMLSpanElement>(null);
  const bubble = useRef<HTMLSpanElement>(null);
  useEffect(trackInputModality, []);

  // A layout effect rather than the mouse handler, because the bubble has no
  // rendered size until it is in the document and reading it after the paint
  // shows one frame in the wrong place.
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

  // A bubble opened by keyboard has to be closable by keyboard without moving
  // focus first, and a pointerdown closes it because a press means somebody is
  // acting rather than reading.
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
        onFocus={() => !pointerWasLast && setShown(true)}
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
            // Hidden, not unrendered, until it has been measured: `visibility`
            // still lays the bubble out, which is what there is to measure.
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

// Whether the last input was a pointer rather than a key. Focus opens a bubble
// only after a key, because opening on focus is for keyboard users: a click
// focuses what it lands on, and a window hands focus back to its opener when
// it closes, so a bubble opened then stands where the pointer has left.
// Tracked for the whole page, since the focus often lands on one element after
// the press on another.
let pointerWasLast = false;
let tracking = false;

function trackInputModality() {
  if (tracking) return;
  tracking = true;
  document.addEventListener('pointerdown', () => (pointerWasLast = true), true);
  document.addEventListener('keydown', () => (pointerWasLast = false), true);
}

/**
 * .glim-bubble's own 280px, widened for the one caller that needs it: a row
 * tooltip stacks several labelled fields, and the extra 40px keeps a host name
 * or a short path off a second line. Passed as an inline style, which wins over
 * the class.
 */
const TOOLTIP_MAX_WIDTH = 320;

/**
 * placeBubble puts a bubble where the viewport cannot clip it. `w` and `h` are
 * its real rendered size, measured once it is in the document, because the
 * height depends on how many lines the tip wraps to and a constant is wrong in
 * both directions at once.
 *
 * Clamp, then flip, with an 8px margin, ported from the shared engine
 * (glimstone/reference/tooltip.ts). `left` is the bubble's centre, because
 * `.glim-bubble` carries `transform: translateX(-50%)`, and `width:
 * max-content` on that class is what makes measuring legitimate: without it a
 * fixed box with only `left` set resizes in response to the `left` computed
 * here.
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
    // RefObject<T | null>, matching what useRef<T>(null) returns under React
    // 19's types.
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
 * useTooltip is InfoBubble's sibling for content that is already on screen for
 * its own reason, such as a file name in a table row. A pointer crosses dozens
 * of rows a second on the way down a list, so this opens after a short hold
 * rather than on arrival, takes a panel of content rather than one string, and
 * picks which side of the trigger to open on. It keeps InfoBubble's portal into
 * <body>, so a table's `overflow-x-auto` cannot clip it.
 *
 * `disabled` is whether the trigger is disabled; the bubble closes every time
 * it flips.
 */
export function useTooltip<T extends HTMLElement = HTMLElement>(
  content: ReactNode,
  disabled = false,
): TooltipHandle<T> {
  const id = useId();
  const ref = useRef<T>(null);
  const bubble = useRef<HTMLSpanElement>(null);
  const [shown, setShown] = useState(false);
  const [at, setAt] = useState<{ left: number; top: number } | null>(null);
  const openTimer = useRef<number | undefined>(undefined);
  useEffect(trackInputModality, []);

  // A disabled button takes no pointer events, so one that disables itself
  // under the pointer, as a pressed button often does, never sees the
  // mouseleave that would close its bubble. Closing during render keeps the
  // bubble from being painted even once, and the effect drops a hover still
  // waiting to open.
  const [seenDisabled, setSeenDisabled] = useState(disabled);
  if (seenDisabled !== disabled) {
    setSeenDisabled(disabled);
    setShown(false);
  }
  useEffect(() => window.clearTimeout(openTimer.current), [disabled]);

  // Measured, then placed; see placeBubble and InfoBubble's own copy.
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

  // Focus opens at once rather than after the hold, which exists to filter a
  // pointer passing through on its way somewhere else.
  const showOnFocus = useCallback(() => {
    if (!pointerWasLast) show();
  }, [show]);

  // Unmounting mid-hold must not fire the timer into a row that is gone: the
  // table repaints on every websocket tick.
  useEffect(() => () => window.clearTimeout(openTimer.current), []);

  useEffect(() => {
    if (!shown) return;
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && close();
    const onScroll = () => close(); // a measured position goes stale the moment the page moves
    window.addEventListener('keydown', onKey);
    window.addEventListener('scroll', onScroll, true);
    // A press means the person is acting, not reading; without this a click on
    // the trigger leaves its own tip standing over it.
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
      // A plain tabbable <div> with no role reads to a screen reader as a
      // mystery stop. This trigger wraps its own visible text, which supplies
      // the accessible name, so it needs no aria-label.
      role: 'note',
      onMouseEnter: open,
      onMouseLeave: close,
      onFocus: showOnFocus,
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
              // Laid out but invisible until measured; see InfoBubble's copy.
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
 * A segmented control picks exactly one of a few. The chosen segment is filled
 * with the accent, the same treatment as the active nav item, so "this is the
 * one that is on" reads identically everywhere. Tabs.tsx is built from these
 * three strings, so a tab, a filter chip and a segment cannot drift apart.
 */
export const segBase = 'rounded-[var(--radius-control)] font-medium transition-colors';
export const segOn = 'bg-accent text-accentContrast';
// bg-carbon-surface2 at rest, not transparent: every tab is a badge, not just
// the selected one, or the strip reads as unfinished. See GlimStone's
// design-language.md.
export const segOff = 'bg-carbon-surface2 text-carbon-textMuted hover:bg-carbon-surface3 hover:text-carbon-text';

/**
 * hueStyle is how one member of a set claims a palette position: the element
 * carries `glim-hue` and gets these inline properties, and keeping the two
 * together matters because `.glim-hue` with no `--item-hue` under it resolves
 * the accent to nothing. Pass the item's index; positions never come from a
 * hash of an id. It reads the live palette during render, so the calling
 * component must also subscribe with `useRainbow()`.
 */
export function hueStyle(index: number | undefined): CSSProperties {
  if (index === undefined) return {};
  return hueVars(rainbowAt(index)) as CSSProperties;
}

// `py-1.5`: a 14px line box is 20px tall, plus 12px of padding, which is the
// 2rem `--btn-h` names, so a field and the buttons beside it measure the same.
// Written as padding rather than as a height because TextArea shares this
// string and a textarea has to be able to grow.
const inputClass =
  'w-full rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-1.5 text-sm text-carbon-text ' +
  'placeholder:text-carbon-textMuted outline-none transition-shadow ' +
  'focus:shadow-[0_0_0_2px_var(--focus-ring)]';

// className is pulled out and merged rather than left in `props`: a JSX spread
// applies later props last, so a caller's own className would replace the base
// look (padding, background, focus ring) instead of extending it.
export function TextInput({ className = '', ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={`${inputClass} ${className}`} {...props} />;
}

export function TextArea(props: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className={`${inputClass} resize-y`} {...props} />;
}

/**
 * A stepper is part of the field, not a control beside it (GlimStone 1.6.0): no
 * ground of its own, only the ink changing on hover, and greyed out at its own
 * end of the range. A hover background here would bring back the property that
 * got the native spinner removed, one layer down.
 */
const STEPPER =
  'flex h-3.5 w-4 items-center justify-center text-carbon-textMuted transition-colors ' +
  'hover:text-carbon-text disabled:opacity-35 disabled:hover:text-carbon-textMuted';

/**
 * Solid triangles with rounded corners, not two-stroke chevrons (GlimStone
 * 1.6.0): one outlined mark in a set of filled shapes reads as borrowed. The
 * rounding is a matched stroke plus stroke-linejoin rather than arcs in the
 * path, and the path is inset by the stroke's half-width so the painted result
 * lands where the sharp version did.
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
 * Under `color-scheme: dark` the browser's own spinner is one native widget, a
 * themed box behind the arrows that `background-color` on
 * `::-webkit-inner-spin-button` cannot strip, because Chromium renders it as a
 * single image. It is hidden outright (glim-num-hide-spin in index.css) and
 * replaced with two arrows, and the wheel it answered is put back here rather
 * than at the call sites.
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
   * The field's own stepUp()/stepDown() rather than arithmetic beside them, so
   * the range is laid out once, in the attributes the field already carries.
   * The value is read back off the field rather than recomputed.
   */
  function nudge(dir: 'up' | 'down') {
    const el = field.current;
    if (!el) return;
    if (dir === 'up') el.stepUp();
    else el.stepDown();
    if (Number.isNaN(el.valueAsNumber)) return;
    onValue(el.valueAsNumber);
  }
  // The handler is attached once and reads the current props through this box
  // rather than being torn down and rebuilt on every keystroke: a number field
  // re-renders on each character typed into it.
  const live = useRef({ value, step, min, max, onValue, disabled: rest.disabled, readOnly: rest.readOnly });
  useEffect(() => {
    live.current = { value, step, min, max, onValue, disabled: rest.disabled, readOnly: rest.readOnly };
  });

  /**
   * The wheel steps the value, but only while the field has focus: answering it
   * on hover alone edits whatever a pointer passed over on its way down the
   * page. Only the sign of the delta is read, because a trackpad reports
   * fractions. A real listener with `{ passive: false }` rather than React's
   * `onWheel`, which React registers passive at the root, so without
   * preventDefault the page scrolls the field out from under the pointer.
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
 * A password field with a reveal-eye toggle, sharing NumberInput's `relative`
 * wrapper and overlay-button shape. The labels are caller-supplied because this
 * file has no i18n dependency of its own. `autoComplete` is required rather
 * than defaulted to "off", which suppresses a password manager's save, fill and
 * strength prompts.
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
  // The house bubble, not the `title` attribute: every other control in this
  // file explains itself through useTooltip.
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
        // Room for the reveal button, which is the field's own height wide, so
        // the box is square and its glyph is half of it (rule 13).
        className="pr-8"
      />
      {tip.node}
      <button
        type="button"
        // Stops the click from also moving the caret through the <label> a
        // Field wraps around it: the toggle is its own control.
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
 * A switch with its words beside it. Set `hideLabel` where the heading above it
 * already says the same thing; the words then survive as the accessible name,
 * so a screen reader announces something other than "switch". Never drop the
 * label itself.
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
   * This switch's position in a list of switches sharing one card. Without it a
   * card with three toggles on shows one flat accent for all three. Omit it for
   * a lone switch with no siblings to tell apart.
   */
  hue?: number;
}) {
  useRainbow();
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
            position, which a button's inherited text-align centres, and slides
            out past the pill. Tailwind v4 animates `translate` here, not
            `transform`. `bg-carbon-background` rather than a fixed white makes
            the knob the page's own ground on the accent track, so it reads dark
            in dark mode and light in light mode. */}
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
 * ToggleRow is a Toggle used as a whole card row: the caption flush left, the
 * switch flush right on the same line. Never the bare `Toggle`, which glues
 * track and label together at the left edge, and never a `FieldGroup` with the
 * caption on its own line above a hideLabel switch; both read as shifted
 * against every other left-started row on the card.
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
   * Dims the label and the switch and blocks interaction. For a control that
   * reports its own state, not for a sub-switch: a switch dead because a switch
   * one row up is off is left out entirely, since dimmed it is something
   * somebody can see, read and reach for that answers nothing. What is left for
   * this prop is the row whose own state is the message, busy while a request
   * is in flight or a setting this build cannot offer.
   */
  disabled?: boolean;
  /** Passed straight through to the underlying Toggle. Omit for a lone switch
   *  with nothing beside it to distinguish. */
  hue?: number;
}) {
  return (
    <div className={`flex items-center justify-between gap-4 ${disabled ? 'pointer-events-none' : ''}`}>
      {/* The dimming is on the parts, never on the row: `opacity` composites a
          whole subtree, so the (i) inside a dimmed row would render at 40% too,
          in precisely the state it exists for. pointer-events-auto re-opens
          hit-testing for this span alone; the Toggle is a sibling and stays
          under the row's own pointer-events-none.
          data-glim-label for the reason Caption carries it: a ToggleRow draws
          its caption inline, so without this the settings search could find
          every switch but never scroll to it. */}
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
   * This card's place in the palette (GlimStone 1.4.0: the position belongs on
   * the container, not on the one visible element inside it), so a card that
   * owns its position hands it to its badge, its buttons, its switch tracks and
   * its focus ring at once.
   */
  hue?: number;
  /**
   * 'none' drops the default p-5, because a caller's own `className="p-0"`
   * never can: Tailwind's stylesheet orders same-property utilities by their
   * scale value rather than by where they appear in a className string, so
   * `.p-5` comes last and wins the tie. The doubled padding that leaves shows
   * up in a per-group SectionTitle badge, which has no left of its own and
   * lands 20px right of where it should.
   */
  padding?: 'normal' | 'none';
}) {
  useRainbow();
  return (
    <div
      // The trailing space lives inside the string, never after the
      // interpolation: an expression glued to the class before it fuses into
      // one nonsense name and drops both.
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
 * PageHeader opens a page with the one line the navigation cannot carry. The
 * title is not rendered: the sidebar entry for this page is already
 * highlighted and says the same word. It stays in the props because it is the
 * page's accessible name.
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
  // With nothing visible in it the whole header leaves the layout: every page
  // using it sits in a `flex flex-col gap-6`, where a flex child with no
  // content still earns its gap. sr-only rather than `hidden`, because the
  // heading is the page's accessible name, and an absolutely positioned child
  // is not a flex item.
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
// never looks broken and, where it makes sense, offers the way out.
//
// `nested` swaps the raised .glim-card surface for the quieter .glim-well one,
// because this is called both as a whole-page replacement and from inside an
// existing Card as that card's own empty state. A card with its own drop shadow
// inside another card is what index.css forbids.
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
      {/* The sleeping knight (docs/easter-eggs.md): the mark itself blinks,
          twice and slowly, because every caller hands this slot a house glyph
          rather than a figure with eyes to close. One CSS animation-delay, no
          timer and no state, and at motion "off" or under reduced motion the
          rule is never reached. See .kl-doze in index.css. */}
      {icon && <div className="kl-doze text-carbon-textMuted/60">{icon}</div>}
      <div className="text-sm text-carbon-textSub">{title}</div>
      {hint && <div className="text-[11px] text-carbon-textMuted">{hint}</div>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}

/**
 * UnavailableNotice is what a card shows where a control would be, when the
 * environment does not permit the thing the control would do (GlimStone
 * 1.15.0). A grey state about the thing the control touches dims the control
 * instead, and one about a decision taken elsewhere on the page leaves the
 * control out without owing any prose.
 *
 * Passkeys are the case that produced it: WebAuthn binds a credential to a
 * domain name, so a browser refuses the exchange on a bare IP address, which is
 * the default state of a self-hosted app opened over its LAN address.
 *
 * It does not translate and it does not take the server's sentence: the caller
 * passes UI copy in the reader's own language and the verdict travels as a
 * boolean, which web/check-passkey-reason.mjs keeps true.
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
   * Something the reader can do from here, never the control that was refused:
   * offering that again is the failing button this component exists to remove.
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
// EmptyState above, where the same whole-page or inside-a-card split applies.
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

// SectionTitle labels a group of content as a notch badge: a small filled pill
// sitting half over the card's own top edge. It needs its Card ancestor to be
// positioned (`.glim-card` is) so the badge anchors to the card's box.
//
// The badge anchors to the nearest positioned ancestor, so a second
// SectionTitle in the same Card lands on top of the first. Where a card is
// divided into titled sections, give each section's wrapper `relative`;
// Access.tsx's remote-access card is the worked example.
//
// `hint` renders inside the filled badge rather than beside it. `hue` opts the
// badge into a rainbow position; omit it for a card that is the only one of its
// kind on the page. `right` is for a far-right header action. `second` is a
// badge beside the title, filled the same way; with it the h2 takes the notch's
// placement and lays both out in a row, so the pair centres on the card edge
// as one group (GlimStone's rule for a pair of heading badges).
export function SectionTitle({
  children,
  hint,
  hue,
  right,
  second,
  id,
}: {
  children: ReactNode;
  hint?: string;
  hue?: number;
  right?: ReactNode;
  second?: { label: ReactNode; hint?: string };
  /**
   * Names the heading element so a window can point `aria-labelledby` at it.
   * Modal is the one caller that has to say which element is its accessible
   * name, and pointing at the real heading beats copying the words into an
   * aria-label that could later disagree with them.
   */
  id?: string;
}) {
  useRainbow();
  // The half-overlap is self-relative: `top-0` plus `-translate-y-1/2` resolves
  // against the positioned element's own rendered height, so it re-centres
  // whether the badge takes one line or two, which in 42 locales a long card
  // title has to be able to do. With a second badge the group is positioned
  // and the badges are not, or both would land on the same spot.
  const notch = 'absolute top-0 z-10 -translate-y-1/2';
  const hued = hue !== undefined ? 'glim-hue ' : '';
  const hueStyle = hue !== undefined ? (hueVars(rainbowAt(hue)) as CSSProperties) : undefined;
  const look = `glim-section-badge inline-flex items-center gap-1 rounded-[var(--radius-pill)] bg-accent px-3 py-[3.5px]
    text-[12px] font-medium uppercase leading-[15px] tracking-[1.2px] text-accentContrast shadow-[var(--elevation)]`;
  // The position lives on the Card (GlimStone 1.4.0), so a badge carries only
  // its marker class and inherits --item-hue from there. `.glim-hue` is not
  // added unconditionally: a badge wearing the class over a card with no
  // position resolves --accent to nothing and disappears. index.css addresses
  // the badge through `.glim-card.glim-hue .glim-section-badge` instead, which
  // is true exactly when there is something to inherit. `hue` is for a title
  // with no hued card above it and sets the properties here.
  const title = (
    <h2 id={id} className="flex items-center">
      <span className={`${hued}${second ? '' : `${notch} `}${look}`} style={hueStyle}>
        {children}
        {hint && <InfoBubble tip={hint} onColor />}
      </span>
    </h2>
  );
  return (
    <div className="flex items-center gap-3">
      {second ? (
        <div className={`${notch} flex items-center gap-2`}>
          {title}
          <span className={`${hued}${look}`} style={hueStyle}>
            {second.label}
            {second.hint && <InfoBubble tip={second.hint} onColor />}
          </span>
        </div>
      ) : (
        title
      )}
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
  children,
  footer,
  mute,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  /**
   * The answers, built by the caller: this window has no confirm/cancel pair of
   * its own. One of them is always the way out (GlimStone rule 15): a
   * `labelled` Button with the close glyph and Cancel, Close or whatever the
   * window means, so the label engine draws it like every other button. A
   * window whose only answer is to close keeps the row for that one button.
   * There is no corner X, which would offer the same answer twice.
   */
  footer?: ReactNode;
  /**
   * Turns this dialog into one somebody can silence; see lib/dialogmute.ts.
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
      // glim-modal-backdrop, not a number typed here: GlimStone 1.11.0 made the
      // scrim a token so it lives in one place, .65 on a dark ground and .55 on
      // a light one. Any lighter and the eye keeps reading the page behind it.
      className="glim-modal-backdrop fixed inset-0 z-50 grid place-items-center p-6"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      {/* glim-modal-card carries the window's arrival: index.css's motion engine
          holds the keyframe and the intensity steps, so the rise settles with
          every other animation in the app and stops with them under reduced
          motion. Two windows in one app must not arrive in two ways. */}
      <div
        className="glim-card glim-modal-card w-full max-w-md p-5 flex flex-col gap-5"
        role="dialog"
        aria-modal="true"
        // The heading IS the window's name, so it is pointed at rather than
        // copied into an aria-label that could disagree with it later.
        aria-labelledby={titleId}
      >
        {/* A window is a window: same surface, same radius, same elevation,
            title as a badge, so one app does not carry two heading treatments.
            SectionTitle itself rather than a copy of its markup, which is the
            drift this file exists to prevent. */}
        <SectionTitle id={titleId}>{title}</SectionTitle>
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
        {/* justify-end, so the pair ends the line: 1.14.0 puts the advancing
            button on the right, which only holds if the row reaches the edge.
            The window owns the alignment, so anything that belongs left of the
            pair has to be written before it. */}
        {footer && <div className="flex items-center justify-end gap-3">{footer}</div>}
      </div>
    </div>
  );
}
