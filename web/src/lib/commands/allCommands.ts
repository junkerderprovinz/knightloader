// The flat, unfiltered union of every command this build declares, across
// every surface.
//
// types.ts's own ALL_COMMANDS (private) exists for exactly one job:
// useCommands(surface, ctx) filtering a live, mounted context down to what
// is reachable right now. Settings/Shortcuts.tsx needs a different question
// answered - "every command that HAS a default shortcut, wherever it lives"
// - which is deliberately NOT filtered by visible(ctx): a shortcut bound to
// a Collector-only command must still show up and stay rebindable while
// looking at this page from Settings, or a person who left the Collector
// tab open weeks ago could never find it again. Hence a second, small
// aggregator here rather than exporting types.ts's own private array or
// widening useCommands() to take an "ignore visibility" flag it has no
// other caller for.
import { collectorCommands } from './collector';
import { downloadsCommands } from './downloads';
import { GLOBAL_COMMANDS } from './global';
import { languageCommands } from './language';
import { queueCommands } from './queue';
import { settingsCommands } from './settings';
import type { Command } from './types';

/** Every command this build declares, whichever surface it is filed under. */
export function allCommands(): Command[] {
  return [
    ...GLOBAL_COMMANDS,
    ...queueCommands,
    ...languageCommands,
    ...settingsCommands,
    ...downloadsCommands,
    ...collectorCommands,
  ];
}
