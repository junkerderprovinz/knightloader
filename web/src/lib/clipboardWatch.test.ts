import { describe, expect, it } from 'vitest';

import { clipboardLinks } from './clipboardWatch';
import cases from './clipboardWatch.cases.json';

// desktop/clipwatch_test.go runs the same cases against the desktop app's
// watch, so both send the same links for the same copied text.
describe('clipboardLinks', () => {
  for (const c of cases) {
    it(c.name, () => {
      expect(clipboardLinks(c.text)).toEqual(c.links);
    });
  }
});
