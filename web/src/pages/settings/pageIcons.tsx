import type { ComponentType, ReactNode, SVGProps } from 'react';
import {
  IconAccounts,
  IconArchive,
  IconCaptcha,
  IconCollector,
  IconDiagnostics,
  IconDownload,
  IconFilter,
  IconFleet,
  IconHealth,
  IconHelp,
  IconKeyboard,
  IconLink,
  IconModules,
  IconNetwork,
  IconResolvers,
  IconSchedules,
  IconTabAdvanced,
  IconTabApp,
  IconTabGeneral,
  IconTabLook,
  IconTabSecurity,
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
  downloads: IconDownload,
  archives: IconArchive,
  rules: IconFilter,
  network: IconNetwork,
  accounts: IconAccounts,
  instances: IconFleet,
  resolvers: IconResolvers,
  // Uploading is what torrents do that no other backend here does.
  torrents: IconUpload,
  captcha: IconCaptcha,
  automation: IconSchedules,
  // General, Look, App, Security and Advanced wear the glyphs GlimStone
  // gives those tabs in every app; the cog is Settings itself and no tab in it.
  look: IconTabGeneral,
  appearance: IconTabLook,
  pairing: IconLink,
  access: IconTabSecurity,
  advanced: IconTabAdvanced,
  health: IconHealth,
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
