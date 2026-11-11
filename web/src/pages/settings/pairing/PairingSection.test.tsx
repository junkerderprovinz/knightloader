// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { expect, it, vi } from 'vitest';

import type { ConnectInfo, FortressState, RelayConfig } from '../../../lib/api';
import { I18nProvider } from '../../../lib/i18n';
import { PairingSection } from './PairingSection';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const alone: ConnectInfo = {
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
  away: [],
  joinedAgo: 0,
  memberSeen: false,
  pending: [],
  awaiting: false,
  fingerprint: '',
  relayKeyDropped: false,
  droppedInstances: [],
  behind: [],
};
const relay: RelayConfig = { mode: 'project', relayUrl: '', connected: false, serve: false, serveClients: 0 };
const outside: FortressState = {
  joined: false,
  phrase: false,
  device: 'nas',
  passwordSet: true,
  members: [],
  permissions: [],
  pairedWith: [],
  behind: [],
  left: [],
};
const WORDS = 'orbit wagon lemon crisp absent tunnel galaxy harbor pencil ribbon velvet yellow';

const json = (body: unknown) => new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });

it('keeps words just created when a poll sent before them answers late', async () => {
  vi.useFakeTimers();
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  const late: ((r: Response) => void)[] = [];
  let polls = 0;
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      if (url === '/api/relay/config') return Promise.resolve(json(relay));
      if (url === '/api/fortress') return Promise.resolve(json(outside));
      if (url === '/api/connect/activate') return Promise.resolve(json({ phrase: WORDS, info: { ...alone, active: true } }));
      if (url === '/api/connect') {
        polls++;
        if (polls === 1) return Promise.resolve(json(alone));
        return new Promise<Response>((done) => late.push(done));
      }
      return Promise.resolve(json({}));
    }),
  );
  const host = document.createElement('div');
  document.body.append(host);
  const root = createRoot(host);
  await act(async () =>
    root.render(
      <MemoryRouter>
        <I18nProvider>
          <PairingSection identity={null} />
        </I18nProvider>
      </MemoryRouter>,
    ),
  );
  await act(async () => vi.advanceTimersByTime(10_000));
  expect(late).toHaveLength(1);

  const generate = [...host.querySelectorAll<HTMLButtonElement>('button[data-choice]')].find((b) =>
    b.textContent?.startsWith('Generate phrase'),
  )!;
  await act(async () => generate.click());
  const words = () => host.querySelectorAll('li[data-slot]').length;
  expect(words()).toBe(12);

  await act(async () => late[0](json(alone)));
  expect(words()).toBe(12);

  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

it('says why an instance paired by a relay key typed in by hand stands alone', async () => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  const notice = (dropped: boolean) =>
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) =>
        Promise.resolve(
          json(url === '/api/relay/config' ? relay : url === '/api/fortress' ? outside : { ...alone, relayKeyDropped: dropped }),
        ),
      ),
    );
  const host = document.createElement('div');
  document.body.append(host);
  const root = createRoot(host);
  const draw = () =>
    act(async () =>
      root.render(
        <MemoryRouter>
          <I18nProvider>
            <PairingSection key={String(Math.random())} identity={null} />
          </I18nProvider>
        </MemoryRouter>,
      ),
    );

  notice(true);
  await draw();
  expect(host.querySelector('[role="status"]')?.textContent).toContain('relay key you typed in by hand');

  notice(false);
  await draw();
  expect(host.querySelector('[role="status"]')).toBeNull();

  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});
