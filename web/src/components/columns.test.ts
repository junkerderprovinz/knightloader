import { describe, expect, it } from 'vitest';

import type { ExtractJob, Task, TaskStatus } from '../lib/api';
import { extractionsByTask } from './Archives';
import {
  gridTemplate,
  packageUnpacking,
  resolveLayout,
  type CellContext,
  type ColumnId,
  type ResolvedLayout,
} from './columns';

/** The track of one column, by its place among the visible ones. */
function track(template: string, layout: ResolvedLayout, id: ColumnId): string {
  const tracks = template.match(/minmax\([^)]*\)|\S+/g) ?? [];
  return tracks[layout.visible.findIndex((c) => c.id === id)];
}

describe('gridTemplate', () => {
  it('lets the name fill what the other columns leave', () => {
    const layout = resolveLayout('downloads', null);
    const template = gridTemplate(layout);
    expect(track(template, layout, 'name')).toBe(`minmax(${layout.minWidthOf('name')}px, 1fr)`);
    expect(template.match(/1fr/g)).toHaveLength(1);
  });

  it('lets an untouched column give way down to its minimum', () => {
    const layout = resolveLayout('downloads', null);
    expect(track(gridTemplate(layout), layout, 'host')).toBe(
      `minmax(${layout.minWidthOf('host')}px, ${layout.widthOf('host')}px)`,
    );
  });

  it('keeps a dragged column at the width it was dragged to', () => {
    const layout = resolveLayout('downloads', { order: [], hidden: [], widths: { host: 300 }, v: 3 });
    expect(track(gridTemplate(layout), layout, 'host')).toBe('300px');
  });

  it('hands the rest to the last column once the name is dragged', () => {
    const layout = resolveLayout('downloads', { order: [], hidden: [], widths: { name: 400 }, v: 3 });
    const template = gridTemplate(layout);
    const last = layout.visible[layout.visible.length - 1];
    expect(track(template, layout, 'name')).toBe(`minmax(${layout.minWidthOf('name')}px, 400px)`);
    expect(track(template, layout, last.id)).toBe(`minmax(${layout.widthOf(last.id)}px, 1fr)`);
  });

  it('draws a column under the pointer as if its width were stored', () => {
    const layout = resolveLayout('downloads', null);
    const template = gridTemplate(layout, { id: 'name', width: 250 });
    expect(track(template, layout, 'name')).toBe(`minmax(${layout.minWidthOf('name')}px, 250px)`);
  });
});

describe('resolveLayout', () => {
  it('lets the download list narrow the variant column below the collector pickers', () => {
    const downloads = resolveLayout('downloads', null);
    const collector = resolveLayout('collector', null);
    expect(downloads.minWidthOf('variant')).toBeLessThan(collector.minWidthOf('variant'));
    expect(downloads.widthOf('variant')).toBeGreaterThanOrEqual(downloads.minWidthOf('variant'));
  });
});

describe('packageUnpacking', () => {
  const task = (id: string, status: TaskStatus = 'done') => ({ id, status }) as Task;
  const job = (id: string, status: string, parts: string[]): ExtractJob => ({
    id,
    taskId: parts[0],
    name: `${id}.part1.rar`,
    dir: '/downloads',
    status,
    files: 0,
    bytes: 0,
    volumes: parts.length,
    parts,
    queuedAt: '2026-09-25T12:00:00Z',
  });
  const ctx = (...jobs: ExtractJob[]): CellContext => ({
    t: (key) => key,
    base: '/api',
    profile: 'downloads',
    extractions: extractionsByTask(jobs),
  });

  it('counts a set of parts as one archive', () => {
    const items = [task('a1', 'extracting'), task('a2'), task('a3'), task('b1')];
    const got = packageUnpacking(items, ctx(job('a', 'running', ['a1', 'a2', 'a3']), job('b', 'done', ['b1'])));
    expect(got?.state).toBe('running');
    expect([got?.done, got?.total]).toEqual([1, 2]);
  });

  it('puts a failed archive before one still unpacking', () => {
    const items = [task('a1', 'extracting'), task('b1')];
    const got = packageUnpacking(items, ctx(job('a', 'running', ['a1']), job('b', 'error', ['b1'])));
    expect(got?.state).toBe('error');
    expect(got?.job?.id).toBe('b');
  });

  it('leaves the header to a download that is still running', () => {
    const items = [task('a1'), task('a2', 'running')];
    expect(packageUnpacking(items, ctx(job('a', 'done', ['a1'])))).toBeNull();
  });

  // After a restart the server holds no jobs, and each file keeps how its
  // archive's last unpacking ended.
  const kept = (id: string, unpack: Task['unpack'], archivePart = 0, error?: string): Task =>
    ({ id, status: 'done', unpack, archivePart, error }) as Task;

  it('reads how the unpackings ended once their jobs are gone', () => {
    const items = [
      kept('a1', 'done', 1),
      kept('a2', 'done', 2),
      kept('a3', 'done', 3),
      kept('b1', 'password', 0, 'extract: zip: wrong password'),
    ];
    const got = packageUnpacking(items, ctx());
    expect(got?.state).toBe('password');
    expect(got?.job).toBeUndefined();
    expect([got?.done, got?.total]).toEqual([1, 2]);
  });

  it('takes the failure from the error the unpacking left on the file', () => {
    const got = packageUnpacking([kept('b1', 'error', 0, 'extract: rardecode: bad block header')], ctx());
    expect(got?.error).toBe('rardecode: bad block header');
  });

  it('prefers a job to what the file kept from the one before', () => {
    const items = [kept('a1', 'error', 1), kept('a2', 'error', 2)];
    const got = packageUnpacking(items, ctx(job('a', 'running', ['a1', 'a2'])));
    expect(got?.state).toBe('running');
    expect([got?.done, got?.total]).toEqual([0, 1]);
  });
});
