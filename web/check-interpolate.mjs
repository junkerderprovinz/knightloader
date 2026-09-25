// A value put into a sentence arrives as it is. String.replaceAll reads $$, $&,
// $` and $' in a replacement string as patterns, so a saved view named
// "AT$&T" would be offered for deletion as "AT{name}T", and a script error
// quoting bash's "$'\r': command not found" would lose its first characters.
//
// It drives the real src/lib/interpolate.ts. Node strips its types, and the
// hook below gives its extensionless import the ".ts" the bundler adds.
//
// Run: `node web/check-interpolate.mjs`.

import { registerHooks } from 'node:module';

registerHooks({
  resolve(specifier, context, next) {
    if (/^\.\.?\//.test(specifier) && !/\.[a-z]+$/.test(specifier) && context.parentURL?.endsWith('.ts')) {
      return next(`${specifier}.ts`, context);
    }
    return next(specifier, context);
  },
});

// lib/bidi.ts reads the page's direction from <html dir>.
globalThis.document = { documentElement: { dir: 'ltr' } };
const { interpolate } = await import('./src/lib/interpolate.ts');

const problems = [];

function check(what, got, want) {
  if (got !== want) problems.push(`${what}\n      got  ${JSON.stringify(got)}\n      want ${JSON.stringify(want)}`);
}

check('$& stays as typed', interpolate('Delete the view “{name}”?', { name: 'AT$&T' }), 'Delete the view “AT$&T”?');
check('$$ keeps both signs', interpolate('“{name}”', { name: '$$$ deals' }), '“$$$ deals”');
check("$' is not the rest of the sentence", interpolate('Failed: {error}.', { error: "$'\\r': command not found" }), "Failed: $'\\r': command not found.");
check('$` is not the start of the sentence', interpolate('Folder {dir} is missing', { dir: 'C:\\$`x' }), 'Folder C:\\$`x is missing');
check('a number is written as it is', interpolate('{n}/{max}', { n: 5, max: 10 }), '5/10');
check('every place a name appears is filled', interpolate('{a} and {a}', { a: 'x' }), 'x and x');

document.documentElement.dir = 'rtl';
check('a text value is isolated on a right-to-left page', interpolate('{name}', { name: 'a$&b' }), '\u2068a$&b\u2069');
check('a number is not isolated', interpolate('{n}', { n: 3 }), '3');

if (problems.length > 0) {
  console.error(`interpolate: ${problems.length} problem(s)\n`);
  for (const p of problems) console.error(`  - ${p}\n`);
  process.exit(1);
}
console.log('interpolate: ok');
