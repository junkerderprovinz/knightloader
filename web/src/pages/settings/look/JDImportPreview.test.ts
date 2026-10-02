// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';

import { interpolate } from '../../../lib/interpolate';
import type { TranslationKey } from '../../../lib/i18n';
import { de } from '../../../lib/locales/de';
import { itemName, reasonText } from './JDImportPreview';

const german = ((key: TranslationKey, vars?: Record<string, string | number>) =>
  interpolate(de[key], vars)) as Parameters<typeof reasonText>[0];

describe('reasonText', () => {
  it('words a code with its values in the reader’s language', () => {
    expect(
      reasonText(german, {
        code: 'noApiKey',
        params: { service: 'Real-Debrid', url: 'https://real-debrid.com/apitoken' },
        text: 'english',
      }),
    ).toBe(
      'JDownloader hat für Real-Debrid keinen Schlüssel, den KnightLoader nutzen kann. Hol dir einen unter https://real-debrid.com/apitoken und trag ihn auf der Seite Konten ein.',
    );
  });

  it('translates the field and the actions a rule loses one name at a time', () => {
    expect(
      reasonText(german, { code: 'ruleDropped', params: { actions: 'autoStart,moveAfter' }, text: 'english' }),
    ).toBe('Weggelassen, weil Regeln in KnightLoader das nicht können: gleich starten, nach dem Download verschieben');
    expect(
      reasonText(german, { code: 'rulePlaceholder', params: { field: 'downloadDir', tag: '<jd:env:HOME>' }, text: 'english' }),
    ).toContain('Das Feld Download-Ordner verwendet <jd:env:HOME>');
  });

  it('names the switch a built-in folder rule stands for', () => {
    expect(reasonText(german, { code: 'ruleBuiltinPackageFolder', text: 'english' })).toContain(
      '„Jedes Paket in einen eigenen Unterordner legen“',
    );
  });

  it('keeps the server’s sentence for a code it has no words for', () => {
    expect(reasonText(german, { code: 'fromANewerServer', text: 'the server’s words' })).toBe('the server’s words');
  });
});

describe('itemName', () => {
  it('counts the passwords, since that row has no name of its own', () => {
    expect(itemName(german, { kind: 'passwords', name: '', count: 2, total: 5 })).toBe(
      '5 Archivpasswörter, 2 davon neu hier',
    );
  });
});
