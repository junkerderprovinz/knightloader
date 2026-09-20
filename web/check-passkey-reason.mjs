// The server's verdict travels as a boolean; the sentence is this app's own
// copy, translated (GlimStone 1.15.0).
//
// GET /api/auth/passkeys answers with `supported: false` and an English
// `reason` beside it. That sentence is a diagnostic for somebody reading the
// API or the log, while the paragraph on the card explains the feature to
// somebody whose interface runs in their own language, and on a stock install
// opened on a LAN address it is the only thing that card ever shows. Rendering
// the server's string there puts an English paragraph in the middle of a
// Japanese page, and no build notices, because a string is a string.
//
// GlimStone names a live browser test for this: a network intercept rewrites
// the `reason` to a marker no human would write, and the German page is checked
// for the German paragraph and against the marker. This is the half that runs
// without a browser, so the property holds between those runs.
//
// Three things are checked: that no source under src/ reads `reason` off the
// passkey status, including the innocent-looking `status.reason ?? t(...)`;
// that PasskeyStatus in lib/api.ts declares no `reason` field, so tsc refuses
// the render before this script has to; and that the card draws its own
// translated paragraph, since a card with no explanation at all would satisfy
// the first two.
//
// Run: `node web/check-passkey-reason.mjs`
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

// Nobody reads it: the shapes that count as reading it, and nothing wider. The
// bare word "reason" appears in prose all over this tree, so the pattern is
// anchored to property access on something that could be the status, a member
// access or a destructure. What the declaration itself may contain is the next
// section's job, where it is read as code rather than as prose.
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

// And the type does not carry it either.
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

// The card draws an explanation of its own.
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
