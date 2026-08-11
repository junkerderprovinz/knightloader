// The Collector page's bulk/page-level actions, given a second entry point
// beside the toolbar buttons they already have. Every `run` below calls the
// exact same function Collector.tsx's own onClick already calls
// (startSelected/startAll/checkAll), or the same setSelected/
// removal.removeNow the page hands to lib/listview.ts's useReportListView
// (setSelection/removeSelected on CommandContext — see
// lib/commands/downloads.ts's own doc comment, which registers the same
// pair the same way).
import { recheckTasks, startTasks, type Task } from '../api';
import { IconCheck, IconPlay, IconSearch, IconTrash } from '../icons';
import type { Command, CommandContext } from './types';

// ctx.tasks is every task on the instance, unfiltered (CommandContext's own
// doc comment) — the same source Collector.tsx narrows with this exact
// filter before it ever renders a row (its own `collected` useMemo). A
// command reading ctx.tasks directly would count active downloads as
// "staged" too, so every collector command re-applies the same filter.
function staged(ctx: CommandContext): Task[] {
  return ctx.tasks.filter((x) => x.status === 'collected' && !x.skipped);
}

export const collectorCommands: Command[] = [
  {
    id: 'collector.startSelected',
    labelKey: 'collector.startSelected',
    icon: IconPlay,
    group: 'commands.group.collector',
    surfaces: ['collector'],
    enabled: (ctx) => ctx.selection.length > 0,
    visible: (ctx) => ctx.selection.length > 0,
    // The same two calls Collector.tsx's own startSelected() makes: start the
    // selected links, then say how many.
    run: (ctx) => {
      startTasks(ctx.selection);
      ctx.toast(ctx.t('collector.toastStarted', { n: ctx.selection.length }), 'info');
    },
  },
  {
    id: 'collector.startAll',
    labelKey: 'collector.startAll',
    icon: IconPlay,
    group: 'commands.group.collector',
    surfaces: ['collector'],
    enabled: (ctx) => staged(ctx).length > 0,
    visible: (ctx) => staged(ctx).length > 0,
    // Collector.tsx's own startAll(): an empty id list means every staged
    // link on this route.
    run: (ctx) => {
      startTasks([]);
      ctx.toast(ctx.t('collector.toastStarted', { n: staged(ctx).length }), 'info');
    },
  },
  {
    id: 'collector.checkAll',
    labelKey: 'collector.checkAll',
    icon: IconSearch,
    group: 'commands.group.collector',
    surfaces: ['collector'],
    enabled: (ctx) => staged(ctx).length > 0,
    visible: (ctx) => staged(ctx).length > 0,
    // The bar's own "Check all" button (Collector.tsx): an empty id list
    // means every staged link, same as startAll above.
    run: (ctx) => {
      recheckTasks([]);
      ctx.toast(ctx.t('task.recheck'), 'info');
    },
  },
  {
    id: 'collector.selectAll',
    labelKey: 'select.all',
    icon: IconCheck,
    group: 'commands.group.collector',
    surfaces: ['collector'],
    defaultShortcut: 'mod+a',
    enabled: (ctx) => ctx.visible.length > 0,
    visible: (ctx) => ctx.visible.length > 0,
    run: (ctx) => ctx.setSelection(new Set(ctx.visible)),
  },
  {
    id: 'collector.removeSelected',
    labelKey: 'task.remove',
    icon: IconTrash,
    group: 'commands.group.collector',
    surfaces: ['collector'],
    enabled: (ctx) => ctx.selection.length > 0,
    visible: (ctx) => ctx.selection.length > 0,
    // SelectionStrip's own "Remove" button, same as downloads.removeSelected
    // — no defaultShortcut here either, for the same reason: Del is already
    // bound to this exact call by useRemoval.
    run: (ctx) => ctx.removeSelected(ctx.selection),
  },
];
