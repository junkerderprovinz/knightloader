// The dark or light theme, set as data-theme on <html> and kept in localStorage.

type Theme = 'dark' | 'light';

const STORAGE_KEY = 'kl-theme';
const DEFAULT: Theme = 'dark';

// The sidebar button and the theme.toggle command both change the theme, so
// every display of it subscribes here.
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
