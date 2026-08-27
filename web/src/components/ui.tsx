// Primitives of the GlimStone design language. Everything is expressed through the
// shared tokens in index.css, so a sibling app inherits the look by adopting
// that file — see the comment block there.
import { useCallback, useEffect, useId, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import type { ButtonHTMLAttributes, CSSProperties, InputHTMLAttributes, ReactNode, RefObject } from 'react';
import { hueVars, rainbowAt } from '../lib/appearance';
import { IconClose, IconEye, IconEyeOff } from '../lib/icons';

type ButtonKind = 'primary' | 'secondary' | 'ghost' | 'danger';

const kindClass: Record<ButtonKind, string> = {
  primary: 'bg-accent text-accentContrast hover:brightness-110',
  secondary: 'bg-carbon-surface2 text-carbon-text hover:bg-carbon-surface3',
  ghost: 'text-carbon-textSub hover:bg-carbon-hover hover:text-carbon-text',
  danger: 'bg-statusFailBg text-carbon-text hover:brightness-110',
};

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
  ...rest
}: {
  kind?: ButtonKind;
  icon?: ReactNode;
  hue?: number;
} & ButtonHTMLAttributes<HTMLButtonElement>) {
  const iconOnly = icon && !children;
  const hued = hue !== undefined;
  return (
    <button
      className={`inline-flex items-center justify-center gap-2 rounded-[var(--radius-control)] text-sm font-medium
        transition duration-150 select-none disabled:opacity-35 disabled:pointer-events-none
        motion-safe:active:scale-[.98] ${iconOnly ? 'p-2' : 'px-3.5 py-2'}
        ${hued ? 'glim-hue bg-accent text-accentContrast hover:brightness-110' : kindClass[kind]} ${className}`}
      style={hued ? (hueVars(rainbowAt(hue)) as CSSProperties) : undefined}
      {...rest}
    >
      {icon}
      {children}
    </button>
  );
}

type IconBadgeKind = 'neutral' | 'danger';

const iconBadgeClass: Record<IconBadgeKind, string> = {
  neutral: 'bg-carbon-surface2 text-carbon-textSub hover:bg-carbon-surface3 hover:text-carbon-text',
  danger: 'bg-statusFailBg text-statusFail hover:brightness-110',
};

/**
 * A small square colour tile around one glyph - the shape a cluster of
 * icon-only actions (a row's hover controls, a header's small utility
 * buttons) reads as, distinct from `Button`'s own icon-only mode, which
 * stays transparent until hovered and reads as bare floating glyphs rather
 * than a control (jdp, on Rules.tsx's row actions specifically: "die icons
 * die bei mouseover auf die regel erscheinen sind nicht im Glimstone. das
 * sollen farbige quadratischen badges mit icon sein"). `h-8 w-8` matches
 * the sibling apps' own icon-badge footprint (BombVault's Settings.tsx).
 *
 * `hue` opts a badge into the rainbow palette the same way Button and
 * SectionTitle already do (jdp: "Bitte alle quadratischen badges in die
 * farbengine aufnehmen. Die icons sollen keine farbe haben sondern nur der
 * badge selbst") — but via `.glim-tint-badge`, not `.glim-hue` +
 * `.glim-hue-icon`: a badge is a compact, isolated square with no
 * checked/active state of its own to read `--accent` through (unlike a
 * Button, which paints its whole fill with `bg-accent`), so it needs the
 * stronger at-rest wash index.css's own doc comment on `.glim-tint-badge`
 * describes for exactly this case. Deliberately NOT `.glim-hue-icon`: that
 * class colours the glyph itself, which is the one thing this request asks
 * to keep neutral — only the tile takes the hue, the icon stays
 * `currentColor` from `iconBadgeClass[kind]` regardless.
 */
export function IconBadge({
  icon,
  kind = 'neutral',
  hue,
  active,
  className = '',
  style,
  title,
  ...rest
}: {
  icon: ReactNode;
  kind?: IconBadgeKind;
  hue?: number;
  /**
   * Marks a TOGGLE badge (a filter switching on/off, not a one-shot action)
   * as currently engaged — added for Collector.tsx's "Nicht prüfbar" /
   * "Ungeprüft" filters, jdp 2026-08-25: moved off ListToolbar's text-chip
   * strip into this same badge row. Left `undefined` by every other call
   * site in the app (a one-shot action has no pressed state to report), so
   * `aria-pressed` is only ever rendered where a caller opts in — an action
   * button staying a plain button, not silently becoming a toggle button
   * for assistive tech everywhere else this component is already used. A
   * halo ring in the tile's own current colour, not a Button-style
   * `bg-accent` fill: the doc comment above already covers why a fill would
   * cost the tile its own hue/kind colour, and this app's own rule against
   * border lines (Look.tsx's colour swatches use the identical halo) rules
   * out a plain border too.
   */
  active?: boolean;
} & ButtonHTMLAttributes<HTMLButtonElement>) {
  const hued = hue !== undefined;
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
        className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-[var(--radius-control)]
          transition duration-150 select-none disabled:opacity-35 disabled:pointer-events-none
          motion-safe:active:scale-[.98] ${hued ? 'glim-tint-badge' : ''} ${iconBadgeClass[kind]}
          ${active ? 'shadow-[0_0_0_2px_var(--carbon-bg),0_0_0_2px_currentColor]' : ''} ${className}`}
        style={hued ? { ...(hueVars(rainbowAt(hue)) as CSSProperties), ...style } : style}
        {...(title ? tipHoverProps : undefined)}
        {...rest}
      >
        {icon}
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
    <span className="flex shrink-0 items-center text-xs text-carbon-textSub">
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
  const [at, setAt] = useState<{ left: number; top?: number; bottom?: number } | null>(null);
  const ref = useRef<HTMLSpanElement>(null);

  // Same clamp/flip math as useTooltip's own `place()` below (GlimStone
  // 1.2.0: "the tooltip/info-bubble viewport-clip fix" - this one had been
  // missed when useTooltip first got it, so an InfoBubble opened near an
  // edge - a card's own bottom-right corner, the last item in a short
  // viewport - could render partly or fully off-screen with nothing to pull
  // it back in). Horizontally clamped against the WORST-CASE (max) width
  // rather than a measured one - cheaper than a second-pass measure for a
  // small read-only panel, and only ever errs towards more margin, never
  // towards clipping. Vertically flipped by the viewport's bottom edge
  // rather than a measured height, since nothing has rendered yet to
  // measure at the moment this runs.
  function open() {
    const r = ref.current?.getBoundingClientRect();
    if (!r) return;
    const margin = 12;
    const half = TOOLTIP_MAX_WIDTH / 2;
    const left = Math.min(Math.max(r.left + r.width / 2, margin + half), window.innerWidth - margin - half);
    if (r.bottom + TOOLTIP_EST_HEIGHT <= window.innerHeight) setAt({ left, top: r.bottom + 8 });
    else setAt({ left, bottom: window.innerHeight - r.top + 8 });
  }

  // Escape closes it, because a bubble opened by keyboard has to be closable by
  // keyboard without moving focus somewhere else first.
  useEffect(() => {
    if (!at) return;
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setAt(null);
    const onScroll = () => setAt(null); // a measured position goes stale the moment the page moves
    window.addEventListener('keydown', onKey);
    window.addEventListener('scroll', onScroll, true);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('scroll', onScroll, true);
    };
  }, [at]);

  return (
    <>
      <span
        ref={ref}
        role="note"
        tabIndex={0}
        aria-label={label ?? (typeof tip === 'string' ? tip : undefined)}
        onMouseEnter={open}
        onMouseLeave={() => setAt(null)}
        onFocus={open}
        onBlur={() => setAt(null)}
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
      {at &&
        createPortal(
          <span
            role="tooltip"
            dir="auto"
            className="glim-bubble glim-fade"
            style={{ left: at.left, top: at.top, bottom: at.bottom, maxWidth: TOOLTIP_MAX_WIDTH }}
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
 * Tall enough for the biggest tooltip this build opens. It only decides
 * which side of the trigger the bubble opens on (see useTooltip below), so
 * guessing high costs nothing and guessing low clips the bottom of one
 * opened near the edge of the screen - the one direction worth being
 * generous in.
 */
const TOOLTIP_EST_HEIGHT = 320;

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
 * anything positioned inside the scrolling table itself; Escape and a
 * scroll both close it, because a position measured off the trigger goes
 * stale the moment the page moves under it.
 */
export function useTooltip<T extends HTMLElement = HTMLElement>(content: ReactNode): TooltipHandle<T> {
  const id = useId();
  const ref = useRef<T>(null);
  const [at, setAt] = useState<{ left: number; top?: number; bottom?: number } | null>(null);
  const openTimer = useRef<number | undefined>(undefined);

  const place = useCallback(() => {
    const r = ref.current?.getBoundingClientRect();
    if (!r) return;
    const margin = 12;
    const half = TOOLTIP_MAX_WIDTH / 2;
    // Clamped by the worst-case (max) width rather than a measured one.
    // ColumnMenu clamps a real measured size, in a second pass after its own
    // first render — worth doing for a dense clickable menu, more machinery
    // than a small read-only panel needs to just not run off the screen. So
    // the trigger's own centre is used unless that would carry the WIDEST
    // possible bubble off either edge, which only ever makes this err
    // towards more margin than strictly necessary, never towards clipping.
    const left = Math.min(Math.max(r.left + r.width / 2, margin + half), window.innerWidth - margin - half);
    if (r.bottom + TOOLTIP_EST_HEIGHT <= window.innerHeight) setAt({ left, top: r.bottom + 8 });
    // Flipped by the viewport's BOTTOM edge rather than by a measured
    // height: the content has not rendered yet at the moment this runs, so
    // there is nothing to measure, and pinning the bubble's own bottom edge
    // needs none.
    else setAt({ left, bottom: window.innerHeight - r.top + 8 });
  }, []);

  const open = useCallback(() => {
    window.clearTimeout(openTimer.current);
    openTimer.current = window.setTimeout(place, TOOLTIP_DELAY_MS);
  }, [place]);

  const close = useCallback(() => {
    window.clearTimeout(openTimer.current);
    setAt(null);
  }, []);

  // Unmounting mid-hold must not fire the timer into a row that is gone - the
  // table repaints on every websocket tick, so a row under the pointer
  // disappearing before its own open timer fires is routine, not an edge case.
  useEffect(() => () => window.clearTimeout(openTimer.current), []);

  useEffect(() => {
    if (!at) return;
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && close();
    const onScroll = () => close(); // a measured position goes stale the moment the page moves
    window.addEventListener('keydown', onKey);
    window.addEventListener('scroll', onScroll, true);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('scroll', onScroll, true);
    };
  }, [at, close]);

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
      onFocus: place,
      onBlur: close,
      'aria-describedby': at ? id : undefined,
    },
    node:
      at && content
        ? createPortal(
            <span
              id={id}
              role="tooltip"
              dir="auto"
              className="glim-bubble glim-fade"
              style={{ left: at.left, top: at.top, bottom: at.bottom, maxWidth: TOOLTIP_MAX_WIDTH }}
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
export const segOff = 'bg-carbon-surface2 text-carbon-textMuted hover:bg-carbon-hover hover:text-carbon-text';

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
 * Swatch is a colour in a row of colours — one control for both jobs the Look
 * page has.
 *
 * They used to be two: the accent presets were coloured squares with a ring on
 * the chosen one, the palette was a row of raw `<input type="color">` boxes
 * with the browser's own chrome around them. Two controls, one job, sitting in
 * the same card four rows apart. Here the square IS the control in both cases;
 * when it is editable the native picker sits invisibly on top of it, so the
 * click lands where the colour is and the keyboard still reaches it.
 *
 * `onPick` alone makes a preset (choose this colour). `onColor` makes an
 * editable position (open a picker for it). Passing both is a preset that can
 * also be edited, which is what the free colour beside the presets is.
 */
export function Swatch({
  color,
  label,
  selected = false,
  onPick,
  onColor,
}: {
  color: string;
  /** Accessible name — a colour with no name is a square nobody can describe. */
  label: string;
  selected?: boolean;
  onPick?: () => void;
  onColor?: (hex: string) => void;
}) {
  // The ring is drawn in the swatch's own colour with the page ground between,
  // so it reads as a halo rather than as a border — rule 5, no lines.
  const shell = `relative h-7 w-7 shrink-0 overflow-hidden rounded-[var(--radius-control)]
    transition-transform motion-safe:hover:scale-110 ${
      selected ? 'shadow-[0_0_0_2px_var(--carbon-bg),0_0_0_4px_currentColor]' : ''
    }`;
  const style: CSSProperties = { backgroundColor: color, color };

  if (onColor) {
    return (
      <span className={shell} style={style} title={label}>
        <input
          type="color"
          aria-label={label}
          value={color}
          onChange={(e) => onColor(e.target.value)}
          onClick={onPick ? () => onPick() : undefined}
          // Invisible, but full-size and focusable: the swatch under it is what
          // is seen, the input is what is operated. `opacity-0` rather than
          // `hidden`, or it stops being reachable by keyboard.
          className="absolute inset-0 h-full w-full cursor-pointer appearance-none border-0 bg-transparent p-0 opacity-0"
        />
      </span>
    );
  }

  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      aria-pressed={selected}
      onClick={onPick}
      className={shell}
      style={style}
    />
  );
}

/**
 * SwatchRow lays swatches out and keeps whatever ends the row — the reset, in
 * both places that use it. It wraps rather than scrolls: eight squares and a
 * word must never be the reason a settings page scrolls sideways.
 */
export function SwatchRow({
  label,
  children,
  after,
}: {
  label: string;
  children: ReactNode;
  after?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-center gap-2" role="group" aria-label={label}>
      {children}
      {after}
    </div>
  );
}

const inputClass =
  'w-full rounded-[var(--radius-control)] bg-carbon-surface2 px-3 py-2 text-sm text-carbon-text ' +
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
 * The browser's own up/down spinner paints as one native widget under
 * `color-scheme: dark` (see :root in index.css) - a themed rounded box
 * behind the arrows that `background-color` on `::-webkit-inner-spin-button`
 * cannot strip, because Chromium renders that widget as a single image, not
 * a styleable box plus glyphs (jdp: "diese hoch und runterzähler haben einen
 * kleinen dunklen hintergrund ... bitte den entfernen"). The native spinner
 * is hidden outright (glim-num-hide-spin in index.css) and replaced with two
 * plain chevrons in the same muted ink as everything else in this file, so
 * "no visible box" actually means no box, not a differently-coloured one.
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
  function clamp(n: number) {
    if (min !== undefined && n < min) return min;
    if (max !== undefined && n > max) return max;
    return n;
  }
  return (
    <span className="relative inline-block w-full">
      <input
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
          onClick={() => onValue(clamp(value + step))}
          className="flex h-3.5 w-4 items-center justify-center rounded-[var(--radius-control)] text-carbon-textMuted
            hover:bg-carbon-surface3 hover:text-carbon-text"
        >
          <svg viewBox="0 0 10 6" width={9} height={5} aria-hidden>
            <path d="M1 5 5 1 9 5" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </button>
        <button
          type="button"
          tabIndex={-1}
          aria-hidden
          onClick={() => onValue(clamp(value - step))}
          className="flex h-3.5 w-4 items-center justify-center rounded-[var(--radius-control)] text-carbon-textMuted
            hover:bg-carbon-surface3 hover:text-carbon-text"
        >
          <svg viewBox="0 0 10 6" width={9} height={5} aria-hidden>
            <path d="M1 1 5 5 9 1" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
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
  return (
    <div className="relative">
      <TextInput
        type={reveal ? 'text' : 'password'}
        autoComplete={autoComplete}
        autoFocus={autoFocus}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="pr-9"
      />
      <button
        type="button"
        // Stops the click from also refocusing/moving the caret through a
        // <label>-wrapped Field the way a plain click would - the toggle is
        // its own control, not a second way to focus the field.
        onMouseDown={(e) => e.preventDefault()}
        onClick={() => setReveal((r) => !r)}
        title={reveal ? hideLabel : showLabel}
        aria-label={reveal ? hideLabel : showLabel}
        className="absolute inset-y-0 right-0 flex w-9 items-center justify-center text-carbon-textMuted transition-colors hover:text-carbon-text"
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
  /** Dims the whole row and blocks interaction - a control that vanishes
   *  teaches nobody what the mode can do; one that disagrees with its own
   *  disabled sibling controls does. */
  disabled?: boolean;
  /** Passed straight through to the underlying Toggle - see its own doc
   *  comment. Omit for a lone switch with nothing beside it to distinguish. */
  hue?: number;
}) {
  return (
    <div className={`flex items-center justify-between gap-4 ${disabled ? 'pointer-events-none opacity-40' : ''}`}>
      {/* pointer-events-auto re-opens hit-testing for just this span, undoing the
          row-wide pointer-events-none above it - CSS pointer-events isn't a one-way
          lock, a descendant can switch itself back on. Without this a disabled row's
          own (i) could never be hovered or focused, so the one place that explains
          WHY the row is disabled was exactly the thing the disabled state hid. The
          Toggle switch itself is a sibling, still under the row's pointer-events-none,
          so the check stays un-clickable either way. */}
      <span className="pointer-events-auto flex items-center gap-1.5 text-sm text-carbon-text">
        {label}
        {hint && <InfoBubble tip={hint} />}
      </span>
      <Toggle hideLabel label={label} checked={checked} onChange={onChange} hue={hue} />
    </div>
  );
}

// Card is the one raised surface. Never nest it inside another Card.
export function Card({
  children,
  className = '',
  hover = false,
  padding = 'normal',
}: {
  children: ReactNode;
  className?: string;
  hover?: boolean;
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
      className={`glim-card ${padding === 'normal' ? 'p-5' : ''} ${
        hover ? 'transition-transform duration-150 motion-safe:hover:-translate-y-0.5' : ''
      } ${className}`}
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
// pill that sits HALF OVER the card's own top edge (absolute, -11px, a
// shadow lifting it off the surface) rather than as a plain heading inside
// the card's normal content flow. Two prior passes on this component got it
// wrong in opposite directions - first an accent-soft wash sitting inline
// (matching GlimStone's repo/docs, never actually live anywhere), then
// plain bare text (matching a DIFFERENT, older BombVault instance that
// turned out not to be the real reference container at all, per jdp: "Nein
// das ist falsch! Hier ist der Testcontainer erreichbar..."). This is the
// real container's own markup, read directly off it and ported verbatim:
// solid `bg-accent`/`text-accentContrast` fill, 12px/500/1.2px-tracking
// uppercase, `rounded-[var(--radius-pill)]`, `absolute -top-[11px] z-10
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
}: {
  children: ReactNode;
  hint?: string;
  hue?: number;
  right?: ReactNode;
}) {
  return (
    <div className="flex items-center gap-3">
      <h2 className="flex items-center">
        <span
          className={`${hue !== undefined ? 'glim-hue glim-section-badge' : ''} absolute -top-[11px] z-10 inline-flex min-h-[22px]
            items-center gap-1 whitespace-nowrap rounded-[var(--radius-pill)] bg-accent px-3 py-0.5 text-[12px]
            font-medium uppercase tracking-[1.2px] text-accentContrast shadow-[var(--elevation)]`}
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
  children,
  footer,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 grid place-items-center bg-black/50 p-6"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="glim-card w-full max-w-md p-5 flex flex-col gap-5" role="dialog" aria-modal="true">
        <div className="flex items-center gap-3">
          <h2 className="text-sm font-semibold text-carbon-text">{title}</h2>
          <span className="flex-1" />
          <Button kind="ghost" icon={<IconClose width={16} height={16} />} onClick={onClose} aria-label={title} />
        </div>
        {children}
        {footer && <div className="flex items-center gap-3">{footer}</div>}
      </div>
    </div>
  );
}
