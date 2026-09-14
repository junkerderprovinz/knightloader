// The hidden fourth motion level stays hidden, stays switchable off, and stays
// behind the accessibility gate.
//
// GlimStone 1.17.0 adds `storm`: a fourth intensity nobody is offered, reached
// by setting motion to the top level and tapping that same option five more
// times. The numbers are the cheap half. The rule it exists to establish is the
// expensive one, and it is what this script guards:
//
//   AN EASTER EGG THAT CHANGES BEHAVIOUR MUST BE SWITCHABLE BACK OFF, AND MUST
//   NOT QUIETLY BECOME A PERMANENT ENTRY IN A SETTINGS LIST.
//
// WHY THIS IS NOT THE WEB'S SCRIPT WITH THE PATHS CHANGED. There is no CSS on a
// phone, so there is no `@media (prefers-reduced-motion: no-preference)` block
// to check a selector's position inside. The gate here is a function call -
// AccessibilityInfo.isReduceMotionEnabled() - and a function call has no
// position, only callers. So the phone's version of "the storm sits inside the
// gate" is "there is exactly ONE reader of that signal, every level resolves
// through it, and it answers `off` for every level including the hidden one."
// That is checked by RUNNING the module rather than by reading it: node strips
// the types and calls the real functions, so these are measurements and not
// pattern matches.
//
// WHAT BREAKS WITHOUT EACH CHECK, and every one of them is invisible in a
// build, a type check and a screenshot alike:
//
//   gate      A "more animation" switch that does not pass through the reduced
//             motion signal is the one way this level could genuinely do harm:
//             a phone whose owner asked the operating system for less motion
//             would get MORE movement than the three visible levels can
//             produce. The other three are safe because of where the gate sits
//             and not because of a check in front of each of them, so the
//             fourth has to reach it by the same road. A second screen reading
//             AccessibilityInfo for itself is the failure: the signal then has
//             two readers that can disagree, and the one that forgets is the
//             one that animates.
//
//   numbers   A level is a different NUMBER, never a different animation. A
//             storm that leaves a figure out inherits the top visible level's
//             own value and arrives as a slightly longer wild, which gets
//             reported as "the egg does nothing"; one that invents a figure the
//             other levels do not have is a forked animation wearing an
//             intensity's clothes. And the one figure the language names for
//             the phone by hand - springDamping 0.34 against wild's 0.68 - is
//             checked as a value, because it is the whole reason the two
//             surfaces overshoot ALIKE rather than merely both overshooting.
//
//   picker    The list a picker renders must not contain storm. This is the
//             defect the whole rule came from: the first build stored a "found
//             it" flag and one gesture put a fourth option in the settings for
//             ever after, which turns a secret into a setting somebody has to
//             explain to themselves months later with no memory of how it got
//             there.
//
//   stored    ...while a STORED storm is still accepted at boot, or the gesture
//             produces a setting that silently forgets itself on the next
//             launch. Validating a stored value and populating a picker are two
//             different questions and this is the axis where treating them as
//             one shows up, so the two lists have to be two lists.
//
//   gesture   Five taps on the top level, from the top level, and nothing else.
//             Four taps must not open it, a tap on another level must reset the
//             count, and tapping "off" five times must do nothing at all - a
//             secret that opens under annoyance is a bug report waiting to be
//             filed.
//
//   memory    The "found it" fact lives in the settings screen's own state and
//             NEVER in storage. It is the same mistake as the picker one, one
//             layer down: persist it and the option is back on the next launch,
//             which is exactly the permanent entry the rule forbids. Grep-shaped
//             on purpose - any storage write whose key or value mentions the
//             hidden level is the failure, wherever it is written.
//
// WHAT IT DOES NOT SEE. It cannot tell whether a screen actually SPENDS the
// numbers it is handed: a component that keeps its old literal 4px shake passes
// here and is caught by looking at the screen. It judges the picker's call site
// by shape, so a fourth entry assembled through a variable it cannot follow
// would slip past.
//
// Run by hand and by CI, from mobile/: `node check-hidden-motion-level.mjs`
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const rel = (p) => relative(here, p).replace(/\\/g, '/');

const problems = [];
const fail = (why) => problems.push(why);

const MOTION_MODULE = 'src/theme/motion.ts';
const GATE_FILE = 'src/theme/MotionContext.tsx';

// --- the module, actually run ----------------------------------------------

let m = null;
try {
  m = await import(new URL(`./${MOTION_MODULE}`, import.meta.url).href);
} catch (e) {
  fail(`${MOTION_MODULE}: cannot be loaded - ${String(e).split('\n')[0]}`);
}

if (m) {
  const levels = m.MOTION_LEVELS;
  const stored = m.MOTION_STORED;

  if (!Array.isArray(levels)) fail(`${MOTION_MODULE}: no MOTION_LEVELS array - the list a picker renders has to be a list of its own`);
  else if (levels.includes('storm')) {
    fail(`${MOTION_MODULE}: MOTION_LEVELS contains 'storm', so the picker offers the hidden level as an ordinary fourth entry`);
  }

  if (!Array.isArray(stored)) fail(`${MOTION_MODULE}: no MOTION_STORED array - validating a stored value and populating a picker are two questions`);
  else if (!stored.includes('storm')) {
    fail(`${MOTION_MODULE}: MOTION_STORED does not accept 'storm', so a found level forgets itself on the next launch`);
  }

  // The boot reader validates against the STORED list, measured rather than read.
  if (typeof m.asMotion !== 'function') fail(`${MOTION_MODULE}: no asMotion() - nothing turns a stored string back into a level at boot`);
  else {
    if (m.asMotion('storm') !== 'storm') {
      fail(`${MOTION_MODULE}: asMotion('storm') answers ${JSON.stringify(m.asMotion('storm'))} - a stored storm is thrown away at boot`);
    }
    if (m.asMotion('nonsense') !== m.DEFAULT_MOTION) {
      fail(`${MOTION_MODULE}: asMotion() lets a value through that no picker and no gesture can produce`);
    }
  }

  // --- gate: reduced motion wins over every level, the hidden one included ---
  if (typeof m.resolveMotion !== 'function') {
    fail(`${MOTION_MODULE}: no resolveMotion() - there is no single place where the OS signal beats the chosen level`);
  } else {
    for (const level of new Set([...(stored ?? []), 'storm'])) {
      const got = m.resolveMotion(level, true);
      if (got !== 'off') {
        fail(
          `${MOTION_MODULE}: resolveMotion(${JSON.stringify(level)}, reduced) answers ${JSON.stringify(got)} and not 'off' - ` +
            `a phone whose owner asked the system for less motion would still get this level's numbers`,
        );
      }
    }
    if (m.resolveMotion('storm', false) !== 'storm') {
      fail(`${MOTION_MODULE}: resolveMotion does not pass 'storm' through when nothing is reduced - the level is unreachable`);
    }
  }

  // --- numbers: same figures at every level, and the two the language names ---
  const table = m.MOTION;
  if (!table || typeof table !== 'object') {
    fail(`${MOTION_MODULE}: no MOTION table - a level has to BE a set of numbers, or it is a forked animation`);
  } else {
    const top = table.wild;
    if (!top) fail(`${MOTION_MODULE}: MOTION has no 'wild' entry - the top visible level is what the hidden one is measured against`);
    else {
      const want = Object.keys(top).sort();
      for (const level of ['off', 'subtle', 'wild', 'storm']) {
        const entry = table[level];
        if (!entry) {
          fail(`${MOTION_MODULE}: MOTION has no '${level}' entry`);
          continue;
        }
        const got = Object.keys(entry).sort();
        const missing = want.filter((k) => !got.includes(k));
        const extra = got.filter((k) => !want.includes(k));
        if (missing.length) {
          fail(
            `${MOTION_MODULE}: level '${level}' never sets ${missing.join(', ')}, so it inherits the top visible level's own value - ` +
              `an intensity is a different number, never a missing one`,
          );
        }
        if (extra.length) {
          fail(
            `${MOTION_MODULE}: level '${level}' sets ${extra.join(', ')}, which no other level has - ` +
              `an intensity is a different number, never a knob of its own`,
          );
        }
      }
      // The one figure GlimStone 1.17.0 writes down for the phone by hand.
      if (table.storm?.springDamping !== 0.34 || table.wild?.springDamping !== 0.68) {
        fail(
          `${MOTION_MODULE}: springDamping is ${JSON.stringify(table.wild?.springDamping)} at wild and ` +
            `${JSON.stringify(table.storm?.springDamping)} at storm; GlimStone 1.17.0 names 0.68 and 0.34 for the phone, ` +
            `so that this surface and the web overshoot ALIKE rather than merely both overshooting`,
        );
      }
      // Somebody who asked for less movement asked for less movement, not for a
      // faster bounce: only the top two levels may overshoot at all.
      for (const level of ['off', 'subtle']) {
        if (table[level]?.springDamping < 1) {
          fail(`${MOTION_MODULE}: level '${level}' overshoots (springDamping ${table[level].springDamping} < 1) - a quieter level is not a bouncier one`);
        }
      }
    }
  }

  // --- the gesture, exercised ------------------------------------------------
  if (typeof m.stormTap !== 'function') {
    fail(`${MOTION_MODULE}: no stormTap() - the gesture is the whole mechanism and it belongs in one function`);
  } else {
    const top = Array.isArray(levels) ? levels[levels.length - 1] : 'wild';
    const taps = m.STORM_TAPS ?? 5;

    let s = { taps: 0 };
    const four = [];
    for (let i = 0; i < taps - 1; i++) four.push(m.stormTap(s, top, top));
    if (four.some((x) => x !== undefined)) fail(`${MOTION_MODULE}: stormTap opens the level before the ${taps}th tap`);
    if (m.stormTap(s, top, top) !== 'storm') fail(`${MOTION_MODULE}: ${taps} taps on the top level do not reach the storm`);

    // Any other level resets the count, so a run cannot be assembled out of
    // taps that were never all on the same option.
    s = { taps: 0 };
    for (let i = 0; i < taps - 1; i++) m.stormTap(s, top, top);
    m.stormTap(s, 'off', top);
    if (m.stormTap(s, top, top) === 'storm') fail(`${MOTION_MODULE}: a tap on another level does not reset the count`);

    // Tapping the floor five times means somebody is annoyed, not curious.
    s = { taps: 0 };
    let opened = false;
    for (let i = 0; i < taps + 2; i++) if (m.stormTap(s, 'off', 'off') !== undefined) opened = true;
    if (opened) fail(`${MOTION_MODULE}: the storm can be reached from 'off' - a secret that opens under annoyance is a bug report waiting to be filed`);

    // Reachable only from the level already in force, never from a cold tap.
    s = { taps: 0 };
    opened = false;
    for (let i = 0; i < taps + 2; i++) if (m.stormTap(s, top, 'subtle') !== undefined) opened = true;
    if (opened) fail(`${MOTION_MODULE}: the storm opens while another level is in force - the gesture is pressing a button that is ALREADY pressed`);
  }
}

// --- one reader of the OS signal, and it is the gate ------------------------

const walk = (dir) => {
  const out = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) out.push(...walk(p));
    else if (/\.tsx?$/.test(name)) out.push(p);
  }
  return out;
};

/** Comments blanked, offsets kept byte for byte, so a line number is the real
 *  one AND a paragraph EXPLAINING what is not persisted is not read as a
 *  persistence. Written the naive way this check fails on its own doctrine. */
const strip = (text) =>
  text
    .replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '))
    .replace(/^([^\n'"`]*?)\/\/.*$/gm, (_, head) => head);

const files = walk(join(here, 'src'));
const readers = [];
for (const file of files) {
  const code = strip(readFileSync(file, 'utf8'));
  if (/isReduceMotionEnabled|['"]reduceMotionChanged['"]/.test(code)) readers.push(rel(file));
}
if (readers.length === 0) {
  fail('nothing in src/ reads AccessibilityInfo.isReduceMotionEnabled - the accessibility gate does not exist, and a motion axis without one is the one shape this level could do harm in');
} else if (readers.length > 1 || readers[0] !== GATE_FILE) {
  fail(
    `the reduced-motion signal is read in ${readers.length} place(s) (${readers.join(', ')}); it belongs in ${GATE_FILE} and nowhere else - ` +
      `two readers are two answers, and the one that forgets is the one that animates`,
  );
}

// --- the "found it" flag never reaches storage ------------------------------

for (const file of files) {
  const code = strip(readFileSync(file, 'utf8'));
  code.split('\n').forEach((line, i) => {
    if (!/AsyncStorage|SecureStore|setItem|getItem/i.test(line)) return;
    if (!/storm|stormFound|motionFound|gefunden/i.test(line)) return;
    fail(
      `${rel(file)}:${i + 1} a storage line mentions the hidden level: "${line.trim().slice(0, 90)}" - ` +
        `the CHOSEN value persists like any other, the "found it" fact never does`,
    );
  });
}

// --- the picker's call site -------------------------------------------------

const SETTINGS = 'src/screens/SettingsScreen.tsx';
const settings = strip(readFileSync(join(here, SETTINGS), 'utf8'));
if (!/MOTION_LEVELS/.test(settings)) {
  fail(`${SETTINGS}: the motion picker does not build its options from MOTION_LEVELS, so nothing keeps the hidden level out of it`);
}
if (!/stormTap/.test(settings)) {
  fail(`${SETTINGS}: nothing calls stormTap - the gesture has no call site, so the level is unreachable`);
}
if (/useState[^\n]*stormFound|const \[stormFound/.test(settings) === false && /stormFound/.test(settings)) {
  fail(`${SETTINGS}: stormFound is referenced but is not this screen's own state - "found" lives in the screen and never anywhere that outlives it`);
}

if (problems.length) {
  console.error(`check-hidden-motion-level: ${problems.length} problem(s) with the hidden fourth motion level.`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

console.log(
  'ok: storm has the same numbers every level has, no picker offers it, a stored one is accepted at boot, ' +
    'the gesture reaches it only from the top level, one place reads the reduced-motion signal, ' +
    'and nothing writes the discovery to storage',
);
