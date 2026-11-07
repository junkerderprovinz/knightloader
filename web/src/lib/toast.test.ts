import { expect, it } from 'vitest';

import { softBreaks } from './toast';

it('breaks a path after each slash so a name stays whole', () => {
  expect(softBreaks('Saved to /downloads/Burg/plan.pdf')).toEqual(['Saved to /', 'downloads/', 'Burg/', 'plan.pdf']);
  expect(softBreaks('D:\\Burg\\plan.pdf')).toEqual(['D:\\', 'Burg\\', 'plan.pdf']);
  expect(softBreaks('Nothing to break')).toEqual(['Nothing to break']);
});
