// A field's placeholder is not a button's name.
//
// `search.placeholder` is "Search this list…", the grey hint inside the empty
// box, written as an invitation and ending in an ellipsis because typing
// finishes the sentence. As the label of the badge that opens that box it made
// the toolbar read "Clear selection · Search this list… · Move into a package".
// One string doing both jobs pulls in two directions, a placeholder wants to be
// a whole invitation and a label wants to be one word, and the type system
// notices nothing because both are `string`. A placeholder is also the most
// tempting string in a catalogue to reuse, being the only one already written
// about the thing the control opens.
//
// Three spellings are caught:
//
//   title={t('*.placeholder')}    ui.tsx's IconBadge prints `title` as the
//                                 badge's visible words once Beschriftung is
//                                 set to text (see its `labelled` prop).
//   labelKey: '*.placeholder'     the command palette lists the entry by this
//                                 string and matches typing against it.
//   aria-label={t('*.placeholder')}
//                                 a control's accessible name. Renaming the
//                                 badge's `title` and leaving the aria-label
//                                 behind gives a button that prints "Suche" and
//                                 announces "Search this list…".
//
// Allowed: `aria-label={t(k)}` on the field whose own `placeholder={t(k)}` is
// the same key, within the same element, as components/SearchField.tsx and
// pages/settings/SettingsSearch.tsx both do. There the placeholder is the name
// of the thing, because the thing is the box. The exemption asks for the same
// key near enough to be the same tag, so a badge that happens to sit in a file
// with a search field cannot claim it.
//
// Not seen: a key that means "placeholder" without being spelled that way, and
// which element an attribute belongs to, which would mean parsing JSX, so the
// window below is a proximity test rather than a tree walk.
//
// src/lib/locales is skipped. The 42 catalogues each define
// `search.placeholder`, and counting those would leave the blind-run tripwire
// below unable to fall however many call sites stopped matching.
//
// Run: `node web/check-placeholder-as-label.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const web = dirname(fileURLToPath(import.meta.url));
const src = join(web, 'src');

/** The catalogues are data, not call sites. */
const LOCALES = join(src, 'lib', 'locales');

function sources(dir) {
  const found = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) {
      if (path !== LOCALES) found.push(...sources(path));
    } else if (/\.tsx?$/.test(entry)) found.push(path);
  }
  return found;
}

const files = sources(src);
if (files.length < 20) {
  console.error(`check-placeholder-as-label: only ${files.length} source files found - wrong directory?`);
  process.exit(1);
}

/** `title={t('a.b.placeholder')}`, with or without interpolation arguments. */
const TITLE = /\btitle=\{\s*t\(\s*'([A-Za-z0-9_.]*\.placeholder)'/g;
/** `labelKey: 'a.b.placeholder'` in a command definition. */
const LABEL_KEY = /\blabelKey:\s*'([A-Za-z0-9_.]*\.placeholder)'/g;
/** `aria-label={t('a.b.placeholder')}` - a control's spoken name. */
const ARIA = /\baria-label=\{\s*t\(\s*'([A-Za-z0-9_.]*\.placeholder)'/g;
/** Every mention of a placeholder key at all, so a blind run can report itself. */
const ANY = /'[A-Za-z0-9_.]*\.placeholder'/g;

/**
 * How far either side of an `aria-label` the matching `placeholder=` may sit for
 * the two to count as the same tag. One attribute apart is the real case (both
 * call sites put them on consecutive lines); this is generous enough for a
 * formatter to move things around and far too small to reach the next element.
 */
const SAME_TAG = 200;

const problems = [];
let seen = 0;

for (const path of files) {
  const text = readFileSync(path, 'utf8');
  seen += [...text.matchAll(ANY)].length;
  const where = (at) => `${path.slice(src.length + 1).replace(/\\/g, '/')}:${text.slice(0, at).split('\n').length}`;
  for (const found of text.matchAll(TITLE)) {
    problems.push(`${where(found.index)} title={t('${found[1]}')} - a badge's title is the words it prints`);
  }
  for (const found of text.matchAll(LABEL_KEY)) {
    problems.push(`${where(found.index)} labelKey: '${found[1]}' - the palette lists a command by this string`);
  }
  for (const found of text.matchAll(ARIA)) {
    // The field's own name, or a control wearing the field's hint? Only a
    // `placeholder=` carrying this key, close enough to be the same tag, makes
    // it the former.
    const near = text.slice(Math.max(0, found.index - SAME_TAG), found.index + SAME_TAG);
    if (near.includes(`placeholder={t('${found[1]}')}`)) continue;
    problems.push(`${where(found.index)} aria-label={t('${found[1]}')} - a control is announced by this string`);
  }
}
problems.sort();

// Four is the floor because four is what legitimately remains: the collector's
// paste box, the list search field, the settings search field and the captcha
// key field each name their own input. Fewer than that and the pattern has
// stopped matching rather than the code having got better.
if (seen < 4) {
  console.error(
    `check-placeholder-as-label: only ${seen} '*.placeholder' key mention(s) in src - the pattern stopped matching, so this check is blind.`,
  );
  process.exit(1);
}

if (problems.length) {
  console.error(`check-placeholder-as-label: ${problems.length} control(s) named by a field's placeholder.`);
  console.error("A placeholder is an invitation to type; a control needs a name. Give the control its own key:");
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

console.log(
  `check-placeholder-as-label: ${files.length} files, ${seen} placeholder key mentions, none of them naming a control.`,
);
