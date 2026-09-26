import { afterEach, describe, expect, it, vi } from 'vitest';

import { fetchTasks, setEnabled, type Task } from './api';

const reply = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

// What an older build sends: a hold flag and a waiting reason this build lacks.
const task = (over: Omit<Partial<Task>, 'waiting'> & { hold?: boolean; waiting?: string }): Task =>
  ({
    id: 'a',
    url: 'https://files.example/a',
    name: 'a.mkv',
    package: '',
    resolver: 'direct',
    size: 1000,
    loaded: 0,
    speed: 0,
    status: 'queued',
    createdAt: '2026-09-24T12:00:00Z',
    priority: 0,
    position: 0,
    enabled: true,
    ...over,
  }) as Task;

afterEach(() => vi.unstubAllGlobals());

describe('a peer on an older build', () => {
  it('shows a held link as a disabled one', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => reply([task({ hold: true, waiting: 'hold' }), task({ id: 'b' })])));
    const [held, free] = await fetchTasks('/api/instances/old');
    expect(held.enabled).toBe(false);
    expect(held.waiting).toBe('disabled');
    expect('hold' in held).toBe(false);
    expect(free.enabled).toBe(true);
  });

  it('has Hold released when a link is enabled there', async () => {
    const calls: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        calls.push(url);
        return reply({ ids: ['a'], count: 1 });
      }),
    );
    await setEnabled(['a'], true, '/api/instances/old');
    expect(calls).toEqual(['/api/instances/old/tasks/enabled', '/api/instances/old/tasks/hold']);
  });

  it('leaves this instance and a disable to the switch alone', async () => {
    const calls: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        calls.push(url);
        return reply({ ids: ['a'], count: 1 });
      }),
    );
    await setEnabled(['a'], true);
    await setEnabled(['a'], false, '/api/instances/old');
    expect(calls).toEqual(['/api/tasks/enabled', '/api/instances/old/tasks/enabled']);
  });
});
