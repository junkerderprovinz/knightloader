import { describe, expect, it } from 'vitest';
import { glyphFor } from './glyphFor';

// The label keys GlimStone's fortress, its windows and its app cards ask a glyph for.
const FORTRESS_KEYS = [
  'fortressJoin',
  'fortressLeave',
  'fortressShowPhrase',
  'fortressChangePhrase',
  'unlock',
  'copy',
  'confirm',
  'delete',
  'open',
  'info',
  'deviceServer',
  'devicePhone',
  'common.cancel',
  'common.close',
];

describe('glyphFor', () => {
  it.each(FORTRESS_KEYS)('gives %s a glyph', (key) => {
    expect(glyphFor(key)).toBeTruthy();
  });

  it('leaves a key it does not know to the button', () => {
    expect(glyphFor('somethingElse')).toBeUndefined();
  });
});
