import { expect, it } from 'vitest';

import { SPONSORBLOCK_CATEGORIES } from './api';
import { AVAILABLE } from './locales';

// The phone app names subscriptions and the SponsorBlock categories too, so a
// person who sets them up in both reads the same words.

/** The one call of node:fs used here; the web build carries no Node types. */
interface NodeFs {
  readFileSync(path: string, encoding: 'utf8'): string;
}

// Read off the disk from web/, where the tests run: as a module the app's
// catalogue would be compiled against the app's own tsconfig.
const fs = (globalThis as unknown as { process: { getBuiltinModule(id: 'node:fs'): NodeFs } }).process.getBuiltinModule('node:fs');

/** The raw literal a catalogue gives a key, as written in its source. */
function literal(file: string, key: string): string | undefined {
  const line = fs
    .readFileSync(file, 'utf8')
    .split('\n')
    .find((l) => l.startsWith(`  '${key}': `));
  return line?.slice(key.length + 6, -1);
}

/** Every language where a web key reads differently from its app key. */
function differences(pairs: [web: string, app: string][]): string[] {
  const differ: string[] = [];
  for (const lang of AVAILABLE) {
    for (const [webKey, appKey] of pairs) {
      const ours = literal(`src/lib/locales/${lang}.ts`, webKey);
      const theirs = literal(`../mobile/src/i18n/${lang}.ts`, appKey);
      if (ours === undefined || ours !== theirs) differ.push(`${lang} ${webKey}: web ${ours}, app ${theirs}`);
    }
  }
  return differ;
}

it('names every SponsorBlock category as the phone app does, in every language', () => {
  const camel = (cat: string) => cat.replace(/_(\w)/g, (_, c: string) => c.toUpperCase());
  const pairs = SPONSORBLOCK_CATEGORIES.map((cat): [string, string] => [
    `settings.resolvers.sponsorBlock.${camel(cat)}`,
    `subs.sb.${cat}`,
  ]);
  expect(differences(pairs)).toEqual([]);
});

it('names subscriptions as the phone app does, in every language', () => {
  expect(
    differences([
      ['settings.module.feeds', 'subs.title'],
      ['health.part.feeds', 'subs.title'],
      ['settings.feeds.notFound', 'subs.notFound'],
    ]),
  ).toEqual([]);
});
