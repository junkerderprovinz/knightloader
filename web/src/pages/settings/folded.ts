/**
 * FOLDED maps the id of a page whose cards are drawn on another page to that
 * page, so an address, a remembered page, a stored rail order or a rebound
 * shortcut naming it still lands on them. A module of its own, since the
 * shortcut dispatcher must not pull in every settings page with the registry.
 */
const FOLDED: Record<string, string> = {
  schedule: 'automation',
  eventtargets: 'automation',
  scripts: 'automation',
  connections: 'network',
  reconnect: 'network',
  categories: 'rules',
};

/** pageId returns the page a possibly folded id is drawn on. */
export const pageId = (id: string): string => FOLDED[id] ?? id;
