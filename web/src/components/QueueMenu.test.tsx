import { describe, expect, it } from 'vitest';

import type { Task } from '../lib/api';
import { queueMenuGroup, type QueueVerbs } from './ListToolbar';

const task = (id: string): Task => ({
  id,
  url: `https://files.example/${id}`,
  name: `${id}.mkv`,
  package: 'Open Movies',
  resolver: 'direct',
  size: 1000,
  loaded: 0,
  speed: 0,
  status: 'queued',
  createdAt: '2026-09-24T12:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
});

const verbs = (stopMark: string): QueueVerbs => ({ choices: [], stopMark, mark: async () => {} });

function stopEntry(stopMark: string): string | undefined {
  const group = queueMenuGroup({
    chosen: [task('a')],
    ids: ['a'],
    base: '/api',
    t: (key) => key,
    fail: () => {},
    queue: verbs(stopMark),
  });
  return group.items.find((item) => item.id === 'stopMark')?.label;
}

describe('the stop mark entry', () => {
  it('sets the mark on a row that does not carry it', () => {
    expect(stopEntry('')).toBe('queue.stopMark');
    expect(stopEntry('b')).toBe('queue.stopMark');
  });

  it('names the removal on the row that carries it', () => {
    expect(stopEntry('a')).toBe('queue.stopMarkOff');
  });
});
