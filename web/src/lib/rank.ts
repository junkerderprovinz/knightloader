// Ranking a label against a query, with folding that works across all the
// shipped languages. The command palette and the settings search share it so
// both agree on what "close enough" means.

/**
 * Letters NFD cannot decompose, and how they are typed instead. Without this
 * table, folding does nothing for Polish, Turkish, Croatian and the Nordic
 * languages. Lowercase keys only, since fold() lowercases first.
 * check-rank.mjs covers it.
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

const UNDECOMPOSABLE_RE = new RegExp(`[${Object.keys(UNDECOMPOSABLE).join('')}]`, 'g');

/**
 * fold makes two strings comparable: lowercase, accents stripped via NFD, and
 * the letters above replaced, so "grosse" finds "Größe". Apply it to both
 * sides. Scripts without Latin diacritics compare as written.
 */
export function fold(s: string): string {
  return s
    .normalize('NFD')
    .replace(/\p{Diacritic}/gu, '')
    .toLowerCase()
    .replace(UNDECOMPOSABLE_RE, (c) => UNDECOMPOSABLE[c]);
}

/**
 * scoreFolded ranks a folded label against a folded query; lower is better
 * and -1 means no match. A substring wins, earlier and on a word boundary
 * first, so "down" ranks "Downloads" above "Slow down". A subsequence match
 * ("cmdp" for "Command palette") ranks behind every substring. Callers ranking
 * a fixed corpus on every keystroke fold it once and use this directly.
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
 * scoreProse ranks folded prose by substring only, on scoreFolded's scale.
 * Subsequence matching suits abbreviated names; over long hint texts almost
 * any query matches every paragraph.
 */
export function scoreProse(text: string, query: string): number {
  if (!query) return 0;
  const i = text.indexOf(query);
  if (i === -1) return -1;
  if (i === 0) return 0;
  return text[i - 1] === ' ' ? 1 : 2 + i;
}

/** score is scoreFolded with the folding done for you, for short lists. */
export function score(label: string, query: string): number {
  return scoreFolded(fold(label), fold(query));
}
