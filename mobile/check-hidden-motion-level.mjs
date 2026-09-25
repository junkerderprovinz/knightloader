// The hidden fourth motion level stays hidden, stays switchable off, and passes
// the accessibility gate only for what comes to an end.
//
// GlimStone 1.17.0 adds `storm`: a fourth intensity nobody is offered, reached
// by setting motion to the top level and tapping that same option five more
// times. The rule it establishes is that an easter egg which changes behaviour
// has to be switchable back off and must not turn into a permanent entry in a
// settings list.
//
// The web's version of this script checks where a selector sits inside
// `@media (prefers-reduced-motion: no-preference)`. A phone has no such block,
// only a function call, so the phone's equivalent is that one file reads the
// signal and every level resolves through it. That is checked by running the
// module: node strips the types and calls the real functions, so these are
// measurements rather than pattern matches.
//
// What each check catches, all of it invisible in a build, a type check and a
// screenshot alike:
//
//   gate      The three offered levels answer `off` while the system asks for
//             less motion, because somebody who set that never chose any of
//             them. The storm is the one exception (GlimStone 2.1.0): five taps
//             on an option already chosen are a request, not an inherited
//             default. Exempting it is half the rule and restoring it is the
//             other half, so under reduced motion it has to keep its own
//             one-shot figures rather than falling to a quieter level's, and
//             its loop has to stop, since wanting more movement is not wanting
//             something that never stops. A second screen reading
//             AccessibilityInfo for itself gives the signal two readers that
//             can disagree, and the one that forgets is the one that animates.
//
//   numbers   A level is a different number, never a different animation. A
//             storm that leaves a figure out inherits the top visible level's
//             value and arrives as a slightly longer wild; one that invents a
//             figure the other levels lack is a forked animation wearing an
//             intensity's clothes. Every figure GlimStone's reference table
//             (motionNative.ts) sets reaches its level unchanged, because
//             that table is what makes the apps of the family move alike
//             rather than merely all move, and the two quiet levels never
//             overshoot.
//
//   picker    The list a picker renders must not contain storm. The first build
//             stored a "found it" flag, so one gesture put a fourth option in
//             the settings for good.
//
//   stored    A stored storm is still accepted at boot, or the gesture produces
//             a setting that forgets itself on the next launch. Validating a
//             stored value and populating a picker are two questions, so the
//             two lists have to be two lists.
//
//   gesture   Five taps on the top level, from the top level, and nothing else.
//             Four taps do not open it, a tap on another level resets the
//             count, and tapping "off" five times does nothing: a secret that
//             opens under annoyance is a bug report waiting to be filed.
//
//   memory    The "found it" fact lives in the settings screen's own state and
//             never in storage; persist it and the option is back on the next
//             launch. Grep-shaped, so any storage write whose key or value
//             mentions the hidden level is the failure.
//
// It cannot tell whether a screen actually spends the numbers it is handed: a
// component that keeps its own literal 4px shake passes here. It judges the
// picker's call site by shape, so a fourth entry assembled through a variable
// would slip past.
//
// Run by hand and by CI, from mobile/: `node check-hidden-motion-level.mjs`
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { registerHooks } from 'node:module';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

// The app imports its own modules without an extension, which Metro resolves
// and Node does not, so a relative import without one is tried as a .ts file.
registerHooks({
  resolve(specifier, context, next) {
    if (/^\.\.?\//.test(specifier) && !/\.\w+$/.test(specifier)) return next(`${specifier}.ts`, context);
    return next(specifier, context);
  },
});

const here = dirname(fileURLToPath(import.meta.url));
const rel = (p) => relative(here, p).replace(/\\/g, '/');

const problems = [];
const fail = (why) => problems.push(why);

const MOTION_MODULE = 'src/theme/motion.ts';
const NATIVE_MODULE = 'src/theme/motionNative.ts';
const GATE_FILE = 'src/theme/MotionContext.tsx';

let m = null;
try {
  m = await import(new URL(`./${MOTION_MODULE}`, import.meta.url).href);
} catch (e) {
  fail(`${MOTION_MODULE}: cannot be loaded - ${String(e).split('\n')[0]}`);
}
let ref = null;
try {
  ref = await import(new URL(`./${NATIVE_MODULE}`, import.meta.url).href);
} catch (e) {
  fail(`${NATIVE_MODULE}: cannot be loaded - ${String(e).split('\n')[0]}`);
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

  // The boot reader validates against the stored list, measured rather than read.
  if (typeof m.asMotion !== 'function') fail(`${MOTION_MODULE}: no asMotion() - nothing turns a stored string back into a level at boot`);
  else {
    if (m.asMotion('storm') !== 'storm') {
      fail(`${MOTION_MODULE}: asMotion('storm') answers ${JSON.stringify(m.asMotion('storm'))} - a stored storm is thrown away at boot`);
    }
    if (m.asMotion('nonsense') !== m.DEFAULT_MOTION) {
      fail(`${MOTION_MODULE}: asMotion() lets a value through that no picker and no gesture can produce`);
    }
  }

  // Reduced motion wins over every offered level; the storm keeps what ends and
  // loses what loops.
  if (typeof m.resolveMotion !== 'function' || typeof m.motionNumbers !== 'function') {
    fail(`${MOTION_MODULE}: no resolveMotion() or motionNumbers() - there is no single place where the OS signal meets the chosen level`);
  } else {
    const table = m.MOTION ?? {};
    const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);
    for (const level of levels ?? []) {
      const got = m.resolveMotion(level, true);
      if (got !== 'off') {
        fail(
          `${MOTION_MODULE}: resolveMotion(${JSON.stringify(level)}, reduced) answers ${JSON.stringify(got)} and not 'off' - ` +
            `a phone whose owner asked the system for less motion would still get this level's numbers`,
        );
      }
      if (!same(m.motionNumbers(level, true), table.off)) {
        fail(`${MOTION_MODULE}: motionNumbers(${JSON.stringify(level)}, reduced) is not the 'off' row - an offered level still moves under reduced motion`);
      }
    }
    for (const level of stored ?? []) {
      if (!same(m.motionNumbers(level, false), table[level])) {
        fail(`${MOTION_MODULE}: motionNumbers(${JSON.stringify(level)}) without reduced motion is not that level's own row`);
      }
    }
    if (m.resolveMotion('storm', false) !== 'storm') {
      fail(`${MOTION_MODULE}: resolveMotion does not pass 'storm' through when nothing is reduced - the level is unreachable`);
    }
    if (m.resolveMotion('storm', true) !== 'storm') {
      fail(
        `${MOTION_MODULE}: the storm resolves to ${JSON.stringify(m.resolveMotion('storm', true))} under reduced motion - ` +
          `five taps on an option already chosen are a request, and GlimStone 2.1.0 exempts it from the signal`,
      );
    }
    const sn = m.motionNumbers('storm', true);
    const st = table.storm ?? {};
    if (sn.wiggleDeg !== 0 || sn.wiggleDur !== 0) {
      fail(`${MOTION_MODULE}: the storm's wiggle still loops under reduced motion - the exemption stops at anything continuous`);
    }
    for (const k of Object.keys(st).filter((k) => !k.startsWith('wiggle'))) {
      if (sn[k] !== st[k]) {
        fail(
          `${MOTION_MODULE}: under reduced motion the storm's ${k} is ${JSON.stringify(sn[k])}, not its own ${JSON.stringify(st[k])} - ` +
            `exempted without being restored, it ends up quieter than the level it asked for`,
        );
      }
    }
  }

  // The same figures at every level, and GlimStone's own as it wrote them.
  const table = m.MOTION;
  if (!table || typeof table !== 'object') {
    fail(`${MOTION_MODULE}: no MOTION table - a level has to be a set of numbers, or it is a forked animation`);
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
      const native = ref?.NATIVE_MOTION;
      if (!native?.wild) {
        fail(`${NATIVE_MODULE}: no NATIVE_MOTION table - copy reference/motionNative.ts from GlimStone as it is`);
      } else {
        for (const level of ['off', 'subtle', 'wild', 'storm']) {
          for (const [k, v] of Object.entries(native[level] ?? {})) {
            if (table[level]?.[k] !== v) {
              fail(
                `${MOTION_MODULE}: level '${level}' has ${k} ${JSON.stringify(table[level]?.[k])} where GlimStone's table says ` +
                  `${JSON.stringify(v)} - the table is what makes the apps of the family move alike`,
              );
            }
          }
        }
      }
      // Somebody who asked for less movement asked for less movement, not for a
      // faster bounce: only the top two levels may overshoot at all.
      for (const level of ['off', 'subtle']) {
        for (const k of ['damping', 'bounce']) {
          if (table[level]?.[k] < 1) {
            fail(`${MOTION_MODULE}: level '${level}' overshoots (${k} ${table[level][k]} < 1) - a quieter level is not a bouncier one`);
          }
        }
      }
    }
  }

  // The gesture, exercised.
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
    if (opened) fail(`${MOTION_MODULE}: the storm opens while another level is in force - the gesture is pressing a button that is already pressed`);
  }
}

const walk = (dir) => {
  const out = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) out.push(...walk(p));
    else if (/\.tsx?$/.test(name)) out.push(p);
  }
  return out;
};

/** Comments blanked with their offsets kept byte for byte, so a line number is
 *  the real one and a paragraph about what is not persisted is not read as a
 *  persistence. */
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

for (const file of files) {
  const code = strip(readFileSync(file, 'utf8'));
  code.split('\n').forEach((line, i) => {
    if (!/AsyncStorage|SecureStore|setItem|getItem/i.test(line)) return;
    if (!/storm|stormFound|motionFound|gefunden/i.test(line)) return;
    fail(
      `${rel(file)}:${i + 1} a storage line mentions the hidden level: "${line.trim().slice(0, 90)}" - ` +
        `the chosen value persists like any other, the "found it" fact never does`,
    );
  });
}

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
