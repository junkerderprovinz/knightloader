// The Collector page's page-level actions as commands. Each run() calls the
// same function as the page's own buttons, or what the page publishes
// through pageContext.ts.
import { recheckTasks, startTasks, type Task } from '../api';
import { IconCheck, IconFolder, IconPlay, IconSearch, IconTrash } from '../icons';
import type { Command, CommandContext } from './types';

// ctx.tasks holds every task, so this applies the same filter the Collector
// page renders with.
function staged(ctx: CommandContext): Task[] {
  return ctx.tasks.filter((x) => x.status === 'collected' && !x.skipped && !x.variantOff);
}

export const collectorCommands: Command[] = [
  {
    id: 'collector.chooseFile',
    labelKey: 'container.choose',
    icon: IconFolder,
    group: 'commands.group.collector',
    surfaces: ['collector'],
    defaultShortcut: 'mod+shift+o',
    enabled: () => true,
    visible: () => true,
    // The same file picker AddLinksForm's folder badge opens.
    run: (ctx) => ctx.openFilePicker(),
  },
  {
    id: 'collector.startSelected',
    labelKey: 'collector.startSelected',
    icon: IconPlay,
    group: 'commands.group.collector',
    surfaces: ['collector'],
    defaultShortcut: 'mod+shift+enter',
    enabled: (ctx) => ctx.selection.length > 0,
    visible: (ctx) => ctx.selection.length > 0,
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
    defaultShortcut: 'mod+shift+a',
    enabled: (ctx) => staged(ctx).length > 0,
    visible: (ctx) => staged(ctx).length > 0,
    // An empty id list starts every staged link.
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
    defaultShortcut: 'mod+shift+u',
    enabled: (ctx) => staged(ctx).length > 0,
    visible: (ctx) => staged(ctx).length > 0,
    // An empty id list checks every staged link.
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
    // No shortcut: useRemoval already binds Del to the same call.
    run: (ctx) => ctx.removeSelected(ctx.selection),
  },
];
