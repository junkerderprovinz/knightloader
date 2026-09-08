import { fetchDeploymentInfo, fetchUpdateCheck } from '../api';
import { IconRetry, IconSearch } from '../icons';
import { requestSearchFocus } from '../../pages/settings/jump';
import type { Command } from './types';

/**
 * One command per settings sub-page, so the palette can jump straight to
 * e.g. "Settings: Torrents" instead of a click through Settings' own rail.
 *
 * The id list and its order mirror registry.tsx's PAGES map exactly.
 *
 * "Exactly" is load-bearing and was not true for a while: accounts, instances
 * and appearance all grew real components in registry.tsx and none of them was
 * added here, so three settings pages were simply unreachable from the palette
 * while this comment still explained why one of them had no component. A list
 * that has to be kept in step by hand drifts, and the drift is invisible
 * because a missing command looks exactly like a command nobody typed - hence
 * web/check-settings-pages.mjs, which fails when the two sets part company.
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
  { id: 'diagnostics', labelKey: 'settings.nav.diagnostics' },
  { id: 'help', labelKey: 'settings.nav.help' },
  { id: 'browsertools', labelKey: 'settings.nav.browsertools' },
  { id: 'scripts', labelKey: 'settings.nav.scripts' },
  { id: 'shortcuts', labelKey: 'settings.nav.shortcuts' },
];

export const settingsCommands: Command[] = [
  ...SETTINGS_PAGES.map(
    ({ id, labelKey }): Command => ({
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
    }),
  ),
  {
    // Not a page entry, so it is not in SETTINGS_PAGES above and
    // check-settings-pages.mjs's parity check (which reads `{ id: '<id>'` out of
    // that array) is untouched by it.
    //
    // It navigates AND asks for focus, in that order, because those are two
    // different things and only one of them can be done from here: the field is
    // mounted three routes away and this function has no reference to it, so the
    // ask goes through the same module-scope store the jump itself uses. Landing
    // on Settings with the cursor already in the box is the whole point - a
    // command that only navigated would leave somebody who typed "search
    // settings" one more click away from searching settings.
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
    // Real JDownloader 2's own mod+U ("check for updates") — source:
    // jd/gui/swing/jdgui/components/toolbar/actions/UpdateAction.java,
    // `setAccelerator(KeyEvent.VK_U)` (the single-arg AppAction overload,
    // which itself adds the platform menu-shortcut mask).
    defaultShortcut: 'mod+u',
    enabled: () => true,
    visible: () => true,
    // Exactly the two calls settings/Look.tsx's own UpdateCard makes
    // (fetchDeploymentInfo to pick the desktop/container wording,
    // fetchUpdateCheck for the result), reported as a toast instead of
    // UpdateCard's own inline state - this command has no page mounted to
    // hold that state in, and the page itself still shows the full card
    // for anyone who wants the persistent "Check for updates" button and
    // the clickable release link.
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
