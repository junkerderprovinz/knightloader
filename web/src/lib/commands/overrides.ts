// Where a rebound keyboard shortcut lives, and the one comparison both the
// Shortcuts settings tab and the global keyboard dispatcher (built in
// parallel this same wave) need to agree on: what a command's binding
// actually is right now, override or default.
import { peekUIState, useUIState } from '../uistate';
import type { Command } from './types';

/**
 * The uistate field every rebound shortcut is written under - one flat
 * Record<commandId, comboString>, deliberately obvious and greppable so the
 * dispatcher reads the exact same document rather than a second storage
 * path that could drift from this one. See uistate.ts's own doc comment for
 * why a field on the shared bucket, and not a new endpoint, is already the
 * whole answer here: server-persisted, debounced, survives a reload and
 * follows the user across browsers on this single-user instance.
 */
export const SHORTCUT_OVERRIDES_FIELD = 'commands.shortcutOverrides';

export type ShortcutOverrides = Record<string, string>;

const EMPTY_OVERRIDES: ShortcutOverrides = {};

/** The settings tab's own read/write handle on the override document. */
export function useShortcutOverrides(): [ShortcutOverrides, (next: ShortcutOverrides) => void] {
  return useUIState<ShortcutOverrides>(SHORTCUT_OVERRIDES_FIELD, EMPTY_OVERRIDES);
}

/**
 * readShortcutOverrides is the non-hook reader - what the global keyboard
 * dispatcher calls from inside a document-level keydown handler, not from a
 * component. peekUIState never waits on the network read (its own doc
 * comment), which is the right default on a hot path: the bucket is already
 * filled in by the one GET this app issues at boot, well before a person's
 * first keystroke.
 */
export function readShortcutOverrides(): ShortcutOverrides {
  return peekUIState<ShortcutOverrides>(SHORTCUT_OVERRIDES_FIELD, EMPTY_OVERRIDES);
}

/** The binding actually in effect for a command: its override if it has one, else its default. */
export function effectiveShortcut(cmd: Command, overrides: ShortcutOverrides): string | undefined {
  return overrides[cmd.id] ?? cmd.defaultShortcut;
}

/**
 * findConflict is what the rebind flow refuses against: the other command,
 * if any, whose CURRENT binding (its own override, or its default) already
 * matches the combo just pressed. The command being edited is excluded, so
 * pressing the key it already has is a harmless no-op save rather than a
 * refusal naming itself as the conflict.
 */
export function findConflict(
  commands: Command[],
  overrides: ShortcutOverrides,
  combo: string,
  excludeId: string,
): Command | undefined {
  return commands.find((c) => c.id !== excludeId && effectiveShortcut(c, overrides) === combo);
}
