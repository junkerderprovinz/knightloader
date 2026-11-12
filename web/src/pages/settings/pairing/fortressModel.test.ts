import { describe, expect, it } from 'vitest';
import type { FortressMember } from '../../../lib/api';
import { en } from '../../../lib/locales/en';
import { fortressApps, fortressPermissions, permissionOf, phraseCode } from './fortressModel';

const t = (key: keyof typeof en, vars: Record<string, string | number> = {}) =>
  en[key].replace(/\{(\w+)\}/g, (_, name: string) => String(vars[name]));

const member = (over: Partial<FortressMember>): FortressMember => ({
  id: 'm1',
  app: 'arrowloop',
  name: 'ArrowLoop',
  version: '2.1.0',
  device: 'Tower',
  deployment: 'container',
  kind: 'instance',
  online: true,
  relay: false,
  ...over,
});

describe('fortressApps', () => {
  it('puts each other app on one card with a row per place it runs, and leaves KnightLoader out', () => {
    const apps = fortressApps(
      [
        member({}),
        member({ id: 'm2', device: 'Cellar', online: false, lastSeen: 1000 }),
        member({ id: 'm3', app: 'knightloader', name: 'KnightLoader' }),
        member({ id: 'm4', app: 'bombvault', name: 'BombVault', kind: 'mobile', relay: true }),
      ],
      (unix) => `at ${unix}`,
    );
    expect(apps.map((a) => a.id)).toEqual(['arrowloop', 'bombvault']);
    expect(apps[0].instances).toEqual([
      { id: 'm1', device: 'Tower', deviceKind: 'server', form: 'container', version: '2.1.0', presence: 'connected', relay: false, lastSeen: undefined },
      { id: 'm2', device: 'Cellar', deviceKind: 'server', form: 'container', version: '2.1.0', presence: 'away', relay: false, lastSeen: 'at 1000' },
    ]);
    expect(apps[1].instances[0]).toMatchObject({ deviceKind: 'phone', relay: true });
  });

  it('says nothing of when a member that was never seen was last there', () => {
    const [app] = fortressApps([member({ online: false })], (unix) => `at ${unix}`);
    expect(app.instances[0].lastSeen).toBeUndefined();
  });
});

describe('fortressPermissions', () => {
  const members = [member({}), member({ id: 'm2', app: 'bombvault', name: 'BombVault' })];

  it('words what KnightLoader runs at ArrowLoop and what a backup app may do here, naming both apps', () => {
    const perms = fortressPermissions(
      [
        { from: 'knightloader', to: 'arrowloop', action: 'jobs.run', on: true },
        { from: 'bombvault', to: 'knightloader', action: 'backup.import', on: false },
      ],
      members,
      t,
    );
    expect(perms).toEqual([
      {
        id: 'knightloader arrowloop jobs.run',
        from: 'knightloader',
        to: ['arrowloop'],
        on: true,
        title: en['pairing.fortressAction.jobsRun'],
        detail: 'KnightLoader at ArrowLoop',
      },
      {
        id: 'bombvault knightloader backup.import',
        from: 'bombvault',
        to: ['knightloader'],
        on: false,
        title: en['pairing.fortressAction.backupImport'],
        detail: 'BombVault at KnightLoader',
      },
    ]);
  });

  it('shows an action it has no words for by its name', () => {
    const [p] = fortressPermissions([{ from: 'knightloader', to: 'shiplog', action: 'update.run', on: true }], members, t);
    expect(p).toMatchObject({ title: 'update.run', detail: 'KnightLoader at shiplog' });
  });

  it('reads a switch back from its id', () => {
    expect(permissionOf('knightloader arrowloop jobs.list', false)).toEqual({
      from: 'knightloader',
      to: 'arrowloop',
      action: 'jobs.list',
      on: false,
    });
  });
});

describe('phraseCode', () => {
  const words = ['abandon', 'about'];
  const project = 'wss://parleyport.halleluja.design/relay/connect';
  const code = (relayMode: 'project' | 'own' | 'off', relayUrl: string) => phraseCode(words, { relayMode, relayUrl, projectRelayUrl: project });

  it('carries the words alone on the project relay or none', () => {
    expect(code('project', project)).toBe('abandon about');
    expect(code('off', '')).toBe('abandon about');
  });

  it('names an own relay on a second line as the socket the app dials', () => {
    expect(code('own', 'https://relay.example.com')).toBe('abandon about\nwss://relay.example.com/relay/connect');
    expect(code('own', 'http://10.0.0.5:8080/alpha/')).toBe('abandon about\nws://10.0.0.5:8080/alpha/relay/connect');
    expect(code('own', 'wss://example.com/r/connect')).toBe('abandon about\nwss://example.com/r/connect');
  });

  it('leaves out an own relay that is the project relay or no address', () => {
    expect(code('own', 'https://parleyport.halleluja.design')).toBe('abandon about');
    expect(code('own', 'relay.example.com')).toBe('abandon about');
    expect(code('own', 'ftp://relay.example.com')).toBe('abandon about');
  });
});
