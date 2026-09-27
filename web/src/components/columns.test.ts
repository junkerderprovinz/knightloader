import { describe, expect, it } from 'vitest';

import type { ExtractJob, Task, TaskStatus } from '../lib/api';
import { extractionsByTask } from './Archives';
import {
  gridTemplate,
  isParked,
  packageFailures,
  packageStatus,
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

  // The failed one is flagged beside it, as a failed link is beside a running one.
  it('lets an archive still unpacking speak before a failed one', () => {
    const items = [task('a1', 'extracting'), task('b1')];
    const got = packageUnpacking(items, ctx(job('a', 'running', ['a1']), job('b', 'error', ['b1'])));
    expect(got?.state).toBe('running');
    expect(got?.job?.id).toBe('a');
  });

  it('lets a failed archive speak once nothing is left to unpack', () => {
    const items = [task('a1'), task('b1')];
    const got = packageUnpacking(items, ctx(job('a', 'done', ['a1']), job('b', 'error', ['b1'])));
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
    expect(got?.failure?.error).toBe('rardecode: bad block header');
  });

  it('prefers a job to what the file kept from the one before', () => {
    const items = [kept('a1', 'error', 1), kept('a2', 'error', 2)];
    const got = packageUnpacking(items, ctx(job('a', 'running', ['a1', 'a2'])));
    expect(got?.state).toBe('running');
    expect([got?.done, got?.total]).toEqual([0, 1]);
  });

  it('speaks for a package whose downloads are in and whose archive failed', () => {
    const items = [kept('a1', 'error', 1), kept('a2', 'error', 2), task('notes.nfo')];
    expect(packageUnpacking(items, ctx())?.state).toBe('error');
  });
});

describe('packageStatus', () => {
  const task = (status: TaskStatus) => ({ id: status, status }) as Task;

  it('reads as the work still going on, failure or not', () => {
    expect(packageStatus([task('done'), task('error'), task('running')])).toBe('running');
    expect(packageStatus([task('error'), task('queued')])).toBe('queued');
    expect(packageStatus([task('error'), task('paused')])).toBe('paused');
  });

  it('reads as failed once nothing is going on', () => {
    expect(packageStatus([task('done'), task('error'), task('done')])).toBe('error');
  });

  it('reads as done when everything is', () => {
    expect(packageStatus([task('done'), task('done')])).toBe('done');
  });

  const seeding = { ...task('done'), seeding: true };

  it('reads as seeding while a finished torrent in it still uploads', () => {
    expect(packageStatus([task('done'), seeding])).toBe('seeding');
  });

  it('reads as the download still owed before a torrent that seeds', () => {
    expect(packageStatus([seeding, task('queued')])).toBe('queued');
    expect(packageStatus([seeding, task('running')])).toBe('running');
  });

  it('keeps seeding as the word beside a failure, as work still going on', () => {
    expect(packageStatus([seeding, task('error')])).toBe('seeding');
  });
});

describe('isParked', () => {
  const row = (enabled: boolean) => ({ id: String(enabled), status: 'queued', enabled }) as Task;

  it('parks a disabled link', () => {
    expect(isParked([row(false)])).toBe(true);
    expect(isParked([row(true)])).toBe(false);
  });

  it('parks a package only when every link in it is disabled', () => {
    expect(isParked([row(false), row(false)])).toBe(true);
    expect(isParked([row(false), row(true)])).toBe(false);
  });

  it('does not park an empty package', () => {
    expect(isParked([])).toBe(false);
  });
});

describe('packageFailures', () => {
  const ctx = (...jobs: ExtractJob[]): CellContext => ({
    t: (key) => key,
    base: '/api',
    profile: 'downloads',
    extractions: extractionsByTask(jobs),
  });
  const row = (id: string, status: TaskStatus, unpack?: Task['unpack'], archivePart = 0) =>
    ({ id, status, unpack, archivePart }) as Task;

  it('counts a link that did not download', () => {
    expect(packageFailures([row('a', 'error'), row('b', 'running'), row('c', 'done')], ctx())).toBe(1);
  });

  it('counts an archive that did not unpack once, however many parts it has', () => {
    const items = [row('a1', 'done', 'error', 1), row('a2', 'done', 'error', 2), row('b', 'running')];
    expect(packageFailures(items, ctx())).toBe(1);
  });

  it('counts a set whose job failed for want of a password', () => {
    const failed: ExtractJob = {
      id: 'j',
      taskId: 'a1',
      name: 'a.part1.rar',
      dir: '/downloads',
      status: 'error',
      password: true,
      files: 0,
      bytes: 0,
      volumes: 2,
      parts: ['a1', 'a2'],
      queuedAt: '2026-09-25T12:00:00Z',
    };
    expect(packageFailures([row('a1', 'done'), row('a2', 'done'), row('c', 'error')], ctx(failed))).toBe(2);
  });

  it('counts nothing in a package that is fine', () => {
    expect(packageFailures([row('a', 'done', 'done'), row('b', 'running')], ctx())).toBe(0);
  });
});
