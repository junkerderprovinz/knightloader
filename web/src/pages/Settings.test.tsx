// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { useLabelMode } from '../lib/labelModes';
import { setHidden, useHidden } from '../lib/sidebarPrefs';
import { SettingsPage } from './Settings';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let stored: Record<string, unknown>;
let modules: Record<string, unknown>[];
const sockets: { onmessage?: (e: { data: string }) => void }[] = [];

beforeEach(() => {
  stored = { instanceName: 'nas', buttonLabels: 'both', sidebarLabels: 'both', tabLabels: 'both', bottomBarLabels: 'both' };
  modules = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const path = url.split('?')[0];
      if (path.startsWith('/api/features/') && init?.method === 'PUT') {
        const { enabled } = JSON.parse(String(init.body)) as { enabled: boolean };
        modules = modules.map((m) => (path.endsWith(`/${m.id}`) ? { ...m, enabled } : m));
      }
      if (path === '/api/settings' && init?.method === 'PATCH') stored = { ...stored, ...JSON.parse(String(init.body)) };
      // The Modules page is on offer once a test has a module to list there.
      const pages = modules.length > 0 ? [{ id: 'modules', modules: null }] : [];
      const body = path === '/api/settings' ? stored : path.startsWith('/api/features') ? { modules, pages } : {};
      return Response.json(body);
    }),
  );
  vi.stubGlobal(
    'WebSocket',
    class {
      onmessage?: (e: { data: string }) => void;
      constructor() {
        sockets.push(this);
      }
      send() {}
      close() {}
    },
  );
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: false,
    media: query,
    addEventListener() {},
    removeEventListener() {},
  }));
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  setHidden('instances', false);
  sockets.length = 0;
  vi.unstubAllGlobals();
});

function Probe() {
  return <output data-testid="buttons">{useLabelMode('buttons')}</output>;
}

const buttons = () => host.querySelector('[data-testid="buttons"]')!.textContent;

describe('SettingsPage', () => {
  it('redraws the labels when another tab saves a new mode', async () => {
    await act(async () =>
      root.render(
        <MemoryRouter initialEntries={['/settings/look']}>
          <I18nProvider>
            <Probe />
            <SettingsPage />
          </I18nProvider>
        </MemoryRouter>,
      ),
    );
    expect(buttons()).toBe('both');

    stored = { ...stored, buttonLabels: 'glyph' };
    await act(async () => {
      for (const s of sockets) s.onmessage?.({ data: JSON.stringify({ type: 'settings' }) });
    });
    expect(buttons()).toBe('glyph');
  });

  it('puts the page of a module into the sidebar when the module is switched on', async () => {
    stored = { ...stored, hideInstancesFromSidebar: true };
    modules = [{ id: 'federation', verdict: 'shipped', page: 'instances', enabled: false, switch: 'setting', parked: false }];
    setHidden('instances', true);
    function Rail() {
      return <output data-testid="rail">{useHidden('instances') ? 'hidden' : 'shown'}</output>;
    }
    await act(async () =>
      root.render(
        <MemoryRouter initialEntries={['/settings/modules']}>
          <I18nProvider>
            <Rail />
            <Routes>
              <Route path="/settings/*" element={<SettingsPage />} />
            </Routes>
          </I18nProvider>
        </MemoryRouter>,
      ),
    );
    const rail = () => host.querySelector('[data-testid="rail"]')!.textContent;
    expect(rail()).toBe('hidden');

    await act(async () => (host.querySelector('[role="switch"]') as HTMLElement).click());
    expect(rail()).toBe('shown');
  });
});
