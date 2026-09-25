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
  for (const [k, v] of Object.entries(vars)) {
    const text = typeof v === 'number' ? String(v) : isolate(v);
    s = s.replaceAll(`{${k}}`, () => text);
  }
  return s;
}
