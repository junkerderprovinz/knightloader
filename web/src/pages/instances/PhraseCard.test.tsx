// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import type { ConnectInfo } from '../../lib/api';
import { I18nProvider } from '../../lib/i18n';
import { PhraseCard } from './PhraseCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const base: ConnectInfo = {
  active: true,
  connected: true,
  passwordSet: true,
  relayUrl: 'wss://relay.halleluja.design/relay/connect',
  selfHosted: false,
  relayMode: 'project',
  projectRelayUrl: 'wss://relay.halleluja.design/relay/connect',
  name: 'nas',
  members: [],
  joinedAgo: 0,
  memberSeen: false,
};

function draw(group: Partial<ConnectInfo>) {
  act(() =>
    root.render(
      <MemoryRouter>
        <I18nProvider>
          <PhraseCard group={{ ...base, ...group }} onGroup={() => {}} onRefresh={() => {}} />
        </I18nProvider>
      </MemoryRouter>,
    ),
  );
}

const stage = () => host.querySelector('[data-stage]')!.getAttribute('data-stage');
const badge = () => host.querySelector('[data-testid="pair-state"]')?.textContent ?? '';

describe('PhraseCard', () => {
  it('offers the two tiles outside a group', () => {
    draw({ active: false });
    expect(stage()).toBe('unpaired');
    const tiles = [...host.querySelectorAll('button[aria-pressed]')].map((b) => b.textContent);
    expect(tiles[0]).toContain('Generate phrase');
    expect(tiles[1]).toContain('Enter phrase');
  });

  it('turns both tiles off and says why without a login password', () => {
    draw({ active: false, passwordSet: false });
    expect(host.textContent).toContain('Set a login password first');
    for (const tile of host.querySelectorAll<HTMLButtonElement>('button[aria-pressed]')) expect(tile.disabled).toBe(true);
  });

  it('says Searching in the first minute after entering a phrase', () => {
    draw({ joinedAgo: 12 });
    expect(stage()).toBe('searching');
    expect(badge()).toContain('Searching');
    expect(host.textContent).toContain('0:12');
  });

  it('says Still alone after a minute and what to do', () => {
    draw({ joinedAgo: 75 });
    expect(stage()).toBe('alone');
    expect(badge()).toContain('Still alone');
    expect(host.textContent).toContain('Nobody in this group yet');
    expect(host.textContent).toContain('Leave this group');
    expect(host.textContent).not.toContain('Relay not reachable');
  });

  it('lists what to check when the relay cannot be reached and nobody came', () => {
    draw({ joinedAgo: 75, connected: false });
    expect(host.textContent).toContain('Relay not reachable');
    expect(host.textContent).toContain('relay.halleluja.design');
  });

  it('points at the relay card when there is no relay', () => {
    draw({ joinedAgo: 75, relayMode: 'off', connected: false });
    expect(host.textContent).toContain('No relay picked?');
    expect(host.querySelector('[data-testid="relay-line"]')!.textContent).toContain('No relay');
  });

  it('says Paired only while another instance is there', () => {
    draw({ joinedAgo: 500, memberSeen: true, members: [{ id: 'b', name: 'office' }] });
    expect(stage()).toBe('paired');
    expect(badge()).toContain('Paired');
    expect(host.textContent).toContain('office');

    draw({ joinedAgo: 500, memberSeen: true, members: [] });
    expect(badge()).toContain('Searching');
  });
});
