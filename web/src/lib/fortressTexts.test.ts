import { describe, expect, it } from 'vitest';

import texts from '../../../internal/fortress/texts.json';
import { fortressTexts } from './fortressTexts';
import { load, AVAILABLE } from './locales';

describe('internal/fortress/texts.json', () => {
  it('holds the texts the locales have, so the status speaks like the interface', async () => {
    const dicts: Record<string, Record<string, string>> = {};
    for (const lang of AVAILABLE) dicts[lang] = await load(lang);
    // Run `node scripts/gen-fortress-texts.mjs` when this fails.
    expect(texts).toEqual(fortressTexts(dicts));
  });
});
