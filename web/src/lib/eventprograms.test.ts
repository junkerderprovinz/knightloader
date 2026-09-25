import { describe, expect, it } from 'vitest';

import { newProgramId } from './eventprograms';

describe('a new program row', () => {
  // The server hands a stored command line back to whichever row carries its
  // id, so a new row must never get the id a deleted one had.
  it('gets an id that no earlier row had', () => {
    const seen = new Set<string>();
    for (let i = 0; i < 1000; i++) {
      const id = newProgramId();
      expect(id).toMatch(/^[0-9a-f]{16}$/);
      expect(seen.has(id)).toBe(false);
      seen.add(id);
    }
  });
});
