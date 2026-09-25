import { describe, expect, it } from 'vitest';
import { en } from '../../../lib/locales/en';
import { presetOf, scopesLabel, withScope } from './tokenScopes';

const t = (key: keyof typeof en) => en[key];

describe('presetOf', () => {
  it('names the preset a set of rights matches, whatever the order', () => {
    expect(presetOf(['admin', 'control', 'add', 'read'])).toBe('full');
    expect(presetOf(['add', 'read'])).toBe('addRead');
    expect(presetOf(['read'])).toBe('read');
  });

  it('calls anything else custom', () => {
    expect(presetOf(['read', 'control'])).toBe('custom');
    expect(presetOf(['add'])).toBe('custom');
    expect(presetOf([])).toBe('custom');
  });
});

describe('scopesLabel', () => {
  it('shows a preset by its name and anything else right by right', () => {
    expect(scopesLabel(t, ['read', 'add'])).toBe(en['settings.access.tokens.preset.addRead']);
    expect(scopesLabel(t, ['read', 'control'])).toBe(
      `${en['settings.access.tokens.scope.read']}, ${en['settings.access.tokens.scope.control']}`,
    );
  });
});

describe('withScope', () => {
  it('keeps the server order when a right is switched on', () => {
    expect(withScope(['admin'], 'read', true)).toEqual(['read', 'admin']);
  });

  it('drops a right that is switched off and leaves the rest', () => {
    expect(withScope(['read', 'add', 'control'], 'add', false)).toEqual(['read', 'control']);
  });
});
