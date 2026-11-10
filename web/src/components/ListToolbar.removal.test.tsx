// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Task } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { ToastProvider } from '../lib/toast';
import { useRemoval } from './ListToolbar';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
/** What the server says a removal with files takes of what unpacking wrote. */
let unpacked: Response;

const answer = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

const fetchMock = vi.fn((url: string, _init?: RequestInit) =>
  Promise.resolve(url.endsWith('/tasks/unpacked-weight') ? unpacked : answer({})),
);

beforeEach(() => {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
  unpacked = answer({ files: 0, bytes: 0 });
  fetchMock.mockClear();
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const task = { id: 't1', name: 'film.mkv', loaded: 4096 } as Task;

function Harness({ withFiles, all, ids }: { withFiles: boolean; all: Task[]; ids: string[] }) {
  const removal = useRemoval({ all, selected: new Set(ids), base: '/api', onDone: () => {} });
  return (
    <>
      <button type="button" data-ask onClick={() => removal.ask(ids, withFiles)} />
      {removal.dialog}
    </>
  );
}

async function openDialog(withFiles = false, all: Task[] = [task], ids: string[] = [task.id]) {
  await act(async () =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <Harness withFiles={withFiles} all={all} ids={ids} />
        </ToastProvider>
      </I18nProvider>,
    ),
  );
  await act(async () => host.querySelector<HTMLButtonElement>('[data-ask]')!.click());
  return document.body.querySelector('[role="dialog"]')!;
}

const buttons = (el: Element) => [...el.querySelectorAll('button')].map((b) => b.textContent);

const filesSwitch = (el: Element) => el.querySelector<HTMLButtonElement>('[role="switch"]')!;

describe('the removal dialog', () => {
  it('keeps the files unless the switch is turned on', async () => {
    const dialog = await openDialog();
    expect(filesSwitch(dialog).getAttribute('aria-checked')).toBe('false');
    expect(buttons(dialog)).toContain('Remove from the list');
    expect(buttons(dialog)).not.toContain('Remove and delete the files');
    expect(dialog.textContent).not.toContain('cannot be undone');
  });

  it('deletes the files once the switch is on, and says how much', async () => {
    const dialog = await openDialog();
    await act(async () => filesSwitch(dialog).click());
    expect(buttons(dialog)).toContain('Remove and delete the files');
    expect(dialog.textContent).toContain('This also erases 1 file(s)');
  });

  it('starts with the switch on for Shift+Del', async () => {
    const dialog = await openDialog(true);
    expect(filesSwitch(dialog).getAttribute('aria-checked')).toBe('true');
  });

  const parts = [
    { id: 'p1', name: 'film.part1.rar', loaded: 1000 },
    { id: 'p2', name: 'film.part2.rar', loaded: 1000 },
  ] as Task[];

  it('counts what the server says the unpacking wrote into what goes', async () => {
    unpacked = answer({ files: 3, bytes: 3000 });
    const dialog = await openDialog(true, parts, ['p1', 'p2']);
    expect(dialog.textContent).toContain('This also erases 5 file(s), 5.0 kB');
    const asked = fetchMock.mock.calls.find(([url]) => url.endsWith('/tasks/unpacked-weight'));
    expect(JSON.parse(String(asked?.[1]?.body))).toEqual({ ids: ['p1', 'p2'] });
  });

  it('counts only the downloads on a peer that does not delete what unpacking wrote', async () => {
    unpacked = answer({ error: 'not found' }, 404);
    const dialog = await openDialog(true, parts, ['p1']);
    expect(dialog.textContent).toContain('This also erases 1 file(s), 1.0 kB');
  });

  it('shows no count before the server has answered', async () => {
    let reply: (r: Response) => void = () => {};
    fetchMock.mockImplementationOnce(() => new Promise<Response>((resolve) => (reply = resolve)));
    const dialog = await openDialog(true, parts, ['p1']);
    expect(dialog.textContent).not.toContain('This also erases');
    await act(async () => reply(answer({ files: 2, bytes: 24 })));
    expect(dialog.textContent).toContain('This also erases 3 file(s)');
  });
});
