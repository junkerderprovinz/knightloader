import { langPickerOpen, setLangPickerOpen } from '../langPickerOpen';
import type { Command } from './types';

/**
 * Opens/closes the sidebar's language dropdown (components/LanguagePicker.tsx).
 * Its `open` flag now lives in lib/langPickerOpen.ts precisely so a global
 * command can reach it (see that file's own doc comment) - `run` below calls
 * the same setLangPickerOpen() the picker's own click-outside and Escape
 * handling already call, never a second copy of "close the menu".
 *
 * langPickerOpen() is read directly (not through CommandContext, which has
 * no field for it) because it is a plain module-level getter, not a hook -
 * calling it from `visible` is exactly what a synchronous, non-hook getter
 * is for.
 */
export const languageCommands: Command[] = [
  {
    id: 'shell.openLanguagePicker',
    labelKey: 'lang.open',
    group: 'commands.group.language',
    surfaces: ['global'],
    enabled: () => true,
    visible: () => !langPickerOpen(),
    run: () => setLangPickerOpen(true),
  },
  {
    id: 'shell.closeLanguagePicker',
    labelKey: 'lang.close',
    group: 'commands.group.language',
    surfaces: ['global'],
    enabled: () => true,
    visible: () => langPickerOpen(),
    run: () => setLangPickerOpen(false),
  },
];
