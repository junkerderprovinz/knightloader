// Checks that no Go timestamp is read as a yes/no by being truthy.
//
// encoding/json does not drop a zero time.Time, so an unset moment arrives as
// "0001-01-01T00:00:00Z", which is truthy: `!!task.nextTry` is true for every
// task carrying the field, and it type-checks. Use happened() from
// src/lib/countdown.ts instead.
//
// The rule covers every time.Time, including ones tagged omitzero: the tag is
// far from the code reading the field and may change without it.
//
// Which json names are timestamps is read from the Go sources.
//
// Comparisons depend on the other operand. A value there (`a.createdAt <
// b.createdAt`, `now - burst.at > WINDOW`) is fine; undefined, null, '' or ""
// is a truthiness test in disguise and is reported, either way round.
//
// Not checked: a timestamp copied into a local first or reached by a computed
// key; the right side of `&&`, which is often a value; readings that are not
// yes/no, such as fmtDate(t.finishedAt); already formatted values.
//
// Names are matched by the last segment of any member access, so everyday
// names (`at`, `now`, `since`) can give false alarms; the report names the Go
// type to make those easy to spot. `Date.now` is skipped.
//
// Run from web/: `node check-go-timestamps.mjs`
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

// Which json names are Go timestamps: `Field time.Time `json:"name,..."``. Only
// the name matters, not omitempty or omitzero.

const goFiles = [...walk(join(repo, 'internal'), ['.go']), ...walk(join(repo, 'cmd'), ['.go'])].filter(
  (f) => !f.endsWith('_test.go'),
);
const DECL = /^\s*([A-Z]\w*)\s+time\.Time\s+`json:"([A-Za-z_]\w*)/;
// The nearest `struct {` above a field names its type in the report,
// unexported ones included. An inline anonymous struct takes over as the
// current type, which still names a struct holding the field.
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

// Where the web reads one as a yes/no. Both sides of each access are looked
// at: in `a.createdAt > b.createdAt ? -1 : 1` the `?` after the second access
// would look like a test, and the `>` before it shows it is a comparison.

const names = [...stamps.keys()].sort();
const ACCESS = new RegExp(
  String.raw`(?<![\w$.'"\`])[A-Za-z_$][\w$]*(?:\??\.[\w$]+)*\??\.(${names.join('|')})\b(?![\w$])`,
  'g',
);
/** The tail of a comparison, which makes the access a value and not a test. */
const COMPARISON = /(?:[=!<>]=|[<>])$/;
/** Longest first, so `!==` is never read as `!=` followed by a stray `=`. */
const OP = String.raw`(?:===|!==|==|!=|<=|>=|<|>)`;
/** "Nothing here", in the four spellings a Go zero time is not. The blanking
 *  below leaves empty string literals intact so they can be read here. */
const NOTHING = String.raw`(?:undefined|null|''|"")`;
/** `x.nextTry !== undefined`: the access on the left, nothing on the right. */
const NOTHING_RIGHT = new RegExp(String.raw`^(${OP})\s*(${NOTHING})(?![\w$])`);
/** `undefined !== x.nextTry`: the same test written the other way round. */
const NOTHING_LEFT = new RegExp(String.raw`(?<![\w$.])(${NOTHING})\s*(${OP})$`);
/** The access is the left side of some comparison. */
const STARTS_COMPARISON = new RegExp(String.raw`^${OP}`);

const webFiles = walk(join(here, 'src'), ['.ts', '.tsx']);
const lineAt = (src, i) => src.slice(0, i).split('\n').length;

const problems = [];
let scanned = 0;
for (const f of webFiles) {
  // Comments and strings are blanked, keeping offsets, so a field named in
  // prose or a translation key does not count. Template literals stay, since
  // their `${}` holds real expressions.
  const src = readFileSync(f, 'utf8')
    .replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '))
    .replace(/(^|[^:'"\\])\/\/[^\n]*/g, (c, p) => p + ' '.repeat(c.length - p.length))
    // Empty strings survive, for NOTHING.
    .replace(/'(?:[^'\\\n]|\\.)*'|"(?:[^"\\\n]|\\.)*"/g, (c) => (c.length === 2 ? c : ' '.repeat(c.length)));
  scanned++;
  const rel = 'web/' + relative(here, f).replace(/\\/g, '/');

  for (const m of src.matchAll(ACCESS)) {
    const field = m[1];
    const before = src.slice(0, m.index).replace(/\s+$/, '');
    const after = src.slice(m.index + m[0].length).replace(/^\s+/, '');

    // The platform's clock, not a server field.
    if (m[0] === 'Date.now') continue;

    let why = null;
    // The whole test as written, such as `!task.nextTry`.
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
      // A value on the other side: a sort, or two moments compared.
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
