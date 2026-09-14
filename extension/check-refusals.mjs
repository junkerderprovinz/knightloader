// The three answers to a control that cannot act, and this extension gives each
// of them the right one.
//
// THE RULE (GlimStone 1.15.0, "The second way in", which states the test that
// decides all three, and 1.16.0, "Switches", which settles the pair that had
// been contradicting each other for six releases):
//
//   Does the grey state say something about the THING the control touches
//   (dim it, it is reporting), about a decision taken elsewhere on the page
//   (leave it out), or about the environment not permitting the thing at all
//   (leave it out AND write the reason)? Only the third owes prose.
//
// WHAT BREAKS WITHOUT IT, one failure per check, all of them measured on this
// page before they were fixed and none of them visible in a syntax check, a
// locale check or a screenshot of the default state:
//
//   inert     A wrapper that is dimmed AND switched off at once is the one
//             shape both halves of 1.16.0 rule out. If the value still acts,
//             deadening it lies about something still in force; if it does not,
//             dimming it leaves furniture somebody can see, read and reach for
//             that answers nothing. And `pointer-events: none` is invisible to
//             the KEYBOARD, which is the fault that makes the pair worth
//             banning outright rather than arguing case by case: with rainbow
//             mode on, this page rendered the accent row at opacity .5 with
//             pointer-events none, and focusing the second swatch and pressing
//             it still set --accent to #1d99f3 and wrote it to storage. A row
//             unreachable by mouse and perfectly reachable by Tab is worse than
//             either honest answer. A control that genuinely refuses says so
//             through its own `disabled`, which a screen reader reads and the
//             keyboard cannot fire.
//
//   subctl    A control that hangs off a switch goes ABSENT while that switch
//             is off, never dimmed and never simply left standing. The
//             Click'n'Load countdown stood fully live while the feature was
//             switched off - and the number is read in exactly one place,
//             popup.js's startCountdown(), which only runs for a parked send
//             whose origin is 'cnl', which handleCnl() refuses to park while
//             the switch is off. Nothing at all was behind it.
//
//   notice    A capability the environment forbids owes a paragraph, and a
//             paragraph owes a real fill. The follow-the-instance switch was
//             offered with no group to take a look from: it turned on
//             optimistically, failed against a relay session with nothing to
//             talk to, and snapped back. The box that replaces it has to exist,
//             has to be wired to both its sentences, and has to spend
//             --status-warn-bg-soft - a frame is only a frame if its token
//             exists, which is the trap 1.15.0 added that token to close.
//
//   copy      The reason is UI copy in the reader's own language, never a
//             server's or a library's own sentence. Checked as the language
//             asks: the key exists in every catalogue (check-locales.mjs) and
//             the call site reads it through t() rather than pasting a literal.
//
// WHAT IT DOES NOT SEE. It reads four files and judges shape, never behaviour:
// it cannot tell whether the countdown row is hidden at the right MOMENT, only
// that something hides it, and it cannot tell whether the notice says anything
// useful. Those were measured live in a browser against the rendered page; this
// is what stops them coming back.
//
// Run by CI and by hand: `node extension/check-refusals.mjs`
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const read = (...p) => readFileSync(join(here, ...p), 'utf8');

/** Block comments blanked with their offsets kept, so a line number stays the
 *  real one AND a paragraph EXPLAINING a banned shape is not read as one. */
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

// --- inert: nothing is switched off by a wrapper ----------------------------
//
// Grep-shaped on purpose. This extension has no component framework and no
// class-list utilities, so a wrapper refusal can only be written one way: an
// inline `pointerEvents`. Any occurrence is the defect, wherever it is written.
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
// The honest form has to actually be present, or the check above passes on a
// page that simply stopped refusing anything.
if (!/function setRefused\s*\(/.test(options)) {
  fail('src/options.js: no setRefused() - the refusal that replaced the wrapper is gone, so nothing switches a locked control off');
}
if (!/\bb\.disabled\s*=|\.disabled\s*=\s*off\b/.test(options)) {
  fail('src/options.js: nothing sets a control\'s own `disabled` - a refusal that is only a dimming is a control that still answers');
}
if (!/:disabled/.test(css)) {
  fail('src/glimstone.css: no :disabled rules - a refused control would look exactly like a live one');
}

// --- subctl: the countdown goes with its switch -----------------------------
if (!/cnlCountdownRow\.hidden\s*=/.test(options)) {
  fail(
    'src/options.js: the Click\'n\'Load countdown row is never hidden. With the feature off nothing can read the number ' +
      '(popup.js startCountdown runs only for an origin the disabled catcher cannot produce), so the row answers nothing and goes.',
  );
}
// Both places, or the row only catches up on the next page load - which is a
// dimmed control wearing a delay.
{
  const hits = [...options.matchAll(/cnlCountdownRow\.hidden\s*=/g)];
  if (hits.length === 1) {
    fail(
      `src/options.js:${lineOf(options, hits[0].index)} the countdown row is hidden in one place only. ` +
        `It has to follow the switch on the CLICK as well as on render, or it waits for a reload.`,
    );
  }
}

// --- notice: the refusal owes a paragraph, and the paragraph owes a fill -----
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
// One shape, one treatment: the token is defined in every theme block, or the
// box has a fill on one theme and none on the other.
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

// --- copy: the reason is translated, never a literal ------------------------
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
