// The colour picker: a saturation/value square, a hue bar and a hex field,
// drawn in the page's own DOM.
//
// Ported from glimstone's packages/core/src/colorPicker.ts with the types
// stripped; values and geometry are copied. The design language rules out
// <input type="color">, which on Windows opens a separate window outside the
// page. Unlike the reference, the SV square has no hairline, since surfaces
// here are separated by shade.

function pickerHexToHsv(hex) {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex || '');
  if (!m) return null;
  const n = parseInt(m[1], 16);
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

function pickerHsvToHex(h, s, v) {
  const c = v * s;
  const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
  const m = v - c;
  let r = 0;
  let g = 0;
  let b = 0;
  if (h < 60) { r = c; g = x; }
  else if (h < 120) { r = x; g = c; }
  else if (h < 180) { g = c; b = x; }
  else if (h < 240) { g = x; b = c; }
  else if (h < 300) { r = x; b = c; }
  else { r = c; b = x; }
  const f = (u) => Math.round((u + m) * 255).toString(16).padStart(2, '0');
  return `#${f(r)}${f(g)}${f(b)}`;
}

/** Accepts "2f6feb" or "#2F6FEB", returns "#rrggbb" lowercase, or null. */
function normalizeHex(value) {
  const trimmed = String(value ?? '').trim().replace(/^#/, '');
  return /^[0-9a-f]{6}$/i.test(trimmed) ? `#${trimmed.toLowerCase()}` : null;
}

const PICKER_HUE_MAX = 359.9;
const pickerClamp = (n, lo, hi) => Math.min(hi, Math.max(lo, n));

/**
 * pickerKeyStep is what an arrow key does to the picker: on the square left
 * and right move the saturation and up and down the brightness, on the hue bar
 * every arrow moves the hue. Shift takes bigger steps, Home and End go to an
 * end of the bar. Returns null for a key the picker leaves alone.
 */
function pickerKeyStep(part, key, shift, hsv) {
  if (part === 'hue') {
    if (key === 'Home') return { ...hsv, h: 0 };
    if (key === 'End') return { ...hsv, h: PICKER_HUE_MAX };
    const dir = { ArrowRight: 1, ArrowUp: 1, ArrowLeft: -1, ArrowDown: -1 }[key];
    return dir ? { ...hsv, h: pickerClamp(hsv.h + dir * (shift ? 30 : 3), 0, PICKER_HUE_MAX) } : null;
  }
  const step = shift ? 0.1 : 0.02;
  const ds = { ArrowRight: 1, ArrowLeft: -1 }[key] ?? 0;
  const dv = { ArrowUp: 1, ArrowDown: -1 }[key] ?? 0;
  if (!ds && !dv) return null;
  return { ...hsv, s: pickerClamp(hsv.s + ds * step, 0, 1), v: pickerClamp(hsv.v + dv * step, 0, 1) };
}

/**
 * colorPicker builds the bare widget. The square and the bar are named sliders
 * (GlimStone 4.1.0), `text` holding their names as { area, hue }. onChange
 * fires with a lowercase hex on every step of a drag with mouse, pen or finger
 * and on every arrow key.
 */
function colorPicker(initialHex, onChange, text) {
  const el = document.createElement('div');
  el.className = 'glim-picker';

  const sv = document.createElement('div');
  sv.className = 'glim-picker-sv';
  sv.tabIndex = 0;
  sv.setAttribute('role', 'slider');
  sv.setAttribute('aria-label', text.area);
  const dot = document.createElement('span');
  dot.className = 'glim-picker-dot';
  sv.appendChild(dot);

  const hue = document.createElement('div');
  hue.className = 'glim-picker-hue';
  hue.tabIndex = 0;
  hue.setAttribute('role', 'slider');
  hue.setAttribute('aria-label', text.hue);
  hue.setAttribute('aria-valuemin', '0');
  hue.setAttribute('aria-valuemax', '360');
  const hdot = document.createElement('span');
  hdot.className = 'glim-picker-hdot';
  hue.appendChild(hdot);

  el.append(sv, hue);

  let state = pickerHexToHsv(initialHex) || { h: 220, s: 0.8, v: 0.9 };

  function paint() {
    const hex = pickerHsvToHex(state.h, state.s, state.v);
    sv.style.background =
      `linear-gradient(to top, #000, rgba(0,0,0,0)), linear-gradient(to right, #fff, hsl(${Math.round(state.h)},100%,50%))`;
    dot.style.left = `${state.s * 100}%`;
    dot.style.top = `${(1 - state.v) * 100}%`;
    hdot.style.left = `${(state.h / 360) * 100}%`;
    el.style.setProperty('--glim-picker-colour', hex);
    // The square moves along two axes, so the one value that names its place
    // is the colour itself.
    sv.setAttribute('aria-valuetext', hex);
    hue.setAttribute('aria-valuenow', String(Math.round(state.h)));
  }

  function emit() {
    onChange(pickerHsvToHex(state.h, state.s, state.v));
  }

  // Pointer capture keeps the drag going once the pointer leaves the square,
  // for mouse, pen and finger alike, with no listener on the document.
  function drag(target, apply) {
    function at(event) {
      const rect = target.getBoundingClientRect();
      apply(
        pickerClamp((event.clientX - rect.left) / rect.width, 0, 1),
        pickerClamp((event.clientY - rect.top) / rect.height, 0, 1),
      );
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
    state = { ...state, h: Math.min(PICKER_HUE_MAX, x * 360) };
  });

  for (const [part, target] of [['area', sv], ['hue', hue]]) {
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
    setValue: (hex) => {
      const parsed = pickerHexToHsv(hex);
      if (!parsed) return;
      // A grey or black has no hue of its own, so the bar stays where it was.
      state = parsed.s === 0 || parsed.v === 0 ? { ...parsed, h: state.h } : parsed;
      paint();
    },
    getValue: () => pickerHsvToHex(state.h, state.s, state.v),
  };
}

let openPickerPopover = null;

/**
 * openColorPickerPopover floats the picker under the swatch that opened it, so
 * the settings card does not grow. Only one is open at a time. `text` names
 * the square and the bar, as colorPicker takes it.
 */
function openColorPickerPopover(trigger, initialHex, onChange, onClose, text) {
  openPickerPopover?.close();

  const panel = document.createElement('div');
  panel.className = 'glim-picker-popover';

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

  const rect = trigger.getBoundingClientRect();
  const vw = document.documentElement.clientWidth || window.innerWidth;
  const vh = document.documentElement.clientHeight || window.innerHeight;
  // In a right-to-left page the panel lines up with the trigger's right edge.
  // The direction is read from <html>, which is what i18n.js actually applied.
  const rtl = document.documentElement.dir === 'rtl';
  const anchor = rtl ? rect.right - panel.offsetWidth : rect.left;
  const left = Math.max(8, Math.min(vw - 8 - panel.offsetWidth, anchor));
  const fitsBelow = rect.bottom + 8 + panel.offsetHeight <= vh;
  panel.style.left = `${left}px`;
  panel.style.top = `${fitsBelow ? rect.bottom + 8 : Math.max(8, rect.top - 8 - panel.offsetHeight)}px`;

  let closed = false;
  function close() {
    if (closed) return;
    closed = true;
    panel.remove();
    document.removeEventListener('pointerdown', onPointerDown, true);
    document.removeEventListener('keydown', onKeyDown);
    window.removeEventListener('scroll', close, true);
    window.removeEventListener('resize', close);
    if (openPickerPopover?.el === panel) openPickerPopover = null;
    // Not in the reference. Re-rendering the trigger's row while the popover
    // is open would strand the outside-click handler on a detached node, so
    // the caller redraws the row here.
    onClose?.();
  }
  // The trigger is excluded so a second click on the swatch reaches the
  // caller's own handler.
  function onPointerDown(event) {
    const target = event.target;
    if (target instanceof Node && (panel.contains(target) || trigger.contains(target))) return;
    close();
  }
  function onKeyDown(event) {
    if (event.key === 'Escape') close();
  }
  document.addEventListener('pointerdown', onPointerDown, true);
  document.addEventListener('keydown', onKeyDown);
  // A fixed panel loses its trigger when the page moves, so it closes.
  window.addEventListener('scroll', close, true);
  window.addEventListener('resize', close);

  openPickerPopover = { el: panel, close };
  return { close };
}
