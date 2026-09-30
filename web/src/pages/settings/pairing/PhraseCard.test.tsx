// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { ConnectInfo } from '../../../lib/api';
import { I18nProvider } from '../../../lib/i18n';
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
  vi.unstubAllGlobals();
  localStorage.clear();
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
  address: '',
  members: [],
  apps: [],
  joinedAgo: 0,
  memberSeen: false,
};

function draw(group: Partial<ConnectInfo>, onGroup: (g: ConnectInfo) => void = () => {}) {
  act(() =>
    root.render(
      <MemoryRouter>
        <I18nProvider>
          <PhraseCard group={{ ...base, ...group }} onGroup={onGroup} onRefresh={() => {}} />
        </I18nProvider>
      </MemoryRouter>,
    ),
  );
}

const stage = () => host.querySelector('[data-stage]')!.getAttribute('data-stage');
const tiles = () => [...host.querySelectorAll<HTMLButtonElement>('button[data-choice]')];
const tile = (title: string) => tiles().find((b) => b.textContent?.startsWith(title))!;
const dialog = () => document.body.querySelector('[role="dialog"]');
const button = (text: string) => [...host.querySelectorAll('button')].find((b) => b.textContent === text)!;
const WORDS = 'orbit wagon lemon crisp absent tunnel galaxy harbor pencil ribbon velvet yellow';

function answer(body: object) {
  const calls: string[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: RequestInit) => {
      calls.push(`${init?.method ?? 'GET'} ${url}`);
      return Promise.resolve(new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } }));
    }),
  );
  return calls;
}
const badge = () => host.querySelector('[data-testid="pair-state"]')?.textContent ?? '';

describe('PhraseCard', () => {
  it('offers the two tiles outside a group', () => {
    draw({ active: false });
    expect(stage()).toBe('unpaired');
    const titles = tiles().map((b) => b.textContent);
    expect(titles[0]).toContain('Generate phrase');
    expect(titles[1]).toContain('Enter phrase');
    expect(dialog()).toBeNull();
  });

  it('shows a generated phrase in the words window', async () => {
    const calls = answer({ phrase: WORDS, info: { ...base, joinedAgo: 0 } });
    draw({ active: false });
    await act(async () => tile('Generate phrase').click());
    expect(calls).toContain('POST /api/connect/activate');
    const slots = [...dialog()!.querySelectorAll('li[data-slot]')].map((li) => li.textContent);
    expect(slots).toEqual(WORDS.split(' ').map((w, i) => `${i + 1}${w}`));
    const row = [...dialog()!.querySelectorAll('button')].map((b) => b.textContent);
    expect(row.slice(-2)).toEqual(['Close', 'Copy']);
  });

  it('takes a first instance\'s words in a window with Close, Paste and Pair', () => {
    draw({ active: false });
    act(() => tile('Enter phrase').click());
    expect([...dialog()!.querySelectorAll('h2')].map((h) => h.textContent)).toContain('Enter phrase');
    expect(dialog()!.querySelectorAll('li[data-slot]')).toHaveLength(12);
    const row = [...dialog()!.querySelectorAll('button')].map((b) => b.textContent);
    expect(row.slice(-3)).toEqual(['Close', 'Paste', 'Pair']);
  });

  it('offers both tiles without a login password and says what that means', () => {
    draw({ active: false, passwordSet: false });
    expect(host.textContent).toContain('Anyone who can open this web interface');
    for (const tile of tiles()) expect(tile.disabled).toBe(false);
  });

  it('says nothing about a password once one is set', () => {
    draw({ active: false });
    expect(host.textContent).not.toContain('Anyone who can open this web interface');
  });

  it('says Searching in the first minute after entering a phrase', () => {
    draw({ joinedAgo: 12 });
    expect(stage()).toBe('searching');
    expect(badge()).toContain('Searching');
    expect(host.textContent).toContain('0:12');
  });

  it('says Still alone after a minute and offers the two ways out, with nothing open', () => {
    draw({ joinedAgo: 75 });
    expect(stage()).toBe('alone');
    expect(badge()).toContain('Still alone');
    expect(host.textContent).toContain('Nothing entered over there yet?');
    expect(host.textContent).toContain('Generated a phrase over there too?');
    expect(host.querySelector('textarea')).toBeNull();
    expect(host.querySelector('ol')).toBeNull();
    expect(host.textContent).not.toContain('Relay not reachable');
  });

  it('joins the other group in one step, without leaving this one first', async () => {
    const calls: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string, init?: RequestInit) => {
        calls.push(`${init?.method ?? 'GET'} ${url}`);
        return Promise.resolve(
          new Response(JSON.stringify({ ...base, members: [{ id: 'b', name: 'office', direct: true, relay: false }] }), {
            headers: { 'Content-Type': 'application/json' },
          }),
        );
      }),
    );
    const onGroup = vi.fn();
    draw({ joinedAgo: 75 }, onGroup);
    act(() => tile('Generated a phrase over there too?').click());
    // One title with one (i), the twelve slots and the buttons in one row.
    expect([...host.querySelectorAll('h2')].map((h) => h.textContent)).toContain('Enter its words');
    expect(host.querySelectorAll('li[data-slot]')).toHaveLength(12);

    const area = host.querySelector('textarea')!;
    const set = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!;
    act(() => {
      set.call(area, 'orbit wagon lemon crisp absent tunnel galaxy harbor pencil ribbon velvet yellow');
      area.dispatchEvent(new Event('input', { bubbles: true }));
    });
    const pair = [...host.querySelectorAll('button')].find((b) => b.textContent === 'Pair')!;
    await act(async () => pair.click());

    expect(calls.filter((c) => c.includes('/api/connect'))).toEqual(['POST /api/connect/join']);
    expect(onGroup).toHaveBeenCalledWith(expect.objectContaining({ members: [expect.objectContaining({ id: 'b' })] }));
    expect(host.querySelector('textarea')).toBeNull();
  });

  it('puts the note about a missing password away for good in this browser', () => {
    draw({ active: false, passwordSet: false });
    const dismiss = host.querySelector<HTMLButtonElement>('button[aria-label="Dismiss"]')!;
    act(() => dismiss.click());
    expect(host.textContent).not.toContain('Anyone who can open this web interface');

    act(() => root.unmount());
    root = createRoot(host);
    draw({ active: false, passwordSet: false });
    expect(host.textContent).not.toContain('Anyone who can open this web interface');
  });

  it('lists what to check when the relay cannot be reached and nobody came', () => {
    draw({ joinedAgo: 75, connected: false });
    expect(host.textContent).toContain('Relay not reachable');
    const open = [...host.querySelectorAll('button')].find((b) => b.textContent === 'What to check')!;
    act(() => open.click());
    expect(host.textContent).toContain('relay.halleluja.design');
  });

  it('points at the relay card when there is no relay', () => {
    draw({ joinedAgo: 75, relayMode: 'off', connected: false });
    expect(host.textContent).toContain('Is the other instance on another network?');
  });

  it('shows all twelve words in a window with Copy beside Close', async () => {
    answer({ phrase: WORDS });
    draw({ joinedAgo: 75, passwordSet: false });
    await act(async () => tile('Nothing entered over there yet?').click());

    const slots = [...dialog()!.querySelectorAll('li[data-slot]')].map((li) => li.textContent);
    expect(slots).toEqual(WORDS.split(' ').map((w, i) => `${i + 1}${w}`));
    // The title names the words once; nothing inside repeats it.
    expect(dialog()!.textContent!.split('The twelve words').length - 1).toBe(1);
    const row = [...dialog()!.querySelectorAll('button')].map((b) => b.textContent);
    expect(row.slice(-2)).toEqual(['Close', 'Copy']);
  });

  it('lists this instance, the other instances and every phone, one row each', () => {
    draw({
      joinedAgo: 500,
      memberSeen: true,
      members: [{ id: 'b', name: 'office', direct: false, relay: true }],
      apps: [
        { id: 'p', name: 'Pixel 8', deployment: 'mobile', connected: true, lastSeen: 1 },
        { id: 'q', name: 'Old phone', deployment: 'mobile', connected: false, lastSeen: 1 },
      ],
    });
    const rows = [...host.querySelectorAll('[data-testid="members"] > li')].map((li) => li.textContent);
    expect(rows).toEqual(['nasThis instance', 'officeVia relay', 'Pixel 8Connected', 'Old phoneNot connected']);
    expect(host.textContent).not.toContain('This instance appears as');
    expect(host.textContent).not.toContain('In the group');
    // The row without a badge is as high as those with one.
    const heights = [...host.querySelectorAll('[data-testid="members"] > li')].map((li) => li.classList.contains('h-11'));
    expect(heights).toEqual([true, true, true, true]);
  });

  it('takes a phone out of the group only after the window asks', async () => {
    const calls = answer([]);
    draw({
      joinedAgo: 500,
      memberSeen: true,
      apps: [{ id: 'q', name: 'Old phone', deployment: 'mobile', connected: false, lastSeen: 1 }],
    });
    const bin = host.querySelector<HTMLButtonElement>('[aria-label="Remove Old phone"]')!;
    await act(async () => bin.click());
    expect(dialog()?.textContent).toContain('twelve words');
    expect(calls.some((c) => c.startsWith('DELETE'))).toBe(false);

    const confirm = [...dialog()!.querySelectorAll('button')].find((b) => b.textContent === 'Remove')!;
    await act(async () => confirm.click());
    expect(calls).toContain('DELETE /api/connect/apps/q');
    expect(dialog()).toBeNull();
  });

  it('leaves the relay to the relay card and ends the first line with the state', () => {
    for (const connected of [true, false]) {
      draw({ joinedAgo: 12, connected });
      expect(host.textContent).not.toMatch(/Connected|Not connected|No relay/);
      const state = host.querySelector('[data-testid="pair-state"]')!;
      expect(state.parentElement!.lastElementChild).toBe(state);
    }
    draw({ joinedAgo: 75 });
    const state = host.querySelector('[data-testid="pair-state"]')!;
    expect(state.textContent).toBe('Still alone');
    expect(state.parentElement!.lastElementChild).toBe(state);
  });

  it('says Paired only while another instance is there', () => {
    draw({ joinedAgo: 500, memberSeen: true, members: [{ id: 'b', name: 'office', direct: false, relay: true }] });
    expect(stage()).toBe('paired');
    expect(badge()).toContain('Paired');
    expect(host.textContent).toContain('office');

    draw({ joinedAgo: 500, memberSeen: true, members: [] });
    expect(badge()).toContain('Searching');
  });

  it('says Paired when the only other member is a connected phone', () => {
    draw({ joinedAgo: 500, memberSeen: true, apps: [{ id: 'p', name: 'Pixel 8', deployment: 'mobile', connected: true, lastSeen: 1 }] });
    expect(badge()).toContain('Paired');
    expect(host.textContent).toContain('Pixel 8');
  });
});
