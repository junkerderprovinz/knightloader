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
import { pause, resume, restartTasks, moveTasks, queueMove } from '../api';
import { IconArrowDown, IconArrowUp, IconBottom, IconCheck, IconPause, IconPlay, IconRetry, IconSearch, IconTop, IconTrash } from '../icons';
import type { Command, CommandContext } from './types';

/**
 * The one package an up/down queue-move command is allowed to act on:
 * PackageActions.tsx's own `packages` useMemo, re-derived here from
 * ctx.tasks/ctx.selection rather than imported, since that component only
 * ever runs inside a mounted Downloads page and a command's
 * enabled()/visible() have to answer the same question with no component
 * mounted at all. Null whenever the selection is empty or spans more than
 * one package — queueMove takes one package name, and "send three packages
 * up one slot at once" has no defensible answer about which of them arrives
 * there first (PackageActions.tsx's own comment, verbatim reasoning, on why
 * its own "queue order" menu is offered only while `packages.length === 1`).
 *
 * Only moveUp/moveDown below use this — moveTop/moveBottom instead mirror
 * Downloads.tsx's own always-visible toolbar badges, which move the raw
 * selection via moveTasks() rather than a resolved package name, and carry
 * no such restriction. queueMove is package-scoped for all four directions,
 * but this app's own UI only exposes an id-based, unrestricted equivalent
 * for top/bottom (the toolbar badges) — up/down exist only behind
 * PackageActions' single-package-gated submenu, so that is the one both
 * commands below stay consistent with.
 */
function singlePackage(ctx: CommandContext): string | null {
  const chosen = ctx.tasks.filter((x) => ctx.selection.includes(x.id));
  if (chosen.length === 0) return null;
  const names = new Set(chosen.map((x) => x.package ?? ''));
  return names.size === 1 ? [...names][0]! : null;
}

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
  {
    id: 'downloads.toggleSearch',
    labelKey: 'search.placeholder',
    icon: IconSearch,
    group: 'commands.group.downloads',
    surfaces: ['downloads'],
    // Real JDownloader 2's own mod+F ("Find") on its downloads table —
    // source: org/jdownloader/gui/views/downloads/bottombar/SearchMenuItem.java,
    // `KeyStroke.getKeyStroke(KeyEvent.VK_F, Toolkit.getDefaultToolkit().getMenuShortcutKeyMask())`.
    defaultShortcut: 'mod+f',
    enabled: () => true,
    visible: () => true,
    // ctx.toggleSearch is Downloads.tsx's own `setSearchOpen((v) => !v)`
    // (pageContext.ts) — the identical toggle its search badge already calls.
    run: (ctx) => ctx.toggleSearch(),
  },
  {
    id: 'downloads.moveTop',
    labelKey: 'task.moveTop',
    icon: IconTop,
    group: 'commands.group.downloads',
    surfaces: ['downloads'],
    // Real JDownloader 2's own Alt+Home — source:
    // org/jdownloader/gui/toolbar/action/MoveToTopAction.java, whose
    // accelerator is PackageControllerTable.KEY_STROKE_ALT_HOME
    // (`KeyStroke.getKeyStroke(KeyEvent.VK_HOME, InputEvent.ALT_MASK)`).
    defaultShortcut: 'alt+home',
    enabled: (ctx) => ctx.selection.length > 0,
    visible: (ctx) => ctx.selection.length > 0,
    // The same moveTasks(...) SelectionStrip's own "Move top" badge calls
    // (Downloads.tsx) — the selection's raw ids, not a resolved package
    // name, so this stays enabled for exactly the same selections that
    // badge is already clickable for.
    run: (ctx) => void moveTasks(ctx.selection, 'top', ctx.base),
  },
  {
    id: 'downloads.moveUp',
    labelKey: 'task.moveUp',
    icon: IconArrowUp,
    group: 'commands.group.downloads',
    surfaces: ['downloads'],
    // Real JDownloader 2's own Alt+Up — source:
    // org/jdownloader/gui/toolbar/action/MoveUpAction.java
    // (PackageControllerTable.KEY_STROKE_ALT_UP).
    defaultShortcut: 'alt+up',
    enabled: (ctx) => singlePackage(ctx) !== null,
    visible: (ctx) => singlePackage(ctx) !== null,
    run: (ctx) => void queueMove({ package: singlePackage(ctx)! }, 'up', ctx.base),
  },
  {
    id: 'downloads.moveDown',
    labelKey: 'task.moveDown',
    icon: IconArrowDown,
    group: 'commands.group.downloads',
    surfaces: ['downloads'],
    // Real JDownloader 2's own Alt+Down — source:
    // org/jdownloader/gui/toolbar/action/MoveDownAction.java
    // (PackageControllerTable.KEY_STROKE_ALT_DOWN).
    defaultShortcut: 'alt+down',
    enabled: (ctx) => singlePackage(ctx) !== null,
    visible: (ctx) => singlePackage(ctx) !== null,
    run: (ctx) => void queueMove({ package: singlePackage(ctx)! }, 'down', ctx.base),
  },
  {
    id: 'downloads.moveBottom',
    labelKey: 'task.moveBottom',
    icon: IconBottom,
    group: 'commands.group.downloads',
    surfaces: ['downloads'],
    // Real JDownloader 2's own Alt+End — source:
    // org/jdownloader/gui/toolbar/action/MoveToBottomAction.java
    // (PackageControllerTable.KEY_STROKE_ALT_END).
    defaultShortcut: 'alt+end',
    enabled: (ctx) => ctx.selection.length > 0,
    visible: (ctx) => ctx.selection.length > 0,
    // The same moveTasks(...) SelectionStrip's own "Move bottom" badge calls.
    run: (ctx) => void moveTasks(ctx.selection, 'bottom', ctx.base),
  },
];
