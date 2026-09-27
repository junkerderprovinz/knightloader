// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { ExtractJob } from '../lib/api';
import { useExtractJobs } from './Archives';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let asked: string[];
let seen: ExtractJob[];

const running: ExtractJob = {
  id: 'j',
  taskId: 'a1',
  name: 'a1.rar',
  dir: '/downloads',
  status: 'running',
  files: 0,
  bytes: 0,
  unpacked: 450,
  size: 1000,
  volumes: 1,
  parts: ['a1'],
  queuedAt: '2026-09-27T12:00:00Z',
};

beforeEach(() => {
  vi.useFakeTimers();
  asked = [];
  seen = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      asked.push(url);
      return new Response(JSON.stringify([running]), { headers: { 'Content-Type': 'application/json' } });
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
  vi.useRealTimers();
});

function Probe({ instance, unpacking }: { instance: string; unpacking: boolean }) {
  seen = useExtractJobs(instance, unpacking);
  return null;
}

async function show(unpacking: boolean) {
  await act(async () => root.render(<Probe instance="cellar" unpacking={unpacking} />));
}

async function wait(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

it("reads a peer's unpackings through the forward while one of its archives unpacks", async () => {
  await show(true);
  await wait(4100);
  expect(asked).toEqual(Array(3).fill('/api/instances/cellar/extract'));
  expect(seen.map((j) => j.id)).toEqual(['j']);
});

it('asks an idle peer once and then leaves it alone', async () => {
  await show(false);
  await wait(10_000);
  expect(asked).toHaveLength(1);
});

it('reads the peer once more when its last archive stops unpacking', async () => {
  await show(true);
  await wait(100);
  await show(false);
  await wait(10_000);
  expect(asked).toHaveLength(2);
});
