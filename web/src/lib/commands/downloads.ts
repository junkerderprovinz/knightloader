// The Downloads page's bulk/page-level actions, given a second entry point
// (keyboard shortcut / future command palette) beside the toolbar buttons
// they already have. Every `run` below calls the exact same function
// Downloads.tsx's own onClick already calls (pauseAll/resumeAll/
// retryFailed), or the exact same setSelected/removal.removeNow/cleanup.preview
// the page publishes through lib/commands/pageContext.ts (setSelection/
// removeSelected/cleanup on CommandContext — see lib/commands/types.ts and
// Downloads.tsx's own usePublishCommandPageContext call). Nothing here
// reimplements an action.
//
// "stop queue"/"start queue" are deliberately absent: they already exist as
// commands/queue.ts's queue.stopAll/queue.resumeAll, reachable from every
// surface (its own doc comment) — a second copy here would be the exact "two
// lists that can disagree" lib/commands/types.ts's own doc comment warns
// against.
import { pause, resume, restartTasks } from '../api';
import { IconCheck, IconPause, IconPlay, IconRetry, IconTrash } from '../icons';
import type { Command } from './types';

export const downloadsCommands: Command[] = [
  {
    id: 'downloads.pauseAll',
    labelKey: 'downloads.pauseAll',
    icon: IconPause,
    group: 'commands.group.downloads',
    surfaces: ['downloads'],
    defaultShortcut: 'mod+shift+p',
    // Same guard the toolbar button uses: the button itself only renders
    // while counts.running > 0 (Downloads.tsx), mirrored here against the
    // unfiltered task stream CommandContext carries instead of the page's
    // own `counts`.
    enabled: (ctx) => ctx.tasks.some((x) => x.status === 'running'),
    visible: (ctx) => ctx.tasks.some((x) => x.status === 'running'),
    run: (ctx) => {
      for (const x of ctx.tasks) if (x.status === 'running') void pause(x.id, ctx.base);
    },
  },
  {
    id: 'downloads.resumeAll',
    labelKey: 'downloads.resumeAll',
    icon: IconPlay,
    group: 'commands.group.downloads',
    surfaces: ['downloads'],
    defaultShortcut: 'mod+shift+r',
    enabled: (ctx) => ctx.tasks.some((x) => x.status === 'paused'),
    visible: (ctx) => ctx.tasks.some((x) => x.status === 'paused'),
    run: (ctx) => {
      for (const x of ctx.tasks) if (x.status === 'paused') void resume(x.id, ctx.base);
    },
  },
  {
    id: 'downloads.retryFailed',
    labelKey: 'downloads.retryFailed',
    icon: IconRetry,
    group: 'commands.group.downloads',
    surfaces: ['downloads'],
    defaultShortcut: 'mod+shift+z',
    enabled: (ctx) => ctx.tasks.some((x) => x.status === 'error'),
    visible: (ctx) => ctx.tasks.some((x) => x.status === 'error'),
    // restartTasks([], base) is the bulk form: an empty id list means every
    // failed link on this route, the same call Downloads.tsx's own
    // retryFailed() makes.
    run: (ctx) => void restartTasks([], ctx.base),
  },
  {
    id: 'downloads.selectAll',
    labelKey: 'select.all',
    icon: IconCheck,
    group: 'commands.group.downloads',
    surfaces: ['downloads'],
    defaultShortcut: 'mod+a',
    enabled: (ctx) => ctx.visible.length > 0,
    visible: (ctx) => ctx.visible.length > 0,
    // The same set ListActionBar's own "Select all" button builds
    // (ListToolbar.tsx) — every row currently on screen, not the whole list.
    run: (ctx) => ctx.setSelection(new Set(ctx.visible)),
  },
  {
    id: 'downloads.removeSelected',
    labelKey: 'task.remove',
    icon: IconTrash,
    group: 'commands.group.downloads',
    surfaces: ['downloads'],
    enabled: (ctx) => ctx.selection.length > 0,
    visible: (ctx) => ctx.selection.length > 0,
    // SelectionStrip's own "Remove" button (ListToolbar.tsx) calls this same
    // removeNow with the selection — files are left untouched, the same rule
    // that button follows. No defaultShortcut: Del is already bound to this
    // exact call by useRemoval itself, and a second listener on the same key
    // would only race it.
    run: (ctx) => ctx.removeSelected(ctx.selection),
  },
  {
    id: 'downloads.clearFinished',
    labelKey: 'cleanup.finished',
    icon: IconTrash,
    group: 'commands.group.downloads',
    surfaces: ['downloads'],
    defaultShortcut: 'mod+shift+f',
    enabled: (ctx) => !!ctx.cleanup.classes?.includes('finished'),
    // Offered once the server has actually announced the class and there is
    // something for it to find — the same "an entry that cannot act on
    // anything is left out, not shown greyed" rule ListToolbar.tsx's own menu
    // follows.
    visible: (ctx) => !!ctx.cleanup.classes?.includes('finished') && ctx.tasks.some((x) => x.status === 'done'),
    // preview() is the exact function the "Clean up" dropdown's "Remove
    // finished" entry calls (useCleanup, ListToolbar.tsx) — it previews the
    // count and raises the same confirm dialog (Downloads.tsx's own
    // useCleanup instance renders it), rather than removing rows with no
    // chance to see how many first.
    run: (ctx) => ctx.cleanup.preview('finished'),
  },
];
