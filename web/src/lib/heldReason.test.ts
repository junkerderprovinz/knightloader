// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';

import { heldReason } from './heldReason';
import { interpolate } from './interpolate';
import { de } from './locales/de';
import type { TranslationKey } from './i18n';

const german = (key: TranslationKey, vars?: Record<string, string | number>) => interpolate(de[key], vars);

describe('heldReason', () => {
  it('words a banned tracker in the reader’s language', () => {
    expect(
      heldReason(german, 'bannedTracker', { host: 'tracker.example.org' }, 'announces tracker.example.org, which is on the banned trackers list'),
    ).toBe('meldet sich bei tracker.example.org, einem der gesperrten Tracker');
  });

  it('words a filter rule with and without a reason of its own', () => {
    expect(heldReason(german, 'filterRule', { rule: 'Muster' }, 'blocked by filter rule "Muster"')).toBe(
      'von der Linkfilter-Regel „Muster“ abgelehnt',
    );
    expect(
      heldReason(german, 'filterRuleReason', { reason: 'zu groß', rule: 'Grenze' }, 'zu groß (link filter rule "Grenze")'),
    ).toBe('zu groß (Linkfilter-Regel „Grenze“)');
  });

  it('keeps the server’s sentence for a code it has no words for, or none', () => {
    expect(heldReason(german, 'fromANewerServer', { x: '1' }, 'the server’s words')).toBe('the server’s words');
    expect(heldReason(german, undefined, undefined, 'the server’s words')).toBe('the server’s words');
    expect(heldReason(german, undefined, undefined, undefined)).toBe('');
  });
});
