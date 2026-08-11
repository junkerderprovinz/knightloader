import type { Command } from './types';

/**
 * One command per settings sub-page, so the palette can jump straight to
 * e.g. "Settings: Torrents" instead of a click through Settings' own rail.
 *
 * The id list and its order mirror registry.tsx's PAGES map exactly - same
 * set, same reasoning: 'accounts' is left out because registry.tsx leaves it
 * out too (its own comment there - registered on the server, no component
 * here yet), and a command that navigated to it would land on the "not
 * built yet" placeholder every other entry point already avoids.
 *
 * labelKey reuses each page's existing settings.nav.<id> key (tx.ts's own
 * `label()` helper, Tabs.tsx) rather than minting a second "Settings: X"
 * string per page - one English name per page, not two that can drift.
 *
 * run() calls the exact navigate() every page already gets from
 * react-router's useNavigate(), pointed at the exact path Settings.tsx's own
 * pagePath() builds (`/settings/${id}`) - never a re-derivation of that
 * route.
 */
const SETTINGS_PAGES: { id: string; labelKey: Command['labelKey'] }[] = [
  { id: 'general', labelKey: 'settings.nav.general' },
  { id: 'modules', labelKey: 'settings.nav.modules' },
  { id: 'downloads', labelKey: 'settings.nav.downloads' },
  { id: 'archives', labelKey: 'settings.nav.archives' },
  { id: 'look', labelKey: 'settings.nav.look' },
  { id: 'access', labelKey: 'settings.nav.access' },
  { id: 'advanced', labelKey: 'settings.nav.advanced' },
  { id: 'rules', labelKey: 'settings.nav.rules' },
  { id: 'connections', labelKey: 'settings.nav.connections' },
  { id: 'reconnect', labelKey: 'settings.nav.reconnect' },
  { id: 'resolvers', labelKey: 'settings.nav.resolvers' },
  { id: 'torrents', labelKey: 'settings.nav.torrents' },
  { id: 'captcha', labelKey: 'settings.nav.captcha' },
  { id: 'schedule', labelKey: 'settings.nav.schedule' },
  { id: 'diagnostics', labelKey: 'settings.nav.diagnostics' },
  { id: 'system', labelKey: 'settings.nav.system' },
  { id: 'help', labelKey: 'settings.nav.help' },
  { id: 'browsertools', labelKey: 'settings.nav.browsertools' },
  { id: 'scripts', labelKey: 'settings.nav.scripts' },
  { id: 'shortcuts', labelKey: 'settings.nav.shortcuts' },
];

export const settingsCommands: Command[] = SETTINGS_PAGES.map(({ id, labelKey }) => ({
  id: `settings.open.${id}`,
  labelKey,
  group: 'commands.group.settings',
  // 'global', not 'settings' - the whole point named in this file's own doc
  // comment above ("jump straight to Settings: Torrents instead of a click
  // through Settings' own rail") requires being reachable from EVERY page,
  // the same way global.ts's own six "go to X" nav commands are. Scoped to
  // 'settings' this list would only ever appear once already on a settings
  // page, where clicking the rail tab is no slower than the palette - the
  // one place these commands are actually worth pressing mod+k for is
  // everywhere else, which is exactly what 'settings' excluded.
  surfaces: ['global'],
  enabled: () => true,
  visible: () => true,
  run: (ctx) => ctx.navigate(`/settings/${id}`),
}));
