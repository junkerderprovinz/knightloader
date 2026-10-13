// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { expect, it, vi } from 'vitest';

import type { ConnectInfo, RelayConfig } from '../../../lib/api';
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
  address: '',
  networkAddress: '',
  members: [],
  apps: [],
  joinedAgo: 0,
  memberSeen: false,
  pending: [],
  awaiting: false,
  fingerprint: '',
};
const relay: RelayConfig = { mode: 'project', relayUrl: '', keySet: false, connected: false, serve: false, serveClients: 0 };
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
          <PairingSection identity={() => null} />
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
