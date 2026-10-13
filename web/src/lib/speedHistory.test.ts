import { describe, expect, it } from 'vitest';

import { alignedUpload, uploadOf } from './speedHistory';

describe('alignedUpload', () => {
  it('takes the upload a record carries', () => {
    expect(alignedUpload([1, 2, 3], 3)).toEqual([1, 2, 3]);
  });

  it('reads a record from a server that kept no upload as nothing sent', () => {
    expect(alignedUpload(undefined, 3)).toEqual([0, 0, 0]);
  });

  it('never puts the two lines on different clocks', () => {
    expect(alignedUpload([5, 6], 3)).toEqual([0, 0, 0]);
  });
});

describe('uploadOf', () => {
  it('adds up the torrents that download or seed, as the server does', () => {
    const tasks = [
      { status: 'running' as const, uploadSpeed: 100 },
      { status: 'done' as const, seeding: true, uploadSpeed: 40 },
      { status: 'done' as const, uploadSpeed: 900 },
      { status: 'paused' as const, uploadSpeed: 800 },
      { status: 'running' as const },
    ];
    expect(uploadOf(tasks)).toBe(140);
  });
});
