// Keyboard-shortcut formatting and matching shared by every command surface.
//
// A shortcut is written as lowercase, '+'-joined tokens ("mod+k",
// "mod+shift+p", "esc") - lib/commands/types.ts's own doc comment on
// Command.defaultShortcut. 'mod' is the one platform-aware token: Cmd on
// macOS, Ctrl everywhere else, the same convention VS Code and Sublime both
// use for exactly this reason - a literal "ctrl+k" typed into every
// command's defaultShortcut would be wrong on a Mac, and there is no reason
// to make every command declaration itself platform-aware.
//
// The mac check mirrors Access.tsx's own iOS sniff (navigator.userAgent,
// same reasoning: nothing else in this app needs a platform check, so this
// is the second call site rather than a shared helper neither would reuse
// for anything but this one thing).
import type { TranslationKey } from '../locales/en';

const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad|iPod/.test(navigator.userAgent);

// Mac's own two modifier glyphs (⌘/⌥) are what is actually printed on the
// physical keyboard everywhere in the world - never translated, same as a
// keycap's shape does not change by locale. Ctrl/Shift/Alt on every other
// platform are printed words, and those words genuinely differ by
// locale (jdp, 2026-08-24: "in deutsch steht da Ctrl anstall Strg!") - the
// three text labels below are looked up through `t` when the caller has one
// (Shortcuts.tsx, CaptureModal), and fall back to their English default
// (this file's own literal, unlocalized string) when it does not, so a
// caller written before this localization existed keeps behaving exactly
// as it did.
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
// The default English word shown when no translator is passed in, and the
// i18n key each is looked up under when one is - see
// `settings.shortcuts.key.*` in lib/locales/en.ts (and every other locale).
const TEXT_KEY: Record<string, { fallback: string; i18nKey: string }> = {
  mod: { fallback: 'Ctrl', i18nKey: 'settings.shortcuts.key.ctrl' },
  ctrl: { fallback: 'Ctrl', i18nKey: 'settings.shortcuts.key.ctrl' },
  alt: { fallback: 'Alt', i18nKey: 'settings.shortcuts.key.alt' },
  // Shown as literal text everywhere, never as the ⇧ glyph (jdp: "Shift
  // nicht als Symbol darstellen sondern als text 'Shift'") - but, unlike
  // Ctrl/Alt, always the same word "Shift" regardless of locale: that is
  // what this app's own casual convention already treats as a keyboard-
  // universal label (the same reasoning `esc` above already applies to
  // "Esc" rather than translating it either), not a translatable UI string.
  shift: { fallback: 'Shift', i18nKey: '' },
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
 * formatShortcut turns "mod+k" into "⌘K" on a Mac or "Strg+K"/"Ctrl+K"
 * (locale-dependent) everywhere else - the one thing the palette shows
 * beside a command's label, and the only place in this app that has to say
 * "here is the key" rather than just react to it. `t` is optional
 * (undefined keeps every existing call site's English-only behaviour
 * exactly as it was) - pass `useT().t` from a component to localize Ctrl/Alt.
 */
export function formatShortcut(shortcut: string, t?: (key: TranslationKey) => string): string {
  const parts = tokens(shortcut).map((key) => label(key, t));
  // A Mac chord reads as one glyph run ("⌘⇧K") ONLY when every part is a
  // single-glyph symbol - the moment "Shift" (or a localized "Strg") is a
  // real word rather than one character, squishing it against its
  // neighbour with no separator reads as a typo, not a shortcut, so the
  // whole combo falls back to '+'-joining exactly like the non-Mac path
  // the instant any part is more than one character.
  const allGlyphs = isMac && parts.every((p) => [...p].length === 1);
  return allGlyphs ? parts.join('') : parts.join('+');
}

/**
 * matchesShortcut checks a live KeyboardEvent against a "mod+k"-shaped
 * string. Exported for whichever component ends up dispatching shortcuts
 * app-wide (see lib/commands/types.ts's own doc comment on
 * CommandDispatcher.tsx) as well as for the one shortcut the palette has to
 * recognise on its own before that dispatcher exists to do it for anything
 * else - see CommandPalette.tsx's own comment on why.
 */
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
