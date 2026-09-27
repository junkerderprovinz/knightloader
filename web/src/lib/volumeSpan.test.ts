import { describe, expect, it } from 'vitest';
import { readCount, readSpan, storedSpan } from './volumeSpan';

describe('volumeSpan', () => {
  it('reads a whole count inside the unit\'s range', () => {
    expect(readCount('45', 'd')).toBe(45);
    expect(readCount(' 7 ', 'd')).toBe(7);
    expect(readCount('730', 'd')).toBe(730);
    expect(readCount('120', 'm')).toBe(120);
  });

  it('refuses a count outside the range or not a whole number', () => {
    for (const text of ['0', '731', '-3', '4.5', '1e2', 'abc', '']) expect(readCount(text, 'd')).toBeNull();
    expect(readCount('121', 'm')).toBeNull();
  });

  it('splits a span into its count and unit', () => {
    expect(readSpan('45d')).toEqual({ n: 45, unit: 'd' });
    expect(readSpan('6m')).toEqual({ n: 6, unit: 'm' });
    expect(readSpan('all')).toBeNull();
    expect(readSpan('200m')).toBeNull();
  });

  it('falls back to thirty days for a remembered span the server would refuse', () => {
    expect(storedSpan('all')).toBe('all');
    expect(storedSpan('18m')).toBe('18m');
    for (const v of ['days', '0d', 12, null, undefined, '7w']) expect(storedSpan(v)).toBe('30d');
  });
});
