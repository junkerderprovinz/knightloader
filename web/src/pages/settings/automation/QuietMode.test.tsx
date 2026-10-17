// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { PlaybackView, Settings } from '../../../lib/api';
import { SettingsProvider, type SettingsDraft } from '../context';
import { QuietModeCard } from './QuietMode';

vi.mock('../../../lib/api', async (original) => ({
  ...(await original<typeof import('../../../lib/api')>()),
  // No live stream in jsdom; the card reads the queue once at the start.
  connectWS: () => () => {},
}));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

// A lit logo is read once it is in view, which jsdom has nothing of.
globalThis.IntersectionObserver ??= class {
  observe() {}
  disconnect() {}
} as unknown as typeof IntersectionObserver;

let root: Root;
let host: HTMLDivElement;
let posts: { url: string; body: unknown }[];
let view: PlaybackView;
let quiet: boolean;

const json = (v: unknown) =>
  Promise.resolve(new Response(JSON.stringify(v), { headers: { 'Content-Type': 'application/json' } }));

const playing = (local: boolean): PlaybackView => ({
  status: {
    enabled: true,
    active: !local,
    streams: [
      {
        server: 'plex',
        title: 'Pilot',
        series: 'Some Show',
        user: 'anna',
        player: 'TV',
        paused: false,
        local,
        counts: !local,
        reason: local ? 'local' : undefined,
      },
    ],
    servers: [{ id: 'plex', checkedAt: '2026-10-16T20:00:00Z', ok: true }],
  },
  servers: [{ id: 'plex', kind: 'plex', url: 'http://10.0.0.2:32400', hasToken: true, host: '10.0.0.2:32400' }],
  kinds: ['plex', 'jellyfin', 'emby'],
});

beforeEach(() => {
  posts = [];
  quiet = true;
  view = playing(false);
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: RequestInit) => {
      if (init?.method === 'POST') posts.push({ url, body: JSON.parse(String(init.body)) });
      if (url === '/api/playback') return json(view);
      if (url === '/api/queue') {
        if (init?.method === 'POST') quiet = false;
        return json({ halted: false, running: 0, quiet, playback: quiet });
      }
      if (url === '/api/playback/servers') return json(view);
      return json({});
    }),
  );
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function draw() {
  const cfg = {
    quiet: { speedLimit: 0, maxConcurrent: 1, uploadLimit: 0 },
    playback: { enabled: true, ignoreLocal: false, pausedMinutes: 30, holdExtraction: true },
  } as unknown as Settings;
  const draft = { cfg, saved: cfg, patch: vi.fn() } as unknown as SettingsDraft;
  // No module in the registry, so the switch draws nothing and its fields stand alone.
  const features = { features: { modules: [], pages: [] }, toggle: async () => {} };
  await act(async () =>
    root.render(
      <SettingsProvider draft={draft} features={features}>
        <QuietModeCard hue={0} />
      </SettingsProvider>,
    ),
  );
}

const button = (scope: ParentNode, text: string) =>
  [...scope.querySelectorAll('button')].find((b) => b.textContent?.trim() === text);

describe('QuietModeCard', () => {
  it('says why quiet mode is on and waives the stream on request', async () => {
    await draw();
    expect(host.textContent).toContain('Something is playing, so quiet mode is on.');
    expect(host.textContent).toContain('Some Show: Pilot');
    await act(async () => button(host, 'Off for this stream')!.click());
    expect(posts).toContainEqual({ url: '/api/queue', body: { quiet: false } });
    expect(host.textContent).toContain('you switched quiet mode off for it');
    expect(button(host, 'Off for this stream')).toBeUndefined();
  });

  it('marks a stream at home that does not count', async () => {
    view = playing(true);
    await draw();
    expect(host.textContent).toContain('Nothing is playing that counts.');
    expect(host.textContent).toContain('At home, not counted');
  });

  it('adds a server without an id and with the kind picked', async () => {
    await draw();
    await act(async () => button(host, 'Add a media server')!.click());
    await act(async () => button(document.body, 'Jellyfin')!.click());
    const address = document.body.querySelector<HTMLInputElement>('input[placeholder="http://192.168.1.10:8096"]')!;
    await act(async () => {
      const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
      set.call(address, 'http://10.0.0.5:8096');
      address.dispatchEvent(new Event('input', { bubbles: true }));
    });
    await act(async () => button(document.body, 'Save server')!.click());
    expect(posts).toContainEqual({
      url: '/api/playback/servers',
      body: { id: '', name: '', kind: 'jellyfin', url: 'http://10.0.0.5:8096', token: '' },
    });
  });
});
