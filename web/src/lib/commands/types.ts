// The command registry core — build-plan.md's Wave-1D note locks the Command
// shape verbatim ("Fix the command-record type … and the useCommands(surface,
// ctx) hook. Every wave registers its commands as it builds them; if waves
// 1–11 keep writing inline onClick, the Wave 12 customiser cannot be built at
// all") and 12.12 repeats why: this is one of exactly two things in the whole
// plan that are retrofit-impossible, so it is built once here and every other
// surface's own command file (commands/downloads.ts, commands/collector.ts, …)
// is additive from this point on, never a redesign.
//
// One aggregator (ALL_COMMANDS below), one hook (useCommands) that both the
// command palette and the keyboard dispatcher read — so "what can I do right
// now" has exactly one answer, never two lists that quietly disagree.

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

/**
 * Which page(s)/contexts show a command. 'global' commands are visible on
 * every surface — see useCommands below, which is where that rule actually
 * lives.
 *
 * 'overview' is not one of the six build-plan.md's own Wave-1D note names as
 * the minimum, but Dashboard.tsx is exactly as real a routed page as the
 * other five and a future wave giving it its own commands should not have to
 * reopen this union to do it — cheap to add now, the same reasoning
 * build-plan.md gives for fixing this type in Wave 1 at all rather than
 * leaving it for Wave 12.
 */
export type CommandSurface =
  | 'global'
  | 'overview'
  | 'downloads'
  | 'collector'
  | 'instances'
  | 'accounts'
  | 'settings';

/**
 * Everything a command's enabled()/visible()/run() genuinely need, built
 * from hooks every page already has rather than invented for this file:
 * useNavigate() (react-router), useInstanceScope() (lib/instance.tsx),
 * useTasks(instance) (lib/useTasks.ts) and useListView() (lib/listview.ts —
 * the exact seam the shell's own overview strip already uses to learn a
 * page's Visible/Selected without that page prop-drilling it up). Reusing
 * that seam here rather than inventing a second one means a command's idea
 * of "the selection" can never drift from what the strip beside it is
 * already showing.
 *
 * useCommandContext() below assembles exactly this. Call it once per
 * mounting component (the keyboard dispatcher, the palette) and pass the
 * result down — each call opens its own task subscription
 * (lib/api.ts's connectWS has "no shared multiplexer yet", its own words),
 * so two independent call sites cost two sockets, the same tradeoff this
 * app already accepts at half a dozen other call sites (CaptchaModal,
 * IdleActionBanner, StatusStrip, useTasks itself).
 */
export interface CommandContext {
  /** Which surface asked — the same value passed to useCommands(). */
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
  /**
   * Replaces the selection on whichever list published a
   * CommandPageContext (lib/commands/pageContext.ts) — the identical
   * `setSelected` its own toolbar button calls. A no-op where no page has
   * published one.
   */
  setSelection: (next: Set<string>) => void;
  /**
   * That page's own `removal.removeNow` (ListToolbar.tsx's useRemoval),
   * published the same way — never a raw `deleteTasks`, so a command's
   * removal gets the same toast and cleared selection the strip's own
   * Remove button gets. A no-op where no page has published one.
   */
  removeSelected: (ids: string[]) => void;
  /**
   * That page's own clean-up flow (ListToolbar.tsx's useCleanup), narrowed
   * to what a command needs: which classes the server actually offers, and
   * the same `preview()` the "Clean up" menu's own entries call. `classes`
   * is null wherever no page has published one, or has not loaded them yet
   * — either way, nothing this build calls "finished" et al. against it.
   */
  cleanup: {
    classes: CleanupClass[] | null;
    preview: (cls: CleanupClass) => void;
  };
  /** The same t() the page itself renders with, for a run() that builds a toast sentence. */
  t: (key: TranslationKey, vars?: Record<string, string | number>) => string;
  /** A result banner without a command needing its own toast plumbing. */
  toast: (message: string, tone?: ToastTone) => void;
  /** This instance's master switch (QueueBar.tsx's own useQueueControl). Null before the first fetch answers. */
  queue: QueueState | null;
  /** QueueBar's own toggle — the same function its button calls, never a second `setQueue`. */
  toggleQueue: () => void;
}

export interface Command {
  /** Stable, unique across the whole app: "downloads.pauseAll". */
  id: string;
  labelKey: TranslationKey;
  icon?: ComponentType<{ className?: string }>;
  /**
   * Palette grouping, e.g. "Downloads" — but see this codebase's own i18n
   * rule (lib/locales/en.ts is the compile-time source of truth; nothing
   * hardcodes English past it). `group` is typed `string` here because the
   * Command shape is locked verbatim from build-plan.md's Wave-1D note, not
   * because a raw English label belongs in it — every command below sets it
   * to a real TranslationKey string ("commands.group.navigation") rather
   * than literal text, so the palette can render `t(cmd.group as
   * TranslationKey)` and stay in the same i18n contract as everything else
   * in this app. Follow that convention in every later surface file: reuse
   * an existing commands.group.* key or add one, never a bare English word.
   */
  group: string;
  surfaces: CommandSurface[];
  /**
   * e.g. "mod+k" — "mod" is Cmd on Mac, Ctrl elsewhere. See shortcuts.ts.
   *
   * A binding that includes "mod" fires everywhere, including while typing
   * in a text field; one that does not is suppressed while focus is inside
   * an input/textarea/contenteditable. See CommandDispatcher.tsx's own doc
   * comment for the reasoning — that rule lives there, not here, since it
   * is the dispatcher's decision to make each time it matches a keystroke,
   * not a property of the command record itself.
   */
  defaultShortcut?: string;
  enabled: (ctx: CommandContext) => boolean;
  visible: (ctx: CommandContext) => boolean;
  run: (ctx: CommandContext) => void | Promise<void>;
}

/**
 * Every command this app has, across every surface.
 *
 * One array, appended to as each surface's own file lands — the same shape
 * pages/settings/registry.tsx's PAGES map already uses for exactly this
 * reason: a later wave adds a file, then one import and one spread here,
 * rather than a second registry that can drift from this one. Nothing else
 * may hold its own second command list; the palette and the keyboard
 * dispatcher both read this exclusively through useCommands() below.
 */
const ALL_COMMANDS: Command[] = [
  ...GLOBAL_COMMANDS,
  ...queueCommands,
  ...languageCommands,
  ...settingsCommands,
  ...downloadsCommands,
  ...collectorCommands,
];

/**
 * useCommands is what both the palette and the keyboard dispatcher read: the
 * commands live right now, already filtered by visible(ctx) and sorted by
 * group then id — so two callers evaluating the same (surface, ctx) can
 * never render two different orders either.
 *
 * A 'global' command is included for every surface, which is what makes
 * mod+k for the palette itself work no matter which page is open. This
 * does NOT filter by enabled(ctx): a command that is visible but currently
 * disabled still belongs in the palette's list (greyed out, so its
 * shortcut has an answer for why it did nothing) — enabled() is the
 * dispatcher's and the palette's own call to make at the moment a command
 * is actually invoked, not a reason to hide it.
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

/**
 * useCommandContext assembles a CommandContext from this app's existing
 * hooks — see this file's own doc comment on CommandContext for which ones,
 * and for the one-subscription-per-call-site cost of calling this more than
 * once. `surface` is not read from the route here on purpose: a component
 * mounted once at the top of the tree (the keyboard dispatcher) has to
 * derive it from the current location itself, while a component that only
 * ever renders on one page can simply pass its own surface literal — this
 * hook does not privilege either caller.
 */
const NO_CLEANUP = { classes: null, preview: () => {} };

export function useCommandContext(surface: CommandSurface): CommandContext {
  const navigate = useNavigate();
  const { instance, base } = useInstanceScope();
  const tasksById = useTasks(instance);
  const list = useListView();
  // The command surface's own bridge (lib/commands/pageContext.ts) — what a
  // downloads/collector-style page publishes beyond the read-only visibility
  // lib/listview.ts already reports: the actual setSelected/removal/cleanup
  // its own toolbar already calls. Null wherever no such page is mounted
  // (Settings, Accounts, or no page at all).
  const page = useCommandPageContext();
  const { toast } = useToast();
  const { t } = useT();
  // The exact hook QueueBar.tsx's own switch is built on — see its doc
  // comment. A command context that fetched the queue state its own way
  // would risk answering a "is the queue halted" question with a second,
  // possibly-stale opinion from the one QueueBar is actually showing.
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
      t,
      toast,
      queue,
      toggleQueue: () => void toggleQueue(),
    }),
    [surface, navigate, instance, base, tasksById, list, page, t, toast, queue, toggleQueue],
  );
}
