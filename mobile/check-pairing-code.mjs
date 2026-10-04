// The app reads the QR code beside the phrase the way the server writes it.
//
// relay.PairingCode puts the instance's own relay on a second line, and
// src/api/pairingCode.ts reads it back. Every row of the Go test's table,
// TestPairingCode in internal/relay/key_test.go, is scanned here: a code
// without a second line pairs on the project relay, a wss:// relay is taken
// over, and anything else is refused rather than dialled, since the app
// permits no cleartext traffic.
//
// Run by hand and by CI, from mobile/: `node check-pairing-code.mjs`
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const { readPairingCode } = await import(pathToFileURL(join(here, 'src', 'api', 'pairingCode.ts')).href);
const goTest = readFileSync(join(here, '..', 'internal', 'relay', 'key_test.go'), 'utf8');

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};

const phrase = goTest.match(/^func TestPairingCode\(t \*testing\.T\) \{\n\tconst phrase = "([^"]+)"/m)?.[1];
const table = goTest.match(/^func TestPairingCode\([\s\S]*?^\}$/m);
if (!phrase || !table) {
  console.error('TestPairingCode was not found; the patterns in this check no longer match internal/relay/key_test.go');
  process.exit(1);
}

const rows = [...table[0].matchAll(/\{"([^"]*)", [^,]+, phrase(?: \+ "\\n([^"]*)")?\}/g)];
if (rows.length === 0) problems.push('TestPairingCode has no rows this check can read');
for (const [, name, relay] of rows) {
  const code = relay === undefined ? phrase : `${phrase}\n${relay}`;
  const want =
    relay === undefined
      ? { words: phrase }
      : relay.startsWith('wss://')
        ? { words: phrase, relay }
        : { words: phrase, refused: relay };
  expect(name, readPairingCode(code), want);
}

const relay = 'wss://relay.example.com/relay/connect';
expect('words typed on several lines are still words', readPairingCode(phrase.replaceAll(' ', '\n')), {
  words: phrase.replaceAll(' ', '\n'),
});
expect('a relay on the same line', readPairingCode(`${phrase} ${relay}`), { words: phrase, relay });
expect('a trailing line break', readPairingCode(`${phrase}\n${relay}\n`), { words: phrase, relay });
expect('the scheme in capitals', readPairingCode(`${phrase}\nWSS://relay.example.com`), {
  words: phrase,
  relay: 'wss://relay.example.com',
});
for (const address of [
  'ws://relay.example.com/relay/connect',
  'http://relay.example.com',
  'https://relay.example.com',
  'wss://',
  'wss://user@relay.example.com',
  'ftp://relay.example.com',
]) {
  expect(`${address} is refused`, readPairingCode(`${phrase}\n${address}`), { words: phrase, refused: address });
}

if (problems.length) {
  console.error('The app reads the pairing code differently from how the server writes it:\n' + problems.map((p) => '  ' + p).join('\n'));
  process.exit(1);
}
console.log(`Pairing codes: ${rows.length} server cases and the app's own read as written.`);
