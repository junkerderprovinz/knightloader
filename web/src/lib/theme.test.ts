// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  applyOled,
  applyStoredTheme,
  darkShowing,
  getTheme,
  onThemeChange,
  resolvesDark,
  setTheme,
  storedOled,
  toggleTheme,
  watchDarkShowing,
} from './theme';

const shown = () => document.documentElement.getAttribute('data-theme');

const mediaListeners = new Set<() => void>();
let deviceLight = false;

/** What the device says to "light?", told to whoever listens for a change. */
function deviceIsLight(light: boolean) {
  deviceLight = light;
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: query.includes('light') === deviceLight,
    addEventListener: (_: string, fn: () => void) => mediaListeners.add(fn),
    removeEventListener: (_: string, fn: () => void) => mediaListeners.delete(fn),
  }));
  for (const fn of mediaListeners) fn();
}

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute('data-theme');
  deviceIsLight(false);
});

afterEach(() => {
  vi.unstubAllGlobals();
  mediaListeners.clear();
});

describe('the theme a browser starts with', () => {
  it('follows the device in a browser that has never been here', () => {
    applyStoredTheme();
    expect(getTheme()).toBe('system');
    expect(shown()).toBeNull();
  });

  it('stays dark in a browser that was here before there was a choice', () => {
    localStorage.setItem('kl-appearance', '{}');
    applyStoredTheme();
    expect(getTheme()).toBe('dark');
    expect(shown()).toBe('dark');
  });

  it('still follows the device on the second visit, once the appearance is cached', () => {
    applyStoredTheme();
    localStorage.setItem('kl-appearance', '{}');
    applyStoredTheme();
    expect(getTheme()).toBe('system');
    expect(shown()).toBeNull();
  });

  it('keeps a choice made earlier', () => {
    localStorage.setItem('kl-theme', 'light');
    localStorage.setItem('kl-appearance', '{}');
    applyStoredTheme();
    expect(shown()).toBe('light');
  });
});

describe('choosing a theme', () => {
  it('hands the page back to the device for "system"', () => {
    setTheme('light');
    expect(shown()).toBe('light');
    setTheme('system');
    expect(shown()).toBeNull();
    expect(localStorage.getItem('kl-theme')).toBe('system');
  });

  it('tells whoever shows the theme', () => {
    const seen: string[] = [];
    const stop = onThemeChange((theme) => seen.push(theme));
    setTheme('system');
    toggleTheme();
    stop();
    setTheme('dark');
    expect(seen).toEqual(['system', 'light']);
  });
});

describe('the toggle', () => {
  it('flips between dark and light', () => {
    setTheme('dark');
    expect(toggleTheme()).toBe('light');
    expect(toggleTheme()).toBe('dark');
  });

  it('turns away from what the device shows under "system", and keeps that as a choice', () => {
    deviceIsLight(true);
    setTheme('system');
    expect(toggleTheme()).toBe('dark');
    expect(getTheme()).toBe('dark');
    expect(shown()).toBe('dark');
  });
});

describe('OLED', () => {
  beforeEach(() => document.documentElement.removeAttribute('data-oled'));

  it('is off in a browser that never chose it', () => {
    applyStoredTheme();
    expect(storedOled()).toBe(false);
    expect(document.documentElement.hasAttribute('data-oled')).toBe(false);
  });

  it('is kept in this browser and comes back on the next load', () => {
    applyOled(true);
    expect(document.documentElement.getAttribute('data-oled')).toBe('');
    document.documentElement.removeAttribute('data-oled');
    applyStoredTheme();
    expect(document.documentElement.getAttribute('data-oled')).toBe('');
    applyOled(false);
    expect(storedOled()).toBe(false);
    expect(document.documentElement.hasAttribute('data-oled')).toBe(false);
  });

  it('stays switched on through a light theme, where it shows nothing', () => {
    applyOled(true);
    setTheme('light');
    expect(storedOled()).toBe(true);
    expect(darkShowing()).toBe(false);
    setTheme('dark');
    expect(darkShowing()).toBe(true);
  });
});

describe('whether dark shows', () => {
  it('follows a chosen theme over the device', () => {
    expect(resolvesDark('dark', false)).toBe(true);
    expect(resolvesDark('light', true)).toBe(false);
  });

  it('follows the device without a choice, dark unless it asks for light', () => {
    expect(resolvesDark(null, true)).toBe(true);
    expect(resolvesDark(null, false)).toBe(false);
    setTheme('system');
    expect(darkShowing()).toBe(true);
    deviceIsLight(true);
    expect(darkShowing()).toBe(false);
  });

  it('tells the watcher when a choice or the device changes what shows, and only then', () => {
    const seen: boolean[] = [];
    setTheme('dark');
    const stop = watchDarkShowing((dark) => seen.push(dark));
    setTheme('light');
    setTheme('light');
    setTheme('system');
    deviceIsLight(true);
    deviceIsLight(true);
    stop();
    setTheme('dark');
    deviceIsLight(false);
    expect(seen).toEqual([false, true, false]);
  });
});
