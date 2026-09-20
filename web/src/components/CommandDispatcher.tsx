import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';
import { isEditableTarget } from '../lib/intake';
import { matchesShortcut } from '../lib/commands/shortcuts';
import { effectiveShortcut, readShortcutOverrides } from '../lib/commands/overrides';
import { useCommandContext, useCommands, type CommandSurface } from '../lib/commands/types';
import { OPEN_PALETTE_ID } from '../lib/commands/global';

/**
 * Commands whose shortcut fires even inside a text field. Everything else,
 * such as selectAll on mod+a, yields to the field's own editing shortcut.
 */
const ALWAYS_ACTIVE = new Set<string>([OPEN_PALETTE_ID]);

// surfaceForPath keys on the first path segment, like app/Layout.tsx's section.
function surfaceForPath(pathname: string): CommandSurface {
  const section = pathname.split('/')[1] ?? '';
  switch (section) {
    case '':
      return 'overview';
    case 'downloads':
      return 'downloads';
    case 'collector':
      return 'collector';
    case 'instances':
      return 'instances';
    case 'accounts':
      return 'accounts';
    case 'settings':
      return 'settings';
    default:
      return 'global';
  }
}

/**
 * CommandDispatcher is the one keydown listener for command shortcuts, mounted
 * in app/Layout.tsx so they work on every route. It reads the same commands the
 * palette lists and matches the binding in effect, including user overrides.
 */
export function CommandDispatcher() {
  const location = useLocation();
  const surface = surfaceForPath(location.pathname);
  const ctx = useCommandContext(surface);
  const commands = useCommands(surface, ctx);

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      // Commands are one-shot, so a held key must not repeat them.
      if (e.repeat || e.isComposing) return;
      const overrides = readShortcutOverrides();
      for (const cmd of commands) {
        const binding = effectiveShortcut(cmd, overrides);
        if (!binding) continue;
        if (!matchesShortcut(e, binding)) continue;
        if (!ALWAYS_ACTIVE.has(cmd.id) && isEditableTarget(e.target)) continue;
        if (!cmd.enabled(ctx)) continue;
        e.preventDefault();
        void cmd.run(ctx);
        return;
      }
    }
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [commands, ctx]);

  return null;
}
