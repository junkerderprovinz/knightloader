import { describe, expect, it } from 'vitest';

import type { ExtractJob } from '../lib/api';
import { extractionsByTask } from './Archives';

function job(id: string, status: string, parts?: string[]): ExtractJob {
  return {
    id,
    taskId: parts?.[0] ?? 'first',
    name: 'film.part1.rar',
    dir: '/downloads',
    status,
    files: 0,
    bytes: 0,
    volumes: parts?.length ?? 1,
    parts,
    queuedAt: '2026-09-25T12:00:00Z',
  };
}

describe('extractionsByTask', () => {
  it('gives every part of a set the job of its archive', () => {
    const running = job('a', 'running', ['p1', 'p2', 'p3']);
    const map = extractionsByTask([running]);
    expect([...map.keys()]).toEqual(['p1', 'p2', 'p3']);
    expect(map.get('p3')).toBe(running);
  });

  it('lets a retry replace the failure before it', () => {
    const retry = job('b', 'running', ['p1', 'p2']);
    const map = extractionsByTask([job('a', 'error', ['p1', 'p2']), retry]);
    expect(map.get('p1')).toBe(retry);
    expect(map.get('p2')).toBe(retry);
  });

  it('hands the rows back to their own status once the job is cancelled', () => {
    const map = extractionsByTask([job('a', 'done', ['p1', 'p2']), job('b', 'cancelled', ['p1', 'p2'])]);
    expect(map.size).toBe(0);
  });

  it('knows only the first volume when a peer sends no parts', () => {
    const map = extractionsByTask([job('a', 'running')]);
    expect([...map.keys()]).toEqual(['first']);
  });
});
