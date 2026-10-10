// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { showCaptcha } from '../components/CaptchaModal';
import { Sidebar } from '../components/Sidebar';
import {
  fetchAccounts,
  fetchCaptchas,
  fetchHealthReport,
  restartTasks,
  type Account,
  type CaptchaChallenge,
  type HealthReport,
  type Task,
} from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { usePhoneLayout } from '../lib/phoneLayout';
import { writeUIState } from '../lib/uistate';
import { useTasks } from '../lib/useTasks';
import { Dashboard } from './Dashboard';

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  connectWS: vi.fn(() => () => {}),
  fetchAuth: vi.fn(async () => ({ enabled: false, authenticated: true })),
  fetchSettings: vi.fn(async () => ({ instanceName: 'Home', speedLimit: 0, diskLowSpace: 10, diskCriticalSpace: 5 })),
  fetchInstances: vi.fn(async () => []),
  fetchCaptchas: vi.fn(async () => []),
  fetchHealthReport: vi.fn(async () => ({ status: 'ok', subsystems: [], volumes: [] })),
  fetchAccounts: vi.fn(async () => []),
  fetchAccountCatalogue: vi.fn(async () => []),
  fetchHosterLogins: vi.fn(async () => []),
  fetchVolumeUsage: vi.fn(async () => ({ used: 0, cap: 0, action: 'report', throttle: 0, periodStart: '', periodEnd: '', reached: false })),
  restartTasks: vi.fn(async () => new Response('{}')),
}));
vi.mock('../lib/useTasks', () => ({ useTasks: vi.fn(() => ({})) }));
vi.mock('../lib/phoneLayout', () => ({ usePhoneLayout: vi.fn(() => false), useTabletLayout: vi.fn(() => false) }));
// The cards that read the server on their own are stood in for by their names.
vi.mock('../components/SpeedGraph', () => ({ SpeedGraph: () => <div>speed graph</div> }));
vi.mock('../components/VolumeCard', () => ({ VolumeCard: () => <div>volume card</div> }));
vi.mock('../components/DiskSpaceTile', () => ({ DiskSpaceTile: () => <div>disk card</div> }));
vi.mock('../components/TorrentCard', () => ({ TorrentCard: () => null }));
vi.mock('../components/InstanceCard', () => ({ InstanceRow: ({ name }: { name: string }) => <div>{name}</div> }));
vi.mock('../components/EventBell', () => ({ EventBell: () => null }));
vi.mock('../components/CaptchaModal', () => ({ showCaptcha: vi.fn() }));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
// jsdom lays nothing out and has no ResizeObserver, which the width selector
// measures its segments with.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
// The interface state is read and written through the real module.
vi.stubGlobal(
  'fetch',
  vi.fn(async () => new Response('{}')),
);

let root: Root;
let host: HTMLDivElement;

const task = (over: Partial<Task>): Task =>
  ({
    id: 'a',
    url: 'https://files.example/a.zip',
    name: 'a.zip',
    package: '',
    resolver: 'http',
    status: 'done',
    speed: 0,
    loaded: 100,
    size: 100,
    enabled: true,
    nextTry: '0001-01-01T00:00:00Z',
    createdAt: '2026-10-10T10:00:00Z',
    ...over,
  }) as Task;

async function render(withRail = false) {
  await act(async () =>
    root.render(
      <MemoryRouter>
        <I18nProvider>
          {withRail && <Sidebar />}
          <main>
            <Dashboard />
          </main>
        </I18nProvider>
      </MemoryRouter>,
    ),
  );
}

const main = () => host.querySelector('main')!;
const heading = () => main().querySelector('h2')!.textContent;
const needRows = () => [...main().querySelectorAll('[data-need]')];
const button = (text: string, within: Element = main()) =>
  [...within.querySelectorAll('button')].find((b) => (b.textContent || b.getAttribute('aria-label') || '').includes(text));
/** The cards on show, in the order they are drawn, with the columns each takes. */
const cards = () =>
  [...main().querySelectorAll<HTMLElement>('[data-ov-card]')]
    .filter((el) => !el.classList.contains('hidden'))
    .map((el) => [el.dataset.ovCard, el.style.gridColumn] as const);

beforeEach(() => {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.clearAllMocks();
  vi.mocked(useTasks).mockReturnValue({});
  vi.mocked(usePhoneLayout).mockReturnValue(false);
  vi.mocked(fetchCaptchas).mockResolvedValue([]);
  vi.mocked(fetchAccounts).mockResolvedValue([]);
  vi.mocked(fetchHealthReport).mockResolvedValue({ status: 'ok', subsystems: [], volumes: [] } as unknown as HealthReport);
  writeUIState('overview.cards', undefined);
  writeUIState('overview.needsDone', undefined);
});

describe('the head of the overview', () => {
  it('says that nothing is in the list yet', async () => {
    await render();
    expect(heading()).toBe('No downloads yet.');
    expect(needRows()).toHaveLength(0);
  });

  it('says that everything is downloaded only when nothing runs, waits or needs the person', async () => {
    vi.mocked(useTasks).mockReturnValue({ a: task({}) });
    await render();
    expect(heading()).toBe('Everything is downloaded');
  });

  it('counts the running downloads and gives the speed, the time left and the queue', async () => {
    vi.mocked(useTasks).mockReturnValue({
      a: task({ status: 'running', speed: 1024, loaded: 0, size: 61440 }),
      b: task({ id: 'b', status: 'queued', loaded: 0, size: 0 }),
    });
    await render();
    expect(heading()).toBe('1 download is running');
    const line = main().querySelector('h2 + p')!.textContent!;
    expect(line).toContain('KiB/s');
    expect(line).toContain('1min left');
    expect(line).toContain('1 in the queue');
    // While the queue runs and nothing waits for the person, the ring is its progress.
    expect(main().querySelector('.kl-ring-progress')).not.toBeNull();
    expect(main().querySelector('.kl-ring')!.textContent).toBe('0%');
  });

  it('draws one ring segment per kind of download while the queue rests', async () => {
    vi.mocked(useTasks).mockReturnValue({
      a: task({ resolver: 'jd' }),
      b: task({ id: 'b', resolver: 'torrent' }),
      c: task({ id: 'c', resolver: 'torrent', status: 'queued' }),
    });
    await render();
    expect(main().querySelectorAll('.kl-ring-seg')).toHaveLength(2);
    expect(main().querySelector('.kl-ring')!.getAttribute('aria-label')).toBe(
      'JDownloader: Done, Built-in torrent client: Queued',
    );
  });
});

describe('what needs the person', () => {
  const failed = { a: task({ status: 'error', gaveUp: true, name: 'broken.zip', errorCode: 'gone' }), b: task({ id: 'b' }) };

  it('shows the same count in the head, on the rows and on the rail', async () => {
    vi.mocked(useTasks).mockReturnValue(failed);
    vi.mocked(fetchHealthReport).mockResolvedValue({
      status: 'failed',
      subsystems: [{ id: 'jd', state: 'failed', remedy: 'jd.unreachable' }],
      volumes: [],
    } as unknown as HealthReport);
    await render(true);
    expect(heading()).toBe('2 things need you');
    expect(needRows()).toHaveLength(2);
    expect(main().querySelector('.kl-ring')!.textContent).toBe('2');
    const rail = host.querySelector('nav a[href="/"]')!;
    expect(rail.querySelector('.kl-count')!.textContent).toBe('2');
  });

  it('retries a failed download from its row and says why it failed', async () => {
    vi.mocked(useTasks).mockReturnValue(failed);
    await render();
    const row = needRows()[0];
    expect(row.textContent).toContain('broken.zip');
    expect(row.textContent).toContain('The source says this is not there any more.');
    await act(async () => button('Try again', row)!.click());
    expect(restartTasks).toHaveBeenCalledWith(['a']);
  });

  it('sets a row aside with Done, and the count follows', async () => {
    vi.mocked(useTasks).mockReturnValue(failed);
    await render(true);
    await act(async () => button('Done', needRows()[0])!.click());
    expect(needRows()).toHaveLength(0);
    expect(heading()).toBe('Everything is downloaded');
    expect(host.querySelector('nav a[href="/"] .kl-count')).toBeNull();
  });

  it('brings a waiting captcha to the front from its row', async () => {
    vi.mocked(fetchCaptchas).mockResolvedValue([
      { id: 'c1', source: 'jd', host: 'files.example', kind: 'image', expiresAt: '0001-01-01T00:00:00Z' } as CaptchaChallenge,
    ]);
    await render();
    expect(heading()).toBe('1 thing needs you');
    expect(needRows()[0].textContent).toContain('Captcha for files.example');
    expect(button('Done', needRows()[0])).toBeUndefined();
    await act(async () => button('Solve')!.click());
    expect(showCaptcha).toHaveBeenCalledWith('c1');
  });

  it('lists an account that stopped working', async () => {
    vi.mocked(fetchAccounts).mockResolvedValue([
      { id: 'x', service: 'realdebrid', account: '', label: '', enabled: true, configured: true, ok: false, detail: 'bad token' } as Account,
    ]);
    await render();
    expect(needRows()[0].textContent).toContain('realdebrid is not working');
    expect(needRows()[0].textContent).toContain('bad token');
  });
});

describe('the cards of the overview', () => {
  it('opens on the cards that have something to show, each row filled', async () => {
    await render();
    // Nothing needs the person, so that card gives its place up.
    expect(cards()).toEqual([
      ['speed', 'span 6'],
      ['recent', 'span 3'],
      ['disk', 'span 3'],
    ]);
  });

  it('hides a card, lists it under the hidden ones and shows it again', async () => {
    await render();
    await act(async () => button('Customize')!.click());
    const recent = main().querySelector('[data-ov-card="recent"]')!;
    await act(async () => button('Hide', recent)!.click());
    expect(cards().map(([id]) => id)).not.toContain('recent');
    await act(async () => button('Recent')!.click());
    expect(cards().map(([id]) => id)).toContain('recent');
  });

  it('keeps a place for a card with nothing to show while the cards are arranged', async () => {
    await render();
    await act(async () => button('Customize')!.click());
    await act(async () => button('Torrents')!.click());
    expect(main().querySelector('[data-ov-card="torrents"]')!.textContent).toContain('Nothing to show right now.');
    await act(async () => button('Done')!.click());
    expect(cards().map(([id]) => id)).not.toContain('torrents');
  });

  it('moves a card and sets its width', async () => {
    await render();
    await act(async () => button('Customize')!.click());
    const disk = main().querySelector('[data-ov-card="disk"]')!;
    await act(async () => button('Move up', disk)!.click());
    expect(cards().map(([id]) => id)).toEqual(['needs', 'speed', 'disk', 'recent']);
    await act(async () => button('⅔', disk)!.click());
    expect(cards().slice(2)).toEqual([
      ['disk', 'span 6'],
      ['recent', 'span 6'],
    ]);
  });

  it('goes back to the default arrangement on Reset', async () => {
    await render();
    await act(async () => button('Customize')!.click());
    await act(async () => button('Hide', main().querySelector('[data-ov-card="disk"]')!)!.click());
    await act(async () => button('Reset')!.click());
    expect(cards().map(([id]) => id)).toEqual(['needs', 'speed', 'recent', 'disk']);
  });

  it('keeps one order on a phone, with what needs the person first', async () => {
    vi.mocked(usePhoneLayout).mockReturnValue(true);
    vi.mocked(useTasks).mockReturnValue({ a: task({ status: 'error', gaveUp: true }) });
    writeUIState('overview.cards', { order: ['disk', 'recent', 'needs', 'speed'], hidden: [], widths: { disk: 2 } });
    await render();
    expect(cards()).toEqual([
      ['needs', ''],
      ['speed', ''],
      ['recent', ''],
      ['disk', ''],
      ['instances', ''],
      ['volume', ''],
    ]);
  });
});
