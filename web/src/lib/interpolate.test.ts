// @vitest-environment jsdom
import { afterEach, expect, it } from 'vitest';

import { interpolate } from './interpolate';

afterEach(() => document.documentElement.removeAttribute('lang'));

const HELD = '{n|one:# Link abgelehnt|other:# Links abgelehnt}';

it('picks the plural form the count takes', () => {
  document.documentElement.lang = 'de';
  expect(interpolate(HELD, { n: 1 })).toBe('1 Link abgelehnt');
  expect(interpolate(HELD, { n: 12 })).toBe('12 Links abgelehnt');
});

it('knows the forms a language has beyond one and other', () => {
  document.documentElement.lang = 'ru';
  const ru = '{n|one:# ссылка|few:# ссылки|many:# ссылок|other:# ссылки}';
  expect(interpolate(ru, { n: 1 })).toBe('1 ссылка');
  expect(interpolate(ru, { n: 3 })).toBe('3 ссылки');
  expect(interpolate(ru, { n: 12 })).toBe('12 ссылок');
});

it('falls back to other where a form is missing', () => {
  document.documentElement.lang = 'pl';
  expect(interpolate('{n|other:# linków}', { n: 2 })).toBe('2 linków');
});

it('fills the other placeholders around the plural', () => {
  document.documentElement.lang = 'en';
  expect(interpolate('{n|one:# link|other:# links} from {file}', { n: 2, file: 'x' })).toContain('2 links from');
});
