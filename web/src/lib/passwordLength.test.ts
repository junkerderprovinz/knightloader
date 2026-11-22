import { describe, expect, it } from 'vitest';

import { ApiError } from './api';
import { isShort, refusedTooLong, tooLong } from './passwordLength';

describe('the length of a password', () => {
  it.each([
    ['nothing yet', '', false],
    ['one letter', 'a', true],
    ['eleven letters', 'abcdefghijk', true],
    ['eleven letters that take twenty-two bytes', 'äöüßäöüéäöü', true],
    ['eleven Japanese characters in thirty-three bytes', '日本語のパスワードです', true],
    ['twelve letters', 'abcdefghijkl', false],
    ['twelve letters that take twenty-four bytes', 'äöüßäöüéäöüß', false],
  ])('counts %s as short: %s', (_, password, want) => {
    expect(isShort(password)).toBe(want);
  });

  it.each([
    ['one letter', 'a', false],
    ['72 bytes', 'a'.repeat(72), false],
    ['73 bytes', 'a'.repeat(73), true],
    ['37 letters that take 74 bytes', 'ä'.repeat(37), true],
  ])('counts %s as too long: %s', (_, password, want) => {
    expect(tooLong(password)).toBe(want);
  });

  it('reads the server’s refusal of a password that is too long by its code', () => {
    expect(refusedTooLong(new ApiError('too long', 'passwordTooLong', undefined, 400))).toBe(true);
    expect(refusedTooLong(new ApiError('wrong password', undefined, undefined, 400))).toBe(false);
    expect(refusedTooLong(new Error('offline'))).toBe(false);
  });
});
