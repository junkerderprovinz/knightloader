// A FlatList whose rows read state the list does not know about has to say so.
//
// VirtualizedList re-renders a cell only when `data` changes by reference or
// when `extraData` does. A `renderItem` that closes over component state, a
// drag in flight, a status map or a poll's figures, is invisible to that rule,
// so the rows stop updating. It survives review because it usually works: most
// `data` props are rebuilt on every render, so the reference keeps changing and
// the cells are redrawn for the wrong reason. The defect only appears once
// somebody makes that reference stable, which is normally called an
// optimisation.
//
// The check is blunt: find every <FlatList>, and every <MovingList>, which is
// one inside, read its renderItem body, and if that body mentions an identifier
// that is neither destructured from the render argument nor imported nor a
// local helper, demand an `extraData`. A false positive costs an extraData,
// which is never wrong.
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

const ROOT = new URL('./src/', import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, '$1');

function walk(dir) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) out.push(...walk(p));
    else if (name.endsWith('.tsx')) out.push(p);
  }
  return out;
}

/** The span of one JSX element, from its opening tag to its matching `/>` or `>`. */
function elementSpan(src, start) {
  let depth = 0;
  for (let i = start; i < src.length; i++) {
    const ch = src[i];
    if (ch === '{') depth++;
    else if (ch === '}') depth--;
    else if (depth === 0 && ch === '>') return src.slice(start, i + 1);
  }
  return src.slice(start);
}

/** The body of a renderItem prop, if the element has one. */
function renderItemBody(el) {
  const at = el.indexOf('renderItem=');
  if (at < 0) return null;
  let depth = 0;
  for (let i = el.indexOf('{', at); i < el.length; i++) {
    if (el[i] === '{') depth++;
    else if (el[i] === '}') {
      depth--;
      if (depth === 0) return el.slice(at, i + 1);
    }
  }
  return null;
}

const problems = [];
for (const file of walk(ROOT)) {
  const src = readFileSync(file, 'utf8');
  for (const hit of src.matchAll(/<(FlatList|MovingList)\b/g)) {
    const tag = hit[1];
    const el = elementSpan(src, hit.index);
    if (el.includes('extraData')) continue;
    const body = renderItemBody(el);
    if (!body) continue;

    // What the row is allowed to read without declaring anything: whatever the
    // render argument destructures, plus the usual JSX and helper noise.
    const args = body.match(/\(\s*\{([^}]*)\}/);
    const allowed = new Set(
      (args ? args[1] : '')
        .split(',')
        .map((s) => s.trim().split(':')[0].trim())
        .filter(Boolean),
    );

    // Identifiers the body reads by subscript, `status[item.id]`, are the shape
    // that hurts: a map held in state, keyed by the row. Anything that is not an
    // allowed argument and not a component (capitalised) is suspect.
    const reads = new Set();
    for (const m of body.matchAll(/\b([a-z][A-Za-z0-9_]*)\s*\[/g)) {
      const name = m[1];
      if (allowed.has(name)) continue;
      if (['styles', 'props', 'String', 'Object', 'Array', 'Math'].includes(name)) continue;
      reads.add(name);
    }
    if (reads.size > 0) {
      problems.push(
        `src/${file.split(/[\\/]src[\\/]/).pop()}: <${tag}> has no extraData but its rows read ` +
          `${[...reads].sort().join(', ')} - state the list cannot see. ` +
          `Add extraData with those values, or the rows will stop updating.`,
      );
    }
  }
}

if (problems.length > 0) {
  console.error('FlatList rows reading undeclared state:\n');
  for (const p of problems) console.error('  ' + p);
  console.error(
    '\nVirtualizedList redraws a cell only when data changes by reference or extraData does.\n' +
      'A row that closes over state must declare it, or it freezes at whatever it said first.',
  );
  process.exit(1);
}
console.log('ok: every FlatList whose rows read outside state declares extraData');
