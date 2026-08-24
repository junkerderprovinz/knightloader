import type { ComponentType, ReactNode, SVGProps } from 'react';
import {
  IconAccounts,
  IconArchive,
  IconBrowser,
  IconCaptcha,
  IconClipboard,
  IconClock,
  IconCode,
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
  IconRetry,
  IconSliders,
} from '../../lib/icons';
import { Access } from './Access';
import { AccountsTab } from './Accounts';
import { Advanced } from './Advanced';
import { Archives } from './Archives';
import { BrowserTools } from './BrowserTools';
import { Captcha } from './Captcha';
import { Connections } from './Connections';
import { Diagnostics } from './Diagnostics';
import { DownloadsSettings } from './DownloadsSettings';
import { EmptyPage } from './Empty';
import { Help } from './Help';
import { Look } from './Look';
import { Modules } from './Modules';
import { Reconnect } from './Reconnect';
import { Resolvers } from './Resolvers';
import { Rules } from './Rules';
import { Schedule } from './Schedule';
import { Scripts } from './Scripts';
import { Shortcuts } from './Shortcuts';
import { Torrents } from './Torrents';

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
  reconnect: () => <Reconnect />,
  // yt-dlp's own format/subtitle/output-template configuration - the one
  // resolver with anything real to configure. See Resolvers.tsx's own doc
  // comment for why the routing order itself lives on the Accounts page
  // instead.
  resolvers: () => <Resolvers />,
  // Seed target, transfer limit, port + UPnP mapping, DHT/PEX - build-plan.md's
  // 11.5E. See Torrents.tsx's own doc comment.
  torrents: () => <Torrents />,
  // Wave 7 (7B) fills this line in - captcha settings: solver order and each
  // solver's own API key. See Captcha.tsx's own doc comment.
  captcha: () => <Captcha />,
  // Timetable editor - see Schedule.tsx's own doc comment for why it reads
  // and writes PUT /api/schedule directly rather than joining the shared
  // settings draft every other page here uses.
  schedule: () => <Schedule />,
  // The settings tab and the sidebar's "Konten" nav item render the exact
  // same page (Accounts.tsx here just adds a "hide the sidebar entry"
  // toggle on top of the shared pages/Accounts.tsx) — see that file's own
  // doc comment for why it stayed absent from this map for a while.
  accounts: () => <AccountsTab />,
  diagnostics: () => <Diagnostics />,
  help: () => <Help />,
  // The bookmarklet, the extension download and the PWA install step
  // (build-plan.md's 11D) — see BrowserTools.tsx's own doc comment.
  browsertools: () => <BrowserTools />,
  // The script editor (build-plan.md's 11B) — see Scripts.tsx's own doc
  // comment.
  scripts: () => <Scripts />,
  // Every command with a default keyboard shortcut, rebindable - build-plan.md's
  // Wave 12. See Shortcuts.tsx's own doc comment.
  shortcuts: () => <Shortcuts />,
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
  modules: IconModules,
  downloads: IconDownloads,
  archives: IconArchive,
  rules: IconFilter,
  connections: IconGlobe,
  reconnect: IconRetry,
  accounts: IconAccounts,
  // yt-dlp's format/subtitle/output-template config - a filled-in form, the
  // same idea IconClipboard already draws for "a template with fields".
  resolvers: IconClipboard,
  // Reused rather than a fresh glyph: IconInstances already draws "another
  // device on the network" (see its own use on Reconnect.tsx's UPnP method
  // tab), which is as close as the existing set gets to a peer-swarm idea,
  // and it is not spoken for anywhere else in this map.
  torrents: IconInstances,
  captcha: IconCaptcha,
  schedule: IconClock,
  look: IconLook,
  access: IconLock,
  advanced: IconSliders,
  diagnostics: IconDiagnostics,
  help: IconHelp,
  scripts: IconCode,
  shortcuts: IconKeyboard,
  browsertools: IconBrowser,
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
export const FALLBACK_PAGE = 'downloads';
