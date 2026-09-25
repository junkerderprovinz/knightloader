// The user's appearance settings, applied as GlimStone does everywhere: an
// attribute or a custom property on <html>, so nothing below needs to know
// which setting produced a value.
//
// Ported from glimstone's reference/appearance.ts with the types stripped. The
// values are copied verbatim so every app of the family looks the same.

/**
 * The GlimStone release these ports follow, reference/react/version.ts's
 * number. It lives beside the copies it describes so the About card cannot
 * claim a release the files are not from, and it is a link, so it has to name
 * a published release. Bump it in the change that lifts the ports.
 */
const GLIMSTONE_VERSION = '2.12.0';

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

/**
 * luminance is the perceptual brightness that decides black or white ink. The
 * sRGB channels are linearised first; raw values overstate blue and understate
 * green.
 */
function luminance(r, g, b) {
  const lin = (c) => {
    const v = c / 255;
    return v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

/** contrastOn is the ink to put on a colour, computed rather than configured. */
function contrastOn(hex) {
  if (!validHex(hex)) return '#FFFFFF';
  const { r, g, b } = parseHex(hex);
  // Carbon's ink; a warm near-black looks like a smudge on yellow.
  return luminance(r, g, b) > 0.55 ? '#161616' : '#FFFFFF';
}

/**
 * systemTheme is the machine's current setting. The picker offers only light
 * and dark, and preselects this one until the user chooses, as the language
 * picker does.
 */
function systemTheme() {
  return typeof matchMedia === 'function' && matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function applyTheme(theme) {
  const root = document.documentElement;
  // Without a stored choice the machine's setting is read each time, so the
  // extension follows it until the user picks a side.
  root.setAttribute('data-theme', theme === 'light' || theme === 'dark' ? theme : systemTheme());
}

function applyShape(shape) {
  document.documentElement.setAttribute('data-shape', SHAPES_STORED.includes(shape) ? shape : DEFAULT_SHAPE);
}

/**
 * applyAccent overrides the accent tokens, or removes the override so the
 * theme's own gold returns, including the darker one of the light theme.
 */
function applyAccent(hex) {
  const root = document.documentElement.style;
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
  ]);

  const accent = validHex(s.accent) ? s.accent : '';

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
  if (Object.keys(accentCustoms).length === 0 && accent) {
    const slot = accentChosen !== undefined ? accentChosen : accentSlot(accent);
    if (slot >= 0) accentCustoms[String(slot)] = accent;
  }

  return {
    // Never blank, so the picker can show which theme is in force.
    theme: s.theme === 'light' || s.theme === 'dark' ? s.theme : systemTheme(),
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
    // Local like the theme: the instance knows nothing of it, so following the
    // instance's look leaves it alone.
    disco: s.rainbowDisco === true,
    // Whether the look comes from the default instance; the theme never does.
    followInstance: s.followInstance === true,
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
  applyShape(a.shape);
  applyAccent(a.accent);
  applyRainbow(a.rainbow);
  return a;
}

/**
 * adoptFromInstance takes the look from the default instance through the
 * relay's GET /api/appearance, which needs no host permission.
 *
 * The theme is not taken: light or dark belongs to the screen in front of the
 * user, not to a server. Returns false when there is no instance to ask, so the
 * caller can say so.
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
