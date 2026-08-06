// Appearance is the pair of looks the user owns: how rounded the interface is,
// and the one colour it uses for activity. Both are applied to the document
// root, so every component picks them up through the same tokens it already
// reads and nothing has to be told about the change.

export type Shape = 'round' | 'soft' | 'square';

export const SHAPES: Shape[] = ['round', 'soft', 'square'];

/** The built-in accent. Empty in settings means this. */
export const DEFAULT_ACCENT = '#E9A83C';

/**
 * ACCENTS are the presets offered in the picker. A free colour field sits
 * beside them, so this list is a shortcut rather than a restriction.
 */
export const ACCENTS: { name: string; hex: string }[] = [
  { name: 'Gold', hex: '#E9A83C' },
  { name: 'Ember', hex: '#E2703A' },
  { name: 'Crimson', hex: '#D9534F' },
  { name: 'Moss', hex: '#5CA271' },
  { name: 'Steel', hex: '#5B8DBE' },
  { name: 'Iris', hex: '#8C7AE6' },
  { name: 'Rose', hex: '#D2688F' },
  { name: 'Slate', hex: '#8A8F98' },
];

/** applyShape sets the attribute the radius tokens key off. */
export function applyShape(shape: Shape | string | undefined): void {
  const s = SHAPES.includes(shape as Shape) ? (shape as Shape) : 'round';
  document.documentElement.setAttribute('data-shape', s);
}

/**
 * applyAccent overrides the accent tokens, or clears the override so the
 * theme's own gold comes back. The contrast colour is computed rather than
 * configured: a light accent with white text on it is unreadable, and asking
 * the user to pick a second colour to fix the first one is not a setting, it is
 * a trap.
 */
export function applyAccent(hex: string | undefined): void {
  const root = document.documentElement.style;
  if (!hex || !/^#[0-9a-fA-F]{6}$/.test(hex)) {
    root.removeProperty('--accent');
    root.removeProperty('--accent-contrast');
    root.removeProperty('--accent-soft');
    return;
  }
  const { r, g, b } = parse(hex);
  root.setProperty('--accent', hex);
  root.setProperty('--accent-contrast', luminance(r, g, b) > 0.55 ? '#17130E' : '#FFFFFF');
  root.setProperty('--accent-soft', `rgba(${r}, ${g}, ${b}, 0.14)`);
}

function parse(hex: string): { r: number; g: number; b: number } {
  const n = parseInt(hex.slice(1), 16);
  return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
}

/**
 * luminance is the perceptual brightness used to decide black or white on top.
 * The sRGB channels are linearised first, because the raw values overstate how
 * bright blue is and understate green, which is exactly the case that produces
 * unreadable buttons.
 */
function luminance(r: number, g: number, b: number): number {
  const lin = (c: number) => {
    const v = c / 255;
    return v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

/**
 * Appearance is mirrored into localStorage purely so the first paint after a
 * reload is already right. The settings on the server stay the source of truth;
 * this only avoids a flash of the default look while they are being fetched.
 */
const CACHE = 'kl-appearance';

export function cacheAppearance(shape: string, accent: string): void {
  try {
    localStorage.setItem(CACHE, JSON.stringify({ shape, accent }));
  } catch {
    // A browser with storage disabled simply pays one flash per load.
  }
}

/** Applied at boot, before React renders anything. */
export function applyCachedAppearance(): void {
  try {
    const raw = localStorage.getItem(CACHE);
    if (!raw) {
      applyShape('round');
      return;
    }
    const { shape, accent } = JSON.parse(raw) as { shape?: string; accent?: string };
    applyShape(shape);
    applyAccent(accent);
  } catch {
    applyShape('round');
  }
}
