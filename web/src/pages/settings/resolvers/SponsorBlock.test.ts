import { expect, it } from 'vitest';

import { SPONSORBLOCK_CATEGORIES } from '../../../lib/api';
import { AVAILABLE } from '../../../lib/locales';

// The phone app names the categories too, so a person who sets SponsorBlock
// in both reads the same words.

/** The one call of node:fs used here; the web build carries no Node types. */
interface NodeFs {
  readFileSync(path: string, encoding: 'utf8'): string;
}

// Read off the disk from web/, where the tests run: as a module the app's
// catalogue would be compiled against the app's own tsconfig.
const fs = (globalThis as unknown as { process: { getBuiltinModule(id: 'node:fs'): NodeFs } }).process.getBuiltinModule('node:fs');

const webKey = (cat: string) => `settings.resolvers.sponsorBlock.${cat.replace(/_(\w)/g, (_, c: string) => c.toUpperCase())}`;
const appKey = (cat: string) => `subs.sb.${cat}`;

/** The raw literal a catalogue gives a key, as written in its source. */
function literal(file: string, key: string): string | undefined {
  const line = fs
    .readFileSync(file, 'utf8')
    .split('\n')
    .find((l) => l.startsWith(`  '${key}': `));
  return line?.slice(key.length + 6, -1);
}

it('names every SponsorBlock category as the phone app does, in every language', () => {
  const differ: string[] = [];
  for (const lang of AVAILABLE) {
    for (const cat of SPONSORBLOCK_CATEGORIES) {
      const ours = literal(`src/lib/locales/${lang}.ts`, webKey(cat));
      const theirs = literal(`../mobile/src/i18n/${lang}.ts`, appKey(cat));
      if (ours === undefined || ours !== theirs) differ.push(`${lang} ${cat}: web ${ours}, app ${theirs}`);
    }
  }
  expect(differ).toEqual([]);
});
