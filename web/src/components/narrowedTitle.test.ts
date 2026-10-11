import { expect, it } from 'vitest';

import type { TranslationKey } from '../lib/i18n';
import { en } from '../lib/locales/en';
import { narrowedTitle, type QuickFilterId } from './ListToolbar';

const t = (key: TranslationKey) => en[key];
const title = (...ids: QuickFilterId[]) => narrowedTitle(new Set(ids), t, 'Download list');

it('names a list after the one quick filter that narrows it', () => {
  expect(title('failed')).toBe(en['filter.failed']);
});

it('keeps the list its own name with no filter or several', () => {
  expect(title()).toBe('Download list');
  expect(title('failed', 'running')).toBe('Download list');
});
