// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { standInForStorage } from './storageShim';

const throwing = () => {
  throw new DOMException('The document is sandboxed and lacks the allow-same-origin flag.', 'SecurityError');
};

describe('standInForStorage', () => {
  it('leaves working storage alone', () => {
    const before = window.localStorage;
    standInForStorage();
    expect(window.localStorage).toBe(before);
  });

  it('keeps a sandboxed page working in memory', () => {
    const win = {} as Window;
    Object.defineProperty(win, 'localStorage', { get: throwing, configurable: true });
    Object.defineProperty(win, 'sessionStorage', { get: throwing, configurable: true });
    const doc = {} as Document;
    Object.defineProperty(doc, 'cookie', { get: throwing, set: throwing, configurable: true });

    standInForStorage(win, doc);

    win.localStorage.setItem('kl-theme', 'dark');
    expect(win.localStorage.getItem('kl-theme')).toBe('dark');
    expect(win.localStorage.length).toBe(1);
    expect(win.sessionStorage.getItem('kl-theme')).toBeNull();
    doc.cookie = 'a=1; Path=/';
    doc.cookie = 'b=2';
    doc.cookie = 'a=3';
    expect(doc.cookie).toBe('a=3; b=2');
  });
});
