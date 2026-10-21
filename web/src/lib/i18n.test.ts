// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { applyStoredLanguage } from './i18n';

const shown = () => document.documentElement.getAttribute('lang');

function openAt(url: string) {
  history.replaceState(null, '', url);
}

beforeEach(() => {
  localStorage.clear();
  vi.spyOn(navigator, 'language', 'get').mockReturnValue('en-US');
});

afterEach(() => {
  openAt('/');
  vi.restoreAllMocks();
});

describe('the language the desktop installer was set to', () => {
  it('is taken on the first start and kept like a choice', () => {
    openAt('/?lang=de');
    applyStoredLanguage();
    expect(shown()).toBe('de');
    expect(localStorage.getItem('kl-lang')).toBe('de');

    openAt('/');
    applyStoredLanguage();
    expect(shown()).toBe('de');
  });

  it('gives way to a language picked in the app', () => {
    localStorage.setItem('kl-lang', 'fr');
    openAt('/?lang=de');
    applyStoredLanguage();
    expect(shown()).toBe('fr');
    expect(localStorage.getItem('kl-lang')).toBe('fr');
  });

  it('is ignored when the app has no such language', () => {
    openAt('/?lang=xx');
    applyStoredLanguage();
    expect(shown()).toBe('en');
    expect(localStorage.getItem('kl-lang')).toBeNull();
  });

  it('leaves the browser language in charge when there is none', () => {
    vi.spyOn(navigator, 'language', 'get').mockReturnValue('it-IT');
    applyStoredLanguage();
    expect(shown()).toBe('it');
    expect(localStorage.getItem('kl-lang')).toBeNull();
  });
});
