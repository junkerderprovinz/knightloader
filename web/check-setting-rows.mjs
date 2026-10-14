// A dropdown or an input field stands beside its name, at the end of its card
// or window.
//
// The app grew two ways of naming a control: Field, which puts the name over
// the control at the full width, and SettingRow, which puts it at the start of
// the line and the control at the end. A card or a window that mixes them
// reads as two designs, so every single-line control goes in a SettingRow with
// `control` set, which makes the row the control's <label> and gives an input
// a width that fits beside the name. A list, a text area or a code editor
// keeps its name above it, since it needs the full width.
//
// Checks every .tsx under src:
//   a <Field> or <FieldGroup> whose content holds a single-line control and
//   nothing that needs the width, and a <SettingRow> without `control` around
//   an input field or a dropdown, which would fill the row and push its own
//   name aside.
//
// Run: `node web/check-setting-rows.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, 'src');

const SINGLE_LINE = [
  'Dropdown',
  'NumberInput',
  'UnitNumberInput',
  'TextInput',
  'PasswordInput',
  'PathInput',
  'SettingPathInput',
  'TimePicker',
  'ActionSelect',
  'TriggerSelect',
  'SuggestField',
];
const WIDE = ['ListArea', 'TextArea', 'LinesArea', 'HeadersArea', 'CodeEditor'];

// Files left as they are, with the reason.
const EXEMPT = {
  'src/components/PaypalDialog.tsx': 'the donation windows keep their own design',
};

function files(dir) {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) return files(p);
    return p.endsWith('.tsx') && !p.endsWith('.test.tsx') ? [p] : [];
  });
}

/** The index of the `>` that ends the tag opening at `at`. */
function tagEnd(s, at) {
  let depth = 0;
  for (let i = at; i < s.length; i++) {
    if (s[i] === '{') depth++;
    else if (s[i] === '}') depth--;
    else if (s[i] === '>' && depth === 0) return i;
  }
  return -1;
}

/** Each element named `tag` with its opening tag and its content. */
function* elements(s, tag) {
  const open = new RegExp(`<${tag}(?=[\\s>])`, 'g');
  let m;
  while ((m = open.exec(s))) {
    const end = tagEnd(s, m.index);
    if (s[end - 1] === '/') continue;
    const close = s.indexOf(`</${tag}>`, end);
    yield { at: m.index, head: s.slice(m.index, end + 1), body: s.slice(end + 1, close) };
  }
}

const uses = (body, names) => names.filter((n) => new RegExp(`<${n}[\\s/>]`).test(body));

const problems = [];
for (const file of files(root)) {
  const name = relative(here, file).replaceAll('\\', '/');
  if (name in EXEMPT) continue;
  const s = readFileSync(file, 'utf8');
  const where = (at) => `${name}:${s.slice(0, at).split('\n').length}`;
  for (const tag of ['Field', 'FieldGroup']) {
    for (const f of elements(s, tag)) {
      const single = uses(f.body, SINGLE_LINE);
      if (single.length > 0 && uses(f.body, WIDE).length === 0) {
        problems.push(`${where(f.at)} names a ${single[0]} above it; use <SettingRow control=...>`);
      }
    }
  }
  for (const r of elements(s, 'SettingRow')) {
    const inputs = uses(r.body, SINGLE_LINE);
    if (inputs.length > 0 && !/\scontrol=/.test(r.head)) {
      problems.push(`${where(r.at)} holds a ${inputs[0]} without control=, so the field fills the row`);
    }
  }
}

if (problems.length > 0) {
  console.error(`${problems.length} row problem(s):`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log('ok: every dropdown and input field stands beside its name');
