import { setQueue } from '../api';
import { IconPause, IconPlay } from '../icons';
import type { Command } from './types';

/**
 * The master switch QueueBar.tsx (the shell-bar transport control, mounted
 * once in app/Layout.tsx, so it is on every page - not the downloads page's
 * own copy, commands/downloads.ts's stopQueue/startQueue) already carries -
 * see QueueBar.tsx's own doc comment for why halting rather than aborting:
 * it stops the scheduler handing out new work and leaves anything already
 * running to finish, so a transfer never loses bytes to a command run by
 * mistake.
 *
 * `surfaces: ['global']` is what makes these two reachable from every page,
 * matching where the switch they mirror actually lives - unlike
 * commands/downloads.ts's copy, which only shows up while Downloads itself
 * is open.
 *
 * CommandContext (this file's own './types') carries no live queue snapshot
 * - `halted` is state QueueBar/QuickSettings each hold locally, not a
 * shared store - so both commands are always offered and both are
 * idempotent: `run` sets the ABSOLUTE state (halted: true / false) via the
 * same setQueue() QueueBar's own toggle() calls, rather than flipping
 * whatever the queue happened to be. Pressing "Stop queue" while already
 * halted is a no-op the server itself is asked to make, the same as
 * pressing pause twice on a media player.
 */
export const queueCommands: Command[] = [
  {
    id: 'queue.stopAll',
    labelKey: 'queue.stop',
    icon: IconPause,
    group: 'commands.group.queue',
    surfaces: ['global'],
    defaultShortcut: 'mod+shift+h',
    enabled: () => true,
    visible: () => true,
    run: async (ctx) => {
      await setQueue({ halted: true }, ctx.base);
    },
  },
  {
    id: 'queue.resumeAll',
    labelKey: 'queue.start',
    icon: IconPlay,
    group: 'commands.group.queue',
    surfaces: ['global'],
    defaultShortcut: 'mod+shift+g',
    enabled: () => true,
    visible: () => true,
    run: async (ctx) => {
      await setQueue({ halted: false }, ctx.base);
    },
  },
];
