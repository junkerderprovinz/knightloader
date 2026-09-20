import { fetchDeploymentInfo, fetchUpdateCheck } from '../api';
import { IconRetry, IconSearch } from '../icons';
import { requestSearchFocus } from '../../pages/settings/jump';
import type { Command } from './types';

/**
 * One command per settings page, so the palette can jump straight to
 * "Torrents". The ids and their order match registry.tsx's PAGES, which
 * check-settings-pages.mjs enforces. Labels reuse each page's
 * settings.nav.<id> key.
 */
const SETTINGS_PAGES: { id: string; labelKey: Command['labelKey'] }[] = [
  { id: 'modules', labelKey: 'settings.nav.modules' },
  { id: 'downloads', labelKey: 'settings.nav.downloads' },
  { id: 'archives', labelKey: 'settings.nav.archives' },
  { id: 'look', labelKey: 'settings.nav.look' },
  { id: 'appearance', labelKey: 'settings.nav.appearance' },
  { id: 'accounts', labelKey: 'settings.nav.accounts' },
  { id: 'instances', labelKey: 'settings.nav.instances' },
  { id: 'access', labelKey: 'settings.nav.access' },
  { id: 'advanced', labelKey: 'settings.nav.advanced' },
  { id: 'rules', labelKey: 'settings.nav.rules' },
  { id: 'categories', labelKey: 'settings.nav.categories' },
  { id: 'connections', labelKey: 'settings.nav.connections' },
  { id: 'reconnect', labelKey: 'settings.nav.reconnect' },
  { id: 'resolvers', labelKey: 'settings.nav.resolvers' },
  { id: 'torrents', labelKey: 'settings.nav.torrents' },
  { id: 'captcha', labelKey: 'settings.nav.captcha' },
  { id: 'schedule', labelKey: 'settings.nav.schedule' },
  { id: 'health', labelKey: 'settings.nav.health' },
  { id: 'diagnostics', labelKey: 'settings.nav.diagnostics' },
  { id: 'help', labelKey: 'settings.nav.help' },
  { id: 'browsertools', labelKey: 'settings.nav.browsertools' },
  { id: 'scripts', labelKey: 'settings.nav.scripts' },
  { id: 'eventtargets', labelKey: 'settings.nav.eventtargets' },
  { id: 'shortcuts', labelKey: 'settings.nav.shortcuts' },
];

export const settingsCommands: Command[] = [
  ...SETTINGS_PAGES.map(
    ({ id, labelKey }): Command => ({
      id: `settings.open.${id}`,
      labelKey,
      group: 'commands.group.settings',
      // Global: the jump is worth having from other pages, not from settings,
      // where the rail is just as quick.
      surfaces: ['global'],
      enabled: () => true,
      visible: () => true,
      run: (ctx) => ctx.navigate(`/settings/${id}`),
    }),
  ),
  {
    // Not a page, so it stays out of SETTINGS_PAGES and the parity check.
    // The search field is not mounted yet, so focus is requested through the
    // same module-level store the jump uses.
    id: 'settings.search',
    labelKey: 'settings.search.command',
    icon: IconSearch,
    group: 'commands.group.settings',
    surfaces: ['global'],
    enabled: () => true,
    visible: () => true,
    run: (ctx) => {
      ctx.navigate('/settings');
      requestSearchFocus();
    },
  },
  {
    id: 'settings.checkForUpdates',
    labelKey: 'settings.look.updatesCheck',
    icon: IconRetry,
    group: 'commands.group.settings',
    surfaces: ['global'],
    // JDownloader 2's own shortcut for checking for updates.
    defaultShortcut: 'mod+u',
    enabled: () => true,
    visible: () => true,
    // The same two calls as Look.tsx's UpdateCard, reported as a toast since
    // no page is mounted to hold the result.
    run: async (ctx) => {
      try {
        const [deploy, check] = await Promise.all([fetchDeploymentInfo(), fetchUpdateCheck()]);
        if (!check.checked) {
          ctx.toast(ctx.t('settings.look.updatesFailed'), 'fail');
        } else if (check.available) {
          const key = deploy.deployment === 'desktop' ? 'settings.look.updatesAvailable' : 'settings.look.updatesAvailableContainer';
          ctx.toast(ctx.t(key, { version: check.latest ?? '' }), 'info');
        } else {
          ctx.toast(ctx.t('settings.look.updatesCurrent', { version: check.current }), 'ok');
        }
      } catch {
        ctx.toast(ctx.t('settings.look.updatesFailed'), 'fail');
      }
    },
  },
];
