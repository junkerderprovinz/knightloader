import type { ReactNode } from 'react';
import { Access } from './Access';
import { AccountsTab } from './Accounts';
import { Advanced } from './Advanced';
import { Appearance } from './Appearance';
import { Archives } from './Archives';
import { Automation } from './Automation';
import { BrowserTools } from './BrowserTools';
import { CollectorSettings } from './CollectorSettings';
import { DownloadsSettings } from './DownloadsSettings';
import { EmptyPage } from './Empty';
import { Health } from './Health';
import { Help } from './Help';
import { InstancesTab } from './Instances';
import { Integrations } from './Integrations';
import { Look } from './Look';
import { Network } from './Network';
import { Pairing } from './Pairing';
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
  collector: () => <CollectorSettings />,
  downloads: () => <DownloadsSettings />,
  archives: () => <Archives />,
  look: () => <Look />,
  // The theming half of Look, as its own page.
  appearance: () => <Appearance />,
  pairing: () => <Pairing />,
  access: () => <Access />,
  advanced: () => <Advanced />,
  rules: () => <Rules />,
  network: () => <Network />,
  resolvers: () => <Resolvers />,
  torrents: () => <Torrents />,
  automation: () => <Automation />,
  // The sidebar's own Konten and Instanzen pages, the latter under its
  // module's switch.
  accounts: () => <AccountsTab />,
  integrations: () => <Integrations />,
  instances: () => <InstancesTab />,
  health: () => <Health />,
  help: () => <Help />,
  browsertools: () => <BrowserTools />,
  shortcuts: () => <Shortcuts />,
};

export { pageIcon } from './pageIcons';
export { pageId } from './folded';

export function renderSettingsPage(id: string): ReactNode {
  return hasContent(id) ? PAGES[id]() : <EmptyPage id={id} />;
}

/**
 * hasContent tells whether a page has a component; the rail dims the others.
 * The id comes from the address bar, and "constructor" is in every object.
 */
export const hasContent = (id: string): boolean => Object.prototype.hasOwnProperty.call(PAGES, id);

export const FALLBACK_PAGE = 'downloads';
