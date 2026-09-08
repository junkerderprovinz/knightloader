// Ranking a label against what somebody typed, and folding the two sides so
// that the comparison means the same thing in all 42 languages.
//
// This was `score()` inside components/CommandPalette.tsx, private to it. It is
// here because the settings search (pages/settings/SettingsSearch.tsx) asks the
// identical question of a much larger corpus, and two scorers would drift into
// two different ideas of what "close enough" means in one app - the palette
// ranking "down" one way and the settings box another, for the same word, is
// the kind of difference nobody reports and everybody notices.

/**
 * Letters NFD cannot help with, and what they are typed as instead.
 *
 * A decomposition splits a letter from a mark sitting on it, and none of these
 * has one: ß is not an s with something above it, ł is a stroke drawn THROUGH
 * the letter, and ı is a plain i that has had its dot taken away. NFD leaves all
 * of them exactly as they were, so without this table the fold below silently
 * does nothing for Polish, Turkish, Croatian, Danish, Norwegian and Icelandic -
 * six of the shipped 42 - while looking like it works because German and French
 * pass.
 *
 * Found by check-rank.mjs, not by reading: the first version of this fold had ß
 * and nothing else, and "baglanti" could not find "Bağlantı".
 *
 * Lowercase keys only: fold() lowercases before it gets here, and every one of
 * these has a lowercase form that toLowerCase() already produces (ẞ→ß, Ø→ø,
 * Þ→þ, …), so a second half of the table would be a second thing to keep right.
 */
const UNDECOMPOSABLE: Record<string, string> = {
  ß: 'ss', // German
  ł: 'l', // Polish
  ø: 'o', // Danish, Norwegian
  đ: 'd', // Croatian, Serbian latin, Vietnamese
  ð: 'd', // Icelandic
  þ: 'th', // Icelandic
  æ: 'ae', // Danish, Norwegian, Icelandic
  œ: 'oe', // French
  ı: 'i', // Turkish dotless i
  ſ: 's', // long s, which older imported strings still carry
};

/** Built from the table above rather than typed out beside it, so the two
 *  cannot drift into disagreeing about which letters are handled. */
const UNDECOMPOSABLE_RE = new RegExp(`[${Object.keys(UNDECOMPOSABLE).join('')}]`, 'g');

/**
 * fold makes two strings comparable across a 42-language catalogue.
 *
 * Lowercasing alone - which is all the palette's own matcher ever did - makes
 * every accented language a set of dead ends: in a German UI "grosse" never
 * finds "Größe", and "uber" never finds "Überwachung", so the search looks
 * broken to exactly the people who cannot type the accent quickly. NFD splits a
 * letter from its accent and the Diacritic strip drops the accent; the table
 * above covers the letters that have no accent to split off.
 *
 * Applied to BOTH sides, always. Folding only the query would make "Größe"
 * unfindable by "größe", which is worse than not folding at all.
 *
 * It touches nothing outside the Latin alphabet, which is the point: Cyrillic,
 * Greek, Arabic, Hebrew, Thai, Devanagari and the CJK scripts have no marks for
 * \p{Diacritic} to strip and no entry in the table, so they compare exactly as
 * they are written.
 */
export function fold(s: string): string {
  return s
    .normalize('NFD')
    .replace(/\p{Diacritic}/gu, '')
    .toLowerCase()
    .replace(UNDECOMPOSABLE_RE, (c) => UNDECOMPOSABLE[c]);
}

/**
 * scoreFolded ranks an ALREADY FOLDED label against an ALREADY FOLDED query.
 *
 * A literal substring match wins, ranked by how early it starts and whether it
 * starts on a word boundary, so "down" ranks "Downloads" above "Slow down".
 * Failing that, a subsequence match - every character of the query appears in
 * the label, in order, not necessarily together - still counts, ranked behind
 * every substring hit, so "cmdp" still finds "Command palette". -1 means "does
 * not match at all". Lower is better; 0 is a prefix hit.
 *
 * No fuzzy-search dependency: this app has none, and one small scorer does not
 * justify adding one.
 *
 * Separate from `score` below because a caller ranking a fixed corpus on every
 * keystroke (the settings index is roughly 700 strings) should fold it once and
 * keep the folded copy, rather than re-normalising the same 700 strings for
 * every character typed.
 */
export function scoreFolded(label: string, query: string): number {
  if (!query) return 0;
  const i = label.indexOf(query);
  if (i === 0) return 0;
  if (i > 0) return label[i - 1] === ' ' ? 1 : 2 + i;
  let cursor = 0;
  for (const ch of query) {
    cursor = label.indexOf(ch, cursor);
    if (cursor === -1) return -1;
    cursor++;
  }
  return 1000;
}

/**
 * scoreProse ranks an ALREADY FOLDED sentence against an ALREADY FOLDED query,
 * by substring only. Same scale as scoreFolded, same -1 for no match.
 *
 * The difference is the whole point, and it was measured rather than reasoned
 * about: run scoreFolded over the settings catalogue's hint texts and "zzzz"
 * matches a 400-character paragraph about archive extraction, because every
 * letter of it appears somewhere in that paragraph in order. Subsequence
 * matching is an ABBREVIATION heuristic - it is what makes "cmdp" find "Command
 * palette" - and an abbreviation of a paragraph is not a thing. Over prose it
 * does not narrow anything down; it returns the whole catalogue, ranked.
 *
 * So: names get subsequence matching, sentences do not. Both still get
 * substring matching, which is what somebody typing "speed" into a search box
 * over explanations actually means.
 */
export function scoreProse(text: string, query: string): number {
  if (!query) return 0;
  const i = text.indexOf(query);
  if (i === -1) return -1;
  if (i === 0) return 0;
  return text[i - 1] === ' ' ? 1 : 2 + i;
}

/**
 * score is scoreFolded with the folding done for you - the safe default, for a
 * caller with a short list and nothing cached.
 *
 * This is the shape the palette already called, so its own call site did not
 * change when the function moved out; what changed is that the palette now
 * folds, which is a fix it was silently missing rather than a new behaviour it
 * has to opt into.
 */
export function score(label: string, query: string): number {
  return scoreFolded(fold(label), fold(query));
}
