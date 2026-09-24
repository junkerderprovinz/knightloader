// Where rebound keyboard shortcuts live, and what a command's current binding
// is, so the Shortcuts settings tab and the keyboard dispatcher agree.
import { useMemo } from 'react';
import { peekUIState, useUIState } from '../uistate';
import { pageId } from '../../pages/settings/folded';
import type { Command } from './types';

const OPEN_PAGE = 'settings.open.';

/**
 * foldPageCommands moves the binding of a folded settings page's command to the
 * command of the page holding its cards, unless that one has a binding of its
 * own. Saving writes the result, so the folded id leaves the document.
 */
function foldPageCommands(o: ShortcutOverrides): ShortcutOverrides {
  let out = o;
  for (const [id, combo] of Object.entries(o)) {
    if (!id.startsWith(OPEN_PAGE)) continue;
    const page = id.slice(OPEN_PAGE.length);
    const target = OPEN_PAGE + pageId(page);
    if (target === id) continue;
    if (out === o) out = { ...o };
    delete out[id];
    out[target] ??= combo;
  }
  return out;
}

/** The uistate field holding every rebound shortcut as commandId -> combo.
 *  The shared bucket is server-persisted, so bindings follow the user. */
export const SHORTCUT_OVERRIDES_FIELD = 'commands.shortcutOverrides';

export type ShortcutOverrides = Record<string, string>;

const EMPTY_OVERRIDES: ShortcutOverrides = {};

/** The settings tab's own read/write handle on the override document. */
export function useShortcutOverrides(): [ShortcutOverrides, (next: ShortcutOverrides) => void] {
  const [stored, write] = useUIState<ShortcutOverrides>(SHORTCUT_OVERRIDES_FIELD, EMPTY_OVERRIDES);
  const folded = useMemo(() => foldPageCommands(stored), [stored]);
  return [folded, write];
}

/** readShortcutOverrides is the non-hook reader for the dispatcher's keydown
 *  handler. It does not wait for the network; the bucket is loaded at boot. */
export function readShortcutOverrides(): ShortcutOverrides {
  return foldPageCommands(peekUIState<ShortcutOverrides>(SHORTCUT_OVERRIDES_FIELD, EMPTY_OVERRIDES));
}

/** The binding actually in effect for a command: its override if it has one, else its default. */
export function effectiveShortcut(cmd: Command, overrides: ShortcutOverrides): string | undefined {
  return overrides[cmd.id] ?? cmd.defaultShortcut;
}

/**
 * findConflict returns the other command whose current binding already
 * matches combo. The command being edited is excluded, so pressing its own
 * key is a harmless save rather than a conflict with itself.
 */
export function findConflict(
  commands: Command[],
  overrides: ShortcutOverrides,
  combo: string,
  excludeId: string,
): Command | undefined {
  return commands.find((c) => c.id !== excludeId && effectiveShortcut(c, overrides) === combo);
}
