// Primitives of the GlimStone design language. Everything is expressed through the
// shared tokens in index.css, so a sibling app inherits the look by adopting
// that file — see the comment block there.
import { useCallback, useEffect, useId, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import type { ButtonHTMLAttributes, CSSProperties, InputHTMLAttributes, ReactNode, RefObject } from 'react';
import { hueVars, rainbowAt } from '../lib/appearance';
import { IconClose } from '../lib/icons';

type ButtonKind = 'primary' | 'secondary' | 'ghost' | 'danger';

const kindClass: Record<ButtonKind, string> = {
  primary: 'bg-accent text-accentContrast hover:brightness-110',
  secondary: 'bg-carbon-surface2 text-carbon-text hover:bg-carbon-surface3',
  ghost: 'text-carbon-textSub hover:bg-carbon-hover hover:text-carbon-text',
  danger: 'text-statusFail hover:bg-statusFailBg',
};

export function Button({
  kind = 'primary',
  icon,
  children,
  className = '',
  ...rest
}: {
  kind?: ButtonKind;
  icon?: ReactNode;
} & ButtonHTMLAttributes<HTMLButtonElement>) {
  const iconOnly = icon && !children;
  return (
    <button
      className={`inline-flex items-center justify-center gap-2 rounded-[var(--radius-control)] text-sm font-medium
        transition duration-150 select-none disabled:opacity-35 disabled:pointer-events-none
        motion-safe:active:scale-[.98] ${iconOnly ? 'p-2' : 'px-3.5 py-2'} ${kindClass[kind]} ${className}`}
      {...rest}
    >
      {icon}
      {children}
    </button>
  );
}

// One caption, so a Field and a FieldGroup cannot drift apart: they are the same
// row of words with the same (i) beside it, and the only difference between them
// is which element wraps the control underneath.
const FIELD_SHELL = 'flex flex-col gap-1.5';

function Caption({ label, hint }: { label: string; hint?: string }) {
  return (
    <span className="flex items-center text-xs text-carbon-textSub">
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
export function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className={FIELD_SHELL}>
      <Caption label={label} hint={hint} />
      {children}
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
export function FieldGroup({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div className={FIELD_SHELL}>
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
export function InfoBubble({ tip, className = '' }: { tip: string; className?: string }) {
  const [at, setAt] = useState<{ top: number; left: number } | null>(null);
  const ref = useRef<HTMLSpanElement>(null);

  function open() {
    const r = ref.current?.getBoundingClientRect();
    if (!r) return;
    setAt({ top: r.bottom + 8, left: r.left + r.width / 2 });
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
        aria-label={tip}
        onMouseEnter={open}
        onMouseLeave={() => setAt(null)}
        onFocus={open}
        onBlur={() => setAt(null)}
        className={`glim-info ms-1.5 inline-flex h-[15px] w-[15px] shrink-0 cursor-help items-center
          justify-center rounded-full align-middle text-carbon-textMuted transition-opacity
          hover:text-carbon-textSub focus-visible:text-carbon-textSub ${className}`}
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
            style={{ top: at.top, left: at.left }}
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
    ref: RefObject<T>;
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
// A quiet fill at rest, not bare text (GlimStone: "every tab is a badge, not
// just the selected one") - an unfilled segment used to carry zero
// background until actually hovered, which read as unfinished on a strip
// with a dozen mostly-plain-text tabs.
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

export function TextInput(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={inputClass} {...props} />;
}

export function TextArea(props: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className={`${inputClass} resize-y`} {...props} />;
}

export function NumberInput({
  value,
  onValue,
  min,
  max,
  step = 1,
  ...rest
}: {
  value: number;
  onValue: (n: number) => void;
  min?: number;
  max?: number;
  step?: number;
} & Omit<InputHTMLAttributes<HTMLInputElement>, 'value' | 'onChange'>) {
  return (
    <input
      type="number"
      className={`${inputClass} glim-num`}
      value={value}
      min={min}
      max={max}
      step={step}
      onChange={(e) => onValue(Number(e.target.value))}
      {...rest}
    />
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
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label: string;
  hideLabel?: boolean;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={hideLabel ? label : undefined}
      onClick={() => onChange(!checked)}
      className="flex items-center gap-3 text-left text-sm text-carbon-text select-none"
    >
      <span
        className={`relative h-5 w-9 shrink-0 rounded-[var(--radius-pill)] transition-colors ${
          checked ? 'bg-accent' : 'bg-carbon-surface3'
        }`}
      >
        {/* left-0 is load-bearing: without it the knob starts from its static
            position, which a button's inherited text-align centres — the knob
            then slides out past the pill. Tailwind v4 also animates the
            `translate` property here, not `transform`. */}
        <span
          className={`absolute left-0 top-0.5 h-4 w-4 rounded-[var(--radius-pill)] bg-white shadow-sm transition-[translate] duration-150 ${
            checked ? 'translate-x-4' : 'translate-x-0.5'
          }`}
        />
      </span>
      {!hideLabel && <span>{label}</span>}
    </button>
  );
}

// Card is the one raised surface. Never nest it inside another Card.
export function Card({
  children,
  className = '',
  hover = false,
}: {
  children: ReactNode;
  className?: string;
  hover?: boolean;
}) {
  return (
    <div
      className={`glim-card p-5 ${
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
export function EmptyState({
  icon,
  title,
  hint,
  action,
}: {
  icon?: ReactNode;
  title: string;
  hint?: string;
  action?: ReactNode;
}) {
  return (
    <div className="glim-card flex flex-col items-center gap-2 p-10 text-center">
      {icon && <div className="text-carbon-textMuted/60">{icon}</div>}
      <div className="text-sm text-carbon-textSub">{title}</div>
      {hint && <div className="text-[11px] text-carbon-textMuted">{hint}</div>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}

// A quiet placeholder while a page's data is still on the wire.
export function LoadingCard({ label }: { label: string }) {
  return (
    <div className="glim-card p-10 text-center text-sm text-carbon-textMuted">{label}</div>
  );
}

// A fault state that says what went wrong and offers a way to recover.
export function ErrorCard({ message, retry, retryLabel }: { message: string; retry?: () => void; retryLabel?: string }) {
  return (
    <div className="glim-card flex flex-col items-center gap-3 p-10 text-center">
      <div className="text-sm text-statusFail">{message}</div>
      {retry && (
        <Button kind="secondary" onClick={retry}>
          {retryLabel}
        </Button>
      )}
    </div>
  );
}

// SectionTitle labels a group of content without adding another raised
// surface of its own - the label itself IS a filled badge rather than plain
// text, matching BombVault's own current theming (jdp: "Das ganze theming
// soll in den Settings so aussehen wie im aktuellen BV Testcontainer").
//
// `hint` sits as a SIBLING immediately beside the badge, not inside its
// coloured fill and not floated to the far right of the row - BombVault's
// own Card component takes an equivalent `hint` prop for exactly this (see
// its Badge.tsx file header: "the InfoBubble... sits as a SIBLING outside
// the badge... keeps InfoBubble's rule-8 contract [neutral, never the
// accent] true"). The previous version put every `right`-slotted node,
// InfoBubble included, behind a `flex-1` spacer that flung it to the far
// edge of the card - correct for a real header action (Add, Refresh), but
// wrong for a one-line explanation that belongs read right after the title
// it explains (jdp: "die cardtitelbadges sind falsch platziert... die
// infobubble der Ecken in den Titelbadge"). `right` still exists, still
// spaced off with its own gap, for the call sites that really do mean a
// far-right action.
export function SectionTitle({
  children,
  hint,
  right,
}: {
  children: ReactNode;
  hint?: string;
  right?: ReactNode;
}) {
  return (
    <div className="flex items-center gap-3">
      <h2 className="flex items-center gap-1.5">
        <span
          className="inline-flex h-[22px] w-fit items-center rounded-[var(--radius-pill)] bg-accentSoft
            px-2.5 text-[11px] font-semibold uppercase tracking-widest text-carbon-textSub"
        >
          {children}
        </span>
        {hint && <InfoBubble tip={hint} />}
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
