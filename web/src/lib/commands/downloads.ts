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
import { MOVE_STATES } from '../../components/ListToolbar';
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
 * Only moveUp/moveDown below use this. moveTop/moveBottom send the selection's
 * raw ids instead — the two shapes the server's own move takes, Selection{Ids}
 * and Selection{Package}, which land in the same app.MoveIn either way
 * (internal/app/app_queue.go). The reason the pair is split that way is the
 * single-package rule above and nothing else: a step of one place has to say
 * which package arrives there first, and "top"/"bottom" does not.
 *
 * THE BADGES THIS COMMENT USED TO POINT AT ARE GONE. It said moveTop/moveBottom
 * "mirror Downloads.tsx's own always-visible toolbar badges", and those four
 * page-level badges were folded into one "Reihenfolge" badge that opens
 * queueMenuGroup (ListToolbar.tsx) — the same group the right-click menu shows.
 * So the surface these two commands have to stay level with is that group, and
 * the group drops its move entry for a selection the server would refuse
 * (MOVE_STATES). See canMoveSelection below, which is how they now do it.
 */
function singlePackage(ctx: CommandContext): string | null {
  const chosen = ctx.tasks.filter((x) => ctx.selection.includes(x.id));
  if (chosen.length === 0) return null;
  const names = new Set(chosen.map((x) => x.package ?? ''));
  return names.size === 1 ? [...names][0]! : null;
}

/**
 * WHETHER THE SERVER WOULD MOVE ANY OF THIS SELECTION.
 *
 * MOVE_STATES is the browser's copy of movable() in internal/app/app_queue.go,
 * declared in ListToolbar.tsx beside the menu that reads it and held level with
 * the Go per status by check-queue-reach.mjs. MoveIn picks the movable rows out
 * of whatever selection it is handed, so a selection holding none of them is a
 * request that changes nothing — and /api/tasks/move answers it 204 either way,
 * which is why nothing on the way back could ever have reported it.
 *
 * Measured on a running instance before this gate existed, with small.bin (done)
 * and missing.bin (error) selected: the palette offered "Nach ganz oben
 * Alt+Pos1" enabled, pressing it sent POST /api/tasks/move -> 204, and
 * GET /api/tasks was byte-identical before and after (both positions still 0).
 */
function canMoveSelection(ctx: CommandContext): boolean {
  return ctx.tasks.some((x) => ctx.selection.includes(x.id) && MOVE_STATES.includes(x.status));
}

/**
 * The same question for the package form: singlePackage()'s answer, but only
 * while that package still has something in it the server would move.
 *
 * It asks about the PACKAGE and not about the selected rows on purpose, because
 * that is what the request does: queueMove({ package }) moves the whole package,
 * so one finished row picked inside a package that is still downloading is an
 * ordinary, working move. Measured against the server with a package whose rows
 * were all done/error: POST /api/queue/move -> 200 {"ids":[],"count":0}.
 */
function movablePackage(ctx: CommandContext): string | null {
  const name = singlePackage(ctx);
  if (name === null) return null;
  return ctx.tasks.some((x) => (x.package ?? '') === name && MOVE_STATES.includes(x.status)) ? name : null;
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
    // The badge's name, not the empty field's hint. This was
    // `search.placeholder` ("Search this list…"), so the palette listed a
    // command called "Search this list…" and matched typing against it - the
    // same string doing two jobs whose requirements point opposite ways, which
    // is what web/check-placeholder-as-label.mjs now refuses. One control, one
    // name: the badge on Downloads.tsx that this command presses says "Suche",
    // and a person looking for it in the palette types that.
    labelKey: 'search.toggle',
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
    // LEFT OUT rather than greyed, which is this file's own habit (see
    // clearFinished above) and the queue menu's: ListToolbar.tsx drops the
    // whole move entry for a selection the server would refuse, so a palette
    // that still listed it — enabled, with its shortcut printed beside it —
    // would be the second list types.ts warns about, disagreeing with the
    // first about the same verb.
    enabled: (ctx) => canMoveSelection(ctx),
    visible: (ctx) => canMoveSelection(ctx),
    // moveTasks(ids, 'top') is the id form of the very step the queue menu's
    // "Nach ganz oben" runs (queueMenuGroup in ListToolbar.tsx): both arrive at
    // app.MoveIn with a Selection of ids. The gate above is that entry's gate,
    // read off the same MOVE_STATES, so the two surfaces cannot disagree about
    // which selections the verb is offered for.
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
    // movablePackage, not singlePackage: the single-package rule says which
    // package a step belongs to, and it says nothing about whether the server
    // would take the step. Measured on a package of done/error rows, this pair
    // was as dead as top/bottom were — 200 {"ids":[],"count":0}, nothing moved.
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
    // Real JDownloader 2's own Alt+Down — source:
    // org/jdownloader/gui/toolbar/action/MoveDownAction.java
    // (PackageControllerTable.KEY_STROKE_ALT_DOWN).
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
    // Real JDownloader 2's own Alt+End — source:
    // org/jdownloader/gui/toolbar/action/MoveToBottomAction.java
    // (PackageControllerTable.KEY_STROKE_ALT_END).
    defaultShortcut: 'alt+end',
    enabled: (ctx) => canMoveSelection(ctx),
    visible: (ctx) => canMoveSelection(ctx),
    // The mirror of moveTop above, down to the gate: the queue menu's "Nach
    // ganz unten", sent as ids.
    run: (ctx) => void moveTasks(ctx.selection, 'bottom', ctx.base),
  },
];
