// Why the link filter, the tracker ban or the download history rejected a link,
// as the upload line, the rejected links, the task detail and the filter's dry
// run word it.

import { fmtDate } from './format';
import type { TranslationKey } from './i18n';

/** The codes internal/rules and internal/app send with a rejection's reason. */
const KEYS: Record<string, TranslationKey> = {
  bannedTracker: 'collector.filtered.reason.bannedTracker',
  filterRule: 'collector.filtered.reason.filterRule',
  filterRuleReason: 'collector.filtered.reason.filterRuleReason',
  downloaded: 'collector.filtered.reason.downloaded',
};

/**
 * rejectionReason is the reason in the reader's language where this build has
 * words for the server's code, and the server's English sentence otherwise, as
 * for a link stored without a code.
 */
export function rejectionReason(
  t: (key: TranslationKey, vars?: Record<string, string | number>) => string,
  code: string | undefined,
  params: Record<string, string> | undefined,
  english: string | undefined,
): string {
  const key = code ? KEYS[code] : undefined;
  if (!key) return english ?? '';
  // The server sends the finish time as a timestamp, for the reader's own date format.
  const vars = params?.finished ? { ...params, finished: fmtDate(params.finished) } : params;
  return t(key, vars);
}
