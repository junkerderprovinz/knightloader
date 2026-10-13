// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { applyStoredTheme, getTheme, onThemeChange, setTheme, toggleTheme } from './theme';

const shown = () => document.documentElement.getAttribute('data-theme');

/** What the device says to "light?". */
function deviceIsLight(light: boolean) {
  vi.stubGlobal('matchMedia', (query: string) => ({ matches: query.includes('light') === light }));
}

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute('data-theme');
  deviceIsLight(false);
});

afterEach(() => {
  vi.unstubAllGlobals();
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
