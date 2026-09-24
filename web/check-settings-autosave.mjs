// What the settings autosave sends and what it keeps of the server's answer.
//
// Every settings page saves itself a moment after the last keystroke, and the
// answer comes back while the person may still be typing. Taking the answer
// wholesale would put the server's value into the box being typed in: a
// half-typed "C:" would come back empty, and "D:\My " trimmed just before the
// next word. A refused value is also not sent again until it is edited, or one
// unusable folder would be refused on every save.
//
// It drives the real functions: Node strips the types out of
// src/pages/settings/paths.ts, which is why that file carries no import.
//
// Run: `node web/check-settings-autosave.mjs`.

import { foldAnswer, pendingFields } from './src/pages/settings/paths.ts';

const problems = [];

function check(what, got, want) {
  const a = JSON.stringify(got);
  const b = JSON.stringify(want);
  if (a !== b) problems.push(`${what}\n      got  ${a}\n      want ${b}`);
}

const stored = { watchDir: '/watch', maxConcurrent: 3, categories: [] };

check(
  'a changed field is sent',
  pendingFields({ ...stored, watchDir: 'C:' }, stored, {}),
  { watchDir: 'C:' },
);
check(
  'a refused value is not sent again',
  pendingFields({ ...stored, watchDir: 'C:' }, stored, { watchDir: 'C:' }),
  {},
);
check(
  'the field goes out again once it is edited',
  pendingFields({ ...stored, watchDir: 'C:\\' }, stored, { watchDir: 'C:' }),
  { watchDir: 'C:\\' },
);

check(
  'what was typed while the save was out stays in the box',
  foldAnswer({ ...stored, watchDir: 'C:\\Us' }, { ...stored, watchDir: '' }, { watchDir: 'C:' }, stored, []).watchDir,
  'C:\\Us',
);
check(
  'a value tidied while its box has focus stays as typed',
  foldAnswer({ ...stored, watchDir: 'D:\\My ' }, { ...stored, watchDir: 'D:\\My' }, { watchDir: 'D:\\My ' }, stored, [
    'watchDir',
  ]).watchDir,
  'D:\\My ',
);
check(
  'a value tidied in a box without focus is shown as stored',
  foldAnswer({ ...stored, watchDir: 'D:\\My ' }, { ...stored, watchDir: 'D:\\My' }, { watchDir: 'D:\\My ' }, stored, [])
    .watchDir,
  'D:\\My',
);
check(
  'an edit to a field the save did not send survives',
  foldAnswer({ ...stored, maxConcurrent: 4 }, stored, {}, stored, []).maxConcurrent,
  4,
);
check(
  'what the server adds to a sent value is taken, such as a new row id',
  foldAnswer(
    { ...stored, categories: [{ name: 'Serien' }] },
    { ...stored, categories: [{ name: 'Serien', id: 'serien' }] },
    { categories: [{ name: 'Serien' }] },
    stored,
    [],
  ).categories,
  [{ name: 'Serien', id: 'serien' }],
);

if (problems.length > 0) {
  console.error(`settings autosave: ${problems.length} problem(s)\n`);
  for (const p of problems) console.error(`  - ${p}\n`);
  process.exit(1);
}
console.log('settings autosave: ok');
