// Keyboard-shortcut formatting and matching for every command surface.
//
// A shortcut is lowercase tokens joined by '+' ("mod+k", "mod+shift+p",
// "esc"). 'mod' is Cmd on macOS and Ctrl elsewhere, as in VS Code, so command
// declarations need not know the platform.
import type { TranslationKey } from '../locales/en';

const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad|iPod/.test(navigator.userAgent);

// The Mac modifier glyphs are printed on keyboards worldwide and never
// translated. Elsewhere the keycaps carry words that differ by locale (Strg
// in German), so TEXT_KEY below is translated when the caller passes t.
const MAC_SYMBOL: Record<string, string> = { mod: '⌘', cmd: '⌘', meta: '⌘', ctrl: '⌘', alt: '⌥' };
const UNIVERSAL_SYMBOL: Record<string, string> = {
  enter: '↵',
  esc: 'Esc',
  escape: 'Esc',
  up: '↑',
  down: '↓',
  left: '←',
  right: '→',
  ' ': 'Space',
  space: 'Space',
};
// The English word used without a translator, and the settings.shortcuts.key.*
// key used with one.
const TEXT_KEY: Record<string, { fallback: string; i18nKey: string }> = {
  mod: { fallback: 'Ctrl', i18nKey: 'settings.shortcuts.key.ctrl' },
  ctrl: { fallback: 'Ctrl', i18nKey: 'settings.shortcuts.key.ctrl' },
  alt: { fallback: 'Alt', i18nKey: 'settings.shortcuts.key.alt' },
  // Always the word "Shift", never the glyph, and not translated, like "Esc".
  shift: { fallback: 'Shift', i18nKey: '' },
  // Keycaps whose words differ by locale (Pos1, Ende, Entf on a German keyboard).
  home: { fallback: 'Home', i18nKey: 'settings.shortcuts.key.home' },
  end: { fallback: 'End', i18nKey: 'settings.shortcuts.key.end' },
  delete: { fallback: 'Del', i18nKey: 'settings.shortcuts.key.delete' },
};

function tokens(shortcut: string): string[] {
  return shortcut.split('+').map((s) => s.trim().toLowerCase());
}

function label(key: string, t?: (key: TranslationKey) => string): string {
  if (isMac && MAC_SYMBOL[key]) return MAC_SYMBOL[key];
  if (UNIVERSAL_SYMBOL[key]) return UNIVERSAL_SYMBOL[key];
  const text = TEXT_KEY[key];
  if (text) {
    if (key === 'shift' || !t) return text.fallback;
    return t(text.i18nKey as TranslationKey) || text.fallback;
  }
  return key.length === 1 ? key.toUpperCase() : key.charAt(0).toUpperCase() + key.slice(1);
}

/**
 * formatShortcut turns "mod+k" into "⌘K" on a Mac and "Ctrl+K" or "Strg+K"
 * elsewhere. Pass `useT().t` to translate the key words; without it they
 * stay English.
 */
export function formatShortcut(shortcut: string, t?: (key: TranslationKey) => string): string {
  const parts = tokens(shortcut).map((key) => label(key, t));
  // A Mac chord runs its glyphs together ("⌘⇧K") only when every part is a
  // single character; a word such as "Shift" needs the '+'.
  const allGlyphs = isMac && parts.every((p) => [...p].length === 1);
  return allGlyphs ? parts.join('') : parts.join('+');
}

/** matchesShortcut checks a KeyboardEvent against a "mod+k"-shaped string. */
export function matchesShortcut(e: KeyboardEvent, shortcut: string): boolean {
  const parts = tokens(shortcut);
  const key = parts[parts.length - 1];
  const mods = new Set(parts.slice(0, -1));
  const needCtrl = mods.has('mod') ? !isMac : mods.has('ctrl');
  const needMeta = mods.has('mod') ? isMac : mods.has('cmd') || mods.has('meta');
  if (e.ctrlKey !== needCtrl) return false;
  if (e.metaKey !== needMeta) return false;
  if (e.shiftKey !== mods.has('shift')) return false;
  if (e.altKey !== mods.has('alt')) return false;
  return e.key.toLowerCase() === key;
}
