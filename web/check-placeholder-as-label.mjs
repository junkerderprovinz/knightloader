// A field's placeholder is not a button's name.
//
// WHY THIS EXISTS. `search.placeholder` is "Search this list…" - the grey hint
// inside the empty search box, written in the voice of an invitation and ending
// in an ellipsis because the sentence is meant to be finished by typing. It was
// also, on the downloads page, the LABEL of the badge that opens that box, so
// the row read "Clear selection · Search this list… · Move into a package"
// (jdp, screenshot: "der Suchbutton soll einfach \"Suche\" heißen"). One string
// was doing two jobs whose requirements point in opposite directions: a
// placeholder is a whole invitation and wants to be long, a button label is a
// name and wants to be one word. Whichever way the string is then edited, one of
// the two call sites gets worse, and nothing in the type system notices - both
// are `string`, both compile, both render.
//
// It is not a one-off slip either. A placeholder is the most tempting string in
// any catalogue to reuse, because it is the only one already written about the
// exact thing the control opens, and the reuse looks free right up to the moment
// a row has to fit on one line: those labels are the longest in the app.
//
// WHAT IT CHECKS.
//
//   title={t('*.placeholder')}   ui.tsx's IconBadge renders `title` as the
//                                badge's VISIBLE words once Beschriftung is set
//                                to "text" or "text and glyph" (see its
//                                `labelled` prop), so a placeholder key here is
//                                literally a sentence printed on a button.
//
//   labelKey: '*.placeholder'    the command palette's own name for an entry
//                                (lib/commands/*). Same defect, different
//                                spelling: the palette lists the command by this
//                                string and matches typing against it.
//
//   aria-label={t('*.placeholder')}
//                                a control's accessible NAME. The third
//                                spelling, and the one a hurried fix reaches
//                                for: renaming the badge's `title` and leaving
//                                its aria-label behind gives a button that
//                                prints "Suche" and announces "Search this
//                                list…", which is worse than either half alone.
//
// WHAT IT DELIBERATELY ALLOWS. `aria-label={t(k)}` on the field whose own
// `placeholder={t(k)}` is the same key, within the same element.
// components/SearchField.tsx and pages/settings/SettingsSearch.tsx both carry
// the pair on one `<input>`, which is the ordinary way to give a box with no
// visible label an accessible name: there the placeholder IS the name of the
// thing, because the thing is the box. The exemption is deliberately narrow —
// the SAME key, and near enough to be the same tag — so it cannot be claimed by
// a badge that merely happens to sit in a file that also has a search field.
//
// WHAT IT DOES NOT SEE. A key that means "placeholder" without being spelled
// `.placeholder`: the suffix is the only evidence available without reading the
// catalogue's prose. Nor does it resolve which element an attribute belongs to —
// that means parsing JSX — so the window below is a proximity test and not a
// tree walk. Both doors it does stand in are the ones the mistake walks through.
//
// IT DOES NOT READ src/lib/locales. It used to, and that made its own
// blind-run tripwire useless: 42 catalogues each define `search.placeholder`,
// so the count sat at 220 and could not have collapsed however many call sites
// stopped matching. Counting the CODE means the number it guards is the number
// it is about.
//
// Run by hand or from CI: `node web/check-placeholder-as-label.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const web = dirname(fileURLToPath(import.meta.url));
const src = join(web, 'src');

/** The catalogues are data, not call sites - see the note above. */
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
    // `placeholder=` carrying THIS key, close enough to be the same tag, makes
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
