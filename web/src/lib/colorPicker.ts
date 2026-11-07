// The colour picker (GlimStone, "The colour engine"): a saturation/value
// square and a hue bar in the page's own DOM, taken from CannonadeCommand's
// inlinePicker(). colorPicker() returns the embeddable widget;
// openColorPickerPopover() floats it next to a trigger, which suits a compact
// settings card.
//
// Not <input type="color">: the native picker often opens as a separate OS
// window outside the page, and automated tests cannot reach it.
//
// Framework-free: it works on the elements it is given and returns plain DOM.

export interface ColorPicker {
  /** The root element - append this wherever the picker should render. */
  el: HTMLDivElement;
  /** Programmatically sync the picker to a hex value (no onChange firing). */
  setValue: (hex: string) => void;
  /** The picker's current value as a 6-digit lowercase hex string. */
  getValue: () => string;
  /** Moves the focus to the square, where the arrow keys move the handle. */
  focus: () => void;
}

/** The accessible names of the square and the hue bar, in the reader's language. */
export interface ColorPickerText {
  area: string;
  hue: string;
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

const HUE_MAX = 359.9;
const clamp = (n: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, n));

/**
 * pickerKeyStep is what an arrow key does: on the square left and right move
 * the saturation and up and down the brightness, on the hue bar every arrow
 * moves the hue. Shift takes bigger steps, Home and End go to an end of the
 * bar. Null for a key the picker leaves alone.
 */
function pickerKeyStep(part: 'area' | 'hue', key: string, shift: boolean, hsv: Hsv): Hsv | null {
  if (part === 'hue') {
    if (key === 'Home') return { ...hsv, h: 0 };
    if (key === 'End') return { ...hsv, h: HUE_MAX };
    const dir = ({ ArrowRight: 1, ArrowUp: 1, ArrowLeft: -1, ArrowDown: -1 } as Record<string, number>)[key];
    return dir ? { ...hsv, h: clamp(hsv.h + dir * (shift ? 30 : 3), 0, HUE_MAX) } : null;
  }
  const step = shift ? 0.1 : 0.02;
  const ds = ({ ArrowRight: 1, ArrowLeft: -1 } as Record<string, number>)[key] ?? 0;
  const dv = ({ ArrowUp: 1, ArrowDown: -1 } as Record<string, number>)[key] ?? 0;
  if (!ds && !dv) return null;
  return { ...hsv, s: clamp(hsv.s + ds * step, 0, 1), v: clamp(hsv.v + dv * step, 0, 1) };
}

/**
 * colorPicker builds an always-visible saturation/value square and hue bar.
 * `onChange` gets a lowercase "#rrggbb" on every step of a drag with mouse,
 * pen or finger and on every arrow key; a caller that writes to disk should
 * debounce it.
 */
export function colorPicker(initialHex: string, onChange: (hex: string) => void, text?: ColorPickerText): ColorPicker {
  const el = document.createElement('div');
  el.className = 'glim-picker';

  const sv = document.createElement('div');
  sv.className = 'glim-picker-sv';
  sv.tabIndex = 0;
  sv.setAttribute('role', 'slider');
  if (text) sv.setAttribute('aria-label', text.area);
  const dot = document.createElement('span');
  dot.className = 'glim-picker-dot';
  sv.appendChild(dot);

  const hue = document.createElement('div');
  hue.className = 'glim-picker-hue';
  hue.tabIndex = 0;
  hue.setAttribute('role', 'slider');
  if (text) hue.setAttribute('aria-label', text.hue);
  hue.setAttribute('aria-valuemin', '0');
  hue.setAttribute('aria-valuemax', '360');
  const hdot = document.createElement('span');
  hdot.className = 'glim-picker-hdot';
  hue.appendChild(hdot);

  el.append(sv, hue);

  let state: Hsv = hexToHsv(initialHex) || { h: 220, s: 0.8, v: 0.9 };

  function paint(): void {
    const hex = hsvToHex(state.h, state.s, state.v);
    sv.style.background = `linear-gradient(to top, #000, rgba(0,0,0,0)), linear-gradient(to right, #fff, hsl(${Math.round(state.h)},100%,50%))`;
    dot.style.left = `${state.s * 100}%`;
    dot.style.top = `${(1 - state.v) * 100}%`;
    hdot.style.left = `${(state.h / 360) * 100}%`;
    el.style.setProperty('--glim-picker-colour', hex);
    // The square moves along two axes, so the one value that names its place is the colour itself.
    sv.setAttribute('aria-valuetext', hex);
    hue.setAttribute('aria-valuenow', String(Math.round(state.h)));
  }

  function emit(): void {
    onChange(hsvToHex(state.h, state.s, state.v));
  }

  // Pointer capture keeps a drag going once the pointer leaves the square, for
  // mouse, pen and finger alike, with no listener on the document.
  function drag(target: HTMLElement, apply: (x: number, y: number) => void): void {
    function at(event: PointerEvent): void {
      const rect = target.getBoundingClientRect();
      apply(clamp((event.clientX - rect.left) / rect.width, 0, 1), clamp((event.clientY - rect.top) / rect.height, 0, 1));
      paint();
      emit();
    }
    target.addEventListener('pointerdown', (event) => {
      if (event.button !== 0) return;
      event.preventDefault();
      target.focus();
      target.setPointerCapture(event.pointerId);
      at(event);
    });
    target.addEventListener('pointermove', (event) => {
      if (target.hasPointerCapture(event.pointerId)) at(event);
    });
  }

  drag(sv, (x, y) => {
    state = { ...state, s: x, v: 1 - y };
  });
  drag(hue, (x) => {
    state = { ...state, h: Math.min(HUE_MAX, x * 360) };
  });

  for (const [part, target] of [
    ['area', sv],
    ['hue', hue],
  ] as const) {
    target.addEventListener('keydown', (event) => {
      const next = pickerKeyStep(part, event.key, event.shiftKey, state);
      if (!next) return;
      event.preventDefault();
      state = next;
      paint();
      emit();
    });
  }

  paint();

  return {
    el,
    setValue: (hex: string) => {
      const parsed = hexToHsv(hex);
      if (!parsed) return;
      // A grey or black has no hue of its own, so the bar stays where it was.
      state = parsed.s === 0 || parsed.v === 0 ? { ...parsed, h: state.h } : parsed;
      paint();
    },
    getValue: () => hsvToHex(state.h, state.s, state.v),
    focus: () => sv.focus(),
  };
}

export interface ColorPickerPopoverHandle {
  /** Closes the popover programmatically (also happens automatically on
   *  outside click, Escape, scroll, or resize). */
  close: () => void;
}

let openPopover: { el: HTMLDivElement; close: () => void } | null = null;

/**
 * openColorPickerPopover opens the picker in a floating panel below
 * `trigger`, so a settings card does not grow with every colour it offers.
 * Only one popover is open at a time; opening another closes the first.
 */
export function openColorPickerPopover(
  trigger: HTMLElement,
  initialHex: string,
  onChange: (hex: string) => void,
  text?: ColorPickerText,
  onClose?: () => void,
): ColorPickerPopoverHandle {
  openPopover?.close();

  const panel = document.createElement('div');
  panel.className = 'glim-picker-popover';

  // The hex field, for typing or reading an exact value. Dragging updates the
  // text and a valid hex moves the picker.
  const hexInput = document.createElement('input');
  hexInput.type = 'text';
  hexInput.className = 'glim-picker-hex';
  hexInput.maxLength = 7;
  hexInput.spellcheck = false;
  hexInput.value = initialHex;
  hexInput.setAttribute('aria-label', 'Hex');

  const picker = colorPicker(
    initialHex,
    (hex) => {
      hexInput.value = hex;
      onChange(hex);
    },
    text,
  );

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
    // The row owning the trigger must not be re-rendered while the popover is
    // open: a replaced trigger strands the outside-click handler. A caller
    // applies the colour live in onChange and redraws its row here.
    onClose?.();
  }
  // Capture phase, ignoring the trigger, so a second click on the trigger
  // reaches the caller's handler.
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
  // A fixed popover loses its anchor on scroll or resize, so it closes.
  window.addEventListener('scroll', close, true);
  window.addEventListener('resize', close);

  openPopover = { el: panel, close };
  return { close };
}
