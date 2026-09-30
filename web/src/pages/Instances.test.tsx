// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { Instances } from './Instances';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

const noGroup = {
  active: false,
  connected: false,
  passwordSet: true,
  relayUrl: '',
  selfHosted: false,
  relayMode: 'project',
  projectRelayUrl: '',
  name: 'nas',
  members: [],
  apps: [],
  joinedAgo: 0,
  memberSeen: false,
};

function serve(connect: object, instances: object[] = []) {
  const answers: Record<string, unknown> = {
    '/api/connect': connect,
    '/api/instances': instances,
    '/api/discovery': [],
    '/api/features': { modules: [], pages: [] },
    '/api/settings': { instanceName: 'nas' },
    '/api/tasks': [],
  };
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) =>
      Promise.resolve(
        new Response(JSON.stringify(answers[url.split('?')[0]] ?? []), { headers: { 'Content-Type': 'application/json' } }),
      ),
    ),
  );
}

beforeEach(() => {
  vi.stubGlobal(
    'WebSocket',
    class {
      close() {}
    },
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
  await act(async () =>
    root.render(
      <MemoryRouter>
        <I18nProvider>
          <Instances />
        </I18nProvider>
      </MemoryRouter>,
    ),
  );
}

describe('Instances', () => {
  it('stands the Pairing button in the middle of the page before there is a group', async () => {
    serve(noGroup);
    await draw();
    expect(host.textContent).toContain('Connect your instances and the Android app with a 12-word phrase');
    expect(host.textContent).not.toContain('This instance');
  });

  it('shows a card for every phone of the group, connected or not', async () => {
    serve({
      ...noGroup,
      active: true,
      apps: [
        { id: 'p1', name: 'Pixel 8', connected: true, lastSeen: 1_800_000_000 },
        { id: 'p2', name: 'Galaxy S24', connected: false, lastSeen: 1_700_000_000 },
      ],
    });
    await draw();
    expect(host.textContent).toContain('Pixel 8');
    expect(host.textContent).toContain('Galaxy S24');
    expect(host.textContent).toContain('Last seen');
    expect(host.textContent).not.toContain('Fleet');
  });

  it('marks every card with the glyph of its kind and gives the phone the logo', async () => {
    serve(
      {
        ...noGroup,
        active: true,
        apps: [{ id: 'p1', name: 'Pixel 8', connected: true, lastSeen: 1_800_000_000 }],
      },
      [
        { name: 'id-laptop', url: '', relayId: 'id-laptop', displayName: 'Laptop', deployment: 'desktop' },
        { name: 'id-nas', url: '', relayId: 'id-nas', displayName: 'NAS', deployment: 'container' },
        { name: 'cellar', url: 'http://192.168.1.9:8749' },
      ],
    );
    await draw();
    const card = (name: string) => [...host.querySelectorAll('.glim-card')].find((c) => c.textContent?.includes(name))!;
    const kind = (name: string) => card(name).querySelector('[data-kind]')?.getAttribute('data-kind') ?? null;
    expect(kind('Pixel 8')).toBe('mobile');
    expect(kind('Laptop')).toBe('desktop');
    expect(kind('NAS')).toBe('container');
    expect(kind('cellar')).toBeNull();
    expect(card('Pixel 8').querySelector('[data-kind="mobile"]')?.getAttribute('aria-label')).toBe('Android app');
    expect(card('Pixel 8').querySelector('img')).not.toBeNull();
  });

  it('shows every instance under its address and opens a member there', async () => {
    const open = vi.fn();
    serve({ ...noGroup, active: true, address: 'https://nas.example.org' }, [
      { name: 'id-laptop', url: '', relayId: 'id-laptop', displayName: 'Laptop', address: 'http://192.168.20.86:8749' },
    ]);
    vi.stubGlobal('open', open);
    await draw();
    expect(host.textContent).toContain('nas.example.org');
    expect(host.textContent).toContain('192.168.20.86:8749');
    expect(host.textContent).not.toContain('http://');

    const cards = [...host.querySelectorAll('button')].filter((b) => b.textContent === 'Open');
    await act(async () => cards[1].click());
    expect(open).toHaveBeenCalledWith('http://192.168.20.86:8749', '_blank', 'noopener,noreferrer');
  });

  it('gives the Pairing button no info bubble', async () => {
    serve({ ...noGroup, active: true, apps: [{ id: 'p1', name: 'Pixel 8', connected: true, lastSeen: 1 }] });
    await draw();
    const pairing = [...host.querySelectorAll('button')].find((b) => b.textContent?.trim() === 'Pairing');
    expect(pairing).toBeDefined();
    expect(pairing!.closest('div')!.querySelector('[role="note"]')).toBeNull();
  });
});
