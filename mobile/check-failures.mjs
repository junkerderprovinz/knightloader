// A failed row says in the reader's language what went wrong and what to do,
// from the code the instance sends (internal/core/errorcode.go), and keeps the
// instance's own sentence for a report.
//
// src/api/taskError.ts imports nothing at run time, so node strips its types
// and these checks call the real function with the English catalogue. Every
// code the instance can send is read from the Go source, so a code added there
// without words here fails the check instead of reading as the general sentence.
//
// Run by hand and by CI, from mobile/: `node check-failures.mjs`
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const { explainFailure } = await import(pathToFileURL(join(here, 'src', 'api', 'taskError.ts')).href);
const { en } = await import(pathToFileURL(join(here, 'src', 'i18n', 'en.ts')).href);

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};

const t = (key, vars = {}) => Object.entries(vars).reduce((s, [k, v]) => s.replaceAll(`{${k}}`, String(v)), en[key]);

const damaged = explainFailure(t, {
  error: 'extract: Vier.minus.drei.part1.rar: rardecode: bad block header',
  errorCode: 'archiveDamaged',
  errorParams: { part: 'Vier.minus.drei.part1.rar' },
});
expect('a damaged part is named', damaged?.line, 'Vier.minus.drei.part1.rar is damaged.');
expect('and says what to do', damaged?.next, 'Download Vier.minus.drei.part1.rar again, or get it from another mirror.');
expect('and keeps the sentence', damaged?.raw, 'extract: Vier.minus.drei.part1.rar: rardecode: bad block header');

expect(
  'a value the instance left out comes from the row',
  explainFailure(t, { error: 'x', errorCode: 'archiveDamaged' }, { part: 'film.part1.rar' })?.line,
  'film.part1.rar is damaged.',
);
expect(
  'nothing recognised reads as the general sentence',
  explainFailure(t, { error: 'rapidgator: error code 7731' })?.line,
  en['failure.unknown.line'],
);
expect(
  'a row stored before codes existed reads by its reason',
  explainFailure(t, { error: 'http request fail, code:404', reason: 'gone' })?.line,
  en['failure.gone.line'],
);
expect(
  'a code from a newer instance reads as the general sentence',
  explainFailure(t, { error: 'x', errorCode: 'somethingNew', reason: 'gone' })?.line,
  en['failure.unknown.line'],
);
expect('a row that has not failed says nothing', explainFailure(t, {}), null);

const go = readFileSync(join(here, '..', 'internal', 'core', 'errorcode.go'), 'utf8');
const codes = [...go.matchAll(/ErrorCode = "(\w+)"/g)].map((m) => m[1]);
if (codes.length < 20) problems.push(`found only ${codes.length} codes in internal/core/errorcode.go`);
for (const code of codes) {
  const got = explainFailure(t, { error: 'x', errorCode: code });
  if (!got || got.line === en['failure.unknown.line']) problems.push(`the instance's code ${code} has no words in the app`);
}

if (problems.length > 0) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log(`failure wording holds (${codes.length} codes)`);
