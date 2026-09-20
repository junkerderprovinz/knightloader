import { langPickerOpen, setLangPickerOpen } from '../langPickerOpen';
import type { Command } from './types';

/** Opens and closes the sidebar's language dropdown through the module-level
 *  flag in lib/langPickerOpen.ts, which the picker itself also uses. */
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
