// @vitest-environment jsdom
import { expect, it } from 'vitest';

import type { SkippedLink } from './api';
import { interpolate } from './interpolate';
import { de } from './locales/de';
import { skippedReason } from './skippedReason';

const t = (key: keyof typeof de, vars?: Record<string, string | number>) => interpolate(de[key], vars);
const link = (fields: Partial<SkippedLink>) => ({ url: 'https://example.org/x', kind: 'duplicate', reason: 'english', at: '', ...fields }) as SkippedLink;

it('says a folded link in the reader\'s language', () => {
  expect(skippedReason(t, link({ code: 'sameLink' }))).toBe('Derselbe Link ist schon in der Liste.');
  expect(skippedReason(t, link({ code: 'heldLink', params: { by: 'downloaded' } }))).toBe(
    'Der Download-Verlauf hat diesen Link schon abgelehnt.',
  );
  expect(skippedReason(t, link({ kind: 'mirror', code: 'sameFile', params: { name: 'a.rar', signal: 'name+size' } }))).toContain(
    'erkannt an Name und Größe',
  );
});

it('quotes the tool for a kind with one sentence', () => {
  expect(skippedReason(t, link({ kind: 'container', reason: 'not a container' }))).toBe(
    'Der Container ergab keine Links: not a container',
  );
});

it('falls back on the server\'s sentence for something it does not know', () => {
  expect(skippedReason(t, link({ code: 'fromTheFuture', reason: 'english' }))).toBe('english');
});
