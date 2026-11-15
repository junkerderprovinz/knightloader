// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import type { TorrentOverview } from '../lib/api';
import { TorrentCard } from './TorrentCard';

// The pictures run SVG animations, which jsdom does not have.
vi.mock('./pictures/SeedRing', () => ({ SeedGauge: () => null, SeedRing: () => null }));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

const overview = (extra: Partial<TorrentOverview>): TorrentOverview => ({
  any: true,
  leeching: 1,
  seeding: 0,
  downloadSpeed: 0,
  uploadSpeed: 0,
  downloadedToday: 0,
  uploadedToday: 0,
  downloaded: 0,
  uploaded: 0,
  ratio: 0,
  top: null,
  ...extra,
});

async function show(data: TorrentOverview) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => new Response(JSON.stringify(data), { status: 200, headers: { 'Content-Type': 'application/json' } })),
  );
  await act(async () => {
    root.render(
      <I18nProvider>
        <MemoryRouter>
          <TorrentCard settings={null} />
        </MemoryRouter>
      </I18nProvider>,
    );
  });
}

beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  host = document.createElement('div');
  document.body.appendChild(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

it('says torrents wait while the VPN that carries them is down', async () => {
  await show(overview({ vpn: true, vpnDown: true }));
  expect(host.textContent).toContain('Torrents wait until the VPN is connected again.');
});

it('says nothing of the VPN while its tunnel is up', async () => {
  await show(overview({ vpn: true, vpnDown: false }));
  expect(host.textContent).not.toContain('VPN');
});
