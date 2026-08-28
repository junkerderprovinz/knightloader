// The three axes that belong to the user, applied here the same way GlimStone
// applies them everywhere: an attribute or a custom property on <html>, and
// nothing below has to know which setting produced a value.
//
// Ported from the shared reference (github.com/junkerderprovinz/glimstone,
// reference/appearance.ts) with its types stripped, because this extension
// deliberately has no build step - src/ ships as plain files so "load
// unpacked" works straight from a checkout (see ../embed.go). The VALUES are
// copied verbatim: re-picking a colour by eye is how two apps that both claim
// one design language stop looking like one product.
//
// The rainbow used to be deliberately absent here, on the reasoning that it
// colours ACTIVITY in a list by position and none of these small pages was a
// list of anything. That reasoning expired: the popup, the send-to window and
// the options page all draw the group as a list of instance cards now, which is
// exactly what the mode is for. jdp, 2026-08-28: "Der Regenbogenmodus fehlt
// komplett in der Erweiterung!" - and an app carrying half a design language is
// not carrying it.

const SHAPES = ['round', 'soft', 'square'];

/** The accent before anyone touches the picker: every app in the family opens
 *  in this colour, so a fresh install already looks like the same product. */
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
 * luminance is the perceptual brightness used to decide black or white on top.
 * The sRGB channels are linearised first, because the raw values overstate how
 * bright blue is and understate green - which is exactly the case that
 * produces an unreadable button.
 */
function luminance(r, g, b) {
  const lin = (c) => {
    const v = c / 255;
    return v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

/** contrastOn is the ink to put ON a colour, computed rather than configured:
 *  asking for a second colour to make the first one readable is not a setting,
 *  it is a trap. */
function contrastOn(hex) {
  if (!validHex(hex)) return '#FFFFFF';
  const { r, g, b } = parseHex(hex);
  // Carbon's own ink, not a warm near-black: on a yellow accent a
  // brown-tinted black reads as a smudge.
  return luminance(r, g, b) > 0.55 ? '#161616' : '#FFFFFF';
}

function applyTheme(theme) {
  const root = document.documentElement;
  // Unset means follow the browser, which is the default and the only honest
  // one: a page that opens dark on a light system has made a decision nobody
  // asked for. tokens.css carries the matching prefers-color-scheme block.
  if (theme === 'light' || theme === 'dark') root.setAttribute('data-theme', theme);
  else root.removeAttribute('data-theme');
}

function applyShape(shape) {
  document.documentElement.setAttribute('data-shape', SHAPES.includes(shape) ? shape : 'round');
}

/**
 * applyAccent overrides the accent tokens, or clears the override so the
 * theme's own gold comes back - including the darkened one the light theme
 * uses, which is why clearing has to remove the properties rather than write
 * the dark default over them.
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
 * readAppearance is what this extension has chosen locally.
 *
 * Local, and not read from a configured instance, for a reason worth stating:
 * fetching it would need a host permission for that origin, and this extension
 * is careful never to ask for one outside a real click on the sync button (see
 * options.js). Paying a permission prompt for a colour would be a bad trade -
 * so the extension carries its own three settings, defaulting to the same
 * values every other app in the family starts from.
 */
async function readAppearance() {
  const s = await chrome.storage.local.get([
    'theme', 'accent', 'shape',
    'rainbow', 'rainbowReactive', 'rainbowRotate', 'rainbowSeed', 'rainbowPalette',
    'followInstance',
  ]);
  return {
    theme: s.theme === 'light' || s.theme === 'dark' ? s.theme : '',
    accent: validHex(s.accent) ? s.accent : '',
    shape: SHAPES.includes(s.shape) ? s.shape : 'round',
    rainbow: {
      on: s.rainbow === true,
      reactive: s.rainbowReactive === true,
      rotate: s.rainbowRotate === true,
      seed: Number.isFinite(s.rainbowSeed) ? s.rainbowSeed : 0,
      palette: usablePalette(s.rainbowPalette),
    },
    // Whether the look is taken from the default instance instead of being
    // chosen here. Theme is never part of it - see applyAppearance.
    followInstance: s.followInstance === true,
  };
}

async function writeAppearance(next) {
  await chrome.storage.local.set(next);
}

/** applyAppearance puts the stored choices on <html>. Called first thing on
 *  every page, before anything is drawn, so nothing is ever painted in one
 *  look and repainted in another. */
async function applyAppearance() {
  const a = await readAppearance();
  applyTheme(a.theme);
  applyShape(a.shape);
  applyAccent(a.accent);
  applyRainbow(a.rainbow);
  return a;
}

/**
 * adoptFromInstance takes the look from the default instance and stores it here.
 *
 * GET /api/appearance exists precisely so this is not a licence to read
 * /api/settings, and it is on the list a group member may reach through the
 * relay - so this costs no permission at all. The older comment above said
 * fetching it would need a host permission for that origin, which was true of
 * the direct fetch this extension used to do and is not true of a relayed call.
 *
 * Theme is deliberately NOT taken. Light or dark is a property of the screen
 * somebody is sitting at, not of a server: a headless container has no opinion
 * about whether this browser is in a dark room, and overwriting the local
 * choice with the instance's would be the one part of the look that gets worse.
 *
 * Returns false when there is nothing to take it from, so the caller can say so
 * rather than silently leaving the switch on with nothing behind it.
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
  await writeAppearance({
    accent: validHex(a.accent) ? a.accent : '',
    shape: SHAPES.includes(a.shape) ? a.shape : 'round',
    rainbow: a.rainbow === true,
    rainbowReactive: a.rainbowReactive === true,
    rainbowRotate: a.rainbowRotate === true,
    rainbowSeed: Number.isFinite(a.rainbowSeed) ? a.rainbowSeed : 0,
    rainbowPalette: Array.isArray(a.rainbowPalette) ? a.rainbowPalette : null,
  });
  return true;
}

/* ---------------------------------------------------------------------------
   The rainbow: the accent in the plural.

   Ported from the shared reference (glimstone/reference/appearance.ts and the
   web UI's own lib/appearance.ts) with the VALUES copied verbatim. Re-picking
   these by eye is how two apps that both claim one design language stop looking
   like one product.
   --------------------------------------------------------------------------- */

/** Eight positions. Sunflower is the default accent, so one row always matches
 *  the single-accent look somebody is coming from. */
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

/** A palette is taken only in full: one unusable entry is not a palette with
 *  seven good colours, it is a palette that turns one row invisible. */
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
 * hueVars are the inline custom properties an element carrying a palette
 * position sets on itself. The `.glim-hue` rules in glimstone.css decide
 * whether that hue shows at rest or waits for a hover, so a component only ever
 * says which colour it owns, never which mode is active.
 *
 * The class and these properties always travel together: `.glim-hue` with no
 * `--item-hue` under it resolves the accent to nothing.
 */
function hueVars(hex) {
  if (!validHex(hex)) return {};
  const { r, g, b } = parseHex(hex);
  return {
    '--item-hue': hex,
    '--item-hue-ink': contrastOn(hex),
    '--item-hue-soft': `rgba(${r}, ${g}, ${b}, 0.22)`,
    '--item-hue-wash': `rgba(${r}, ${g}, ${b}, 0.16)`,
    '--item-hue-badge': `rgba(${r}, ${g}, ${b}, 0.5)`,
    '--item-hue-ring': `rgba(${r}, ${g}, ${b}, 0.55)`,
  };
}

/** setHue puts a palette position on one element: the class and the properties
 *  in one call, so no caller can hand out one without the other. */
function setHue(el, i) {
  el.classList.add('glim-hue');
  const vars = hueVars(rainbowAt(i));
  for (const [k, v] of Object.entries(vars)) el.style.setProperty(k, v);
}

function applyRainbow(next) {
  const merged = { ...RAINBOW_OFF, ...next };
  merged.palette = usablePalette(merged.palette);
  merged.seed = Number.isFinite(merged.seed) ? Math.abs(Math.trunc(merged.seed)) % RAINBOW.length : 0;
  rainbowNow = merged;

  const root = document.documentElement;
  for (let i = 0; i < RAINBOW.length; i++) root.style.setProperty(`--rb-${i}`, rainbowAt(i));
  const mode = !merged.on ? 'off' : merged.reactive ? 'reactive' : 'on';
  if (mode === 'off') root.removeAttribute('data-rainbow');
  else root.setAttribute('data-rainbow', mode);
}
