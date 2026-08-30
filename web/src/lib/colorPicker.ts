// The colour picker engine (design-language.md, "The colour engine" →
// picker section). A saturation/value square + hue bar, drawn entirely in
// the page's own DOM - never a native `<input type="color">`. Extracted
// from CannonadeCommand's own inlinePicker(), the reference an adopting
// app should match rather than build its own variant of. Two ways to show
// it: `colorPicker()` returns the bare, permanently-embeddable widget;
// `openColorPickerPopover()` (below) wraps it in a floating panel anchored
// to a trigger element - the recommended default for a compact settings
// card, where embedding it permanently grows the card every time one is
// added.
//
// Why this exists instead of `<input type="color">`: a native colour input
// hands control to the browser/OS, which can (and on many setups does)
// open its own top-level picker surface entirely outside the page - jdp,
// building CC: "ich will das Farbwählfeld fest integriert" (I want the
// colour field permanently embedded), and the same rejection recurred
// nearly verbatim in an adopting app that tried the native-input shortcut
// anyway ("es soll sich kein komplett neues Fenster öffnen"). A native
// input is also functionally unverifiable in Playwright - its picker
// surface is outside the page's own DOM, so no automated check can ever
// prove it opens, let alone that it opens the RIGHT way. This component
// has neither problem: it's real, styleable DOM either way.
//
// Framework-free, like appearance.ts/selectScroll.ts/tooltip.ts: talks
// only to the elements it's given and returns plain DOM nodes.

export interface ColorPicker {
  /** The root element - append this wherever the picker should render. */
  el: HTMLDivElement;
  /** Programmatically sync the picker to a hex value (no onChange firing). */
  setValue: (hex: string) => void;
  /** The picker's current value as a 6-digit lowercase hex string. */
  getValue: () => string;
}

interface Hsv {
  h: number;
  s: number;
  v: number;
}

function hexToHsv(hex: string): Hsv | null {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex || '');
  const group = m?.[1];
  if (!group) return null;
  const n = parseInt(group, 16);
  const r = ((n >> 16) & 255) / 255;
  const g = ((n >> 8) & 255) / 255;
  const b = (n & 255) / 255;
  const mx = Math.max(r, g, b);
  const mn = Math.min(r, g, b);
  const d = mx - mn;
  let h = 0;
  if (d) {
    if (mx === r) h = 60 * (((g - b) / d) % 6);
    else if (mx === g) h = 60 * ((b - r) / d + 2);
    else h = 60 * ((r - g) / d + 4);
  }
  if (h < 0) h += 360;
  return { h, s: mx ? d / mx : 0, v: mx };
}

function hsvToHex(h: number, s: number, v: number): string {
  const c = v * s;
  const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
  const m = v - c;
  let r = 0;
  let g = 0;
  let b = 0;
  if (h < 60) {
    r = c;
    g = x;
  } else if (h < 120) {
    r = x;
    g = c;
  } else if (h < 180) {
    g = c;
    b = x;
  } else if (h < 240) {
    g = x;
    b = c;
  } else if (h < 300) {
    r = x;
    b = c;
  } else {
    r = c;
    b = x;
  }
  const f = (u: number) => Math.round((u + m) * 255).toString(16).padStart(2, '0');
  return `#${f(r)}${f(g)}${f(b)}`;
}

/** normalizeHex accepts "2f6feb" or "#2F6FEB", returns "#rrggbb" lowercase, or null if invalid. */
function normalizeHex(value: string): string | null {
  const trimmed = value.trim().replace(/^#/, '');
  return /^[0-9a-f]{6}$/i.test(trimmed) ? `#${trimmed.toLowerCase()}` : null;
}

/**
 * Builds an always-visible saturation/value square + hue bar. `onChange`
 * fires with a 6-digit lowercase hex string on every drag update (mouse and
 * touch both wired) - the caller decides whether to commit that
 * immediately or debounce it (CC's own header-colour row waits 700ms after
 * the picker settles before writing to disk, so a drag doesn't spam every
 * intermediate frame; a purely in-memory/localStorage write like an
 * accent or rainbow-palette colour can just apply every event).
 */
export function colorPicker(initialHex: string, onChange: (hex: string) => void): ColorPicker {
  const el = document.createElement('div');
  el.className = 'glim-picker';

  const sv = document.createElement('div');
  sv.className = 'glim-picker-sv';
  const dot = document.createElement('span');
  dot.className = 'glim-picker-dot';
  sv.appendChild(dot);

  const hue = document.createElement('div');
  hue.className = 'glim-picker-hue';
  const hdot = document.createElement('span');
  hdot.className = 'glim-picker-hdot';
  hue.appendChild(hdot);

  el.append(sv, hue);

  let state: Hsv = hexToHsv(initialHex) || { h: 220, s: 0.8, v: 0.9 };

  function paint(): void {
    sv.style.background = `linear-gradient(to top, #000, rgba(0,0,0,0)), linear-gradient(to right, #fff, hsl(${Math.round(state.h)},100%,50%))`;
    dot.style.left = `${state.s * 100}%`;
    dot.style.top = `${(1 - state.v) * 100}%`;
    hdot.style.left = `${(state.h / 360) * 100}%`;
  }

  function emit(): void {
    onChange(hsvToHex(state.h, state.s, state.v));
  }

  function drag(target: HTMLElement, apply: (x: number, y: number) => void): void {
    function move(event: MouseEvent | TouchEvent): void {
      const rect = target.getBoundingClientRect();
      const point = 'touches' in event ? (event.touches[0] ?? event.changedTouches[0]) : event;
      if (!point) return;
      const x = Math.min(1, Math.max(0, (point.clientX - rect.left) / rect.width));
      const y = Math.min(1, Math.max(0, (point.clientY - rect.top) / rect.height));
      apply(x, y);
      paint();
      emit();
      event.preventDefault();
    }
    function up(): void {
      document.removeEventListener('mousemove', move);
      document.removeEventListener('mouseup', up);
      document.removeEventListener('touchmove', move);
      document.removeEventListener('touchend', up);
    }
    function down(event: MouseEvent | TouchEvent): void {
      move(event);
      document.addEventListener('mousemove', move as EventListener);
      document.addEventListener('mouseup', up);
      document.addEventListener('touchmove', move as EventListener);
      document.addEventListener('touchend', up);
    }
    target.addEventListener('mousedown', down as EventListener);
    target.addEventListener('touchstart', down as EventListener);
  }

  drag(sv, (x, y) => {
    state = { ...state, s: x, v: 1 - y };
  });
  drag(hue, (x) => {
    state = { ...state, h: Math.min(359.9, x * 360) };
  });

  paint();

  return {
    el,
    setValue: (hex: string) => {
      const parsed = hexToHsv(hex);
      if (parsed) {
        state = parsed;
        paint();
      }
    },
    getValue: () => hsvToHex(state.h, state.s, state.v),
  };
}

export interface ColorPickerPopoverHandle {
  /** Closes the popover programmatically (also happens automatically on
   *  outside click, Escape, scroll, or resize). */
  close: () => void;
}

let openPopover: { el: HTMLDivElement; close: () => void } | null = null;

/**
 * Opens the picker as a floating popover anchored below `trigger` instead
 * of embedding it permanently in the layout - the default for a compact
 * settings panel, where a permanently-embedded picker grows the
 * surrounding card every time one is added (jdp, adopting this in a
 * compact settings card: "der Farbpicker soll per schwebendem Fenster
 * erscheinen, nicht fix in der card sein"). Reserve the bare
 * `colorPicker()` for a page with genuinely dedicated, permanent space for
 * exactly one control (CannonadeCommand's own settings PAGE, not a card).
 * Only ONE popover is ever open at a time - opening a new one closes
 * whichever was already open, matching how a native `<select>` only ever
 * has one open dropdown.
 */
export function openColorPickerPopover(
  trigger: HTMLElement,
  initialHex: string,
  onChange: (hex: string) => void,
  onClose?: () => void,
): ColorPickerPopoverHandle {
  openPopover?.close();

  const panel = document.createElement('div');
  panel.className = 'glim-picker-popover';

  // The hex field beside the picker (jdp: "das hex feld fehlt im
  // colorpicker") - CannonadeCommand's own `cc-set-hexin` pairs with
  // `inlinePicker()` everywhere it appears; a picker with no way to type or
  // read back an exact value is missing half of what "a colour field"
  // means. Bidirectional: dragging the picker updates the hex text,
  // typing a valid 6-digit hex re-syncs the picker's own dot positions.
  const hexInput = document.createElement('input');
  hexInput.type = 'text';
  hexInput.className = 'glim-picker-hex';
  hexInput.maxLength = 7;
  hexInput.spellcheck = false;
  hexInput.value = initialHex;
  hexInput.setAttribute('aria-label', 'Hex');

  const picker = colorPicker(initialHex, (hex) => {
    hexInput.value = hex;
    onChange(hex);
  });

  hexInput.addEventListener('input', () => {
    const normalized = normalizeHex(hexInput.value);
    if (!normalized) return;
    picker.setValue(normalized);
    onChange(normalized);
  });

  panel.append(picker.el, hexInput);
  document.body.appendChild(panel);

  function position(): void {
    const rect = trigger.getBoundingClientRect();
    const vw = document.documentElement.clientWidth || window.innerWidth;
    const vh = document.documentElement.clientHeight || window.innerHeight;
    const width = panel.offsetWidth;
    const height = panel.offsetHeight;
    const left = Math.max(8, Math.min(vw - 8 - width, rect.left));
    const fitsBelow = rect.bottom + 8 + height <= vh;
    const top = fitsBelow ? rect.bottom + 8 : Math.max(8, rect.top - 8 - height);
    panel.style.left = `${left}px`;
    panel.style.top = `${top}px`;
  }
  position();

  let closed = false;
  function close(): void {
    if (closed) return;
    closed = true;
    panel.remove();
    document.removeEventListener('pointerdown', onPointerDown, true);
    document.removeEventListener('keydown', onKeyDown);
    window.removeEventListener('scroll', close, true);
    window.removeEventListener('resize', close);
    if (openPopover?.el === panel) openPopover = null;
    // `onClose` exists for one specific, non-obvious constraint: while the
    // popover is open, the row that owns the trigger must NOT be re-rendered.
    // Replacing the trigger element strands this popover's own outside-click
    // handler on a node that is no longer in the page, so a second click on
    // the (new) trigger neither closes nor re-opens anything. A caller that
    // redraws its row after a colour change therefore applies the colour live
    // in `onChange` and does the redraw here, once, when the picker is gone.
    // Found building KnightLoader's extension options page, where the reset
    // badge and the diagnostic report both depend on that redraw.
    onClose?.();
  }
  // Capture phase, and excludes the trigger itself - a second click on the
  // trigger re-opens fresh (via the caller's own click handler) rather
  // than being swallowed here first.
  function onPointerDown(event: PointerEvent): void {
    const target = event.target;
    if (target instanceof Node && (panel.contains(target) || trigger.contains(target))) return;
    close();
  }
  function onKeyDown(event: KeyboardEvent): void {
    if (event.key === 'Escape') close();
  }
  document.addEventListener('pointerdown', onPointerDown, true);
  document.addEventListener('keydown', onKeyDown);
  // A fixed-position popover de-anchors from its trigger on scroll/resize -
  // simplest correct behaviour is to close it, same as the tooltip bubble.
  window.addEventListener('scroll', close, true);
  window.addEventListener('resize', close);

  openPopover = { el: panel, close };
  return { close };
}
