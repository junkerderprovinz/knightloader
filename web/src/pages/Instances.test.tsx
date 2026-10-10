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
  const calls: string[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: RequestInit) => {
      calls.push(`${init?.method ?? 'GET'} ${url}`);
      return Promise.resolve(
        new Response(JSON.stringify(answers[url.split('?')[0]] ?? []), { headers: { 'Content-Type': 'application/json' } }),
      );
    }),
  );
  return calls;
}

/** The card of the grid that carries this name; the net's nodes carry it too. */
const card = (name: string) =>
  [...host.querySelectorAll<HTMLElement>('[data-nk]')].find((c) => !c.closest('svg') && c.textContent?.includes(name))!;
const buttons = (within: Element) => [...within.querySelectorAll('button')].map((b) => b.textContent);
const button = (within: Element, text: string) => [...within.querySelectorAll('button')].find((b) => b.textContent === text)!;

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
  it('stands a card that leads to pairing beside this instance before there is a group', async () => {
    serve(noGroup);
    await draw();
    const way = [...host.querySelectorAll('button')].find((b) => b.textContent?.includes('Connect your instances'))!;
    expect(way.textContent).toContain('Connect your instances and the Android app with a 12-word phrase');
    expect(way.parentElement!.querySelector('[data-nk="self"]')!.textContent).toContain('This instance');
    // Nothing to draw a net of yet.
    expect(host.querySelector('[data-testid="group-net"]')).toBeNull();
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

  it('names the kind on every card and gives the phone and the extension a glyph of their own', async () => {
    serve(
      {
        ...noGroup,
        active: true,
        apps: [
          { id: 'p1', name: 'Pixel 8', deployment: 'mobile', connected: true, lastSeen: 1_800_000_000 },
          { id: 'b1', name: 'Browser', deployment: 'extension', connected: false, lastSeen: 1_800_000_000 },
        ],
      },
      [
        { name: 'id-laptop', url: '', relayId: 'id-laptop', displayName: 'Laptop', deployment: 'desktop' },
        { name: 'id-nas', url: '', relayId: 'id-nas', displayName: 'NAS', deployment: 'container' },
        { name: 'cellar', url: 'http://192.168.1.9:8749' },
      ],
    );
    await draw();
    expect(card('Pixel 8').textContent).toContain('Android app');
    expect(card('Browser').textContent).toContain('Browser extension');
    expect(card('Laptop').textContent).toContain('Desktop app');
    expect(card('NAS').textContent).toContain('Container');
    expect(card('cellar').textContent).not.toMatch(/Container|Desktop app|Android app/);
    // The mark stands for a KnightLoader instance; a phone and a browser wear their glyph.
    expect(card('Laptop').querySelector('img')).not.toBeNull();
    expect(card('Pixel 8').querySelector('img')).toBeNull();
    expect(card('Browser').querySelector('img')).toBeNull();
  });

  it('says on each card how the instance is reached', async () => {
    serve(
      {
        ...noGroup,
        active: true,
        members: [
          { id: 'id-laptop', name: 'Laptop', direct: true, relay: false },
          { id: 'id-nas', name: 'NAS', direct: false, relay: true },
        ],
      },
      [
        { name: 'id-laptop', url: '', relayId: 'id-laptop', displayName: 'Laptop' },
        { name: 'id-nas', url: '', relayId: 'id-nas', displayName: 'NAS' },
        { name: 'cellar', url: 'http://192.168.1.9:8749' },
      ],
    );
    await draw();
    expect(card('Laptop').textContent).toContain('Direct');
    expect(card('NAS').textContent).toContain('Via relay');
    expect(card('cellar').textContent).toContain('By address');
  });

  it('draws the group as a net above the cards and lights a card with its node', async () => {
    serve(
      { ...noGroup, active: true, apps: [{ id: 'p1', name: 'Pixel 8', deployment: 'mobile', connected: false, lastSeen: 1 }] },
      [{ name: 'id-laptop', url: '', relayId: 'id-laptop', displayName: 'Laptop' }],
    );
    await draw();
    const wide = host.querySelector('[data-testid="group-net"] svg.wide')!;
    const spokes = [...wide.querySelectorAll('g[data-nk]')];
    expect(spokes.map((g) => g.getAttribute('data-nk'))).toEqual(['member:id-laptop', 'app:p1', 'self']);
    expect(spokes[1].classList.contains('dim')).toBe(true);

    act(() => {
      spokes[0].querySelector('[role="button"]')!.dispatchEvent(new MouseEvent('pointerover', { bubbles: true }));
    });
    expect(card('Laptop').className).toContain('shadow-');
    expect(card('Pixel 8').className).not.toContain('shadow-');
  });

  it('opens the rest about an instance in a window', async () => {
    serve({ ...noGroup, active: true }, [{ name: 'cellar', url: 'http://192.168.1.9:8749' }]);
    await draw();
    expect(buttons(card('cellar'))).toEqual(['Open', 'Details', 'Remove']);
    // This instance has nothing more to say about itself and cannot be taken out.
    expect(buttons(card('nas'))).toEqual(['Open']);

    await act(async () => button(card('cellar'), 'Details').click());
    const window = document.body.querySelector('[role="dialog"]')!;
    expect(window.querySelector('h2')!.textContent).toBe('cellar');
    expect(window.textContent).toContain('192.168.1.9:8749');
    expect(window.textContent).toContain('By address');
  });

  it('takes an instance or a phone out only after the window asks', async () => {
    const calls = serve(
      { ...noGroup, active: true, apps: [{ id: 'p1', name: 'Pixel 8', deployment: 'mobile', connected: true, lastSeen: 1 }] },
      [
        { name: 'id-laptop', url: '', relayId: 'id-laptop', displayName: 'Laptop' },
        { name: 'cellar', url: 'http://192.168.1.9:8749' },
      ],
    );
    await draw();
    const window = () => document.body.querySelector('[role="dialog"]');
    const confirm = () => button(window()!, 'Remove');

    await act(async () => button(card('Laptop'), 'Remove').click());
    expect(window()!.textContent).toContain('Laptop');
    expect(calls.some((c) => c.startsWith('DELETE'))).toBe(false);
    await act(async () => confirm().click());
    expect(calls).toContain('DELETE /api/connect/members/id-laptop');
    expect(window()).toBeNull();

    await act(async () => button(card('cellar'), 'Remove').click());
    await act(async () => confirm().click());
    expect(calls).toContain('DELETE /api/instances/cellar');

    await act(async () => button(card('Pixel 8'), 'Remove').click());
    await act(async () => confirm().click());
    expect(calls).toContain('DELETE /api/connect/apps/p1');
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
