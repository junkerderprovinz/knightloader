// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Way } from '../../../components/pictures/WaysIn';
import { I18nProvider } from '../../../lib/i18n';
import { SettingsProvider, type SettingsDraft } from '../context';
import type { Feature } from '../features';
import { clearJump, pendingJump } from '../jump';
import { useWays } from './ways';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let seen: { ways: Way[]; open: (id: string) => void; at: string };

beforeEach(() => {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  clearJump();
  vi.unstubAllGlobals();
});

const feature = (id: string, fields: Partial<Feature> = {}): Feature => ({
  id,
  verdict: 'shipped',
  page: 'collector',
  enabled: false,
  switch: 'setting',
  parked: false,
  ...fields,
});

function Probe({ watch }: { watch: boolean }) {
  const { ways, open } = useWays(watch, [{ name: 'nas', url: 'http://nas:8749' }]);
  seen = { ways, open, at: useLocation().pathname };
  return null;
}

async function mount(modules: Feature[], answers: { apps?: unknown[]; accounts?: unknown[] } = {}, watch = false) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      const body = url === '/api/connect' ? { members: [], apps: answers.apps ?? [] } : (answers.accounts ?? []);
      return new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });
    }),
  );
  const features = { features: { modules, pages: [] }, toggle: async () => {} };
  await act(async () =>
    root.render(
      <I18nProvider>
        <MemoryRouter initialEntries={['/settings/collector']}>
          <SettingsProvider draft={{} as SettingsDraft} features={features}>
            <Probe watch={watch} />
          </SettingsProvider>
        </MemoryRouter>
      </I18nProvider>,
    ),
  );
}

const state = (id: string) => seen.ways.find((w) => w.id === id)?.state;

describe('the ways into the collector', () => {
  it('are open, off or not set up as the modules stand', async () => {
    await mount([
      feature('cnl', { enabled: true }),
      feature('crawler'),
      feature('watch', { switch: 'parked' }),
      feature('feeds', { switch: 'parked', parked: true }),
    ]);
    expect(state('cnl')).toBe('on');
    expect(state('crawler')).toBe('off');
    // A folder that was never named is not set up; subscriptions put aside are off.
    expect(state('watch')).toBe('unset');
    expect(state('feeds')).toBe('off');
    // Pasting and adding a file have no switch.
    expect([state('paste'), state('file'), state('bookmarklet')]).toEqual(['on', 'on', 'on']);
  });

  it('leave out a way this build does not have', async () => {
    await mount([feature('cnl', { verdict: 'not-built' })]);
    expect(seen.ways.map((w) => w.id)).toEqual(['paste', 'file', 'clipboard', 'extension', 'bookmarklet', 'phone', 'account']);
  });

  it('follow the clipboard watch of this browser', async () => {
    await mount([], {}, true);
    expect(state('clipboard')).toBe('on');
    await mount([], {}, false);
    expect(state('clipboard')).toBe('off');
  });

  it('count a phone and a browser extension once they have joined the group, by name', async () => {
    await mount([], {
      apps: [
        { id: '1', name: 'Pixel', deployment: 'mobile' },
        { id: '2', name: 'Brave', deployment: 'extension' },
      ],
    });
    expect(seen.ways.find((w) => w.id === 'phone')).toMatchObject({ state: 'on', detail: 'Pixel' });
    expect(seen.ways.find((w) => w.id === 'extension')).toMatchObject({ state: 'on', detail: 'Brave' });
  });

  it('count a phone and a browser extension as not set up while the group has neither', async () => {
    await mount([]);
    expect([state('phone'), state('extension')]).toEqual(['unset', 'unset']);
  });

  it('count the other instances while the module is on, and an account that imports', async () => {
    await mount([feature('federation', { enabled: true })], {
      accounts: [
        { label: 'TorBox', service: 'torbox', canImport: true, import: true },
        { label: 'Rapidgator', service: 'rapidgator', canImport: false, import: false },
      ],
    });
    expect(seen.ways.find((w) => w.id === 'instances')).toMatchObject({ state: 'on', detail: 'nas' });
    expect(seen.ways.find((w) => w.id === 'account')).toMatchObject({ state: 'on', detail: 'TorBox' });
  });

  it('count the instances as off with their module, and an account that could import as off', async () => {
    await mount([feature('federation')], {
      accounts: [{ label: 'TorBox', service: 'torbox', canImport: true, import: false }],
    });
    expect([state('instances'), state('account')]).toEqual(['off', 'off']);
  });

  it('go to the row a way is set up in', async () => {
    await mount([feature('downloadclient')]);
    act(() => seen.open('downloadclient'));
    expect(seen.at).toBe('/settings/access');
    expect(pendingJump()).toMatchObject({
      page: 'access',
      title: 'settings.access.tokens.title',
      label: 'settings.module.downloadclient',
    });

    act(() => seen.open('account'));
    expect(seen.at).toBe('/accounts');
  });
});
