import { isolate } from './bidi';

/**
 * interpolate fills a sentence's {placeholders}. A text value is isolated, so
 * in a right-to-left language a size or a Latin name keeps its own order
 * instead of trading places with the words around it. A number is not: the
 * Bidi algorithm already keeps digits together, and an isolate would turn
 * "{n}/{max}" into a neutral pair that reads "5/1". The value goes in through
 * a function, since a replacement string would read "$&" or "$$" in a file or
 * view name as a pattern.
 */
export function interpolate(s: string, vars?: Record<string, string | number>): string {
  if (!vars) return s;
  s = s.replace(PLURAL, (whole, name: string, forms: string) => {
    const n = vars[name];
    return typeof n === 'number' ? pluralForm(forms, n) : whole;
  });
  for (const [k, v] of Object.entries(vars)) {
    const text = typeof v === 'number' ? String(v) : isolate(v);
    s = s.replaceAll(`{${k}}`, () => text);
  }
  return s;
}

/**
 * A count's plural forms in one placeholder, so every catalogue holds the forms
 * its own language has: "{n|one:# Link|other:# Links}". The form is picked by
 * the page's language, # stands for the count, and other is the fallback.
 */
const PLURAL = /\{(\w+)\|([^{}]+)\}/g;

function pluralForm(forms: string, n: number): string {
  const byCategory = new Map(
    forms.split('|').map((f) => {
      const at = f.indexOf(':');
      return [f.slice(0, at), f.slice(at + 1)] as const;
    }),
  );
  let category: string = 'other';
  try {
    category = new Intl.PluralRules(document.documentElement.lang || 'en').select(n);
  } catch {
    // An unknown language tag reads as English.
  }
  return (byCategory.get(category) ?? byCategory.get('other') ?? '').replaceAll('#', String(n));
}
