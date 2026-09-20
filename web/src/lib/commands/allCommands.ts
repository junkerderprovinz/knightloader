// Every command, unfiltered, for Settings/Shortcuts.tsx: a shortcut on a
// Collector-only command has to stay rebindable from the settings page, where
// useCommands would filter it out.
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
