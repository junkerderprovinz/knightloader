import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';
import { isEditableTarget } from '../lib/intake';
import { matchesShortcut } from '../lib/commands/shortcuts';
import { effectiveShortcut, readShortcutOverrides } from '../lib/commands/overrides';
import { useCommandContext, useCommands, type CommandSurface } from '../lib/commands/types';
import { OPEN_PALETTE_ID } from '../lib/commands/global';

/**
 * Commands whose shortcut must reach through a text field rather than defer
 * to whatever native editing behaviour that field already has for the same
 * chord. Deliberately a short, explicit list, not "every binding that
 * happens to include mod": mod+a/mod+f/mod+s/mod+z and the rest of the
 * ordinary mod+letter alphabet are exactly the browser/OS text-editing
 * shortcuts a user expects to keep working normally the moment focus is in
 * a field, and this app's own commands reusing one of those letters (e.g.
 * downloads.selectAll's mod+a) must lose to that expectation while typing,
 * not win. Only the palette itself is genuinely global in the way "reaches
 * through a text field" requires - see CommandDispatcher's own doc comment.
 */
const ALWAYS_ACTIVE = new Set<string>([OPEN_PALETTE_ID]);

/**
 * surfaceForPath turns the current route into the CommandSurface whose
 * commands should be live — keyed on the first path segment, the same rule
 * app/Layout.tsx's own `section` (what keys the page-enter animation) already
 * uses, for the identical reason: a route under a section this file has not
 * met yet still resolves to something real rather than nothing.
 *
 * Falls back to 'global' rather than throwing: /quickadd never mounts
 * <Layout>, so this only ever runs against the six routes Layout's own
 * <Outlet> serves — but a fallback costs nothing and a thrown error over a
 * keystroke is a worse failure than one shortcut quietly not matching.
 */
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
 * The one keydown listener every command's defaultShortcut is matched
 * against. Mounted once in app/Layout.tsx beside CaptchaModal, GlobalIntake,
 * StatusStrip and IdleActionBanner, for the identical reason those are
 * there: a listener scoped to one page's component tree only ever hears a
 * keystroke that lands on that page, and mod+k has to open the palette from
 * every route Layout serves, not just the one where the palette component
 * happens to also be mounted.
 *
 * Reads useCommands() for the currently active surface — the SAME hook and
 * the SAME CommandContext shape the palette reads (lib/commands/types.ts's
 * own doc comment) — so a shortcut only ever fires a command the palette
 * would also be listing right now, never a second, silently different set.
 *
 * Which shortcuts fire while typing: only the short, explicit ALWAYS_ACTIVE
 * list above (today, just opening the palette itself) reaches through a
 * text field — mod+k has to open the palette while the cursor is sitting in
 * the Collector's own paste box, the same way every editor with a command
 * palette already lets Cmd/Ctrl+K through. Everything else, including every
 * OTHER mod+letter binding, is suppressed the moment
 * isEditableTarget(e.target) is true (lib/intake.ts — the same guard
 * ListToolbar's own Delete-key handler and GlobalIntake's paste listener
 * already use). This used to be "any binding naming mod fires everywhere,"
 * which was wrong the moment a second mod+ command existed:
 * downloads.selectAll/collector.selectAll both default to mod+a, and mod+a
 * is also the browser's own select-all-text-in-this-field shortcut - the
 * old rule fired the app's command and ate the native one on every text
 * field on those two pages. A command needing this exemption asks for it
 * by id, explicitly, here - it is this file's call to grant, not a property
 * a binding earns just by naming "mod".
 *
 * Matches against the binding actually in EFFECT, not always
 * `cmd.defaultShortcut`: the Shortcuts settings tab (pages/settings/
 * Shortcuts.tsx) lets a person rebind or clear one, stored in the shared
 * uistate bucket under lib/commands/overrides.ts's own field, and
 * `effectiveShortcut` is the one place both that page and this dispatcher
 * read it — see that file's own doc comment ("the global keyboard
 * dispatcher … need to agree on"). `readShortcutOverrides` is the
 * non-hook reader built for exactly this call site: a document-level
 * keydown handler is not a component render.
 */
export function CommandDispatcher() {
  const location = useLocation();
  const surface = surfaceForPath(location.pathname);
  const ctx = useCommandContext(surface);
  const commands = useCommands(surface, ctx);

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      // A held key auto-repeating must not re-run a command once per
      // repeat event — most commands here are one-shot (navigate, open,
      // toggle), and a browser's own repeat rate would fire several before
      // the first keydown finished being handled.
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
