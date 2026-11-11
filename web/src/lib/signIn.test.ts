import { describe, expect, it } from 'vitest';

import { labelInside } from './signIn';

describe('labelInside', () => {
  it('writes the label into an empty field as its placeholder', () => {
    const field = { placeholder: '' };
    expect(labelInside(field, 'Password')).toBe(true);
    expect(field.placeholder).toBe('Password');
  });

  it('leaves a placeholder of the field’s own in place', () => {
    const field = { placeholder: 'XXXX-XXXX-XXXX' };
    expect(labelInside(field, 'Code from the log')).toBe(false);
    expect(field.placeholder).toBe('XXXX-XXXX-XXXX');
  });

  it('keeps the label above a field with a hint, where the hint stands', () => {
    const field = { placeholder: '' };
    expect(labelInside(field, 'Code', 'From your authenticator app')).toBe(false);
    expect(field.placeholder).toBe('');
  });

  it('keeps the label above a field that is not there yet', () => {
    expect(labelInside(null, 'Password')).toBe(false);
  });

  it('replaces the label it wrote before with one in a new language', () => {
    const field = { placeholder: '' };
    labelInside(field, 'Password');
    expect(labelInside(field, 'Passwort', undefined, 'Password')).toBe(true);
    expect(field.placeholder).toBe('Passwort');
  });
});
