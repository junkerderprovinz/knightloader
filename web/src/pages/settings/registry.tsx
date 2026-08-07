import type { ReactNode } from 'react';
import { Access } from './Access';
import { Advanced } from './Advanced';
import { Archives } from './Archives';
import { DownloadsSettings } from './DownloadsSettings';
import { EmptyPage } from './Empty';
import { General } from './General';
import { Look } from './Look';
import { Modules } from './Modules';

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
  // rules, connections, reconnect, accounts, captcha and schedule are
  // deliberately absent: they are registered on the server, they have working
  // addresses, and until their wave lands they render the registry's own reason.
};

export function renderSettingsPage(id: string): ReactNode {
  const page = PAGES[id];
  return page ? page() : <EmptyPage id={id} />;
}

/** Whether a page has a component, which is what the rail dims. */
export const hasContent = (id: string): boolean => id in PAGES;

/** The page shown when nothing else applies. It always exists. */
export const FALLBACK_PAGE = 'general';
