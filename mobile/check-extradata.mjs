// A FlatList whose rows read state the list does not know about must say so.
//
// This gate exists because that exact defect cost four rounds of "das drag and
// drop funktioniert nicht" (jdp, 2026-08-31 to 2026-09-02) and, once found,
// turned out to be sitting in a second list nobody had reported yet.
//
// VirtualizedList re-renders a cell only when `data` changes BY REFERENCE or
// when `extraData` does. A `renderItem` that closes over component state - a
// drag in flight, a status map, a poll's figures - is invisible to that rule, so
// the rows simply stop updating. It survives review easily because it usually
// works: most `data` props are rebuilt on every render, so the reference keeps
// changing and the cells are redrawn for the wrong reason. The bug only appears
// when somebody makes that reference stable, which is normally called an
// optimisation.
//
// The check is deliberately blunt: find every <FlatList>, read its renderItem
// body, and if that body mentions an identifier that is neither destructured
// from the render argument nor imported nor a local helper, demand an
// `extraData`. False positives are cheap (add extraData, which is never wrong)
// and a miss is four rounds of somebody else's evening.
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

/** The span of one JSX element, from `<FlatList` to its matching `/>` or `>`. */
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
  let from = 0;
  for (;;) {
    const at = src.indexOf('<FlatList', from);
    if (at < 0) break;
    from = at + 9;
    const el = elementSpan(src, at);
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

    // Identifiers the body reads by SUBSCRIPT - `status[item.id]` - are the
    // shape that hurts: a map held in state, keyed by the row. Anything that is
    // not an allowed argument and not a component (capitalised) is suspect.
    const reads = new Set();
    for (const m of body.matchAll(/\b([a-z][A-Za-z0-9_]*)\s*\[/g)) {
      const name = m[1];
      if (allowed.has(name)) continue;
      if (['styles', 'props', 'String', 'Object', 'Array', 'Math'].includes(name)) continue;
      reads.add(name);
    }
    if (reads.size > 0) {
      problems.push(
        `src/${file.split(/[\\/]src[\\/]/).pop()}: <FlatList> has no extraData but its rows read ` +
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
