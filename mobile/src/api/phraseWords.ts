// Reads a pasted or typed phrase the way people hand one over: one word per
// line, numbered, separated by commas. The same rules as the web UI's
// lib/phraseWords.ts, over the same list, so a word flagged here is one every
// instance would refuse too.
import { WORD_COUNT } from './seedphrase';
import { WORDS } from './wordlist';

const KNOWN = new Set(WORDS);

/** splitPhrase returns the words in text, lower case, without the list
 *  numbers and punctuation around them, so "1. orbit," reads as "orbit". */
export function splitPhrase(text: string): string[] {
  return text
    .toLowerCase()
    .split(/[\s,;]+/)
    // Latin letters are enough: every word on the list is plain a to z, and an
    // accented one is kept whole so the error can name it as typed.
    .map((w) => w.replace(/^[^a-zÀ-ɏ]+|[^a-zÀ-ɏ]+$/g, ''))
    .filter(Boolean);
}

export interface PhraseCheck {
  words: string[];
  /** Positions, from 0, of words not on the list. */
  unknown: number[];
  /** Twelve words, all on the list. The checksum is left to the decoder. */
  complete: boolean;
}

/** checkPhrase looks up every word of text. The word still being typed, the
 *  last one with nothing after it, is not flagged yet, since a prefix of a good
 *  word is rarely a word itself. */
export function checkPhrase(text: string): PhraseCheck {
  const words = splitPhrase(text);
  const typing = text !== '' && !/[\s,;]$/.test(text);
  const unknown: number[] = [];
  words.forEach((w, i) => {
    if (!KNOWN.has(w) && !(typing && i === words.length - 1)) unknown.push(i);
  });
  const complete = words.length === WORD_COUNT && words.every((w) => KNOWN.has(w));
  return { words, unknown, complete };
}
