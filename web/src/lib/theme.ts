// Theme — dark / light via data-theme on <html> + localStorage.

type Theme = 'dark' | 'light';

const STORAGE_KEY = 'kl-theme';
const DEFAULT: Theme = 'dark';

// Whoever changes the theme (Sidebar's own button, or the new global
// theme.toggle command in lib/commands/global.ts, invoked from a keyboard
// shortcut with no button anywhere near it) has to reach everyone else
// showing it, or the sidebar's sun/moon icon goes stale the moment the
// command fires from the keyboard instead of a click on that exact button.
// Same shape as lib/listview.ts's own publish/subscribe pair.
const listeners = new Set<(theme: Theme) => void>();

export function getTheme(): Theme {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored === 'dark' || stored === 'light') return stored;
  return DEFAULT;
}

export function setTheme(theme: Theme): void {
  localStorage.setItem(STORAGE_KEY, theme);
  document.documentElement.setAttribute('data-theme', theme);
  for (const fn of listeners) fn(theme);
}

export function toggleTheme(): Theme {
  const next: Theme = getTheme() === 'dark' ? 'light' : 'dark';
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

/** Applied at boot before first render to prevent a theme flash. */
export function applyStoredTheme(): void {
  document.documentElement.setAttribute('data-theme', getTheme());
}
