import { describe, expect, it } from 'vitest';
import { formatSize, parseSize } from './RuleEditor';

describe('formatSize', () => {
  it('names whole decimal sizes in their own unit', () => {
    expect(formatSize(700 * 1000 ** 2)).toBe('700 MB');
    expect(formatSize(10 * 1000 ** 3)).toBe('10 GB');
  });

  it('shows a size saved in binary units in decimal units', () => {
    expect(formatSize(10 * 1024 ** 3)).toBe('10.74 GB');
    expect(formatSize(512 * 1024 ** 2)).toBe('536.87 MB');
  });

  it('keeps bytes below a kilobyte and leaves an empty bound empty', () => {
    expect(formatSize(999)).toBe('999');
    expect(formatSize(0)).toBe('');
    expect(formatSize(undefined)).toBe('');
  });

  it('reads back what it writes for whole sizes', () => {
    expect(parseSize(formatSize(1500 * 1000 ** 2))).toBe(1500 * 1000 ** 2);
  });
});
