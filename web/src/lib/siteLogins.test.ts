import { describe, expect, it } from 'vitest';
import type { SiteLogin } from './api';
import { siteForHost, signInAgainFor, stateNote } from './siteLogins';

const site = (over: Partial<SiteLogin>): SiteLogin => ({
  id: 'patreon',
  name: 'Patreon',
  host: 'patreon.com',
  loginUrl: 'https://www.patreon.com/login',
  domains: ['patreon.com'],
  state: 'signedOut',
  waiting: false,
  ...over,
});

describe('stateNote', () => {
  it('names the account a session signs in as', () => {
    expect(stateNote(site({ state: 'signedIn', account: 'Jane' }))).toEqual({
      key: 'siteLogins.signedInAs',
      vars: { account: 'Jane' },
      warn: false,
    });
  });

  it('says signed in where the site names nobody or did not answer', () => {
    expect(stateNote(site({ state: 'signedIn' })).key).toBe('siteLogins.signedIn');
    expect(stateNote(site({ state: 'unchecked' })).key).toBe('siteLogins.signedIn');
  });

  it('warns once the sign-in ran out', () => {
    expect(stateNote(site({ state: 'expired' }))).toMatchObject({ key: 'siteLogins.expired', warn: true });
  });

  it('says it waits while a sign-in is under way, whatever was stored before', () => {
    expect(stateNote(site({ state: 'expired', waiting: true }))).toMatchObject({ key: 'siteLogins.waiting', warn: false });
  });

  it('says not signed in otherwise', () => {
    expect(stateNote(site({})).key).toBe('siteLogins.signedOut');
  });
});

describe('siteForHost', () => {
  const sites = [site({}), site({ id: 'youtube', name: 'YouTube', host: 'youtube.com', domains: ['youtube.com'] })];
  it('finds the site of a host or a subdomain', () => {
    expect(siteForHost('patreon.com', sites)?.id).toBe('patreon');
    expect(siteForHost('www.patreon.com', sites)?.id).toBe('patreon');
    expect(siteForHost('music.youtube.com', sites)?.id).toBe('youtube');
  });
  it('finds nothing for a look-alike', () => {
    expect(siteForHost('notpatreon.com', sites)).toBeUndefined();
    expect(siteForHost('', sites)).toBeUndefined();
  });
});

describe('signInAgainFor', () => {
  const stored = [site({ state: 'signedIn', account: 'Jane' })];
  it('offers signing in again for a members-only failure of a site with a stored session', () => {
    expect(signInAgainFor('membersOnly', 'patreon.com', stored)?.id).toBe('patreon');
    expect(signInAgainFor('membersOnly', 'patreon.com', [site({ state: 'expired' })])?.id).toBe('patreon');
  });
  it('leaves every other case to the plain advice', () => {
    expect(signInAgainFor('membersOnly', 'patreon.com', [site({})])).toBeUndefined();
    expect(signInAgainFor('botCheck', 'patreon.com', stored)).toBeUndefined();
    expect(signInAgainFor('membersOnly', 'vimeo.com', stored)).toBeUndefined();
    expect(signInAgainFor(undefined, 'patreon.com', stored)).toBeUndefined();
  });
});
