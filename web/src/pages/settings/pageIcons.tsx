import type { ComponentType, ReactNode, SVGProps } from 'react';
import {
  IconAccounts,
  IconArchive,
  IconBolt,
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
  IconTabAdvanced,
  IconTabApp,
  IconTabGeneral,
  IconUpload,
} from '../../lib/icons';

// Kept out of registry.tsx, which imports every page, so a page can draw another
// page's glyph without an import cycle.

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
  // General, Look, App, Remote access and Advanced wear the glyphs GlimStone
  // gives those tabs in every app; the cog is Settings itself and no tab in it.
  look: IconTabGeneral,
  appearance: IconLook,
  access: IconLock,
  advanced: IconTabAdvanced,
  // A pulse, apart from the diagnostics bundle next to it.
  health: IconBolt,
  diagnostics: IconDiagnostics,
  help: IconHelp,
  shortcuts: IconKeyboard,
  browsertools: IconTabApp,
};

/** pageGlyph is the page's glyph unsized, for a badge that sizes its own. */
export function pageGlyph(id: string): ComponentType<SVGProps<SVGSVGElement>> | undefined {
  return ICONS[id];
}

/**
 * pageIcon returns the page's glyph at 22px, the size of the main sidebar's
 * glyphs beside it, or undefined for a page without one.
 */
export function pageIcon(id: string): ReactNode {
  const Icon = ICONS[id];
  return Icon ? <Icon width={22} height={22} /> : undefined;
}
