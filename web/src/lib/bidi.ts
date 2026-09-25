// A value set into right-to-left text is laid out by the Bidi algorithm along
// with the words around it, so "2.7 GiB" reads "GiB 2.7" and "< 0.1 GB" turns
// into "GB 0.1 >". An isolate keeps its order, and only right-to-left pages
// need one: in a left-to-right page these marks would change nothing.

const LRI = '⁦';
const FSI = '⁨';
const PDI = '⁩';

// <html dir> follows the language picker (lib/i18n.tsx), which spares the
// formatters the i18n provider.
const rtl = (): boolean => document.documentElement.dir === 'rtl';

/**
 * ltr keeps a number with its unit, a duration or a count of something in
 * left-to-right order, whatever the page's direction.
 */
export function ltr(s: string): string {
  return s && rtl() ? LRI + s + PDI : s;
}

/**
 * isolate sets a value of either direction into a sentence: it runs the way
 * its own first letter does, an Arabic file name right to left and a size left
 * to right, and the sentence keeps its order around it.
 */
export function isolate(s: string): string {
  return s && rtl() ? FSI + s + PDI : s;
}
