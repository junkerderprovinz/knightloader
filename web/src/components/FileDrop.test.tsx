// @vitest-environment jsdom
import { act, createRef } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { Task, TorrentTree } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { FileDrop, type FileDropHandle } from './FileDrop';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

const reply = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

const tree: TorrentTree = {
  uri: 'data:application/x-bittorrent;base64,ZA==',
  infoHash: '0123456789abcdef0123456789abcdef01234567',
  name: 'movie.mkv',
  private: false,
  totalSize: 900,
  pieceLength: 16384,
  pieces: 1,
  files: [{ path: 'movie.mkv', size: 900, selected: true }],
  trackers: ['udp://tracker.example.org:6969/announce'],
  droppedTrackers: 0,
};

function staging(task: Partial<Task>) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => (url === '/api/torrents/parse' ? reply(tree) : reply({ id: 't1', ...task }))),
  );
}

async function drop(): Promise<string> {
  const ref = createRef<FileDropHandle>();
  await act(async () =>
    root.render(
      <I18nProvider>
        <FileDrop ref={ref} />
      </I18nProvider>,
    ),
  );
  await act(async () => ref.current!.handleFiles([new File(['d'], 'movie.torrent')]));
  return host.textContent ?? '';
}

beforeEach(() => {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

it('says an upload that announces a banned tracker was rejected, with the reason', async () => {
  const reason = 'announces tracker.example.org, which is on the banned trackers list';
  staging({ skipped: true, skipReason: reason });
  const text = await drop();
  expect(text).toContain(`movie.torrent was rejected: ${reason}`);
  expect(text).not.toContain('Added movie.torrent');
});

it('words the reason from its code rather than repeating the server', async () => {
  staging({
    skipped: true,
    skipReason: 'the server’s own sentence',
    skipCode: 'bannedTracker',
    skipParams: { host: 'tracker.example.org' },
  });
  const text = await drop();
  expect(text).toContain('announces tracker.example.org, which is on the banned trackers list');
  expect(text).not.toContain('the server’s own sentence');
});

it('says an upload that was staged was added', async () => {
  staging({ package: '' });
  expect(await drop()).toContain('Added movie.torrent to the collector.');
});
