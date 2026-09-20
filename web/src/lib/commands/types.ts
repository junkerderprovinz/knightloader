// The command registry: one list of commands (ALL_COMMANDS) and one hook
// (useCommands) that both the command palette and the keyboard dispatcher
// read, so "what can I do right now" has a single answer. Each surface adds its
// commands in its own file.

import { useMemo, type ComponentType } from 'react';
import { useNavigate, type NavigateFunction } from 'react-router-dom';
import { type CleanupClass, type QueueState, type Task } from '../api';
import { useInstanceScope } from '../instance';
import { useTasks } from '../useTasks';
import { useListView } from '../listview';
import { useToast, type ToastTone } from '../toast';
import { useT, type TranslationKey } from '../i18n';
import { useQueueControl } from '../../components/QueueBar';
import { useCommandPageContext } from './pageContext';
import { GLOBAL_COMMANDS } from './global';
import { downloadsCommands } from './downloads';
import { collectorCommands } from './collector';
import { queueCommands } from './queue';
import { settingsCommands } from './settings';
import { languageCommands } from './language';

/** Where a command is offered. 'global' commands appear on every surface. */
export type CommandSurface =
  | 'global'
  | 'overview'
  | 'downloads'
  | 'collector'
  | 'instances'
  | 'accounts'
  | 'settings';

/**
 * What a command's enabled(), visible() and run() get, assembled by
 * useCommandContext from hooks the pages already use. The selection comes
 * from lib/listview.ts, the same source the shell's overview strip reads, so
 * the two cannot disagree. Every call opens its own task subscription, so
 * build it once per mounting component and pass it down.
 */
export interface CommandContext {
  /** Which surface asked; the value passed to useCommands(). */
  surface: CommandSurface;
  navigate: NavigateFunction;
  /** '' is this instance; see lib/instance.tsx's InstanceScope. */
  instance: string;
  /** That instance's API prefix, ready for any lib/api.ts call. */
  base: string;
  /** Every task on the current instance, unfiltered. Empty where none is loaded. */
  tasks: Task[];
  /** Ids selected on whichever list is currently mounted (lib/listview.ts). Empty where no list is. */
  selection: string[];
  /** Ids currently visible on that same list, after its own search/filters. Empty where no list is. */
  visible: string[];
  /** Replaces the selection on the page that published a CommandPageContext.
   *  A no-op where none did. */
  setSelection: (next: Set<string>) => void;
  /** The page's own removal, so a command gets the same toast and cleared
   *  selection as the toolbar button. A no-op where no page published one. */
  removeSelected: (ids: string[]) => void;
  /** The page's clean-up flow: the classes the server offers (null until
   *  known or when no page published one) and the same preview() the "Clean
   *  up" menu calls. */
  cleanup: {
    classes: CleanupClass[] | null;
    preview: (cls: CleanupClass) => void;
  };
  /** The page's file-picker trigger. A no-op where no page has published one. */
  openFilePicker: () => void;
  /** The page's search-panel toggle. A no-op where no page has published one. */
  toggleSearch: () => void;
  /** The same t() the page itself renders with, for a run() that builds a toast sentence. */
  t: (key: TranslationKey, vars?: Record<string, string | number>) => string;
  /** A result banner without a command needing its own toast plumbing. */
  toast: (message: string, tone?: ToastTone) => void;
  /** This instance's master switch (QueueBar.tsx's useQueueControl). Null before the first fetch answers. */
  queue: QueueState | null;
  /** The same toggle QueueBar's button calls. */
  toggleQueue: () => void;
}

export interface Command {
  /** Stable, unique across the whole app: "downloads.pauseAll". */
  id: string;
  labelKey: TranslationKey;
  icon?: ComponentType<{ className?: string }>;
  /** Palette grouping, given as a translation key such as
   *  "commands.group.navigation", never as English text. */
  group: string;
  surfaces: CommandSurface[];
  /**
   * e.g. "mod+k", where "mod" is Cmd on Mac and Ctrl elsewhere (see
   * shortcuts.ts). A binding with "mod" also fires while typing in a field;
   * CommandDispatcher.tsx suppresses the others there.
   */
  defaultShortcut?: string;
  enabled: (ctx: CommandContext) => boolean;
  visible: (ctx: CommandContext) => boolean;
  run: (ctx: CommandContext) => void | Promise<void>;
}

// Every command in the app. A new surface adds one import and one spread here.
const ALL_COMMANDS: Command[] = [
  ...GLOBAL_COMMANDS,
  ...queueCommands,
  ...languageCommands,
  ...settingsCommands,
  ...downloadsCommands,
  ...collectorCommands,
];

/**
 * useCommands returns the commands live on a surface, filtered by
 * visible(ctx) and sorted by group then id. Global commands are included
 * everywhere. It does not filter by enabled(ctx): a disabled command still
 * shows in the palette, greyed out, so its shortcut has an explanation.
 */
export function useCommands(surface: CommandSurface, ctx: CommandContext): Command[] {
  return useMemo(
    () =>
      ALL_COMMANDS.filter((c) => c.surfaces.includes('global') || c.surfaces.includes(surface))
        .filter((c) => c.visible(ctx))
        .sort((a, b) => (a.group === b.group ? a.id.localeCompare(b.id) : a.group.localeCompare(b.group))),
    [surface, ctx],
  );
}

const NO_CLEANUP = { classes: null, preview: () => {} };

/**
 * useCommandContext assembles a CommandContext. The caller passes `surface`:
 * the dispatcher derives it from the route, a single-page component passes
 * its own.
 */
export function useCommandContext(surface: CommandSurface): CommandContext {
  const navigate = useNavigate();
  const { instance, base } = useInstanceScope();
  const tasksById = useTasks(instance);
  const list = useListView();
  // What a list page publishes beyond its visibility: setSelected, removal
  // and cleanup. Null where no such page is mounted.
  const page = useCommandPageContext();
  const { toast } = useToast();
  const { t } = useT();
  // The hook QueueBar's switch uses, so both show the same queue state.
  const { queue, toggle: toggleQueue } = useQueueControl(base, instance);

  return useMemo(
    () => ({
      surface,
      navigate,
      instance,
      base,
      tasks: Object.values(tasksById),
      selection: list ? [...list.selected] : [],
      visible: list ? [...list.visible] : [],
      setSelection: page?.setSelection ?? (() => {}),
      removeSelected: page?.removal ? page.removal.removeNow : () => {},
      cleanup: page?.cleanup ? { classes: page.cleanup.classes, preview: page.cleanup.preview } : NO_CLEANUP,
      openFilePicker: page?.openFilePicker ?? (() => {}),
      toggleSearch: page?.toggleSearch ?? (() => {}),
      t,
      toast,
      queue,
      toggleQueue: () => void toggleQueue(),
    }),
    [surface, navigate, instance, base, tasksById, list, page, t, toast, queue, toggleQueue],
  );
}
