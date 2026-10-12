import { describe, expect, it } from 'vitest';

import { ApiError } from './api';
import { lengthProblem, refusedLength } from './passwordLength';

describe('the length of a password', () => {
  it.each([
    ['eight letters', 'abcdefgh', 'short'],
    ['eleven letters', 'abcdefghijk', 'short'],
    ['eleven letters that take twenty-two bytes', 'äöüßäöüéäöü', 'short'],
    ['eleven Japanese characters in thirty-three bytes', '日本語のパスワードです', 'short'],
    ['twelve letters', 'abcdefghijkl', undefined],
    ['twelve letters that take twenty-four bytes', 'äöüßäöüéäöüß', undefined],
    ['72 bytes', 'a'.repeat(72), undefined],
    ['73 bytes', 'a'.repeat(73), 'long'],
    ['37 letters that take 74 bytes', 'ä'.repeat(37), 'long'],
  ])('%s', (_, password, want) => {
    expect(lengthProblem(password)).toBe(want);
  });

  it('reads the server’s refusals by their code', () => {
    expect(refusedLength(new ApiError('too short', 'passwordTooShort', undefined, 400))).toBe('short');
    expect(refusedLength(new ApiError('too long', 'passwordTooLong', undefined, 400))).toBe('long');
    expect(refusedLength(new ApiError('wrong password', undefined, undefined, 400))).toBeUndefined();
  });
});
