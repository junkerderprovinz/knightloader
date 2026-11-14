import type { SiteLogin } from './api';
import type { TranslationKey } from './i18n';

/** What a site's row and window say of its sign-in, and whether as a warning. */
export interface StateNote {
  key: TranslationKey;
  vars?: Record<string, string>;
  warn: boolean;
}

/**
 * stateNote words a site's state. A session the site did not answer about is
 * still used, so it reads as signed in.
 */
export function stateNote(site: SiteLogin): StateNote {
  if (site.waiting) return { key: 'siteLogins.waiting', warn: false };
  switch (site.state) {
    case 'signedIn':
    case 'unchecked':
      return site.account
        ? { key: 'siteLogins.signedInAs', vars: { account: site.account }, warn: false }
        : { key: 'siteLogins.signedIn', warn: false };
    case 'expired':
      return { key: 'siteLogins.expired', warn: true };
    default:
      return { key: 'siteLogins.signedOut', warn: false };
  }
}

/** siteForHost is the site a bare hostname belongs to, by its own domains or the hosts its links come from. */
export function siteForHost(host: string, sites: SiteLogin[]): SiteLogin | undefined {
  const h = host.toLowerCase();
  if (!h) return undefined;
  return sites.find((s) => s.domains.some((d) => h === d || h.endsWith(`.${d}`)) || (s.links ?? []).includes(h));
}

/**
 * signInAgainFor is the site a failed download should be signed in to again:
 * a members-only answer from a site that has a session stored, which most
 * likely ran out. Without a stored session the plain advice stands.
 */
export function signInAgainFor(reason: string | undefined, host: string, sites: SiteLogin[]): SiteLogin | undefined {
  if (reason !== 'membersOnly') return undefined;
  const site = siteForHost(host, sites);
  return site && site.state !== 'signedOut' ? site : undefined;
}
