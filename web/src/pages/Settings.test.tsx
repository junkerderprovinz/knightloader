// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { useLabelMode } from '../lib/labelModes';
import { SettingsPage } from './Settings';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let stored: Record<string, unknown>;
const sockets: { onmessage?: (e: { data: string }) => void }[] = [];

beforeEach(() => {
  stored = { instanceName: 'nas', buttonLabels: 'both', sidebarLabels: 'both', tabLabels: 'both', bottomBarLabels: 'both' };
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      const path = url.split('?')[0];
      const body = path === '/api/settings' ? stored : path === '/api/features' ? { modules: [], pages: [] } : {};
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
});
