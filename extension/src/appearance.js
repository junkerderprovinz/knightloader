// The user's appearance settings, applied as GlimStone does everywhere: an
// attribute or a custom property on <html>, so nothing below needs to know
// which setting produced a value.
//
// Ported from glimstone's packages/core/src/appearance.ts with the types
// stripped. The values are copied verbatim so every app of the family looks
// the same.

/**
 * The GlimStone release these ports follow, packages/core/src/version.ts's
 * number. It lives beside the copies it describes so the Version card cannot
 * claim a release the files are not from, and it is a link, so it has to name
 * a published release. Bump it in the change that lifts the ports.
 */
const GLIMSTONE_VERSION = '4.2.8';

/** The shapes the picker shows. `leaf` is a real shape (see
 *  `data-shape="leaf"` in glimstone.css) that no picker offers; leafTap
 *  reveals it. */
const SHAPES = ['round', 'soft', 'square'];

/** The shapes a stored value may hold. Validate against this and populate the
 *  picker from SHAPES, or a found leaf forgets itself on reload. */
const SHAPES_STORED = [...SHAPES, 'leaf'];

/** Only reaches somebody with no stored shape; a stored choice stays. */
const DEFAULT_SHAPE = 'soft';

/** How many taps on `square`, once it is chosen, reveal the leaf. */
const LEAF_TAPS = 5;

/**
 * leafTap counts the gesture that reveals the leaf: with the shape at
 * `square`, tap `square` five more times. Tapping another shape resets the
 * count. The caller keeps the count and whether the leaf was found in the
 * page's state, never in storage.
 *
 * Returns the shape to switch to, or undefined when the tap was not the fifth.
 */
function leafTap(state, tapped, current) {
  if (tapped !== 'square' || current !== 'square') {
    state.taps = 0;
    return undefined;
  }
  state.taps += 1;
  if (state.taps < LEAF_TAPS) return undefined;
  state.taps = 0;
  return 'leaf';
}

/** The accent every app of the family starts with. */
const DEFAULT_ACCENT = '#FCC419';

const ACCENTS = [
  { name: 'Sunflower', hex: '#FCC419' },
  { name: 'Blue', hex: '#1D99F3' },
  { name: 'Green', hex: '#6FDC8C' },
  { name: 'Red', hex: '#FF8389' },
  { name: 'Purple', hex: '#BE95FF' },
  { name: 'Orange', hex: '#FF832B' },
  { name: 'Teal', hex: '#3DDBD9' },
  { name: 'Pink', hex: '#FF7EB6' },
];

function validHex(hex) {
  return typeof hex === 'string' && /^#[0-9a-fA-F]{6}$/.test(hex);
}

function parseHex(hex) {
  const n = parseInt(hex.slice(1), 16);
  return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
}

/**
 * accentSlot returns the preset slot a colour belongs to: the preset itself, or
 * the nearest one for a hand-mixed colour, so that slot shows it and reopens
 * the picker on it.
 *
 * Squared RGB distance is enough for eight widely separated hues.
 */
function accentSlot(hex) {
  if (!validHex(hex)) return -1;
  const c = parseHex(hex);
  let best = 0;
  let bestD = Infinity;
  for (let i = 0; i < ACCENTS.length; i++) {
    const p = parseHex(ACCENTS[i].hex);
    const d = (c.r - p.r) ** 2 + (c.g - p.g) ** 2 + (c.b - p.b) ** 2;
    if (d < bestD) {
      bestD = d;
      best = i;
    }
  }
  return best;
}

/**
 * sanitiseCustoms keeps only valid hex colours under valid slot indexes from a
 * stored map of hand-mixed colours. Keys are strings, as storage returns them,
 * and the app stores the same shape (sanitiseCustoms in
 * mobile/src/theme/AppearanceContext.tsx).
 */
function sanitiseCustoms(raw) {
  if (!raw || typeof raw !== 'object') return {};
  const out = {};
  for (const [k, v] of Object.entries(raw)) {
    const i = Number(k);
    if (!Number.isInteger(i) || i < 0 || i >= ACCENTS.length) continue;
    if (validHex(v)) out[String(i)] = v;
  }
  return out;
}

// The contrast arithmetic of WCAG 2 and the inks the colour engine computes
// with it (GlimStone's rule 45), web/src/lib/contrast.ts with the types
// stripped. Every ink reaches 4.5:1, whatever colour somebody picked or the
// system gave. The helpers carry an ink prefix because every script of a page
// shares one global scope.

/** The ratio body text needs, and the floor of every ink computed here. */
const MIN_TEXT_CONTRAST = 4.5;

function inkRgb(hex) {
  const { r, g, b } = parseHex(hex);
  return [r, g, b];
}

function inkHex(c) {
  return '#' + c.map((v) => Math.round(v).toString(16).padStart(2, '0')).join('').toUpperCase();
}

/** `amount` of `to` in `from`, the way color-mix() in srgb mixes. */
function inkMix(from, to, amount) {
  return [0, 1, 2].map((i) => from[i] + (to[i] - from[i]) * amount);
}

// Relative luminance. The channels are linearised first, since raw sRGB
// overstates blue and understates green.
function inkLuminance(c) {
  const lin = (v) => {
    const s = v / 255;
    return s <= 0.04045 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * lin(c[0]) + 0.7152 * lin(c[1]) + 0.0722 * lin(c[2]);
}

function inkRatio(a, b) {
  const x = inkLuminance(a);
  const y = inkLuminance(b);
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}

/** contrastRatio is the WCAG ratio of two `#rrggbb` colours, from 1 to 21. */
function contrastRatio(a, b) {
  return inkRatio(inkRgb(a), inkRgb(b));
}

/**
 * inkPushed moves `from` toward `to` just far enough to reach the minimum
 * against `ground`. The ratio only grows on the way, so halving finds the
 * least amount that does it.
 */
function inkPushed(from, to, ground) {
  const reaches = (t) => inkRatio(inkMix(from, to, t).map(Math.round), ground) >= MIN_TEXT_CONTRAST;
  let lo = 0;
  let hi = 1;
  for (let i = 0; i < 16; i++) {
    const mid = (lo + hi) / 2;
    if (reaches(mid)) hi = mid;
    else lo = mid;
  }
  return inkHex(inkMix(from, to, hi));
}

/**
 * contrastOn is the ink on a fill: white or Carbon's near-black, whichever
 * reads better, and on the mid-tones where neither reaches 4.5:1 the
 * near-black goes on toward black until it does.
 */
function contrastOn(hex) {
  if (!validHex(hex)) return '#FFFFFF';
  const fill = inkRgb(hex);
  const onWhite = inkRatio(fill, [255, 255, 255]);
  const onDark = inkRatio(fill, inkRgb('#161616'));
  if (onWhite > onDark && onWhite >= MIN_TEXT_CONTRAST) return '#FFFFFF';
  if (onDark >= MIN_TEXT_CONTRAST) return '#161616';
  return inkPushed(inkRgb('#161616'), [0, 0, 0], fill);
}

/**
 * accentInks is the accent as text on each theme where glimstone.css's own mix
 * falls short of 4.5:1 on that theme's card (#ffffff light, #262626 dark), and
 * undefined where it holds, which it does for every preset.
 */
function accentInks(hex) {
  if (!validHex(hex)) return { light: undefined, dark: undefined };
  const accent = inkRgb(hex);
  const white = [255, 255, 255];
  const darkCard = inkRgb('#262626');
  const light = inkMix(accent, [0, 0, 0], 0.45);
  return {
    light: inkRatio(light, white) >= MIN_TEXT_CONTRAST ? undefined : inkPushed(light, [0, 0, 0], white),
    dark: inkRatio(accent, darkCard) >= MIN_TEXT_CONTRAST ? undefined : inkPushed(accent, white, darkCard),
  };
}

/**
 * The accent setting that takes its colour from the system (GlimStone's rule
 * 44), stored as this word and never as the colour.
 */
const SYSTEM_ACCENT = 'system';

/**
 * browserAccentWorks says whether CSS AccentColor carries the system's colour
 * here. Firefox and Safari resolve it to the system's colour. Chromium
 * resolves it to a fixed colour of its own, so Chrome, Edge, Opera and every
 * other Chromium browser count as no source. Firefox with
 * resistFingerprinting answers with a fixed colour too, which nothing on the
 * page can tell.
 */
function browserAccentWorks() {
  if (typeof CSS === 'undefined' || !CSS.supports('color', 'AccentColor')) return false;
  return !/Chrom(e|ium)\//.test(navigator.userAgent);
}

/** browserAccent is the system's accent as the browser reports it, or undefined where it is no source. */
function browserAccent() {
  if (!browserAccentWorks()) return undefined;
  const probe = document.createElement('span');
  probe.style.color = 'AccentColor';
  probe.style.display = 'none';
  document.documentElement.append(probe);
  const colour = getComputedStyle(probe).color;
  probe.remove();
  return cssColourToHex(colour);
}

/** cssColourToHex turns a computed rgb() or rgba() colour into #RRGGBB, and anything else into undefined. */
function cssColourToHex(colour) {
  const m = colour.match(/^rgba?\(\s*(\d+)[\s,]+(\d+)[\s,]+(\d+)/);
  if (!m) return undefined;
  return '#' + m.slice(1, 4).map((v) => Number(v).toString(16).padStart(2, '0')).join('').toUpperCase();
}

/** The picker's themes, in the web UI's order. `system` follows the device. */
const THEMES = ['dark', 'light', 'system'];

/**
 * applyTheme puts a chosen theme on <html> as data-theme. `system` removes the
 * attribute, and glimstone.css then reads prefers-color-scheme, so the page
 * changes with the device while it is open, as the web UI does (lib/theme.ts).
 */
function applyTheme(theme) {
  const root = document.documentElement;
  if (theme === 'light' || theme === 'dark') root.setAttribute('data-theme', theme);
  else root.removeAttribute('data-theme');
}

/**
 * applyOled puts data-oled on <html>, which glimstone.css applies only while
 * dark shows (GlimStone's rule 43), so it stays on through light. It belongs
 * to the screen, so it is stored in this browser and never taken from an
 * instance.
 */
function applyOled(on) {
  if (on) document.documentElement.setAttribute('data-oled', '');
  else document.documentElement.removeAttribute('data-oled');
}

/** darkShowing says whether dark shows for a theme, chosen or from the device. */
function darkShowing(theme) {
  if (theme === 'dark') return true;
  if (theme === 'light') return false;
  return !matchMedia('(prefers-color-scheme: light)').matches;
}

function applyShape(shape) {
  document.documentElement.setAttribute('data-shape', SHAPES_STORED.includes(shape) ? shape : DEFAULT_SHAPE);
}

/**
 * applyAccent overrides the accent tokens with a colour or the system's, or
 * removes the override so the theme's own gold returns, as it does for the
 * system's colour in a browser that has none. Where glimstone.css's mix would
 * leave the accent as text under 4.5:1, a theme's own ink is set as
 * --accent-ink-light or --accent-ink-dark.
 */
function applyAccent(accent) {
  const hex = accent === SYSTEM_ACCENT ? browserAccent() : accent;
  const root = document.documentElement.style;
  const inks = accentInks(hex);
  for (const [theme, ink] of Object.entries(inks)) {
    if (ink) root.setProperty(`--accent-ink-${theme}`, ink);
    else root.removeProperty(`--accent-ink-${theme}`);
  }
  if (!validHex(hex)) {
    root.removeProperty('--accent');
    root.removeProperty('--accent-contrast');
    root.removeProperty('--accent-soft');
    return;
  }
  const { r, g, b } = parseHex(hex);
  root.setProperty('--accent', hex);
  root.setProperty('--accent-contrast', contrastOn(hex));
  root.setProperty('--accent-soft', `rgba(${r}, ${g}, ${b}, 0.14)`);
}

/**
 * readAppearance returns the appearance stored in this extension, with the same
 * defaults as every app of the family.
 */
async function readAppearance() {
  const s = await chrome.storage.local.get([
    'theme', 'accent', 'shape',
    'accentSlotChosen', 'accentCustoms',
    'rainbow', 'rainbowReactive', 'rainbowRotate', 'rainbowSeed', 'rainbowPalette',
    'rainbowDisco',
    'followInstance',
    'oled',
  ]);

  const accent = validHex(s.accent) || s.accent === SYSTEM_ACCENT ? s.accent : '';

  /* Which slot is chosen and what each slot was mixed to are stored, because
     neither can be derived from the accent once two slots hold the same colour.
     The key is accentSlotChosen because these plain scripts share one global,
     where accentSlot is the function. */
  const accentChosen =
    Number.isInteger(s.accentSlotChosen) && s.accentSlotChosen >= 0 && s.accentSlotChosen < ACCENTS.length
      ? s.accentSlotChosen
      : undefined;
  const accentCustoms = sanitiseCustoms(s.accentCustoms);
  // An install without the map has its mixed colour only in `accent`; give it
  // to the slot that showed it so it is not lost.
  if (Object.keys(accentCustoms).length === 0 && validHex(accent)) {
    const slot = accentChosen !== undefined ? accentChosen : accentSlot(accent);
    if (slot >= 0) accentCustoms[String(slot)] = accent;
  }

  return {
    // An install that never chose has always followed the device.
    theme: THEMES.includes(s.theme) ? s.theme : 'system',
    accent,
    accentCustoms,
    // Undefined until a slot is chosen; callers then fall back to accentSlot().
    accentChosen,
    shape: SHAPES_STORED.includes(s.shape) ? s.shape : DEFAULT_SHAPE,
    rainbow: {
      on: s.rainbow === true,
      reactive: s.rainbowReactive === true,
      rotate: s.rainbowRotate === true,
      seed: Number.isFinite(s.rainbowSeed) ? s.rainbowSeed : 0,
      palette: usablePalette(s.rainbowPalette),
    },
    // The instance knows nothing of it, so following the instance's look
    // leaves it alone.
    disco: s.rainbowDisco === true,
    // Whether the look, the theme included, comes from the default instance.
    followInstance: s.followInstance === true,
    oled: s.oled === true,
  };
}

async function writeAppearance(next) {
  await chrome.storage.local.set(next);
}

/** applyAppearance puts the stored choices on <html>. Every page calls it
 *  before drawing anything, so nothing is painted twice. */
async function applyAppearance() {
  const a = await readAppearance();
  applyTheme(a.theme);
  applyOled(a.oled);
  applyShape(a.shape);
  applyAccent(a.accent);
  applyRainbow(a.rainbow);
  return a;
}

/**
 * adoptFromInstance takes the look from the default instance through the
 * relay's GET /api/appearance, which needs no host permission.
 *
 * The theme is the one last chosen in the instance's web interface. An
 * instance that names none keeps the local theme. Returns false when there is
 * no instance to ask, so the caller can say so.
 */
async function adoptFromInstance() {
  const target = await readDefaultTarget();
  const res = await withGroup(async ({ siblings, call }) => {
    const pick = siblings.find((s) => s.instanceId === target) ?? siblings[0];
    if (!pick) return null;
    return call(pick.instanceId, 'GET', '/api/appearance');
  });
  if (!res || res.status < 200 || res.status >= 300) return false;
  let a;
  try {
    a = JSON.parse(res.body);
  } catch {
    return false;
  }
  // The instance sends one accent and knows nothing of the slots, so the local
  // slot state is removed rather than left to contradict it. Removed, not set to
  // undefined, which chrome.storage would keep as a value. options.js
  // (ADOPTED_KEYS) restores it when following is switched off.
  await chrome.storage.local.remove(['accentSlotChosen', 'accentCustoms']);
  await writeAppearance({
    ...(THEMES.includes(a.theme) ? { theme: a.theme } : {}),
    accent: validHex(a.accent) ? a.accent : '',
    shape: SHAPES_STORED.includes(a.shape) ? a.shape : DEFAULT_SHAPE,
    rainbow: a.rainbow === true,
    rainbowReactive: a.rainbowReactive === true,
    rainbowRotate: a.rainbowRotate === true,
    rainbowSeed: Number.isFinite(a.rainbowSeed) ? a.rainbowSeed : 0,
    rainbowPalette: Array.isArray(a.rainbowPalette) ? a.rainbowPalette : null,
  });
  return true;
}

/**
 * refreshFollowedLook takes the default instance's look again while following
 * is on and puts it on the page. Pages paint the stored copy first and call
 * this after, so none waits on the relay; without it the copy stays as old as
 * the moment following was switched on. Returns whether the look was taken.
 */
async function refreshFollowedLook() {
  const { followInstance } = await chrome.storage.local.get('followInstance');
  if (followInstance !== true) return false;
  if (!(await adoptFromInstance().catch(() => false))) return false;
  const a = await readAppearance();
  applyTheme(a.theme);
  applyShape(a.shape);
  applyAccent(a.accent);
  applyRainbow(a.rainbow);
  return true;
}

/** The rainbow palette, copied from the reference like the accents. Sunflower
 *  is the default accent, so one position always matches the single accent. */
const RAINBOW = [
  '#FF8389', // red 30
  '#FF832B', // orange 40
  '#FCC419', // sunflower
  '#6FDC8C', // green 30
  '#3DDBD9', // teal 30
  '#1D99F3', // blue
  '#BE95FF', // purple 30
  '#FF7EB6', // magenta 30
];

const RAINBOW_OFF = { on: false, reactive: false, rotate: false, seed: 0, palette: RAINBOW };

let rainbowNow = RAINBOW_OFF;

/** A palette is taken only in full, since one bad entry would hide a row. */
function usablePalette(p) {
  if (!Array.isArray(p) || p.length !== RAINBOW.length) return RAINBOW;
  return p.every(validHex) ? p : RAINBOW;
}

/** The colour for one position, with the rotation applied. */
function rainbowAt(i) {
  const p = rainbowNow.palette;
  const off = rainbowNow.rotate ? rainbowNow.seed : 0;
  const n = ((Math.trunc(i) % p.length) + p.length) % p.length;
  return p[(n + off) % p.length];
}

/**
 * hueVars are the custom properties an element at palette position `i` sets
 * on itself. The `.glim-hue` rules in glimstone.css decide whether the hue
 * shows at rest or on hover. They point at the root's `--rb-*` properties
 * rather than holding a colour, so a palette change or disco's walk lands on
 * the root alone and no element has to be touched again. The class needs these
 * properties, or the accent resolves to nothing.
 */
function hueVars(i) {
  const n = ((Math.trunc(i) % RAINBOW.length) + RAINBOW.length) % RAINBOW.length;
  const hue = `var(--rb-${n})`;
  return {
    '--item-hue': hue,
    '--item-hue-ink': `var(--rb-ink-${n})`,
    '--item-hue-soft': `color-mix(in srgb, ${hue} 22%, transparent)`,
    '--item-hue-wash': `color-mix(in srgb, ${hue} 16%, transparent)`,
    '--item-hue-badge': `color-mix(in srgb, ${hue} 50%, transparent)`,
    '--item-hue-ring': `color-mix(in srgb, ${hue} 55%, transparent)`,
  };
}

/** setHue puts a palette position on one element, class and properties
 *  together. */
function setHue(el, i) {
  el.classList.add('glim-hue');
  for (const [k, v] of Object.entries(hueVars(i))) el.style.setProperty(k, v);
}

/**
 * setHues gives a set of blocks their positions in order, such as the cards of
 * the options page. Positions go on the container because
 * `[data-rainbow] .glim-hue` rebinds --accent for the whole subtree, so badges,
 * switches, buttons and focus rings inside follow.
 */
function setHues(elements) {
  elements.filter(Boolean).forEach((el, i) => setHue(el, i));
}

/** The buttons that carry rule 40's tint (glimstone.css). */
const TINTED_BUTTONS = '.glim-btn-neutral, .glim-btn-subtle, .glim-btn-bare';

/**
 * takePageHue gives a button the palette position of its place among the
 * page's buttons, so under rainbow two buttons side by side light up in
 * different colours (GlimStone's rule 40). It is read when the pointer or the
 * keyboard reaches the button, since the order changes with the page.
 */
function takePageHue(el) {
  if (!rainbowNow.on) {
    el.style.removeProperty('--btn-tint');
    return;
  }
  const i = Array.prototype.indexOf.call(document.querySelectorAll(TINTED_BUTTONS), el);
  const n = ((i % RAINBOW.length) + RAINBOW.length) % RAINBOW.length;
  el.style.setProperty('--btn-tint', `color-mix(in srgb, var(--rb-${n}) var(--ink-mix), black)`);
}

/**
 * listenForPageHue hands every tinted button its place's colour as the pointer
 * or the keyboard reaches it. It listens on the document, so the windows and
 * the cards drawn later are covered too. A button with a palette position of
 * its own keeps that one.
 */
function listenForPageHue() {
  const reached = (target, from) => {
    if (!(target instanceof Element)) return;
    const el = target.closest(TINTED_BUTTONS);
    if (!el || el.matches('.glim-hue')) return;
    // pointerover fires again for every child the pointer crosses inside it.
    if (from instanceof Node && el.contains(from)) return;
    takePageHue(el);
  };
  document.addEventListener('pointerover', (e) => reached(e.target, e.relatedTarget));
  document.addEventListener('focusin', (e) => reached(e.target, null));
}

/** applyRainbow stores the state and writes the palette, with the ink for each
 *  colour, onto the root. Disco lives in disco.js. */
function applyRainbow(next) {
  const merged = { ...RAINBOW_OFF, ...next };
  merged.palette = usablePalette(merged.palette);
  merged.seed = Number.isFinite(merged.seed) ? Math.abs(Math.trunc(merged.seed)) % RAINBOW.length : 0;
  rainbowNow = merged;

  const root = document.documentElement;
  for (let i = 0; i < RAINBOW.length; i++) {
    root.style.setProperty(`--rb-${i}`, rainbowAt(i));
    root.style.setProperty(`--rb-ink-${i}`, contrastOn(rainbowAt(i)));
  }
  const mode = !merged.on ? 'off' : merged.reactive ? 'reactive' : 'on';
  if (mode === 'off') root.removeAttribute('data-rainbow');
  else root.setAttribute('data-rainbow', mode);
}
