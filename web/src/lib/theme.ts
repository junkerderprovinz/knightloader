// The theme: dark, light or the device's own, kept in localStorage. A chosen
// theme is data-theme on <html>. Without the attribute index.css reads
// prefers-color-scheme, so "system" changes with the device while the app is
// open.

export type Theme = 'system' | 'dark' | 'light';

const STORAGE_KEY = 'kl-theme';

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
    chosen === 'system' ? (window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark') : chosen;
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
}
