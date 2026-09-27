// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { connectWS, fetchCaptchas, fetchQueue, setQueue, type Task } from '../lib/api';
import { showMain } from '../lib/desktop';
import { I18nProvider } from '../lib/i18n';
import { useTasks } from '../lib/useTasks';
import { TrayOverview } from './TrayOverview';

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  connectWS: vi.fn(() => () => {}),
  fetchCaptchas: vi.fn(async () => []),
  fetchQueue: vi.fn(async () => ({ halted: false, running: 0, quiet: false, limit: 0 })),
  setQueue: vi.fn(async (q: { halted: boolean }) => ({ halted: q.halted, running: 0, quiet: false, limit: 0 })),
}));
vi.mock('../lib/desktop', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/desktop')>()),
  showMain: vi.fn(async () => {}),
}));
vi.mock('../lib/useTasks', () => ({ useTasks: vi.fn(() => ({})) }));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

const task = (over: Partial<Task>): Task =>
  ({ id: 'a', url: 'https://files.example/a.zip', name: 'a.zip', status: 'running', speed: 0, loaded: 0, size: 0, createdAt: '2026-09-27T10:00:00Z', ...over }) as Task;

async function render() {
  await act(async () =>
    root.render(
      <I18nProvider>
        <TrayOverview />
      </I18nProvider>,
    ),
  );
}

function button(text: string) {
  return [...host.querySelectorAll('button')].find((b) => b.textContent?.includes(text));
}

/** The handler the page gave the live stream. */
function stream() {
  return vi.mocked(connectWS).mock.calls[0]![0];
}

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
});

describe('the tray window', () => {
  it('lists what downloads now and the latest of the rest', async () => {
    vi.mocked(useTasks).mockReturnValue({
      r: task({ id: 'r', name: 'running.iso', speed: 2048, loaded: 50, size: 100 }),
      d: task({ id: 'd', name: 'finished.zip', status: 'done' }),
      c: task({ id: 'c', name: 'collected.rar', status: 'collected' }),
    });
    await render();
    expect(host.textContent).toContain('running.iso');
    expect(host.textContent).toContain('finished.zip');
    expect(host.textContent).not.toContain('collected.rar');
  });

  it('says so when nothing downloads', async () => {
    await render();
    expect(host.textContent).toContain('Nothing is downloading');
  });

  it('stops the queue and offers to start it again', async () => {
    await render();
    await act(async () => button('Stop queue')!.click());
    expect(setQueue).toHaveBeenCalledWith({ halted: true });
    expect(button('Start queue')).toBeDefined();
    expect(host.textContent).toContain('Queue stopped');
  });

  it('follows a halt made elsewhere through the stream', async () => {
    await render();
    await act(async () => stream()('queue', { halted: true, running: 0, quiet: false, limit: 0 }));
    expect(button('Start queue')).toBeDefined();
  });

  it('counts the captchas waiting and leads to the main window for them', async () => {
    vi.mocked(fetchCaptchas).mockResolvedValueOnce([{ id: 'c1' }, { id: 'c2' }] as never);
    await render();
    expect(button('2 captcha(s) waiting')).toBeDefined();
    await act(async () => stream()('captchaResolved', { id: 'c1' }));
    const waiting = button('1 captcha(s) waiting')!;
    await act(async () => waiting.click());
    expect(showMain).toHaveBeenCalled();
  });

  it('is not counted as watching the captchas', async () => {
    await render();
    expect(vi.mocked(connectWS).mock.calls[0]![2]).toBeFalsy();
    expect(fetchQueue).toHaveBeenCalled();
  });
});
