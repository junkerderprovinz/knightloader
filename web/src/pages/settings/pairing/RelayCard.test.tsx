// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { ConnectInfo, RelayConfig } from '../../../lib/api';
import { I18nProvider } from '../../../lib/i18n';
import { RelayCard } from './RelayCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
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
  vi.unstubAllGlobals();
});

const group = {
  active: false,
  connected: false,
  passwordSet: true,
  relayUrl: '',
  selfHosted: false,
  relayMode: 'project',
  projectRelayUrl: 'wss://relay.halleluja.design/relay/connect',
  name: 'nas',
  address: '',
  members: [],
  apps: [],
  joinedAgo: 0,
  memberSeen: false,
} satisfies ConnectInfo;

const relay: RelayConfig = { mode: 'project', relayUrl: '', keySet: false, connected: false, serve: false, serveClients: 0 };

function draw(g: Partial<ConnectInfo>, r: Partial<RelayConfig> = {}) {
  act(() =>
    root.render(
      <I18nProvider>
        <RelayCard group={{ ...group, ...g }} relay={{ ...relay, ...r }} onRelay={() => {}} />
      </I18nProvider>,
    ),
  );
}

const state = () => host.querySelector('[data-testid="relay-state"]')!.textContent;

describe('RelayCard', () => {
  it('says there is no group yet instead of Not connected before a phrase', () => {
    draw({});
    expect(state()).toBe('No group yet');
    expect(host.textContent).toContain('The relay connects as soon as this instance has a phrase.');
  });

  it('reports the connection once there is a group', () => {
    draw({ active: true });
    expect(state()).toBe('Not connected');
    draw({ active: true }, { connected: true });
    expect(state()).toBe('Connected');
    expect(host.textContent).not.toContain('as soon as this instance has a phrase');
  });

  it('says No relay when none is picked, group or not', () => {
    draw({ relayMode: 'off' }, { mode: 'off' });
    expect(state()).toBe('No relay');
  });

  it('goes back to the stored mode when picking another one fails', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('the settings could not be saved', { status: 500 })));
    draw({ active: true });
    await act(async () => tab('Own relay').click());
    expect(tab('Project relay').getAttribute('aria-selected')).toBe('true');
    expect(host.querySelector('[data-testid="own-relay"]')).toBeNull();
  });

  it('keeps what is typed while an earlier address is being saved', async () => {
    vi.useFakeTimers();
    try {
      const answers: ((body: RelayConfig) => void)[] = [];
      vi.stubGlobal(
        'fetch',
        vi.fn(
          () =>
            new Promise<Response>((resolve) => answers.push((body) => resolve(Response.json(body)))),
        ),
      );
      act(() =>
        root.render(
          <I18nProvider>
            <Stateful />
          </I18nProvider>,
        ),
      );
      const input = () => host.querySelector<HTMLInputElement>('#relay-address')!;
      type(input(), 'https://relay.example');
      await act(async () => vi.advanceTimersByTime(800));
      type(input(), 'https://relay.example.org');
      await act(async () => answers[0]({ ...relay, mode: 'own', relayUrl: 'https://relay.example' }));
      expect(input().value).toBe('https://relay.example.org');
    } finally {
      vi.useRealTimers();
    }
  });
});

const tab = (name: string) => [...host.querySelectorAll<HTMLButtonElement>('[role="tab"]')].find((b) => b.textContent === name)!;

/** RelayCard with the page's own state around it, so a save's answer reaches the card. */
function Stateful() {
  const [r, setR] = useState<RelayConfig>({ ...relay, mode: 'own' });
  return <RelayCard group={{ ...group, active: true, relayMode: 'own' }} relay={r} onRelay={setR} />;
}

/** Types into a controlled input the way a keystroke does. */
function type(el: HTMLInputElement, value: string) {
  act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, value);
    el.dispatchEvent(new Event('input', { bubbles: true }));
  });
}
