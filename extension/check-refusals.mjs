// Checks how the options page and popup treat a control that cannot act
// (GlimStone 1.15.0 and 1.16.0): dim what still acts, remove what does not, and
// write a reason only when the environment forbids the feature.
//
//   inert     Nothing is dimmed and switched off by a wrapper at once.
//             pointer-events: none still lets the keyboard press the control,
//             so a real refusal uses the control's own disabled.
//   subctl    The Click'n'Load countdown row goes with its switch; with the
//             feature off nothing reads the number.
//   notice    Following the instance without a group is replaced by a notice
//             with a real fill (--status-warn-bg-soft in every theme).
//   copy      The reason is translated through t(), never a literal.
//
// It checks the shape of four files, not behaviour; the timing was verified in
// a browser.
//
// Run by CI and by hand: `node extension/check-refusals.mjs`
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const read = (...p) => readFileSync(join(here, ...p), 'utf8');

/** Blanks comments but keeps offsets, so line numbers stay right and a comment
 *  describing a banned shape is not read as one. */
const decomment = (text) =>
  text
    .replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '))
    .replace(/^([^\n'"`]*?)\/\/.*$/gm, (_, head) => head);

const problems = [];
const fail = (why) => problems.push(why);
const lineOf = (text, index) => text.slice(0, index).split('\n').length;

const optionsRaw = read('src', 'options.js');
const options = decomment(optionsRaw);
const popupRaw = read('src', 'popup.js');
const popup = decomment(popupRaw);
const html = read('src', 'options.html');
const css = read('src', 'glimstone.css');

// inert: without a framework, a wrapper refusal can only be an inline
// pointerEvents, so any occurrence is the defect.
for (const src of [
  ['src/options.js', options],
  ['src/popup.js', popup],
]) {
  const [name, text] = src;
  for (const m of text.matchAll(/pointerEvents\s*=/g)) {
    fail(
      `${name}:${lineOf(text, m.index)} a control group is switched off by a wrapper (style.pointerEvents). ` +
        `GlimStone 1.16.0: either the value still acts, in which case it may not be deadened, or it does not, ` +
        `in which case the control goes - and a wrapper is invisible to the keyboard either way. Use the button's own disabled.`,
    );
  }
}
// The proper refusal has to exist, or the check above passes on a page that
// stopped refusing anything.
if (!/function setRefused\s*\(/.test(options)) {
  fail('src/options.js: no setRefused() - the refusal that replaced the wrapper is gone, so nothing switches a locked control off');
}
if (!/\bb\.disabled\s*=|\.disabled\s*=\s*off\b/.test(options)) {
  fail('src/options.js: nothing sets a control\'s own `disabled` - a refusal that is only a dimming is a control that still answers');
}
if (!/:disabled/.test(css)) {
  fail('src/glimstone.css: no :disabled rules - a refused control would look exactly like a live one');
}

// subctl
if (!/cnlCountdownRow\.hidden\s*=/.test(options)) {
  fail(
    'src/options.js: the Click\'n\'Load countdown row is never hidden. With the feature off nothing can read the number ' +
      '(popup.js startCountdown runs only for an origin the disabled catcher cannot produce), so the row answers nothing and goes.',
  );
}
// On render and on click, or the row only catches up after a reload.
{
  const hits = [...options.matchAll(/cnlCountdownRow\.hidden\s*=/g)];
  if (hits.length === 1) {
    fail(
      `src/options.js:${lineOf(options, hits[0].index)} the countdown row is hidden in one place only. ` +
        `It has to follow the switch on the CLICK as well as on render, or it waits for a reload.`,
    );
  }
}

// notice
for (const id of ['followUnavailable', 'followUnavailableTitle', 'followUnavailableReason']) {
  if (!html.includes(`id="${id}"`)) {
    fail(`src/options.html: no #${id} - the environment refusal has no paragraph, so the control was simply taken away in silence`);
  }
}
if (!/followInstanceRow\.hidden\s*=/.test(options)) {
  fail(
    'src/options.js: the follow-the-instance switch is never removed. Offered with no group it turns on, fails and snaps back, ' +
      'which is the button-that-fails GlimStone 1.15.0 replaces with no control plus the reason.',
  );
}
if (!/--status-warn-bg-soft/.test(css)) {
  fail(
    'src/glimstone.css: --status-warn-bg-soft is not defined. A frame is only a frame if its token exists - ' +
      'the notice would ship with a radius, a border and no fill at all, and nothing in a build could notice.',
  );
}
if (!/\.glim-unavailable\s*\{[^}]*--status-warn-bg-soft/.test(css)) {
  fail('src/glimstone.css: .glim-unavailable does not spend --status-warn-bg-soft, so the box the rule asks for has no ground');
}
// The token has to exist in every theme block, or one theme loses the fill.
{
  const defs = [...css.matchAll(/--status-warn-bg-soft\s*:/g)].length;
  const warnDefs = [...css.matchAll(/--status-warn-bg\s*:/g)].length;
  if (defs !== warnDefs) {
    fail(
      `src/glimstone.css: --status-warn-bg-soft is defined ${defs} time(s) against --status-warn-bg's ${warnDefs}. ` +
        `A status token that misses a theme block leaves that theme with no fill.`,
    );
  }
}

// copy
for (const key of ['options.followNoGroup', 'options.accentRainbowOwns']) {
  if (!options.includes(`'${key}'`)) {
    fail(`src/options.js: ${key} is in the catalogues and read by nothing - the sentence the rule owes never reaches the page`);
  }
}
if (!/followUnavailableReasonEl\.textContent\s*=\s*t\(/.test(options)) {
  fail(
    'src/options.js: the refusal reason is not read through t(). It is the paragraph that explains a feature to somebody ' +
      'whose interface runs in their language, not a diagnostic.',
  );
}

if (problems.length) {
  console.error(`check-refusals: ${problems.length} problem(s).`);
  console.error('GlimStone 1.15.0/1.16.0: dim what still acts, remove what does not, and write the reason only for the environment.');
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

console.log(
  'ok: nothing is dimmed and switched off at once, the countdown goes with its switch, ' +
    'the follow switch is replaced by a translated notice on a real fill',
);
