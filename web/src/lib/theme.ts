// The theme: dark, light or the device's own, kept in localStorage. A chosen
// theme is data-theme on <html>. Without the attribute index.css reads
// prefers-color-scheme, so "system" changes with the device while the app is
// open.
//
// OLED is data-oled on <html>, and index.css applies it only while dark
// shows, so it can stay on through a switch to light (GlimStone's rule 43). It
// belongs to the screen, not to the instance, so it is kept in this browser and
// never in the settings every client shares.

export type Theme = 'system' | 'dark' | 'light';

const STORAGE_KEY = 'kl-theme';
const OLED_KEY = 'kl-oled';

// index.css draws dark unless the device asks for light, so "no preference"
// counts as dark here too.
const DEVICE_LIGHT = '(prefers-color-scheme: light)';

// lib/appearance.ts writes its cache on every visit, so a browser that holds
// one has been here before.
const VISITED_KEY = 'kl-appearance';

// The Appearance page and the theme.toggle command both change the theme, so
// every display of it subscribes here.
const listeners = new Set<(theme: Theme) => void>();

/**
 * getTheme is the stored choice. A browser without one that has been here
 * opened dark until the choice existed and stays dark; a new one follows its
 * device, the default GlimStone gives the theme axis.
 */
export function getTheme(): Theme {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored === 'system' || stored === 'dark' || stored === 'light') return stored;
  return localStorage.getItem(VISITED_KEY) === null ? 'system' : 'dark';
}

function apply(theme: Theme): void {
  if (theme === 'system') document.documentElement.removeAttribute('data-theme');
  else document.documentElement.setAttribute('data-theme', theme);
}

export function setTheme(theme: Theme): void {
  localStorage.setItem(STORAGE_KEY, theme);
  apply(theme);
  for (const fn of listeners) fn(theme);
}

/**
 * toggleTheme flips what is on screen between dark and light and keeps the
 * result as a choice. Under "system" it turns away from what the device
 * shows; the way back to "system" is the Appearance page.
 */
export function toggleTheme(): Theme {
  const chosen = getTheme();
  const shown =
    chosen === 'system' ? (window.matchMedia(DEVICE_LIGHT).matches ? 'light' : 'dark') : chosen;
  const next: Theme = shown === 'dark' ? 'light' : 'dark';
  setTheme(next);
  return next;
}

/** onThemeChange notifies every caller of setTheme()/toggleTheme(), whoever made it. */
export function onThemeChange(fn: (theme: Theme) => void): () => void {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
}

/**
 * applyStoredTheme runs at boot, before the first render, so no other theme
 * flashes. It writes down what a browser without a choice starts with, since
 * the appearance cache that tells the two kinds apart exists from the first
 * visit on.
 */
export function applyStoredTheme(): void {
  const theme = getTheme();
  if (localStorage.getItem(STORAGE_KEY) === null) localStorage.setItem(STORAGE_KEY, theme);
  apply(theme);
  setOledAttribute(storedOled());
}

/** resolvesDark says whether dark shows for a theme choice and what the device asks for. */
export function resolvesDark(theme: string | null | undefined, deviceDark: boolean): boolean {
  if (theme === 'dark') return true;
  if (theme === 'light') return false;
  return deviceDark;
}

/** darkShowing says whether dark shows now, chosen or from the device. */
export function darkShowing(): boolean {
  return resolvesDark(document.documentElement.getAttribute('data-theme'), !window.matchMedia(DEVICE_LIGHT).matches);
}

/**
 * watchDarkShowing calls `fn` whenever dark starts or stops showing, through a
 * theme choice or the device, and returns the function that stops watching.
 */
export function watchDarkShowing(fn: (dark: boolean) => void): () => void {
  let last = darkShowing();
  const check = () => {
    const next = darkShowing();
    if (next === last) return;
    last = next;
    fn(next);
  };
  const media = window.matchMedia(DEVICE_LIGHT);
  media.addEventListener('change', check);
  const stopTheme = onThemeChange(check);
  return () => {
    media.removeEventListener('change', check);
    stopTheme();
  };
}

function setOledAttribute(on: boolean): void {
  if (on) document.documentElement.setAttribute('data-oled', '');
  else document.documentElement.removeAttribute('data-oled');
}

/** applyOled turns OLED on or off and keeps the choice in this browser. */
export function applyOled(on: boolean): void {
  setOledAttribute(on);
  try {
    if (on) localStorage.setItem(OLED_KEY, '1');
    else localStorage.removeItem(OLED_KEY);
  } catch {
    // The switch still holds until the page closes.
  }
  for (const fn of oledListeners) fn(on);
}

/** storedOled says whether OLED is on in this browser. */
export function storedOled(): boolean {
  try {
    return localStorage.getItem(OLED_KEY) === '1';
  } catch {
    return false;
  }
}

const oledListeners = new Set<(on: boolean) => void>();

/** onOledChange notifies every caller of applyOled, such as the desktop window's ground. */
export function onOledChange(fn: (on: boolean) => void): () => void {
  oledListeners.add(fn);
  return () => {
    oledListeners.delete(fn);
  };
}
