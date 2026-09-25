import { describe, expect, it } from 'vitest';

import { ApiError } from './api';
import type { TranslationKey } from './i18n';
import { containerRefusal } from './intake';

const t = (key: TranslationKey) => key;

describe('upload refusals', () => {
  it('says in the reader’s language why an .nzb was not taken', () => {
    const e = new ApiError('an .nzb is fetched from Usenet through a TorBox or Premiumize account', 'noUsenet');
    expect(containerRefusal(t, e)).toBe('container.noUsenet');
  });

  it('passes a refusal without a code on as the server wrote it', () => {
    expect(containerRefusal(t, new ApiError('this .torrent is damaged'))).toBe('this .torrent is damaged');
  });
});
