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
// The rainbow is deliberately absent. It colours ACTIVITY in a list by
// position, and none of these three small pages is a list of activity - it
// would be a mode with nothing to colour.

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
  const s = await chrome.storage.local.get(['theme', 'accent', 'shape']);
  return {
    theme: s.theme === 'light' || s.theme === 'dark' ? s.theme : '',
    accent: validHex(s.accent) ? s.accent : '',
    shape: SHAPES.includes(s.shape) ? s.shape : 'round',
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
  return a;
}
