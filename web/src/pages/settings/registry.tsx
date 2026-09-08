import type { ComponentType, ReactNode, SVGProps } from 'react';
import {
  IconAccounts,
  IconArchive,
  IconBolt,
  IconBrowser,
  IconCaptcha,
  IconClipboard,
  IconClock,
  IconCode,
  IconDiagnostics,
  IconDownloads,
  IconExternalLink,
  IconFilter,
  IconFolder,
  IconGlobe,
  IconHelp,
  IconInstances,
  IconKeyboard,
  IconLock,
  IconLook,
  IconModules,
  IconRetry,
  IconSliders,
  IconUpload,
} from '../../lib/icons';
import { Access } from './Access';
import { AccountsTab } from './Accounts';
import { Advanced } from './Advanced';
import { Appearance } from './Appearance';
import { Archives } from './Archives';
import { BrowserTools } from './BrowserTools';
import { Captcha } from './Captcha';
import { Categories } from './Categories';
import { Connections } from './Connections';
import { Diagnostics } from './Diagnostics';
import { DownloadsSettings } from './DownloadsSettings';
import { EmptyPage } from './Empty';
import { EventTargets } from './EventTargets';
import { Health } from './Health';
import { Help } from './Help';
import { InstancesTab } from './Instances';
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
  // The theming half of the old General tab, as its own page (jdp,
  // 2026-09-07). Both are the same component with a different section.
  appearance: () => <Appearance />,
  access: () => <Access />,
  advanced: () => <Advanced />,
  rules: () => <Rules />,
  // The named drawers. Registered in the same commit as the server's own
  // featurePages() entry, which is the whole point of this map being one line
  // long: the connection manager once shipped as a finished 700-line page that
  // rendered "not built yet" because this line was missing.
  categories: () => <Categories />,
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
  // Same arrangement as accounts directly above, for the same request: the
  // settings tab and the sidebar's "Instanzen" nav item render one page, and
  // this file adds only the toggle that hides the nav item.
  instances: () => <InstancesTab />,
  // The live state of this instance, beside the diagnostics bundle because the
  // two are the operator's pair and are read in that order: this one answers
  // "is it working right now", that one hands over a file for a bug report
  // about why it was not. See Health.tsx's own doc comment.
  health: () => <Health />,
  diagnostics: () => <Diagnostics />,
  help: () => <Help />,
  // The bookmarklet, the extension download and the PWA install step
  // (build-plan.md's 11D) — see BrowserTools.tsx's own doc comment.
  browsertools: () => <BrowserTools />,
  // The script editor (build-plan.md's 11B) — see Scripts.tsx's own doc
  // comment.
  scripts: () => <Scripts />,
  // Where this instance reports to when something happens: operator-defined
  // HTTP targets riding the same event bus the script host listens on. See
  // EventTargets.tsx's own doc comment, especially for why it is not the
  // browser-side notifications card and must not be named like it.
  eventtargets: () => <EventTargets />,
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
  // A drawer is a folder with a name on it, and the folder glyph was not yet
  // claimed by any tab. The filter beside it is deliberately a different
  // drawing: a rule decides by looking at a link, a category is a label
  // somebody put on a whole batch, and the rail should not suggest they are
  // the same kind of thing.
  categories: IconFolder,
  connections: IconGlobe,
  reconnect: IconRetry,
  accounts: IconAccounts,
  // The sidebar's own Instanzen glyph, for the tab that renders the sidebar's
  // own Instanzen page - one idea, one drawing, and this is where that
  // drawing was always going. See torrents below for what that displaced.
  instances: IconInstances,
  // yt-dlp's format/subtitle/output-template config - a filled-in form, the
  // same idea IconClipboard already draws for "a template with fields".
  resolvers: IconClipboard,
  // Torrents borrowed IconInstances while nothing else in this map claimed
  // it ("as close as the existing set gets to a peer-swarm idea"). The
  // instances tab below is that glyph's home use, and two tabs in one rail
  // wearing the same drawing is the exact lie this map's own doc comment
  // warns about - so the loan came back. IconUpload in its place is not a
  // consolation prize: uploading is the one thing torrents do that no other
  // backend here does at all.
  torrents: IconUpload,
  captcha: IconCaptcha,
  schedule: IconClock,
  look: IconSliders,
  // The paint-roller-ish one for the tab that IS the look; General keeps a
  // sliders glyph, which is what a page of mixed preferences is.
  appearance: IconLook,
  access: IconLock,
  advanced: IconSliders,
  // A bolt for the tab that says whether the thing is alive, and the one glyph
  // in the set no tab had claimed. Deliberately not the diagnostics glyph next
  // to it: that one is a bundle you send somebody, this one is a pulse, and two
  // neighbouring tabs wearing one drawing is the lie this map's own doc comment
  // warns about.
  health: IconBolt,
  diagnostics: IconDiagnostics,
  help: IconHelp,
  scripts: IconCode,
  // A message leaving this machine for a server somebody else runs, which is
  // the one fact that separates this page from the browser-side
  // notifications card. Deliberately NOT the bell: that glyph belongs to
  // look/Notifications.tsx, and wearing it here would say the two are the
  // same thing, which is exactly the confusion this feature must not create.
  eventtargets: IconExternalLink,
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
 *
 * 22px, the size lib/icons.tsx already hands out by default and the size the
 * main sidebar's own glyphs are drawn at (jdp: "Die Glyphen und texte der
 * kacheln sind zu klein"). This rail stands directly beside that one; two
 * navigation columns side by side drawing the same kind of glyph at two
 * different sizes is the mismatch, not the absolute number.
 */
export function pageIcon(id: string): ReactNode {
  const Icon = ICONS[id];
  return Icon ? <Icon width={22} height={22} /> : undefined;
}

/** Whether a page has a component, which is what the tab bar dims. */
export const hasContent = (id: string): boolean => id in PAGES;

/** The page shown when nothing else applies. It always exists. */
export const FALLBACK_PAGE = 'downloads';
