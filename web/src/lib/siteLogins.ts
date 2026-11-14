import type { SiteLogin } from './api';
import type { TranslationKey } from './i18n';

/** What a site's row and window say of its sign-in, and whether as a warning. */
export interface StateNote {
  key: TranslationKey;
  vars?: Record<string, string>;
  warn: boolean;
}

/** Whether a session is there and used. One the site did not answer about is still used. */
export const signedIn = (site: SiteLogin) => site.state === 'signedIn' || site.state === 'unchecked';

/** stateNote words a site's state. */
export function stateNote(site: SiteLogin): StateNote {
  if (signedIn(site)) {
    return site.account
      ? { key: 'siteLogins.signedInAs', vars: { account: site.account }, warn: false }
      : { key: 'siteLogins.signedIn', warn: false };
  }
  if (site.state === 'expired') return { key: 'siteLogins.expired', warn: true };
  return { key: 'siteLogins.signedOut', warn: false };
}

/**
 * sourceNote says where a session came from, when that is known, and which
 * browser's session the site turned down.
 */
export function sourceNote(site: SiteLogin): StateNote | undefined {
  if (site.state === 'expired' && site.source === 'browser' && site.browser) {
    return { key: 'siteLogins.turnedDown', vars: { site: site.name, browser: site.browser }, warn: false };
  }
  if (!signedIn(site)) return undefined;
  if (site.source === 'browser' && site.browser) return { key: 'siteLogins.fromBrowser', vars: { browser: site.browser }, warn: false };
  if (site.source === 'extension') return { key: 'siteLogins.fromExtension', warn: false };
  if (site.source === 'kl-browser') return { key: 'siteLogins.fromKlBrowser', warn: false };
  return undefined;
}

/**
 * Where a sign-in can come from: an instance outside a container reads the
 * browsers on its computer, and anywhere else the extension passes it on.
 */
export interface SignInWays {
  readsBrowsers: boolean;
  extensionPaired: boolean;
}

/**
 * readsBrowsers tells an instance that reads the browsers on its computer by
 * what that left behind: a session read from one, or a browser it could not
 * read. One that found neither is taken for a container.
 */
export function readsBrowsers(sites: SiteLogin[]): boolean {
  return sites.some((s) => s.source === 'browser' || s.unreadable.length > 0);
}

/**
 * nextStep is the one thing that signs a site in, for a site without a
 * working session. A container with no extension in its group has no browser
 * to pick a sign-in up from, so the step names the extension.
 */
export function nextStep(site: SiteLogin, ways: SignInWays): StateNote | undefined {
  if (signedIn(site)) return undefined;
  const key = ways.readsBrowsers || ways.extensionPaired ? 'siteLogins.signInHere' : 'siteLogins.signInExtension';
  return { key, vars: { site: site.name }, warn: false };
}

/** unreadableBrowsers is every browser any site could not be read from, once each. */
export function unreadableBrowsers(sites: SiteLogin[]): string[] {
  return [...new Set(sites.flatMap((s) => s.unreadable))];
}

/** siteForHost is the site a bare hostname belongs to, by its own domains or the hosts its links come from. */
export function siteForHost(host: string, sites: SiteLogin[]): SiteLogin | undefined {
  const h = host.toLowerCase();
  if (!h) return undefined;
  return sites.find((s) => s.domains.some((d) => h === d || h.endsWith(`.${d}`)) || (s.links ?? []).includes(h));
}

/**
 * signInFor is the site a failed download should be signed in to: a bot
 * check, a members-only answer or a video for signed-in accounts only, from a
 * site KnightLoader borrows a sign-in for. `again` is a site that has a
 * session, which most likely ran out.
 */
export function signInFor(reason: string | undefined, host: string, sites: SiteLogin[]): { site: SiteLogin; again: boolean } | undefined {
  if (reason !== 'membersOnly' && reason !== 'botCheck' && reason !== 'signInRequired') return undefined;
  const site = siteForHost(host, sites);
  return site && { site, again: site.state !== 'signedOut' };
}
