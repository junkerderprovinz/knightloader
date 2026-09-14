// No Go timestamp is ever read as a yes/no by being truthy.
//
// WHAT BREAKS WITHOUT IT. Every moment this app receives comes from a Go
// `time.Time`, and Go's encoding/json does not drop a zero one: `omitempty` has
// never done anything to a struct, so "nobody has set this" arrives on the wire
// as the string "0001-01-01T00:00:00Z". Measured on a running instance, on a
// failed download that is waiting for nothing at all:
//
//     {"name":"error-00.bin","nextTry":"0001-01-01T00:00:00Z"}
//
// That string is not empty, so it is TRUE. `!!task.nextTry` therefore answers
// yes for every task that has ever carried the field, and the mistake is
// invisible in review because the wrong version is shorter, compiles and type
// checks: the type is `string | undefined` either way.
//
// It has already shipped twice. The "Standing still" quick filter was written
// `!!t.stalledSince` and matched every download in the list, so a stopped queue
// in which nothing had ever moved a byte wore a permanent chip reading
// "Standing still 34". The retry glyph on the name cell was written
// `task.status === 'error' && !!task.nextTry` and told all five failures in a
// list "Wird automatisch wiederholt" while not one of them was waiting for
// anything. Neither is a crash, a type error or a failing test. Both are a
// sentence on screen that is simply not true.
//
// `happened()` in src/lib/countdown.ts is the answer, and this is what makes
// writing it the obvious thing rather than the thing somebody remembers.
//
// THE RULE IS UNIFORM AND THAT IS DELIBERATE. A `time.Time` tagged `omitzero`
// really is dropped by the encoder, so a truthiness test on one of THOSE is
// correct today - and it is still refused here. Two reasons. The tag sits in a
// Go struct three directories away from the .tsx that reads the field, so "is
// this one of the safe ones" is a question nobody can answer by looking at the
// line; and a tag edited from `omitzero` to `omitempty` would silently turn
// correct TypeScript into the bug above, in a file that change never touched.
// One rule that holds for every timestamp is cheaper to obey than an exemption
// list nobody can check by eye, and `happened()` gives the same answer for an
// absent field as the truthiness test it replaces.
//
// WHAT IT READS. The Go sources decide WHICH json names are timestamps - never
// a list typed in here, which would go stale the first time a field was added.
//
// WHAT IT DOES NOT SEE, and each of these is a deliberate limit rather than an
// oversight:
//   - a timestamp pulled into a local first (`const { nextTry } = task`) or
//     reached through a computed key. It matches member access by name.
//   - the RIGHT side of `&&`. `if (a && b.finishedAt)` reads as a boolean and
//     is not matched, because `x = a && b.finishedAt` is the same shape and is
//     a value, not a test - and a guard that cries wolf gets switched off.
//   - anything but a yes/no reading. `fmtDate(t.finishedAt)` is a format and
//     `expiryMs(c.expiresAt) === null` is a proper reader: both are correct and
//     neither is flagged.
//   - a FORMATTED value, which is not a timestamp. `retryAt` in columns.tsx is
//     what fmtDateFull returned, and that is the empty string for a zero time,
//     so testing it is right.
//
// A COMPARISON IS TWO DIFFERENT SENTENCES AND THE OTHER SIDE SAYS WHICH. This
// file used to throw every comparison away - `if (COMPARISON.test(before))
// continue` - to keep the sort comparators quiet, and that opened the same hole
// it was written to close: `task.status === 'error' && task.nextTry !==
// undefined` passed in silence. It is the SAME BUG as `!!task.nextTry` and it
// says the same untrue sentence on the same row. "0001-01-01T00:00:00Z" is not
// undefined, not null and not empty, so all four spellings answer yes for a
// download that is waiting for nothing.
//
// So the other operand decides:
//   - a VALUE on the other side is a sort or an equality between two moments.
//     `a.createdAt < b.createdAt`, `dismissed === failed.at`,
//     `now - burst.at > WINDOW`: correct, and silent.
//   - `undefined`, `null`, `''` or `""` on the other side is a truthiness test
//     wearing a comparison's clothes. Reported, in any of the eight spellings
//     (`===`/`!==`/`==`/`!=` against each of the four, either way round).
// A template literal is not in the second list, for the same reason the scan
// leaves template literals alone everywhere else: `x.at !== ``` is not a thing
// anybody writes, and blanking them would cost the real expressions in `${}`.
//
// THE NAME LIST CARRIES FOUR EVERYDAY WORDS - `at`, `now`, `since`, `queuedAt` -
// and an access is recognised by its LAST SEGMENT over any object at all. So
// `opts.at`, `clock.now` and `range.since` on something with no connection to Go
// would each be reported. Measured, by putting exactly those three into
// src/lib/speedHistory.ts: three failures, none of them real.
//
// Narrowing the list to what web/src/lib/api.ts declares was the obvious answer
// and it is the wrong one, measured both ways:
//   - it removes NOTHING. `at`, `now`, `since` and `queuedAt` are all declared
//     in api.ts, so all four everyday words survive the narrowing.
//   - it goes BLIND. `lastAttempt` and `lastOk` are declared in
//     src/lib/eventtargets.ts and read in settings/eventtargets/TargetHealth.tsx,
//     and api.ts does not contain either word; seven more are declared in other
//     modules. A guard that stops watching a field the app really reads, to
//     lose none of its noise, is worse than the noise.
// So the report names the GO TYPE instead - `notify.TargetHealth.LastAttempt`,
// not just the file - and a false alarm can be recognised as one at a glance
// rather than after opening three directories. `Date.now` is the exception that
// is worth hard-coding: it matches `now` by its last segment, it is the
// platform's clock rather than anything the server sent, and it appears in two
// dozen places in this tree.
//
// Run by hand from web/: `node check-go-timestamps.mjs`
import { readFileSync, readdirSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const repo = join(here, '..');

const die = (why) => {
  console.error(`check-go-timestamps: ${why}`);
  process.exit(1);
};

/** Every file under `root` whose name ends in one of `exts`. */
function walk(root, exts, out = []) {
  let entries;
  try {
    entries = readdirSync(root, { withFileTypes: true });
  } catch {
    return out;
  }
  for (const e of entries) {
    const p = join(root, e.name);
    if (e.isDirectory()) {
      if (e.name === 'node_modules' || e.name === 'dist' || e.name === '.git') continue;
      walk(p, exts, out);
    } else if (exts.some((x) => e.name.endsWith(x))) {
      out.push(p);
    }
  }
  return out;
}

// ---------------------------------------------------------------------------
// 1. Which json names are Go timestamps.
//
// `Field time.Time `json:"name,..."`` in anything the server can send. The tag
// is read for its NAME only: omitempty, omitzero or nothing at all makes no
// difference to the rule, for the reason the header gives at length.
// ---------------------------------------------------------------------------

const goFiles = [...walk(join(repo, 'internal'), ['.go']), ...walk(join(repo, 'cmd'), ['.go'])].filter(
  (f) => !f.endsWith('_test.go'),
);
const DECL = /^\s*([A-Z]\w*)\s+time\.Time\s+`json:"([A-Za-z_]\w*)/;
// The type a field belongs to, so the report can say `notify.TargetHealth`
// rather than only the path. Read line by line and not with one expression over
// the file, because what is wanted is the nearest `struct {` ABOVE the field.
// An inline anonymous struct field takes over as the current type and never
// hands it back, which is the one inaccuracy here and an acceptable one: it
// names a real struct that really holds the field.
// Unexported too: the wire shape of half these routes is a lowercase `feedRow`
// or `targetRow` that never leaves the package, and naming it `api.?` would
// throw away the one word that makes a report recognisable.
const STRUCT = /^\s*(?:type\s+)?([A-Za-z_]\w*)\s+struct\s*\{/;
/** json name -> `pkg.Type.Field (path)` for each declaration, for the message. */
const stamps = new Map();
for (const f of goFiles) {
  const rel = relative(repo, f).replace(/\\/g, '/');
  const pkg = rel.split('/').slice(-2)[0];
  let type = null;
  for (const line of readFileSync(f, 'utf8').split('\n')) {
    const s = line.match(STRUCT);
    if (s) {
      type = s[1];
      continue;
    }
    const d = line.match(DECL);
    if (!d) continue;
    const name = d[2];
    if (!stamps.has(name)) stamps.set(name, new Set());
    stamps.get(name).add(`${pkg}.${type ?? '?'}.${d[1]} (${rel})`);
  }
}
if (stamps.size < 4) {
  die(`only ${stamps.size} Go timestamp field(s) found, which is too few to be this repository - the scan is broken`);
}

// ---------------------------------------------------------------------------
// 2. Where the web reads one as a yes/no.
//
// One pass over every member access whose last segment is a timestamp name,
// then a look at what sits either side of it. Deciding from BOTH sides is what
// keeps the sort comparators out of the report: in
// `a.createdAt > b.createdAt ? -1 : 1` the second access is followed by a `?`
// and would look exactly like a truthiness test read forwards alone - what
// makes it a comparison is the `>` in front of it.
// ---------------------------------------------------------------------------

const names = [...stamps.keys()].sort();
const ACCESS = new RegExp(
  String.raw`(?<![\w$.'"\`])[A-Za-z_$][\w$]*(?:\??\.[\w$]+)*\??\.(${names.join('|')})\b(?![\w$])`,
  'g',
);
/** The tail of a comparison, which makes the access a value and not a test. */
const COMPARISON = /(?:[=!<>]=|[<>])$/;
/** Longest first, so `!==` is never read as `!=` followed by a stray `=`. */
const OP = String.raw`(?:===|!==|==|!=|<=|>=|<|>)`;
/**
 * "There is nothing here", in the four spellings that a Go zero time is not.
 *
 * The empty string is in this list and it has to be, which is why the blanking
 * below keeps a two-character string literal readable instead of wiping it: an
 * `x.finishedAt !== ''` blanked to `x.finishedAt !==   ` looks exactly like a
 * comparison against a value, which is the one shape that is allowed through.
 */
const NOTHING = String.raw`(?:undefined|null|''|"")`;
/** `x.nextTry !== undefined` - the access on the left, nothing on the right. */
const NOTHING_RIGHT = new RegExp(String.raw`^(${OP})\s*(${NOTHING})(?![\w$])`);
/** `undefined !== x.nextTry` - the same test written the other way round. */
const NOTHING_LEFT = new RegExp(String.raw`(?<![\w$.])(${NOTHING})\s*(${OP})$`);
/** The access is the LEFT side of some comparison, whatever is on the right. */
const STARTS_COMPARISON = new RegExp(String.raw`^${OP}`);

const webFiles = walk(join(here, 'src'), ['.ts', '.tsx']);
const lineAt = (src, i) => src.slice(0, i).split('\n').length;

const problems = [];
let scanned = 0;
for (const f of webFiles) {
  // Comments and quoted strings blanked, offsets kept byte for byte, so every
  // reported line is the real one AND a field merely NAMED in a paragraph or
  // in a translation key is not a read of it. Half the files this touches
  // explain the trap at length in their own comments. Template literals are
  // left alone on purpose - they carry real expressions inside `${}`.
  const src = readFileSync(f, 'utf8')
    .replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '))
    .replace(/(^|[^:'"\\])\/\/[^\n]*/g, (c, p) => p + ' '.repeat(c.length - p.length))
    // The EMPTY string survives, alone among string literals: it is an operand
    // this scan has to be able to read (see NOTHING), and it can hold nothing
    // that would need blanking.
    .replace(/'(?:[^'\\\n]|\\.)*'|"(?:[^"\\\n]|\\.)*"/g, (c) => (c.length === 2 ? c : ' '.repeat(c.length)));
  scanned++;
  const rel = 'web/' + relative(here, f).replace(/\\/g, '/');

  for (const m of src.matchAll(ACCESS)) {
    const field = m[1];
    const before = src.slice(0, m.index).replace(/\s+$/, '');
    const after = src.slice(m.index + m[0].length).replace(/^\s+/, '');

    // The platform's clock, not a field on anything the server sent. It matches
    // `now` by its last segment like every other access, and it is written two
    // dozen times in this tree.
    if (m[0] === 'Date.now') continue;

    let why = null;
    // The report shows the whole test the way it is written, rather than the
    // fragment the scan matched: `!task.nextTry`, not `task.nextTry`.
    let quote = m[0];

    const right = after.match(NOTHING_RIGHT);
    const left = before.match(NOTHING_LEFT);
    if (right) {
      why = `compared against ${right[2]}`;
      quote = `${m[0]} ${right[1]} ${right[2]}`;
    } else if (left) {
      why = `compared against ${left[1]}`;
      quote = `${left[1]} ${left[2]} ${m[0]}`;
    } else if (COMPARISON.test(before) || STARTS_COMPARISON.test(after)) {
      // A value on the other side: a sort, or an equality between two moments.
      // Both are correct, and keeping them quiet is the whole reason the shape
      // above has to be told apart rather than the comparison form dropped.
      continue;
    } else {
      const bang = before.match(/(?:^|[^=!<>])(!{1,2})$/);
      if (bang) {
        why = 'negated';
        quote = bang[1] + m[0];
      } else if (/^(?:&&|\|\|)/.test(after)) why = 'used as the left side of && or ||';
      else if (/^\?(?![.?])/.test(after)) why = 'used as a ternary condition';
      else if (/\bif\s*\(\s*$/.test(before) && after.startsWith(')')) why = 'used as an if condition';
    }
    if (!why) continue;

    problems.push(
      `${rel}:${lineAt(src, m.index)} \`${quote}\` reads the Go timestamp \`${field}\` as a yes/no (${why}). ` +
        `It is a time.Time in ${[...stamps.get(field)].join(', ')}, so a moment nobody set arrives as ` +
        `"0001-01-01T00:00:00Z", which is neither undefined nor null nor empty, and this answers yes. ` +
        `Ask happened() from lib/countdown.ts instead.`,
    );
  }
}

if (problems.length) {
  console.error(`check-go-timestamps: ${problems.length} truthiness test(s) on a Go timestamp.`);
  console.error('Each of these answers yes for a moment that never happened:');
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log(
  `ok: ${names.length} Go timestamp fields (${names.join(', ')}), ` +
    `no truthiness test on any of them across ${scanned} web sources`,
);
