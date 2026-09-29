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
});
