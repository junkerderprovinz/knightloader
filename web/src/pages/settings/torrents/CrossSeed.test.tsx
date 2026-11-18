// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { en } from '../../../lib/locales/en';
import { SettingsProvider, type SettingsDraft } from '../context';
import { CROSS_SEED_DEFAULTS, CrossSeedCard, type CrossSeedSettings } from './CrossSeed';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let posts: unknown[];

const json = (body: unknown) => new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });

beforeEach(() => {
  posts = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path.startsWith('/api/torrents/crossseed/log')) {
        return Promise.resolve(
          json({
            entries: [
              { time: '2026-05-01T12:00:00Z', tracker: 'OPS', name: 'Album', infoHash: 'aa', result: 'injected' },
              {
                time: '2026-05-01T11:00:00Z',
                tracker: 'RED',
                name: 'Other',
                infoHash: 'bb',
                result: 'refused',
                reason: 'not whole',
              },
            ],
          }),
        );
      }
      if (path === '/api/torrents/crossseed/test') {
        posts.push(JSON.parse(String(init?.body)));
        return Promise.resolve(json({ ok: true, account: 'listener' }));
      }
      return Promise.resolve(json({}));
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

async function show(c: CrossSeedSettings, onChange: (fields: Partial<CrossSeedSettings>) => void = () => {}) {
  const draft = {
    cfg: {},
    saved: {},
    patch: () => {},
    fieldError: () => undefined,
    showRefusalsAt: () => () => {},
  } as unknown as SettingsDraft;
  const features = { features: { modules: [], pages: [] }, toggle: async () => {} };
  await act(async () =>
    root.render(
      <SettingsProvider draft={draft} features={features}>
        <CrossSeedCard hue={6} c={c} onChange={onChange} />
      </SettingsProvider>,
    ),
  );
}

const on: CrossSeedSettings = {
  ...CROSS_SEED_DEFAULTS,
  enabled: true,
  red: { apiKey: '********', announce: '********' },
  indexers: [{ name: 'Prowlarr', url: 'http://prowlarr:9696/1/api', apiKey: '********' }],
};

describe('the cross-seed card', () => {
  it('shows nothing but the switch while cross-seeding is off', async () => {
    await show(CROSS_SEED_DEFAULTS);
    expect(host.textContent).toContain(en['settings.torrents.crossSeedOn']);
    expect(host.textContent).not.toContain(en['settings.torrents.crossSeedIndexers']);
  });

  it('keeps a stored key out of its box and says it is stored', async () => {
    await show(on);
    const secrets = [...host.querySelectorAll<HTMLInputElement>('input[type="password"]')];
    expect(secrets.every((i) => i.value === '')).toBe(true);
    expect(secrets.filter((i) => i.placeholder === en['settings.torrents.crossSeedStored'])).toHaveLength(3);
  });

  it('adds an indexer as an empty row', async () => {
    const onChange = vi.fn();
    await show(on, onChange);
    const add = [...host.querySelectorAll('button')].find(
      (b) => b.textContent === en['settings.torrents.crossSeedIndexerAdd'],
    )!;
    act(() => add.click());
    expect(onChange).toHaveBeenCalledWith({
      indexers: [...on.indexers!, { name: '', url: '', apiKey: '' }],
    });
  });

  it('tests a key with the settings as edited and says whose it is', async () => {
    await show(on);
    const test = [...host.querySelectorAll('button')].find((b) =>
      b.textContent?.startsWith(en['settings.torrents.crossSeedTestKey']),
    )!;
    await act(async () => {
      test.click();
      await new Promise((r) => setTimeout(r, 20));
    });
    expect(posts).toEqual([{ what: 'red', index: 0, crossSeed: on }]);
    // Without the language provider the sentence keeps its placeholder.
    expect(host.textContent).toContain(en['settings.torrents.crossSeedTestOkAccount']);
  });

  it('shows the folder shared with the daemon under its address', async () => {
    await show({ ...on, daemon: { url: 'http://cross-seed:2468', apiKey: '', folder: '/mnt/cross-seed' } });
    expect(host.textContent).toContain(en['settings.torrents.crossSeedDaemonFolder']);
    const values = [...host.querySelectorAll('input')].map((i) => i.value);
    expect(values).toContain('/mnt/cross-seed');
  });

  it('lists the log newest first with each result and reason', async () => {
    await show(on);
    const rows = [...host.querySelectorAll('li')].filter(
      (li) => li.textContent?.includes('Album') || li.textContent?.includes('Other'),
    );
    expect(rows.map((r) => r.textContent)).toEqual([
      expect.stringContaining(en['settings.torrents.crossSeedResult.injected']),
      expect.stringContaining('not whole'),
    ]);
  });

  it('writes each log entry as one line of a log, the reason beside it', async () => {
    await show(on);
    const rows = [...host.querySelectorAll('li')].filter((li) => li.textContent?.includes('not whole'));
    expect(rows).toHaveLength(1);
    expect(rows[0].querySelector('p')).toBeNull();
    expect(rows[0].closest('ul')!.className).toContain('font-mono');
  });
});
