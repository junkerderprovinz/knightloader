// A value set into right-to-left text is laid out by the Bidi algorithm along
// with the words around it, so "2.7 GiB" in an Arabic line reads "GiB 2.7". An
// isolate keeps its order, and only a right-to-left language needs one, the
// same rule as the web UI's lib/bidi.ts.

const LRI = '⁦';
const FSI = '⁨';
const PDI = '⁩';

// React Native has no <html dir> to read, so the I18nProvider says which way
// the language in force runs whenever it changes.
let rtl = false;

export function setRightToLeft(on: boolean): void {
  rtl = on;
}

/** ltr keeps a number with its unit in left-to-right order. */
export function ltr(s: string): string {
  return s && rtl ? LRI + s + PDI : s;
}

/**
 * isolate sets a value of either direction into a sentence: it runs the way
 * its own first letter does, and the sentence keeps its order around it.
 */
export function isolate(s: string): string {
  return s && rtl ? FSI + s + PDI : s;
}
