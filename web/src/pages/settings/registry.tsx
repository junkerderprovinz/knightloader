import type { ComponentType, ReactNode, SVGProps } from 'react';
import {
  IconAccounts,
  IconArchive,
  IconCaptcha,
  IconClock,
  IconDownloads,
  IconFilter,
  IconGlobe,
  IconLock,
  IconLook,
  IconModules,
  IconRetry,
  IconSettings,
  IconSliders,
} from '../../lib/icons';
import { Access } from './Access';
import { Advanced } from './Advanced';
import { Archives } from './Archives';
import { Connections } from './Connections';
import { DownloadsSettings } from './DownloadsSettings';
import { EmptyPage } from './Empty';
import { General } from './General';
import { Look } from './Look';
import { Modules } from './Modules';
import { Rules } from './Rules';

/**
 * Which component renders which sub-page.
 *
 * The ORDER and the SET of pages are not here — they come from
 * GET /api/features, so the rail, the modules page and the self-describing index
 * all read one list. This map only answers "and what draws it", and a page with
 * no entry draws the registered-but-empty state. That is the whole seam a later
 * wave uses: add a file, add one line here, and the page it was already promised
 * a route for starts rendering.
 */
const PAGES: Record<string, () => ReactNode> = {
  general: () => <General />,
  modules: () => <Modules />,
  downloads: () => <DownloadsSettings />,
  archives: () => <Archives />,
  look: () => <Look />,
  access: () => <Access />,
  advanced: () => <Advanced />,
  rules: () => <Rules />,
  // The connection manager shipped with the settings shell and never got its
  // line here, so a finished 700-line page rendered "not built yet" at an
  // address the modules list was already pointing people at. That is exactly
  // the failure this map is one line long to prevent.
  connections: () => <Connections />,
  // reconnect, accounts, captcha and schedule are deliberately absent: they are
  // registered on the server, they have working addresses, and until their wave
  // lands they render the registry's own reason.
};

/**
 * The glyph beside a page's name in the tab bar.
 *
 * It lives here rather than in the tab bar because it is the same kind of fact
 * as the component: something only this side knows about a page the server
 * named. A page a later wave registers gets its label from the locale and no
 * icon at all until it is listed here — an unlabelled tab is a gap, a tab
 * wearing somebody else's glyph is a lie.
 *
 * Five of the thirteen reuse a glyph the app already draws elsewhere for the
 * same idea (the sidebar's Downloads and Accounts, the connection row's globe,
 * the task list's retry arrow, the gear). One idea, one drawing — the tab bar is
 * not a place to invent a second downloads icon.
 */
const ICONS: Record<string, ComponentType<SVGProps<SVGSVGElement>>> = {
  general: IconSettings,
  modules: IconModules,
  downloads: IconDownloads,
  archives: IconArchive,
  rules: IconFilter,
  connections: IconGlobe,
  reconnect: IconRetry,
  accounts: IconAccounts,
  captcha: IconCaptcha,
  schedule: IconClock,
  look: IconLook,
  access: IconLock,
  advanced: IconSliders,
};

export function renderSettingsPage(id: string): ReactNode {
  const page = PAGES[id];
  return page ? page() : <EmptyPage id={id} />;
}

/**
 * The page's glyph at the one size the tab bar uses. Undefined for a page this
 * side has not met yet, which `Tabs` renders as a label on its own.
 */
export function pageIcon(id: string): ReactNode {
  const Icon = ICONS[id];
  return Icon ? <Icon width={16} height={16} /> : undefined;
}

/** Whether a page has a component, which is what the tab bar dims. */
export const hasContent = (id: string): boolean => id in PAGES;

/** The page shown when nothing else applies. It always exists. */
export const FALLBACK_PAGE = 'general';
