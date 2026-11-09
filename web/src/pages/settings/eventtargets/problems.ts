import type { TranslationKey } from '../../../lib/i18n';

/**
 * problemText is the sentence for a code the kit reports about a channel, from
 * a send or from the settings check, or the code itself when this build has no
 * sentence for it.
 */
export function problemText(t: (key: TranslationKey) => string, code: string): string {
  const key = `settings.eventTargets.problem.${code}` as TranslationKey;
  const text = t(key) as string | undefined;
  return text && text !== key ? text : code;
}
