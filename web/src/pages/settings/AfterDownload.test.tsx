// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError, type AfterDownloadState, type Category } from '../../lib/api';
import { I18nProvider } from '../../lib/i18n';
import { AfterDownloadRow } from './AfterDownload';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

const server = vi.hoisted(() => ({
  state: null as AfterDownloadState | null,
  create: vi.fn(),
}));

vi.mock('../../lib/api', async (actual) => ({
  ...(await actual<typeof import('../../lib/api')>()),
  fetchAfterDownload: () => Promise.resolve(server.state),
  fetchAfterDownloadDestinations: () =>
    Promise.resolve([
      { id: 'nas', name: 'NAS', kind: 'share', where: 'share /archive' },
      { id: 'usb', name: 'Backup disk', kind: 'disk', where: '/mnt/disks/backup' },
    ]),
  createAfterDownloadJob: (req: unknown) => server.create(req),
}));

const tower = {
  id: 'm-tower',
  device: 'Tower',
  jobs: [
    { name: 'Series archive', direction: 'both', mode: 'sync', paused: false, running: false, lastRun: { at: '2026-11-10T12:00:00Z', outcome: 'done' } },
    { name: 'Series to the NAS', direction: 'leftToRight', mode: 'move', paused: false, running: false },
    { name: 'Inbox', direction: 'rightToLeft', mode: 'move', paused: true, running: false },
  ],
} as const;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
  server.state = { fortress: true, engines: [structuredClone(tower) as never], last: {} };
  server.create.mockReset();
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  document.body.innerHTML = '';
});

async function mount(cat: Category) {
  const onChange = vi.fn();
  const draw = (c: Category) =>
    root.render(
      <I18nProvider>
        <MemoryRouter>
          <AfterDownloadRow cat={c} onChange={onChange} />
        </MemoryRouter>
      </I18nProvider>,
    );
  await act(async () => draw(cat));
  return { onChange, redraw: (c: Category) => act(async () => draw(c)) };
}

const trigger = () => host.querySelector<HTMLButtonElement>('button[aria-haspopup="listbox"]')!;
const options = () => [...document.querySelectorAll<HTMLButtonElement>('[role="listbox"] [role="option"]')];
const click = (el: Element) => act(async () => (el as HTMLElement).click());
// Values come into a sentence wrapped in Unicode isolates, which the reader never sees.
const words = (el: Element | null) => (el?.textContent ?? '').replace(/[⁦-⁩]/g, '');

describe('the After download station', () => {
  it('shows the chosen job on one line with its mode', async () => {
    await mount({ id: 'serien', afterDownload: { member: 'm-tower', device: 'Tower', job: 'Series archive' } });
    expect(trigger().textContent).toContain('Series archive');
    expect(trigger().textContent).toContain('Sync');
  });

  it('opens a card for every job and saves the one picked', async () => {
    const { onChange } = await mount({ id: 'serien' });
    await click(trigger());
    const names = options().map((o) => o.textContent);
    expect(names[0]).toContain('Start no job');
    expect(names.slice(1).map((n) => n?.split('ArrowLoop')[0])).toEqual(['Series archive', 'Series to the NAS', 'Inbox']);
    expect(options()[3].disabled).toBe(true);

    await click(options()[2]);
    expect(onChange).toHaveBeenCalledWith({ id: 'serien', afterDownload: { member: 'm-tower', device: 'Tower', job: 'Series to the NAS' } });
    expect(document.querySelector('[role="listbox"]')).toBeNull();
  });

  it('starts no job once that is picked', async () => {
    const { onChange } = await mount({ id: 'serien', afterDownload: { member: 'm-tower', job: 'Series archive' } });
    await click(trigger());
    await click(options()[0]);
    expect(onChange).toHaveBeenCalledWith({ id: 'serien' });
  });

  it('says that a moving job takes the files out of the folder', async () => {
    await mount({ id: 'serien', afterDownload: { member: 'm-tower', job: 'Series to the NAS' } });
    expect(host.textContent).toContain('This job moves the files');
  });

  it('says how the last hand-over went', async () => {
    server.state!.last = { serien: { at: '2026-11-10T12:00:00Z', package: 'Show.S02E05', job: 'Series archive', status: 'queued' } };
    await mount({ id: 'serien', afterDownload: { member: 'm-tower', device: 'Tower', job: 'Series archive' } });
    expect(host.textContent).toContain('Queued');
    expect(words(host)).toContain('Show.S02E05 came in while the job was running');
  });

  it('names a job that is gone from ArrowLoop', async () => {
    await mount({ id: 'serien', afterDownload: { member: 'm-tower', job: 'Old job' } });
    expect(words(trigger())).toContain('Old job (no longer there)');
  });

  it('asks to join a fortress first', async () => {
    server.state = { fortress: false, engines: [], last: {} };
    await mount({ id: 'serien' });
    expect(host.textContent).toContain('Join a fortress with this instance');
    expect(trigger()).toBeNull();
  });

  it('says when no ArrowLoop is in the fortress', async () => {
    server.state = { fortress: true, engines: [], last: {} };
    await mount({ id: 'serien' });
    expect(host.textContent).toContain('No ArrowLoop runs in your fortress yet');
  });
});

describe('a new job in ArrowLoop', () => {
  async function openWindow(cat: Category) {
    const mounted = await mount(cat);
    await click(trigger());
    const create = [...document.querySelectorAll<HTMLButtonElement>('[role="listbox"] button')].find((b) =>
      b.textContent?.includes('New job in ArrowLoop'),
    )!;
    await click(create);
    return mounted;
  }

  const dialog = () => document.querySelector<HTMLElement>('[role="dialog"]')!;
  const button = (words: string) =>
    [...dialog().querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent?.includes(words) || b.title === words)!;
  const typeName = (value: string) =>
    act(async () => {
      const input = dialog().querySelector('input')!;
      const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
      set.call(input, value);
      input.dispatchEvent(new Event('input', { bubbles: true }));
    });

  it('starts from the folder of the category and picks the new job', async () => {
    server.create.mockResolvedValue('Series to the archive');
    const { onChange } = await openWindow({ id: 'serien', dir: '/media/serien/<jd:packagename>' });
    expect(dialog().textContent).toContain('/media/serien');

    await typeName('Series to the archive');
    await click(button('NAS'));
    await click(button('Mirror'));
    await click(button('Create and pick'));

    expect(server.create).toHaveBeenCalledWith({ category: 'serien', member: 'm-tower', name: 'Series to the archive', target: 'nas', mode: 'mirror' });
    expect(onChange).toHaveBeenCalledWith({
      id: 'serien',
      dir: '/media/serien/<jd:packagename>',
      afterDownload: { member: 'm-tower', device: 'Tower', job: 'Series to the archive' },
    });
    expect(document.querySelector('[role="dialog"]')).toBeNull();
  });

  it('waits for a name and a target before it creates', async () => {
    await openWindow({ id: 'serien', dir: '/media/serien' });
    expect(button('Create and pick').disabled).toBe(true);
    await typeName('Series');
    expect(button('Create and pick').disabled).toBe(true);
    await click(button('NAS'));
    expect(button('Create and pick').disabled).toBe(false);
  });

  it('says why ArrowLoop refused it', async () => {
    server.create.mockRejectedValue(new ApiError('taken', 'afterDownload.nameTaken', undefined, 409));
    await openWindow({ id: 'serien', dir: '/media/serien' });
    await typeName('Series archive');
    await click(button('NAS'));
    await click(button('Create and pick'));
    expect(words(dialog())).toContain('ArrowLoop already has a job called "Series archive"');
  });
});
