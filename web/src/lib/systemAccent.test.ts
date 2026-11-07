// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { setSystemAccent, systemAccent } from './appearance';
import { browserAccentWorks, cssColourToHex, watchSystemAccent } from './systemAccent';

const FIREFOX = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:150.0) Gecko/20100101 Firefox/150.0';
const CHROME = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36';

let reported = 'rgb(224, 56, 62)';

function browser(ua: string) {
  vi.stubGlobal('CSS', { supports: (_: string, value: string) => value === 'AccentColor' });
  vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(ua);
  vi.stubGlobal('getComputedStyle', () => ({ color: reported }));
}

beforeEach(() => {
  reported = 'rgb(224, 56, 62)';
  setSystemAccent(undefined);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('the browser as a source', () => {
  it('counts in Firefox and Safari, never in a Chromium browser', () => {
    browser(FIREFOX);
    expect(browserAccentWorks()).toBe(true);
    browser(CHROME);
    expect(browserAccentWorks()).toBe(false);
    browser('Mozilla/5.0 HeadlessChrome/150.0.0.0');
    expect(browserAccentWorks()).toBe(false);
  });

  it('does not count where CSS cannot name AccentColor', () => {
    vi.stubGlobal('CSS', { supports: () => false });
    expect(browserAccentWorks()).toBe(false);
  });

  it('reads a computed colour as hex and nothing else', () => {
    expect(cssColourToHex('rgb(0, 120, 215)')).toBe('#0078D7');
    expect(cssColourToHex('rgba(255, 255, 255, 0.5)')).toBe('#FFFFFF');
    expect(cssColourToHex('AccentColor')).toBeUndefined();
  });

  it('feeds the colour now and again when the window comes back', () => {
    browser(FIREFOX);
    const stop = watchSystemAccent();
    expect(systemAccent()).toBe('#E0383E');
    reported = 'rgb(98, 186, 70)';
    window.dispatchEvent(new Event('focus'));
    expect(systemAccent()).toBe('#62BA46');
    stop();
    reported = 'rgb(0, 122, 255)';
    window.dispatchEvent(new Event('focus'));
    expect(systemAccent()).toBe('#62BA46');
  });

  it('feeds nothing in Chrome, so the wand stays away', () => {
    browser(CHROME);
    watchSystemAccent()();
    expect(systemAccent()).toBeUndefined();
  });
});
