// Theme — dark / light via data-theme on <html> + localStorage.

type Theme = 'dark' | 'light';

const STORAGE_KEY = 'kl-theme';
const DEFAULT: Theme = 'dark';

export function getTheme(): Theme {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored === 'dark' || stored === 'light') return stored;
  return DEFAULT;
}

export function setTheme(theme: Theme): void {
  localStorage.setItem(STORAGE_KEY, theme);
  document.documentElement.setAttribute('data-theme', theme);
}

export function toggleTheme(): Theme {
  const next: Theme = getTheme() === 'dark' ? 'light' : 'dark';
  setTheme(next);
  return next;
}

/** Applied at boot before first render to prevent a theme flash. */
export function applyStoredTheme(): void {
  document.documentElement.setAttribute('data-theme', getTheme());
}
