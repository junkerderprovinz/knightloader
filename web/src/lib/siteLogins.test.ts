import { describe, expect, it } from 'vitest';
import type { SiteLogin } from './api';
import { nextStep, readsBrowsers, siteForHost, signInFor, sourceNote, stateNote, unreadableBrowsers } from './siteLogins';

const site = (over: Partial<SiteLogin>): SiteLogin => ({
  id: 'patreon',
  name: 'Patreon',
  host: 'patreon.com',
  loginUrl: 'https://www.patreon.com/login',
  domains: ['patreon.com'],
  state: 'signedOut',
  unreadable: [],
  readsBrowsers: false,
  disconnected: false,
  ...over,
});

describe('stateNote', () => {
  it('names the account a session signs in as', () => {
    expect(stateNote(site({ state: 'signedIn', account: 'Jane' }))).toEqual({
      key: 'siteLogins.signedInAs',
      vars: { account: 'Jane' },
    });
  });

  it('says signed in where the site names nobody or did not answer', () => {
    expect(stateNote(site({ state: 'signedIn' })).key).toBe('siteLogins.signedIn');
    expect(stateNote(site({ state: 'unchecked' })).key).toBe('siteLogins.signedIn');
  });

  it('says when the sign-in ran out', () => {
    expect(stateNote(site({ state: 'expired' }))).toMatchObject({ key: 'siteLogins.expired' });
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
  it('finds the site of a short-link host its links play from', () => {
    const youtube = site({ id: 'youtube', host: 'youtube.com', domains: ['youtube.com'], links: ['youtu.be'] });
    expect(siteForHost('youtu.be', [youtube])?.id).toBe('youtube');
  });
  it('finds nothing for a look-alike', () => {
    expect(siteForHost('notpatreon.com', sites)).toBeUndefined();
    expect(siteForHost('', sites)).toBeUndefined();
  });
});

describe('sourceNote', () => {
  it('names the browser and profile a session was read from', () => {
    expect(sourceNote(site({ state: 'signedIn', source: 'browser', browser: 'Firefox (default-release)' }))).toEqual({
      key: 'siteLogins.fromBrowser',
      vars: { browser: 'Firefox (default-release)' },
    });
  });
  it('says the extension passed it on', () => {
    expect(sourceNote(site({ state: 'unchecked', source: 'extension' }))?.key).toBe('siteLogins.fromExtension');
  });
  it("says KnightLoader's own sign-in browser passed it on", () => {
    expect(sourceNote(site({ state: 'signedIn', source: 'kl-browser' }))?.key).toBe('siteLogins.fromKlBrowser');
  });
  it("says which browser's session the site turned down", () => {
    expect(sourceNote(site({ state: 'expired', source: 'browser', browser: 'Firefox (default-release)' }))).toEqual({
      key: 'siteLogins.turnedDown',
      vars: { site: 'Patreon', browser: 'Firefox (default-release)' },
    });
  });
  it('says nothing without a session or a known source', () => {
    expect(sourceNote(site({ source: 'browser', browser: 'Firefox' }))).toBeUndefined();
    expect(sourceNote(site({ state: 'signedIn' }))).toBeUndefined();
    expect(sourceNote(site({ state: 'signedIn', source: 'browser' }))).toBeUndefined();
  });
});

describe('nextStep', () => {
  it('says to sign in in the browser where KnightLoader picks it up', () => {
    expect(nextStep(site({}), { readsBrowsers: true })).toEqual({
      key: 'siteLogins.signInHere',
      vars: { site: 'Patreon' },
    });
  });
  it('points a container at the sign-in window, whatever extension its group has', () => {
    expect(nextStep(site({}), { readsBrowsers: false })).toEqual({
      key: 'siteLogins.signInInKl',
      vars: { site: 'Patreon' },
    });
    expect(nextStep(site({ state: 'expired', handsOver: true }), { readsBrowsers: false })?.key).toBe('siteLogins.signInInKl');
  });
  it('has nothing to say while signed in', () => {
    expect(nextStep(site({ state: 'signedIn' }), { readsBrowsers: false })).toBeUndefined();
    expect(nextStep(site({ state: 'unchecked' }), { readsBrowsers: true })).toBeUndefined();
  });
});

describe('a site removed here', () => {
  it('says the browser stays signed in, and names no step that would not sign it in', () => {
    const removed = site({ disconnected: true });
    expect(sourceNote(removed)).toEqual({ key: 'siteLogins.disconnected' });
    expect(nextStep(removed, { readsBrowsers: true })).toBeUndefined();
  });
});

describe('readsBrowsers', () => {
  it('takes what the instance says, also before any browser held a session', () => {
    expect(readsBrowsers([site({ readsBrowsers: true })])).toBe(true);
    expect(readsBrowsers([site({}), site({ id: 'youtube', state: 'signedIn', source: 'extension' })])).toBe(false);
    expect(readsBrowsers([])).toBe(false);
  });
});

describe('unreadableBrowsers', () => {
  it('names each browser KnightLoader could not read once, in order', () => {
    const sites = [site({ unreadable: ['Chrome', 'Edge'] }), site({ id: 'youtube', unreadable: ['Edge', 'Brave'] }), site({ id: 'x' })];
    expect(unreadableBrowsers(sites)).toEqual(['Chrome', 'Edge', 'Brave']);
  });
});

describe('signInFor', () => {
  const stored = [site({ state: 'signedIn', account: 'Jane' })];
  it('offers signing in again for a members-only failure of a site with a stored session', () => {
    expect(signInFor('membersOnly', 'patreon.com', stored)).toMatchObject({ site: { id: 'patreon' }, again: true });
    expect(signInFor('membersOnly', 'patreon.com', [site({ state: 'expired' })])).toMatchObject({ again: true });
  });
  it('offers signing in for a sign-in failure of a known site nobody signed in to', () => {
    expect(signInFor('membersOnly', 'patreon.com', [site({})])).toMatchObject({ site: { id: 'patreon' }, again: false });
    const youtube = site({ id: 'youtube', host: 'youtube.com', domains: ['youtube.com'], links: ['youtu.be'] });
    expect(signInFor('botCheck', 'youtu.be', [youtube])).toMatchObject({ site: { id: 'youtube' }, again: false });
    expect(signInFor('signInRequired', 'youtu.be', [youtube])).toMatchObject({ site: { id: 'youtube' }, again: false });
  });
  it('leaves every other case to the plain advice', () => {
    expect(signInFor('gone', 'patreon.com', stored)).toBeUndefined();
    expect(signInFor('membersOnly', 'vimeo.com', stored)).toBeUndefined();
    expect(signInFor(undefined, 'patreon.com', stored)).toBeUndefined();
  });
});
