// Why a link sits in the holding area, as the upload line, the holding area,
// the task detail and the filter's dry run word it.

import type { TranslationKey } from './i18n';

/** The codes internal/rules and internal/app send with a held link's reason. */
const KEYS: Record<string, TranslationKey> = {
  bannedTracker: 'collector.filtered.reason.bannedTracker',
  filterRule: 'collector.filtered.reason.filterRule',
  filterRuleReason: 'collector.filtered.reason.filterRuleReason',
};

/**
 * heldReason is the reason in the reader's language where this build has words
 * for the server's code, and the server's English sentence otherwise, as for a
 * link stored without a code.
 */
export function heldReason(
  t: (key: TranslationKey, vars?: Record<string, string | number>) => string,
  code: string | undefined,
  params: Record<string, string> | undefined,
  english: string | undefined,
): string {
  const key = code ? KEYS[code] : undefined;
  return key ? t(key, params) : (english ?? '');
}
