// The colour picker: a saturation/value square, a hue bar and a hex field,
// drawn in this page's own DOM.
//
// Ported from the shared reference (github.com/junkerderprovinz/glimstone,
// reference/colorPicker.ts) with its types stripped, the same way
// appearance.js was - this extension has no build step, src/ ships as plain
// files. The VALUES and the geometry are copied, not re-picked by eye.
//
// Why this exists rather than <input type="color">: the design language rules
// the native input out (jdp, building CannonadeCommand: "ich will das
// Farbwählfeld fest integriert", and again in another app: "es soll sich kein
// komplett neues Fenster öffnen"). A native picker also hands its surface to
// the browser, which on Windows opens a genuinely separate top-level window -
// and nothing in an automated check can ever prove that window opened
// correctly, because it is not in the page. This one is real DOM either way.
//
// The one deliberate divergence from the reference: no hairline around the SV
// square. Surfaces here are separated by shade, never by a line.

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

/**
 * colorPicker builds the bare widget. onChange fires on every drag frame with
 * a lowercase hex - the caller decides whether that is cheap enough to apply
 * immediately, which for a stored colour in extension storage it is.
 */
function colorPicker(initialHex, onChange) {
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

  let state = pickerHexToHsv(initialHex) || { h: 220, s: 0.8, v: 0.9 };

  function paint() {
    sv.style.background =
      `linear-gradient(to top, #000, rgba(0,0,0,0)), linear-gradient(to right, #fff, hsl(${Math.round(state.h)},100%,50%))`;
    dot.style.left = `${state.s * 100}%`;
    dot.style.top = `${(1 - state.v) * 100}%`;
    hdot.style.left = `${(state.h / 360) * 100}%`;
  }

  function drag(target, apply) {
    function move(event) {
      const rect = target.getBoundingClientRect();
      const point = event.touches ? (event.touches[0] ?? event.changedTouches[0]) : event;
      if (!point) return;
      const x = Math.min(1, Math.max(0, (point.clientX - rect.left) / rect.width));
      const y = Math.min(1, Math.max(0, (point.clientY - rect.top) / rect.height));
      apply(x, y);
      paint();
      onChange(pickerHsvToHex(state.h, state.s, state.v));
      event.preventDefault();
    }
    function up() {
      document.removeEventListener('mousemove', move);
      document.removeEventListener('mouseup', up);
      document.removeEventListener('touchmove', move);
      document.removeEventListener('touchend', up);
    }
    function down(event) {
      move(event);
      document.addEventListener('mousemove', move);
      document.addEventListener('mouseup', up);
      document.addEventListener('touchmove', move, { passive: false });
      document.addEventListener('touchend', up);
    }
    target.addEventListener('mousedown', down);
    target.addEventListener('touchstart', down, { passive: false });
  }

  drag(sv, (x, y) => { state = { ...state, s: x, v: 1 - y }; });
  drag(hue, (x) => { state = { ...state, h: Math.min(359.9, x * 360) }; });

  paint();

  return {
    el,
    setValue: (hex) => {
      const parsed = pickerHexToHsv(hex);
      if (parsed) { state = parsed; paint(); }
    },
    getValue: () => pickerHsvToHex(state.h, state.s, state.v),
  };
}

let openPickerPopover = null;

/**
 * openColorPickerPopover floats the picker under the swatch that opened it,
 * rather than embedding it: a settings card that grows by a picker every time
 * a colour is added is a card that stops being a card. Only one is ever open,
 * the same way a browser only ever has one dropdown open.
 */
function openColorPickerPopover(trigger, initialHex, onChange, onClose) {
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

  const rect = trigger.getBoundingClientRect();
  const vw = document.documentElement.clientWidth || window.innerWidth;
  const vh = document.documentElement.clientHeight || window.innerHeight;
  const left = Math.max(8, Math.min(vw - 8 - panel.offsetWidth, rect.left));
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
    // onClose is this port's one addition to the shared reference, and it is
    // needed for a reason the reference has not hit yet: while the popover is
    // open, the row that owns the trigger must NOT be re-rendered - replacing
    // the trigger element strands the popover's own outside-click handler on a
    // node no longer in the page. So the caller applies the colour live and
    // redraws the row here, once, when the picker is gone.
    onClose?.();
  }
  // Capture phase, and the trigger is excluded on purpose: a second click on
  // the swatch should re-open through the caller's own handler rather than be
  // swallowed here first.
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
  // Fixed position de-anchors from its trigger the moment the page moves, so
  // it closes rather than floating somewhere wrong - same call the tooltip
  // bubble makes.
  window.addEventListener('scroll', close, true);
  window.addEventListener('resize', close);

  openPickerPopover = { el: panel, close };
  return { close };
}
