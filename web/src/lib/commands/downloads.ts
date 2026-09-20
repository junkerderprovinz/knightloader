// The Downloads page's page-level actions as commands. Each run() calls the
// same function the page's own buttons call, or what the page publishes
// through pageContext.ts; nothing here reimplements an action. Stopping and
// starting the queue live in commands/queue.ts, on every surface.
import { pause, resume, restartTasks, moveTasks, queueMove } from '../api';
import { MOVE_STATES } from '../../components/ListToolbar';
import { IconArrowDown, IconArrowUp, IconBottom, IconCheck, IconPause, IconPlay, IconRetry, IconSearch, IconTop, IconTrash } from '../icons';
import type { Command, CommandContext } from './types';

/**
 * singlePackage is the one package the selection belongs to, or null when it
 * is empty or spans several. A one-place move has to say which package goes
 * first, so up and down act on a single package; top and bottom send the ids.
 */
function singlePackage(ctx: CommandContext): string | null {
  const chosen = ctx.tasks.filter((x) => ctx.selection.includes(x.id));
  if (chosen.length === 0) return null;
  const names = new Set(chosen.map((x) => x.package ?? ''));
  return names.size === 1 ? [...names][0]! : null;
}

/**
 * canMoveSelection reports whether the server would move any of the
 * selection. MOVE_STATES mirrors movable() in internal/app/app_queue.go
 * (checked by check-queue-reach.mjs). The move route answers 204 even when
 * nothing moved, so the command has to know beforehand.
 */
function canMoveSelection(ctx: CommandContext): boolean {
  return ctx.tasks.some((x) => ctx.selection.includes(x.id) && MOVE_STATES.includes(x.status));
}

/**
 * movablePackage is singlePackage while that package still has a movable
 * row. It asks about the package, not the selected rows, because
 * queueMove({ package }) moves the whole package.
 */
function movablePackage(ctx: CommandContext): string | null {
  const name = singlePackage(ctx);
  if (name === null) return null;
  return ctx.tasks.some((x) => (x.package ?? '') === name && MOVE_STATES.includes(x.status)) ? name : null;
}

// The move shortcuts are JDownloader 2's own (Alt+Home, Alt+Up, Alt+Down,
// Alt+End in PackageControllerTable), and mod+F is its "Find".
export const downloadsCommands: Command[] = [
  {
    id: 'downloads.pauseAll',
    labelKey: 'downloads.pauseAll',
    icon: IconPause,
    group: 'commands.group.downloads',
    surfaces: ['downloads'],
    defaultShortcut: 'mod+shift+p',
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
    // An empty id list retries every failed link.
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
    // Every row on screen, like the toolbar's "Select all", not the whole list.
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
    // Keeps the files, like the toolbar's Remove. No shortcut: useRemoval
    // already binds Del to the same call.
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
    // Left out rather than greyed when there is nothing to find, as in the
    // toolbar's menu.
    visible: (ctx) => !!ctx.cleanup.classes?.includes('finished') && ctx.tasks.some((x) => x.status === 'done'),
    // Opens the same count preview and confirmation as the "Clean up" menu.
    run: (ctx) => ctx.cleanup.preview('finished'),
  },
  {
    id: 'downloads.toggleSearch',
    // The search badge's name, not the empty field's placeholder
    // (check-placeholder-as-label.mjs).
    labelKey: 'search.toggle',
    icon: IconSearch,
    group: 'commands.group.downloads',
    surfaces: ['downloads'],
    defaultShortcut: 'mod+f',
    enabled: () => true,
    visible: () => true,
    run: (ctx) => ctx.toggleSearch(),
  },
  {
    id: 'downloads.moveTop',
    labelKey: 'task.moveTop',
    icon: IconTop,
    group: 'commands.group.downloads',
    surfaces: ['downloads'],
    defaultShortcut: 'alt+home',
    // Left out, not greyed, for a selection the server would not move, the
    // same gate the queue menu in ListToolbar.tsx uses.
    enabled: (ctx) => canMoveSelection(ctx),
    visible: (ctx) => canMoveSelection(ctx),
    run: (ctx) => void moveTasks(ctx.selection, 'top', ctx.base),
  },
  {
    id: 'downloads.moveUp',
    labelKey: 'task.moveUp',
    icon: IconArrowUp,
    group: 'commands.group.downloads',
    surfaces: ['downloads'],
    defaultShortcut: 'alt+up',
    enabled: (ctx) => movablePackage(ctx) !== null,
    visible: (ctx) => movablePackage(ctx) !== null,
    run: (ctx) => void queueMove({ package: movablePackage(ctx)! }, 'up', ctx.base),
  },
  {
    id: 'downloads.moveDown',
    labelKey: 'task.moveDown',
    icon: IconArrowDown,
    group: 'commands.group.downloads',
    surfaces: ['downloads'],
    defaultShortcut: 'alt+down',
    enabled: (ctx) => movablePackage(ctx) !== null,
    visible: (ctx) => movablePackage(ctx) !== null,
    run: (ctx) => void queueMove({ package: movablePackage(ctx)! }, 'down', ctx.base),
  },
  {
    id: 'downloads.moveBottom',
    labelKey: 'task.moveBottom',
    icon: IconBottom,
    group: 'commands.group.downloads',
    surfaces: ['downloads'],
    defaultShortcut: 'alt+end',
    enabled: (ctx) => canMoveSelection(ctx),
    visible: (ctx) => canMoveSelection(ctx),
    run: (ctx) => void moveTasks(ctx.selection, 'bottom', ctx.base),
  },
];
