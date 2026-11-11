import { describe, expect, it } from 'vitest';
import { fortressText } from '@glimstone/core';

import { load, AVAILABLE } from './locales';

describe('instances.notReachable', () => {
  it('says what GlimStone says of a fortress member that is away, in every language', async () => {
    for (const lang of AVAILABLE) {
      const dict = await load(lang);
      expect([lang, dict['instances.notReachable']]).toEqual([lang, fortressText(lang).away]);
    }
  });
});
