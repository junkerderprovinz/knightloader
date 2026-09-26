// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';

import { interpolate } from './interpolate';
import type { TranslationKey } from './i18n';
import { de } from './locales/de';
import { rejectionReason } from './rejectionReason';

const german = (key: TranslationKey, vars?: Record<string, string | number>) => interpolate(de[key], vars);

describe('rejectionReason', () => {
  it('words a banned tracker in the reader’s language', () => {
    const english = 'announces tracker.example.org, which is on the banned trackers list';
    expect(rejectionReason(german, 'bannedTracker', { host: 'tracker.example.org' }, english)).toBe(
      'meldet sich bei tracker.example.org, einem der gesperrten Tracker',
    );
  });

  it('words a filter rule with and without a reason of its own', () => {
    expect(rejectionReason(german, 'filterRule', { rule: 'Muster' }, 'rejected by link filter rule "Muster"')).toBe(
      'von der Linkfilter-Regel „Muster“ abgelehnt',
    );
    const own = 'zu groß (link filter rule "Grenze")';
    expect(rejectionReason(german, 'filterRuleReason', { reason: 'zu groß', rule: 'Grenze' }, own)).toBe(
      'zu groß (Linkfilter-Regel „Grenze“)',
    );
  });

  it('keeps the server’s sentence for a code it has no words for, or none', () => {
    expect(rejectionReason(german, 'fromANewerServer', { x: '1' }, 'the server’s words')).toBe('the server’s words');
    expect(rejectionReason(german, undefined, undefined, 'the server’s words')).toBe('the server’s words');
    expect(rejectionReason(german, undefined, undefined, undefined)).toBe('');
  });
});
