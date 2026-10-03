// The refusals the task routes send with a code, in the reader's language.
import { ApiError } from './api';
import type { TranslationKey } from './i18n';

type Translate = (key: TranslationKey, vars?: Record<string, string | number>) => string;

const REFUSALS: Partial<Record<string, TranslationKey>> = {
  nzbGone: 'task.restart.nzbGone',
};

/**
 * taskRefusal says why a task route refused, or null for a refusal without a
 * code this build knows, where the caller shows the server's sentence.
 */
export function taskRefusal(e: unknown, t: Translate): string | null {
  if (!(e instanceof ApiError) || !e.code) return null;
  const key = REFUSALS[e.code];
  return key ? t(key, e.params) : null;
}
