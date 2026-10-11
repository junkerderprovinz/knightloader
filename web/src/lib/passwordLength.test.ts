import { describe, expect, it } from 'vitest';

import { ApiError } from './api';
import { lengthProblem, refusedLength } from './passwordLength';

describe('the length of a password', () => {
  it.each([
    ['seven letters', 'abcdefg', 'short'],
    ['seven letters that take fourteen bytes', 'äöüßäöü', 'short'],
    ['seven Japanese characters in twenty-one bytes', '日本語のパスワ', 'short'],
    ['eight letters', 'abcdefgh', undefined],
    ['eight letters that take sixteen bytes', 'äöüßäöüé', undefined],
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
