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
const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad|iPod/.test(navigator.userAgent);

const SYMBOL: Record<string, string> = {
  mod: isMac ? '⌘' : 'Ctrl',
  cmd: '⌘',
  meta: '⌘',
  ctrl: 'Ctrl',
  shift: '⇧',
  alt: isMac ? '⌥' : 'Alt',
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

function tokens(shortcut: string): string[] {
  return shortcut.split('+').map((s) => s.trim().toLowerCase());
}

/**
 * formatShortcut turns "mod+k" into "⌘K" on a Mac or "Ctrl+K" everywhere
 * else - the one thing the palette shows beside a command's label, and the
 * only place in this app that has to say "here is the key" rather than just
 * react to it.
 */
export function formatShortcut(shortcut: string): string {
  const parts = tokens(shortcut).map((key) => SYMBOL[key] ?? (key.length === 1 ? key.toUpperCase() : key.charAt(0).toUpperCase() + key.slice(1)));
  // A Mac chord reads as one glyph run ("⌘⇧K"); everywhere else keeps the
  // '+' a person actually has to type between two real key names
  // ("Ctrl+Shift+K").
  return isMac ? parts.join('') : parts.join('+');
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
