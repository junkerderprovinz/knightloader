// Copied from GlimStone's packages/core/src/intl.ts. Older WebViews ship Intl
// without some of its constructors, so each use asks first and has a plain
// stand-in.

/** Whether this engine's Intl has the named constructor, such as `RelativeTimeFormat`. */
export function intlHas(name: 'Segmenter' | 'Collator' | 'PluralRules' | 'ListFormat' | 'RelativeTimeFormat'): boolean {
  return typeof Intl === 'object' && typeof (Intl as unknown as Record<string, unknown>)[name] === 'function';
}
