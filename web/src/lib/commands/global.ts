// The commands available everywhere: opening the palette and the event log,
// switching the theme, and a "go to X" for each main page, so the palette also
// works as a keyboard address bar.
import {
  IconAccounts,
  IconBell,
  IconCollector,
  IconDashboard,
  IconDownloads,
  IconInstances,
  IconMoon,
  IconSearch,
  IconSettings,
} from '../icons';
import { setCommandPaletteOpen } from '../commandPaletteOpen';
import { setEventsPanelOpen } from '../eventLog';
import { toggleTheme } from '../theme';
import type { Command, CommandSurface } from './types';

/** Exported so CommandPalette.tsx reads this command's shortcut instead of hard-coding "mod+k". */
export const OPEN_PALETTE_ID = 'app.commandPalette.open';

/** One "go to X" per surface with a real page, sharing the same run/enabled/visible shape. */
const NAV: { id: string; labelKey: Command['labelKey']; icon: Command['icon']; surface: CommandSurface; path: string; shortcut: string }[] = [
  { id: 'nav.goOverview', labelKey: 'commands.goOverview', icon: IconDashboard, surface: 'overview', path: '/', shortcut: 'mod+shift+1' },
  { id: 'nav.goDownloads', labelKey: 'commands.goDownloads', icon: IconDownloads, surface: 'downloads', path: '/downloads', shortcut: 'mod+shift+2' },
  { id: 'nav.goCollector', labelKey: 'commands.goCollector', icon: IconCollector, surface: 'collector', path: '/collector', shortcut: 'mod+shift+3' },
  { id: 'nav.goInstances', labelKey: 'commands.goInstances', icon: IconInstances, surface: 'instances', path: '/instances', shortcut: 'mod+shift+4' },
  { id: 'nav.goAccounts', labelKey: 'commands.goAccounts', icon: IconAccounts, surface: 'accounts', path: '/accounts', shortcut: 'mod+shift+5' },
  { id: 'nav.goSettings', labelKey: 'commands.goSettings', icon: IconSettings, surface: 'settings', path: '/settings', shortcut: 'mod+shift+6' },
];

export const GLOBAL_COMMANDS: Command[] = [
  {
    id: OPEN_PALETTE_ID,
    labelKey: 'commands.openPalette',
    icon: IconSearch,
    group: 'commands.group.general',
    surfaces: ['global'],
    defaultShortcut: 'mod+k',
    enabled: () => true,
    visible: () => true,
    // Opens rather than toggles: CommandPalette.tsx also listens for this
    // keystroke, and a toggle would let the second listener close what the
    // first opened.
    run: () => setCommandPaletteOpen(true),
  },
  {
    id: 'app.events.open',
    labelKey: 'commands.openEvents',
    icon: IconBell,
    group: 'commands.group.general',
    surfaces: ['global'],
    // mod+shift+e is free: mod+k, mod+a, mod+f and mod+u are taken, as are
    // mod+shift+ m/p/r/z/f/o/a/u/h/g/enter and 1-6, and alt+home/up/down/end.
    defaultShortcut: 'mod+shift+e',
    enabled: () => true,
    visible: () => true,
    // Opens rather than toggles, for the same reason as the palette above.
    run: () => setEventsPanelOpen(true),
  },
  {
    id: 'theme.toggle',
    labelKey: 'theme.toggle',
    icon: IconMoon,
    group: 'commands.group.general',
    surfaces: ['global'],
    defaultShortcut: 'mod+shift+m',
    enabled: () => true,
    visible: () => true,
    // toggleTheme notifies its subscribers, so the sidebar's sun/moon icon
    // follows.
    run: () => {
      toggleTheme();
    },
  },
  ...NAV.map(
    ({ id, labelKey, icon, surface, path, shortcut }): Command => ({
      id,
      labelKey,
      icon,
      group: 'commands.group.navigation',
      surfaces: ['global'],
      defaultShortcut: shortcut,
      enabled: () => true,
      // Hidden on its own page, where it would do nothing.
      visible: (ctx) => ctx.surface !== surface,
      run: (ctx) => ctx.navigate(path),
    }),
  ),
];
