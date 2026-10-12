import { describe, expect, it } from 'vitest';
import { lineColours, seedingDefault } from './speedLines';

describe('the seeding line by default', () => {
  it('takes the blue across the wheel from the built-in gold', () => {
    expect(seedingDefault('')).toBe('#1D99F3');
    expect(seedingDefault('#FCC419')).toBe('#1D99F3');
  });

  it('moves away when the accent is the blue', () => {
    expect(seedingDefault('#1D99F3')).toBe('#FF832B');
  });
});

describe('the colours both lines are drawn in', () => {
  it('follow the accent while none is chosen', () => {
    expect(lineColours({}, '#6FDC8C')).toEqual({ download: '#6FDC8C', seeding: seedingDefault('#6FDC8C') });
  });

  it('are the ones chosen, and a value that is no colour counts as none', () => {
    expect(lineColours({ download: '#123456', seeding: '#abcdef' }, '')).toEqual({ download: '#123456', seeding: '#abcdef' });
    expect(lineColours({ download: 'red', seeding: '' }, '')).toEqual({ download: '#FCC419', seeding: '#1D99F3' });
  });
});
