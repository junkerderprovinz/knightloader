// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { FeedSubscription, Settings } from '../../../lib/api';
import { I18nProvider } from '../../../lib/i18n';
import { SettingsProvider, type SettingsDraft } from '../context';
import { FeedsCard } from './Feeds';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

let root: Root;
let host: HTMLDivElement;
/** The draft's subscriptions after the last patch. */
let stored: FeedSubscription[];
/** What POST /api/feeds/resolve answers, by the address it is asked about. */
let resolved: Record<string, unknown>;
/** The body of the last POST /api/feeds/test. */
let tested: unknown;
let testAnswer: unknown;
/** What GET /api/feeds answers; unanswered while undefined. */
let feedTable: unknown;

const OPTIONS = {
  ytdlpQualities: ['best', '1080p', '720p'],
  ytdlpVideoFormats: ['best', 'mp4'],
  ytdlpAudioFormats: ['best', 'mp3'],
  ytdlpAudioBitrates: ['', '192'],
};

const reply = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status }));

beforeEach(() => {
  resolved = {};
  tested = undefined;
  testAnswer = { title: '', entries: [], total: 0, matched: 0 };
  feedTable = undefined;
  vi.stubGlobal(
    'fetch',
    vi.fn((input: string, init?: RequestInit) => {
      const path = new URL(input, 'http://localhost').pathname;
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      if (path === '/api/feeds/resolve') {
        const answer = resolved[body.url];
        if (answer) return reply(answer);
        if (body.url === 'https://nothing.example/') {
          return reply({ error: 'dial tcp: lookup nothing.example: no such host', code: 'feedNotFound' }, 400);
        }
        return reply({ error: 'No feed was found at this address.' }, 400);
      }
      if (path === '/api/feeds/test') {
        tested = body;
        return reply(testAnswer);
      }
      if (path === '/api/options') return reply(OPTIONS);
      if (path === '/api/feeds' && feedTable) return reply(feedTable);
      // The feed table and the priority ladder are left unanswered.
      return new Promise(() => {});
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

function Harness({ feeds }: { feeds: FeedSubscription[] }) {
  const [cfg, setCfg] = useState({ feeds } as unknown as Settings);
  stored = cfg.feeds ?? [];
  const draft = {
    cfg,
    patch: (fields: Partial<Settings>) => setCfg((c) => ({ ...c, ...fields })),
    fieldError: () => undefined,
    showRefusalsAt: () => () => {},
  } as unknown as SettingsDraft;
  return (
    <SettingsProvider draft={draft} features={{ features: { modules: [], pages: [] }, toggle: async () => {} }}>
      <FeedsCard hue={0} />
    </SettingsProvider>
  );
}

function mount(feeds: FeedSubscription[] = []) {
  act(() =>
    root.render(
      <I18nProvider>
        <Harness feeds={feeds} />
      </I18nProvider>,
    ),
  );
}

const button = (name: string) =>
  [...document.querySelectorAll('button')].find((b) => b.textContent === name || b.getAttribute('aria-label') === name);

function click(name: string) {
  const b = button(name);
  if (!b) throw new Error(`no button "${name}"`);
  act(() => b.click());
}

/** A button inside the open window, apart from the card's own of the same name. */
function inDialog(name: string): HTMLButtonElement {
  const found = [...document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button')].find(
    (b) => b.textContent === name,
  );
  if (!found) throw new Error(`no button "${name}" in the window`);
  return found;
}

/** Adds a row and opens its window. */
function addRow() {
  mount();
  click('Add subscription');
}

/** Opens the window of the stored row at `index`. */
function openRow(index: number) {
  const rows = host.querySelectorAll('li button[aria-haspopup="dialog"]');
  act(() => (rows[index] as HTMLButtonElement).click());
}

/** The input of the row whose caption reads `caption`, in the open window. */
function field(caption: string): HTMLInputElement {
  const label = [...document.querySelectorAll('label')].find((l) => l.textContent?.startsWith(caption));
  if (!label) throw new Error(`no field "${caption}"`);
  return label.querySelector('input')!;
}

function type(input: HTMLInputElement, text: string) {
  // React tracks the value it last rendered, so the native setter is what
  // makes the input event count as a change.
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, text);
  act(() => input.dispatchEvent(new Event('input', { bubbles: true })));
}

function typeAndLeave(input: HTMLInputElement, text: string) {
  type(input, text);
  act(() => input.dispatchEvent(new FocusEvent('focusout', { bubbles: true })));
}

async function paste(input: HTMLInputElement, text: string) {
  const event = new Event('paste', { bubbles: true }) as Event & { clipboardData: unknown };
  event.clipboardData = { getData: () => text };
  act(() => input.dispatchEvent(event));
  type(input, text);
  await act(async () => {});
}

/** The line the field names in aria-describedby. */
function reason(input: HTMLInputElement): string | undefined {
  const id = input.getAttribute('aria-describedby');
  return id ? (document.getElementById(id)?.textContent ?? undefined) : undefined;
}

/** A segment by its words, within the strip named `strip` where two strips share them. */
function tab(name: string, strip?: string): HTMLButtonElement {
  const scope = strip ? document.querySelector(`[role="tablist"][aria-label="${strip}"]`) : document;
  if (!scope) throw new Error(`no strip "${strip}"`);
  const found = [...scope.querySelectorAll<HTMLButtonElement>('[role="tab"]')].find((b) => b.textContent === name);
  if (!found) throw new Error(`no tab "${name}"`);
  return found;
}

function switchOf(name: string): HTMLButtonElement {
  const found = [...document.querySelectorAll<HTMLButtonElement>('[role="switch"]')].find(
    (b) => b.getAttribute('aria-label') === name || b.textContent === name,
  );
  if (!found) throw new Error(`no switch "${name}"`);
  return found;
}

it('says under the address why it cannot be used', () => {
  addRow();
  const url = field('Feed address');
  typeAndLeave(url, 'example.org/feed.xml');
  expect(url.getAttribute('aria-invalid')).toBe('true');
  expect(reason(url)).toBe('Use a full address that starts with http:// or https://, like https://example.org/feed.xml.');
});

it('says under the title filter that the pattern does not compile', () => {
  addRow();
  const filter = field('Title filter');
  typeAndLeave(filter, '(1080p');
  expect(filter.getAttribute('aria-invalid')).toBe('true');
  expect(reason(filter)).toBe('This is not a valid regular expression, so the filter is not saved.');
});

it('takes the reason away once the address is typed again', () => {
  addRow();
  const url = field('Feed address');
  typeAndLeave(url, 'example.org/feed.xml');
  type(url, 'https://example.org/feed.xml');
  expect(url.getAttribute('aria-invalid')).toBe('false');
  expect(reason(url)).toBeUndefined();
});

it('keeps an exclude pattern that does not compile out of the draft', () => {
  mount([{ url: 'https://example.org/feed.xml', intervalMinutes: 0 }]);
  openRow(0);
  const exclude = field('Exclude filter');
  typeAndLeave(exclude, '[trailer');
  expect(exclude.getAttribute('aria-invalid')).toBe('true');
  expect(stored[0].excludeFilter).toBeUndefined();
  typeAndLeave(exclude, 'trailer|teaser');
  expect(stored[0].excludeFilter).toBe('trailer|teaser');
});

it('refuses a shortest length above the longest', () => {
  mount([{ url: 'https://example.org/feed.xml', intervalMinutes: 0, maxMinutes: 10 }]);
  openRow(0);
  const shortest = field('Shortest (minutes)');
  typeAndLeave(shortest, '30');
  expect(reason(shortest)).toBe('This is longer than the longest length, so nothing would be taken. It is not saved.');
  expect(stored[0].minMinutes).toBeUndefined();
  typeAndLeave(shortest, '5');
  expect(stored[0].minMinutes).toBe(5);
});

it('subscribes a pasted channel to its feed', async () => {
  resolved['https://www.youtube.com/@kurzgesagt'] = {
    kind: 'rss',
    url: 'https://www.youtube.com/feeds/videos.xml?channel_id=UCsXVk37bltHxD1rDPwtNM8Q',
    title: 'Kurzgesagt',
    channel: { kind: 'channel', url: 'https://www.youtube.com/@kurzgesagt', title: 'Kurzgesagt' },
  };
  addRow();
  await paste(field('Feed address'), 'https://www.youtube.com/@kurzgesagt');
  click('New videos only');
  expect(stored).toEqual([
    {
      url: 'https://www.youtube.com/feeds/videos.xml?channel_id=UCsXVk37bltHxD1rDPwtNM8Q',
      intervalMinutes: 0,
      kind: 'rss',
      title: 'Kurzgesagt',
    },
  ]);
});

it('holds a pasted channel back until its older videos are chosen', async () => {
  resolved['https://www.youtube.com/@kurzgesagt'] = {
    kind: 'rss',
    url: 'https://www.youtube.com/feeds/videos.xml?channel_id=UCsXVk37bltHxD1rDPwtNM8Q',
    title: 'Kurzgesagt',
    channel: { kind: 'channel', url: 'https://www.youtube.com/@kurzgesagt', title: 'Kurzgesagt' },
  };
  addRow();
  await paste(field('Feed address'), 'https://www.youtube.com/@kurzgesagt');
  click('Older videos too');
  // The first check is the only one a backfill applies to, so the row waits.
  expect(stored).toEqual([]);
  expect(tab('Last videos').getAttribute('aria-selected')).toBe('true');
  expect(field('How many videos').value).toBe('10');
  typeAndLeave(field('How many videos'), '25');
  expect(stored).toEqual([]);
  act(() => inDialog('Add subscription').click());
  expect(stored[0]).toMatchObject({
    kind: 'channel',
    url: 'https://www.youtube.com/@kurzgesagt',
    title: 'Kurzgesagt',
    backfill: { mode: 'count', n: 25 },
  });
});

it('holds a typed channel back when its address is left', () => {
  addRow();
  act(() => tab('Channel or playlist').click());
  typeAndLeave(field('Channel or playlist address'), 'https://www.youtube.com/@kurzgesagt');
  expect(stored).toEqual([]);
  act(() => tab('None').click());
  act(() => inDialog('Add subscription').click());
  expect(stored).toHaveLength(1);
  expect(stored[0].kind).toBe('channel');
  expect(stored[0].backfill).toBeUndefined();
});

it('says why a held channel cannot be added', () => {
  addRow();
  act(() => tab('Channel or playlist').click());
  const url = field('Channel or playlist address');
  typeAndLeave(url, 'youtube.com/@kurzgesagt');
  act(() => inDialog('Add subscription').click());
  expect(stored).toEqual([]);
  expect(reason(url)).toBe('Use a full address that starts with http:// or https://, like https://example.org/feed.xml.');
});

it('offers to subscribe to a pasted Mediathek search', async () => {
  const search = 'https://mediathekviewweb.de/#query=%23tatort%20%3E80';
  resolved[search] = {
    kind: 'mediathek',
    title: 'Tatort',
    minMinutes: 80,
    mediathek: { channel: '', topic: 'Tatort', title: '', quality: 'hd', subtitles: false },
  };
  addRow();
  await paste(field('Feed address'), search);
  expect(stored).toEqual([]);
  click('Subscribe to this search');
  expect(stored[0]).toMatchObject({
    url: search,
    kind: 'mediathek',
    title: 'Tatort',
    minMinutes: 80,
    mediathek: { topic: 'Tatort', quality: 'hd' },
  });
});

it('says why a pasted address leads nowhere', async () => {
  addRow();
  const url = field('Feed address');
  await paste(url, 'https://example.org/nothing');
  expect(reason(url)).toBe('No feed was found at this address.');
});

it('says in its own words that a pasted address holds nothing, with the reason beneath', async () => {
  addRow();
  const url = field('Feed address');
  await paste(url, 'https://nothing.example/');
  const said = document.getElementById(url.getAttribute('aria-describedby')!)!;
  const detail = said.querySelector('[data-detail]');
  expect(detail?.textContent).toBe('dial tcp: lookup nothing.example: no such host');
  expect(said.textContent?.replace(detail!.textContent!, '')).toBe(
    'There is nothing to subscribe to at this address. Check it, or paste the address of a feed, a YouTube channel or a MediathekViewWeb search.',
  );
});

it('keeps a Mediathek search that names nothing out of the draft', () => {
  mount([
    {
      url: 'https://mediathekviewweb.de/#query=tatort',
      intervalMinutes: 0,
      kind: 'mediathek',
      mediathek: { channel: '', topic: 'Tatort', title: '', quality: 'hd', subtitles: false },
    },
  ]);
  openRow(0);
  const topic = field('Topic');
  typeAndLeave(topic, '');
  expect(reason(topic)).toBe('Fill in the channel, the topic or the title, or the search would take everything.');
  expect(stored[0].mediathek?.topic).toBe('Tatort');
  typeAndLeave(field('Channel'), 'ARD');
  expect(stored[0].mediathek).toMatchObject({ channel: 'ARD', topic: '' });
});

it('fetches the last videos of a channel on the first check', () => {
  mount([{ url: 'https://www.youtube.com/@kurzgesagt', intervalMinutes: 0, kind: 'channel' }]);
  openRow(0);
  act(() => tab('Last videos').click());
  expect(stored[0].backfill).toEqual({ mode: 'count', n: 10 });
  typeAndLeave(field('How many videos'), '5');
  expect(stored[0].backfill).toEqual({ mode: 'count', n: 5 });
  act(() => tab('None').click());
  expect(stored[0].backfill).toBeUndefined();
});

it('starts nothing on its own until switched on', () => {
  mount([{ url: 'https://example.org/feed.xml', intervalMinutes: 0 }]);
  openRow(0);
  const auto = switchOf('Starts on its own');
  expect(auto.getAttribute('aria-checked')).toBe('false');
  act(() => auto.click());
  expect(stored[0].autoStart).toBe(true);
});

it('takes what was downloaded before only when asked to', () => {
  mount([{ url: 'https://example.org/feed.xml', intervalMinutes: 0 }]);
  openRow(0);
  const again = switchOf('Also take what was downloaded before');
  expect(again.getAttribute('aria-checked')).toBe('false');
  act(() => again.click());
  expect(stored[0].allowDownloaded).toBe(true);
});

it('fetches audio only when that is chosen', async () => {
  mount([{ url: 'https://www.youtube.com/@kurzgesagt', intervalMinutes: 0, kind: 'channel' }]);
  openRow(0);
  await act(async () => {});
  act(() => tab('Audio only').click());
  expect(stored[0].media?.variants).toEqual(['audio']);
  act(() => tab('Site preset').click());
  expect(stored[0].media).toBeUndefined();
});

it('cuts sponsor segments out for this subscription alone', () => {
  mount([{ url: 'https://www.youtube.com/@kurzgesagt', intervalMinutes: 0, kind: 'channel' }]);
  openRow(0);
  act(() => tab('Own choice', 'SponsorBlock').click());
  expect(stored[0].sponsorBlock).toEqual({ remove: [], mark: [] });
  act(() => tab('Cut out', 'Sponsor').click());
  expect(stored[0].sponsorBlock).toEqual({ remove: ['sponsor'], mark: [] });
});

it('names the packages after the channel and lays the files out as a series', () => {
  mount([{ url: 'https://www.youtube.com/@kurzgesagt', intervalMinutes: 0, kind: 'channel' }]);
  openRow(0);
  act(() => tab('Channel or show').click());
  act(() => tab('Series').click());
  expect(stored[0]).toMatchObject({ naming: 'channel', library: 'series' });
});

it('tests the whole row and says why an entry is left out', async () => {
  testAnswer = {
    title: 'Kurzgesagt',
    total: 2,
    matched: 1,
    entries: [
      { title: 'The Black Hole', link: 'https://youtu.be/a', matches: true, duration: 754 },
      { title: 'Short teaser', link: 'https://youtu.be/b', matches: false, duration: 40, reason: 'short' },
    ],
  };
  const row: FeedSubscription = { url: 'https://example.org/feed.xml', intervalMinutes: 0, minMinutes: 2 };
  mount([row]);
  openRow(0);
  click('Test');
  await act(async () => {});
  expect(tested).toMatchObject(row);
  const text = document.body.textContent ?? '';
  expect(text).toContain('12:34');
  expect(text).toContain('Too short');
});

it('marks a row that starts on its own', () => {
  mount([{ url: 'https://example.org/feed.xml', intervalMinutes: 0, autoStart: true }]);
  expect(host.querySelector('[role="img"][aria-label="Starts on its own"]')).not.toBeNull();
});

it('shows a row by its name', () => {
  mount([{ url: 'https://www.youtube.com/@kurzgesagt', intervalMinutes: 0, kind: 'channel', title: 'Kurzgesagt' }]);
  expect(host.querySelector('li')?.textContent).toContain('Kurzgesagt');
});

it('counts an entry downloaded before as left out, even without a filter', async () => {
  testAnswer = {
    title: 'jawed',
    total: 1,
    matched: 0,
    entries: [{ title: 'Me at the zoo', link: 'https://youtu.be/jNQXAC9IVRw', matches: false, duration: 19, reason: 'downloaded' }],
  };
  mount([{ url: 'https://www.youtube.com/@jawed', intervalMinutes: 0, kind: 'channel' }]);
  openRow(0);
  click('Test');
  await act(async () => {});
  const text = document.body.textContent ?? '';
  expect(text).not.toContain('No title filter');
  expect(text).toContain('0 of 1 entries are taken');
  expect(text).toContain('Nothing in this feed is taken right now.');
});

it('titles the card for every kind of subscription', () => {
  mount();
  expect(host.textContent).toContain('Subscriptions');
  expect(host.textContent).not.toContain('Feed subscriptions');
});

it('counts one remembered entry in the singular', async () => {
  feedTable = [
    {
      url: 'https://example.org/feed.xml',
      polling: true,
      lastPolledAt: '2026-11-27T08:00:00Z',
      seeded: true,
      remembered: 1,
    },
  ];
  mount([{ url: 'https://example.org/feed.xml', intervalMinutes: 0 }]);
  await act(async () => {});
  openRow(0);
  expect(document.body.textContent).toContain('1 entry remembered');
});
