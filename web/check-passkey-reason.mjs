// The server's verdict travels as a BOOLEAN; the sentence is this app's own
// copy, translated (GlimStone 1.15.0).
//
// WHAT THIS IS. GlimStone names the test for this surface in the rule itself:
// feed the server field an obviously non-human string, then assert positively
// on the translated text and negatively on that string. That test is run live,
// in a browser, against a real instance - a network intercept rewrites
// /api/auth/passkeys so its `reason` reads a marker no human would write, and
// the German page is checked for the German paragraph and against the marker.
// This file is the half of it that can run without a browser, on every build,
// so the property is held between live runs rather than only at the moment
// somebody remembers to do one.
//
// WHY THE PROPERTY MATTERS ENOUGH TO GUARD. GET /api/auth/passkeys answers with
// `supported: false` and, beside it, a `reason` in English. That sentence is a
// diagnostic: it is for somebody reading the API or the log. The paragraph the
// card shows is not a diagnostic - it is the text that explains a whole feature
// to somebody whose interface is running in their own language, and on a stock
// KnightLoader install (opened on a LAN IP address) it is the only thing that
// card ever shows. Rendering the server's string there puts an English
// paragraph in the middle of a Japanese page, and nothing in a build notices,
// because a string is a string.
//
// It is the mirror image of the failure the fallback rule guards: there the
// app's own copy gets forgotten, here a diagnostic gets promoted to be the copy.
//
// WHAT IT CHECKS.
//
//   not read     no source under src/ reads `reason` off the passkey status.
//                That covers the direct render, and also the innocent-looking
//                `status.reason ?? t(...)` that renders it whenever the server
//                happens to send one - which is every time it matters.
//   not typed    PasskeyStatus in lib/api.ts declares no `reason` field. A type
//                that does not carry it is a stronger guard than a rule about
//                not reading it: tsc then refuses the render before this does.
//   own copy     the card really does draw its own translated paragraph, so
//                this cannot pass by the card having no explanation at all -
//                which is the other way to satisfy "does not show the server's
//                sentence", and much worse than the failure it prevents.
//
// Run by CI and by hand: `node web/check-passkey-reason.mjs`
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join, relative } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const src = join(here, 'src');

/** Every .ts/.tsx under src/. */
function sources(dir = src, out = []) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) sources(path, out);
    else if (/\.tsx?$/.test(name)) out.push(path);
  }
  return out;
}

const problems = [];

// ---------------------------------------------------------------------------
// not read
// ---------------------------------------------------------------------------
//
// The shapes that count as reading it, and nothing wider. A bare word "reason"
// appears in ordinary prose all over this tree, so the pattern is anchored to
// property access on something that could be the status - a member access or a
// destructure - rather than to the word.
//
// Deliberately NOT "the word reason anywhere near the word PasskeyStatus". The
// first draft of this file had exactly that, and it reported api.ts's own
// doc comment - the paragraph explaining why the field is not read - as a
// violation. A check that accuses the sentence stating the rule is a check
// somebody switches off. What the declaration may and may not contain is the
// next section's job, where it can be read as code rather than as prose.
const READS = [
  /\bstatus\s*[?]?\.\s*reason\b/,
  /\bpasskeys?\s*[?]?\.\s*reason\b/,
  /\{[^}]*\breason\b[^}]*\}\s*=\s*(?:await\s+)?fetchPasskeys\(/,
];

for (const file of sources()) {
  const text = readFileSync(file, 'utf8');
  if (!/passkey/i.test(text)) continue;
  for (const re of READS) {
    const hit = re.exec(text);
    if (hit) {
      problems.push(
        `${relative(here, file)} reads the server's own refusal sentence (${hit[0].trim()}). ` +
          "The verdict is `supported`, a boolean; the paragraph is this app's copy, translated",
      );
    }
  }
}

// ---------------------------------------------------------------------------
// not typed
// ---------------------------------------------------------------------------
const api = readFileSync(join(src, 'lib', 'api.ts'), 'utf8');
const decl = /export interface PasskeyStatus \{([\s\S]*?)\n\}/.exec(api);
if (!decl) {
  problems.push('lib/api.ts no longer declares PasskeyStatus, so this check is looking at nothing');
} else {
  if (/^\s*reason\s*[?]?\s*:/m.test(decl[1])) {
    problems.push(
      'lib/api.ts declares `reason` on PasskeyStatus. Leave it off the type: a field the ' +
        'front end cannot see is a field it cannot render, and tsc then refuses the mistake ' +
        'before this script has to notice it',
    );
  }
  if (!/\bsupported\s*:\s*boolean/.test(decl[1])) {
    problems.push('PasskeyStatus has no `supported: boolean`, which is the verdict the card reads');
  }
}

// ---------------------------------------------------------------------------
// own copy
// ---------------------------------------------------------------------------
const card = join(src, 'pages', 'settings', 'access', 'PasskeyCard.tsx');
let cardText = '';
try {
  cardText = readFileSync(card, 'utf8');
} catch {
  problems.push(`${relative(here, card)} is missing, so nothing draws the refusal at all`);
}
for (const key of ['auth.passkey.unavailableTitle', 'auth.passkey.unavailableReason']) {
  if (cardText && !cardText.includes(`'${key}'`)) {
    problems.push(
      `${relative(here, card)} does not draw ${key}. Not showing the server's sentence is only ` +
        'half the rule; the other half is showing an explanation of its own',
    );
  }
}
const en = readFileSync(join(src, 'lib', 'locales', 'en.ts'), 'utf8');
for (const key of ['auth.passkey.unavailableTitle', 'auth.passkey.unavailableReason']) {
  if (!en.includes(`'${key}'`)) {
    problems.push(`${key} is not in en.ts, so 42 catalogues cannot carry it either`);
  }
}

if (problems.length > 0) {
  console.error(`${problems.length} passkey refusal problem(s):`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log("ok: the passkey refusal is the app's own translated copy, and the server's verdict is a boolean");
