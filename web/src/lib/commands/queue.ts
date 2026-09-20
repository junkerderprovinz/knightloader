import { setQueue } from '../api';
import { IconPause, IconPlay } from '../icons';
import type { Command } from './types';

/**
 * The queue's master switch from QueueBar.tsx, on every page. Halting stops
 * new work and lets running transfers finish, so a mistaken command loses no
 * bytes. Both commands set the absolute state rather than toggling, so they
 * are always offered and pressing one twice is harmless.
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
