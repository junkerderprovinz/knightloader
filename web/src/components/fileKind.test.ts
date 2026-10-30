import { expect, it } from 'vitest';

import type { Task } from '../lib/api';
import { fileKindOf } from './columns';

const task = (fields: Partial<Task>) => ({ name: '', url: 'https://example.org/x', ...fields }) as Task;

it('takes a yt-dlp variant at its word', () => {
  expect(fileKindOf(task({ name: 'Interview', variant: 'video:1080p' }))).toBe('video');
  expect(fileKindOf(task({ name: 'Interview', variant: 'audio:opus' }))).toBe('audio');
  expect(fileKindOf(task({ name: 'Interview', variant: 'thumbnail' }))).toBe('image');
  expect(fileKindOf(task({ name: 'Interview', variant: 'subtitle:de' }))).toBe('subtitle');
  expect(fileKindOf(task({ name: 'Interview', variant: 'description' }))).toBe('text');
});

it('reads the kind from the extension', () => {
  expect(fileKindOf(task({ name: 'Interview.MKV' }))).toBe('video');
  expect(fileKindOf(task({ name: 'Interview', ext: 'opus' }))).toBe('audio');
  expect(fileKindOf(task({ name: 'Interview.de.srt' }))).toBe('subtitle');
  expect(fileKindOf(task({ name: 'Chronik.part1.rar' }))).toBe('archive');
  expect(fileKindOf(task({ name: 'Chronik.r01' }))).toBe('archive');
  expect(fileKindOf(task({ name: 'Chronik.7z.001' }))).toBe('archive');
});

it('counts anything else, and a name not resolved yet, as another file', () => {
  expect(fileKindOf(task({ name: 'Burgplan.iso' }))).toBe('other');
  expect(fileKindOf(task({ name: 'README' }))).toBe('other');
  expect(fileKindOf(task({ name: 'https://example.org/x' }))).toBe('other');
});
