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
  // The captcha accounts' order and when they start stand beside their keys.
  captcha: 'accounts',
  // The log, the start report and the bug report stand with the health of the parts.
  diagnostics: 'health',
  // The debrid drive is a card beside the VPN and MCP ones.
  debriddrive: 'integrations',
  // Each module's switch is on the page of what it switches; General stands
  // in for the list of them that was here.
  modules: 'look',
};

/** pageId returns the page a possibly folded id is drawn on. */
export const pageId = (id: string): string => FOLDED[id] ?? id;
