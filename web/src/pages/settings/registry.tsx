import type { ReactNode } from 'react';
import { Access } from './Access';
import { AccountsTab } from './Accounts';
import { Advanced } from './Advanced';
import { Appearance } from './Appearance';
import { Archives } from './Archives';
import { Automation } from './Automation';
import { BrowserTools } from './BrowserTools';
import { Captcha } from './Captcha';
import { CollectorSettings } from './CollectorSettings';
import { Diagnostics } from './Diagnostics';
import { DownloadsSettings } from './DownloadsSettings';
import { EmptyPage } from './Empty';
import { Health } from './Health';
import { Help } from './Help';
import { InstancesTab } from './Instances';
import { Look } from './Look';
import { Modules } from './Modules';
import { Network } from './Network';
import { Resolvers } from './Resolvers';
import { Rules } from './Rules';
import { Shortcuts } from './Shortcuts';
import { Torrents } from './Torrents';

/**
 * PAGES maps a sub-page id to the component that draws it. The set and order
 * of pages come from GET /api/features; a page without an entry draws the
 * registered-but-empty state.
 */
const PAGES: Record<string, () => ReactNode> = {
  modules: () => <Modules />,
  collector: () => <CollectorSettings />,
  downloads: () => <DownloadsSettings />,
  archives: () => <Archives />,
  look: () => <Look />,
  // The theming half of Look, as its own page.
  appearance: () => <Appearance />,
  access: () => <Access />,
  advanced: () => <Advanced />,
  rules: () => <Rules />,
  network: () => <Network />,
  resolvers: () => <Resolvers />,
  torrents: () => <Torrents />,
  captcha: () => <Captcha />,
  automation: () => <Automation />,
  // The settings tab renders the sidebar's own Konten page with one extra
  // toggle; the same holds for instances below.
  accounts: () => <AccountsTab />,
  instances: () => <InstancesTab />,
  health: () => <Health />,
  diagnostics: () => <Diagnostics />,
  help: () => <Help />,
  browsertools: () => <BrowserTools />,
  shortcuts: () => <Shortcuts />,
};

export { pageIcon } from './pageIcons';
export { pageId } from './folded';

export function renderSettingsPage(id: string): ReactNode {
  const page = PAGES[id];
  return page ? page() : <EmptyPage id={id} />;
}

/** hasContent tells whether a page has a component; the rail dims the others. */
export const hasContent = (id: string): boolean => id in PAGES;

export const FALLBACK_PAGE = 'downloads';
