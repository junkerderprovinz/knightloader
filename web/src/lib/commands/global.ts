// The always-visible commands: build-plan.md's Wave-1D note names this file
// as where "mod+k opens the palette" is registered, plus a plain "go to X"
// for each of the app's six main pages so the palette also works as a
// keyboard-driven address bar, not only a page-scoped action list - see
// lib/locales/en.ts's own "commands.go*" block, prepared for exactly these
// six entries.
//
// group is a real TranslationKey string, not literal English, per this
// codebase's own i18n rule and lib/commands/types.ts's own doc comment on
// Command.group.
import {
  IconAccounts,
  IconCollector,
  IconDashboard,
  IconDownloads,
  IconInstances,
  IconMoon,
  IconSearch,
  IconSettings,
} from '../icons';
import { setCommandPaletteOpen } from '../commandPaletteOpen';
import { toggleTheme } from '../theme';
import type { Command, CommandSurface } from './types';

/** Exported so CommandPalette.tsx can read this exact command's own defaultShortcut rather than a second, hardcoded "mod+k". */
export const OPEN_PALETTE_ID = 'app.commandPalette.open';

/** One "go to X" per surface with a real page, sharing the same run/enabled/visible shape. */
const NAV: { id: string; labelKey: Command['labelKey']; icon: Command['icon']; surface: CommandSurface; path: string }[] = [
  { id: 'nav.goOverview', labelKey: 'commands.goOverview', icon: IconDashboard, surface: 'overview', path: '/' },
  { id: 'nav.goDownloads', labelKey: 'commands.goDownloads', icon: IconDownloads, surface: 'downloads', path: '/downloads' },
  { id: 'nav.goCollector', labelKey: 'commands.goCollector', icon: IconCollector, surface: 'collector', path: '/collector' },
  { id: 'nav.goInstances', labelKey: 'commands.goInstances', icon: IconInstances, surface: 'instances', path: '/instances' },
  { id: 'nav.goAccounts', labelKey: 'commands.goAccounts', icon: IconAccounts, surface: 'accounts', path: '/accounts' },
  { id: 'nav.goSettings', labelKey: 'commands.goSettings', icon: IconSettings, surface: 'settings', path: '/settings' },
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
    // Sets the flag rather than toggling it: CommandPalette.tsx already has
    // to recognise this same shortcut itself before any page exists to run
    // this command from (see its own comment on why), and a future keyboard
    // dispatcher matching the identical keystroke a second time must be
    // harmless rather than closing what the first listener just opened -
    // "always open" is idempotent under two listeners, "toggle" is not.
    run: () => setCommandPaletteOpen(true),
  },
  {
    id: 'theme.toggle',
    // Reuses the existing theme.toggle key (lib/locales/en.ts, already
    // shipped for Sidebar.tsx's own button) rather than a near-duplicate.
    labelKey: 'theme.toggle',
    icon: IconMoon,
    group: 'commands.group.general',
    surfaces: ['global'],
    enabled: () => true,
    visible: () => true,
    // toggleTheme() itself notifies every onThemeChange() subscriber
    // (lib/theme.ts) — Sidebar.tsx's own sun/moon icon included — so firing
    // this from the keyboard, with no click anywhere near that button,
    // still leaves it showing the theme that is actually active.
    run: () => {
      toggleTheme();
    },
  },
  ...NAV.map(
    ({ id, labelKey, icon, surface, path }): Command => ({
      id,
      labelKey,
      icon,
      group: 'commands.group.navigation',
      surfaces: ['global'],
      enabled: () => true,
      // Left out of its own destination's list: jumping to the page already
      // open is a no-op with nothing to show for it, the same reason a
      // sidebar never highlights a second "you are here" link beside the one
      // it already draws for the active route.
      visible: (ctx) => ctx.surface !== surface,
      run: (ctx) => ctx.navigate(path),
    }),
  ),
];
