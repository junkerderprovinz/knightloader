import { describe, expect, it } from 'vitest';
import { checkPhrase, splitPhrase } from './phraseWords';

const WORDS = 'orbit wagon lemon crisp absent tunnel galaxy harbor pencil ribbon velvet yellow'.split(' ');

describe('splitPhrase', () => {
  it('reads words separated by spaces', () => {
    expect(splitPhrase(WORDS.join(' '))).toEqual(WORDS);
  });

  it('reads one word per line', () => {
    expect(splitPhrase(WORDS.join('\n'))).toEqual(WORDS);
    expect(splitPhrase(WORDS.join('\r\n'))).toEqual(WORDS);
  });

  it('drops the numbers of a numbered list', () => {
    const numbered = WORDS.map((w, i) => `${i + 1}. ${w}`).join('\n');
    expect(splitPhrase(numbered)).toEqual(WORDS);
    expect(splitPhrase(WORDS.map((w, i) => `${i + 1}) ${w}`).join(' '))).toEqual(WORDS);
    expect(splitPhrase(WORDS.map((w, i) => `${i + 1}.${w}`).join(' '))).toEqual(WORDS);
  });

  it('reads words separated by commas, with or without spaces', () => {
    expect(splitPhrase(WORDS.join(','))).toEqual(WORDS);
    expect(splitPhrase(WORDS.join(', '))).toEqual(WORDS);
    expect(splitPhrase(WORDS.join('; '))).toEqual(WORDS);
  });

  it('lowers the case and ignores space around the phrase', () => {
    expect(splitPhrase(`  ${WORDS.join(' ').toUpperCase()}  \n`)).toEqual(WORDS);
  });
});

describe('checkPhrase', () => {
  it('takes twelve known words as complete', () => {
    const c = checkPhrase(WORDS.join(' '));
    expect(c.complete).toBe(true);
    expect(c.unknown).toEqual([]);
  });

  it('names an unknown word by its position as soon as the next one starts', () => {
    const typed = ['orbit', 'wagon', 'lemmon', 'crisp'];
    const c = checkPhrase(`${typed.join(' ')} `);
    expect(c.unknown).toEqual([2]);
    expect(c.words[c.unknown[0]]).toBe('lemmon');
    expect(c.complete).toBe(false);
  });

  it('waits with the word still being typed', () => {
    expect(checkPhrase('orbit wag').unknown).toEqual([]);
    expect(checkPhrase('orbit wag ').unknown).toEqual([1]);
  });

  it('stays incomplete below twelve words and above', () => {
    expect(checkPhrase(WORDS.slice(0, 11).join(' ')).complete).toBe(false);
    expect(checkPhrase([...WORDS, 'orbit'].join(' ')).complete).toBe(false);
  });

  it('stays incomplete with twelve words while one is unknown', () => {
    const words = [...WORDS];
    words[11] = 'yelow';
    const c = checkPhrase(words.join(' '));
    expect(c.complete).toBe(false);
    expect(checkPhrase(`${words.join(' ')} `).unknown).toEqual([11]);
  });

  it('leaves a wrong but known word to the checksum on the server', () => {
    const swapped = [WORDS[1], WORDS[0], ...WORDS.slice(2)];
    expect(checkPhrase(swapped.join(' ')).complete).toBe(true);
  });
});
