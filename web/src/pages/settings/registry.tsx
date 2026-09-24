import type { ComponentType, ReactNode, SVGProps } from 'react';
import {
  IconAccounts,
  IconArchive,
  IconBolt,
  IconBrowser,
  IconCaptcha,
  IconClipboard,
  IconClock,
  IconCollector,
  IconDiagnostics,
  IconDownloads,
  IconFilter,
  IconGlobe,
  IconHelp,
  IconInstances,
  IconKeyboard,
  IconLock,
  IconLook,
  IconModules,
  IconSliders,
  IconUpload,
} from '../../lib/icons';
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

/**
 * ICONS holds the glyph beside a page's name in the rail. A page missing here
 * gets no icon rather than another page's glyph, and a glyph the app already
 * uses for the same idea elsewhere is reused.
 */
const ICONS: Record<string, ComponentType<SVGProps<SVGSVGElement>>> = {
  modules: IconModules,
  collector: IconCollector,
  downloads: IconDownloads,
  archives: IconArchive,
  rules: IconFilter,
  network: IconGlobe,
  accounts: IconAccounts,
  instances: IconInstances,
  // A template with fields, like IconClipboard elsewhere.
  resolvers: IconClipboard,
  // Uploading is what torrents do that no other backend here does.
  torrents: IconUpload,
  captcha: IconCaptcha,
  automation: IconClock,
  look: IconSliders,
  appearance: IconLook,
  access: IconLock,
  advanced: IconSliders,
  // A pulse, apart from the diagnostics bundle next to it.
  health: IconBolt,
  diagnostics: IconDiagnostics,
  help: IconHelp,
  shortcuts: IconKeyboard,
  browsertools: IconBrowser,
};

export { pageId } from './folded';

export function renderSettingsPage(id: string): ReactNode {
  const page = PAGES[id];
  return page ? page() : <EmptyPage id={id} />;
}

/**
 * pageIcon returns the page's glyph at 22px, the size of the main sidebar's
 * glyphs beside it, or undefined for a page without one.
 */
export function pageIcon(id: string): ReactNode {
  const Icon = ICONS[id];
  return Icon ? <Icon width={22} height={22} /> : undefined;
}

/** hasContent tells whether a page has a component; the rail dims the others. */
export const hasContent = (id: string): boolean => id in PAGES;

export const FALLBACK_PAGE = 'downloads';
