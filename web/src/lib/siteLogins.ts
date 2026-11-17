import { useEffect, useState } from 'react';
import { type SiteLogin, fetchConnect } from './api';
import { appAddress } from './basePath';
import type { TranslationKey } from './i18n';

/** What a site's row and window say of its sign-in. */
export interface StateNote {
  key: TranslationKey;
  vars?: Record<string, string>;
}

/** Whether a session is there and used. One the site did not answer about is still used. */
export const signedIn = (site: SiteLogin) => site.state === 'signedIn' || site.state === 'unchecked';

/** stateNote words a site's state. */
export function stateNote(site: SiteLogin): StateNote {
  if (signedIn(site)) {
    return site.account
      ? { key: 'siteLogins.signedInAs', vars: { account: site.account } }
      : { key: 'siteLogins.signedIn' };
  }
  if (site.state === 'expired') return { key: 'siteLogins.expired' };
  return { key: 'siteLogins.signedOut' };
}

/**
 * sourceNote says where a session came from, when that is known, and which
 * browser's session the site turned down.
 */
export function sourceNote(site: SiteLogin): StateNote | undefined {
  if (site.disconnected && !signedIn(site)) return { key: 'siteLogins.disconnected' };
  if (site.state === 'expired' && site.source === 'browser' && site.browser) {
    return { key: 'siteLogins.turnedDown', vars: { site: site.name, browser: site.browser } };
  }
  if (!signedIn(site)) return undefined;
  if (site.source === 'browser' && site.browser) return { key: 'siteLogins.fromBrowser', vars: { browser: site.browser } };
  if (site.source === 'extension') return { key: 'siteLogins.fromExtension' };
  if (site.source === 'kl-browser') return { key: 'siteLogins.fromKlBrowser' };
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

/** readsBrowsers is whether the instance reads the browsers on its computer, as its sites say. */
export function readsBrowsers(sites: SiteLogin[]): boolean {
  return sites.some((s) => s.readsBrowsers);
}

/**
 * useSignInWays says where this instance gets a sign-in from: outside a
 * container it reads the browsers on its computer, and a container needs a
 * browser of its group that has the extension.
 */
export function useSignInWays(sites: SiteLogin[], enabled = true): SignInWays {
  const [extensionPaired, setExtensionPaired] = useState(false);
  useEffect(() => {
    if (!enabled) return;
    void fetchConnect().then(
      (c) => setExtensionPaired((c.apps ?? []).some((a) => a.deployment === 'extension' && a.approved)),
      () => {},
    );
  }, [enabled]);
  return { readsBrowsers: readsBrowsers(sites), extensionPaired };
}

/**
 * picksUpFromBrowser is whether a sign-in to the site in the user's own
 * browser reaches this instance: it reads the browsers on its computer, or an
 * extension of its group may read the site.
 */
export function picksUpFromBrowser(site: SiteLogin, ways: SignInWays): boolean {
  return ways.readsBrowsers || !!site.handsOver;
}

/**
 * nextStep is the one thing that signs a site in, for a site without a
 * working session. A container with no extension in its group has no browser
 * to pick a sign-in up from, so the step names the extension, and one whose
 * extension may not read the site says to allow it.
 */
export function nextStep(site: SiteLogin, ways: SignInWays): StateNote | undefined {
  // A removed site is not picked up from the browser, so its window says how.
  if (signedIn(site) || site.disconnected) return undefined;
  const key = picksUpFromBrowser(site, ways)
    ? 'siteLogins.signInHere'
    : ways.extensionPaired
      ? 'siteLogins.signInAllow'
      : 'siteLogins.signInExtension';
  return { key, vars: { site: site.name } };
}

/**
 * confirmInTab turns the tab a site was opened in into KnightLoader's word
 * that it has the sign-in and the tab can go, as a site that signs an app in
 * says it. A tab the user closed is left alone.
 */
export function confirmInTab(tab: Window | null, site: string): void {
  if (tab && !tab.closed) tab.location.replace(`${appAddress()}/signedin?site=${encodeURIComponent(site)}`);
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
