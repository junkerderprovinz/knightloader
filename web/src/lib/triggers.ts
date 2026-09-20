// The names of internal/script's triggers, shared by the script editor and the
// event targets page. Which triggers exist comes from GET /api/scripts/triggers
// (see fetchScriptTriggers in lib/scripts.ts); an id without a name here is
// shown as itself, since a newer server may fire one this build does not know.

import { useCallback } from 'react';
import { useT, type TranslationKey } from './i18n';

/** Keyed by internal/script.Trigger's values, in AllTriggers' order. */
export const TRIGGER_LABEL_KEY: Record<string, TranslationKey> = {
  manual: 'settings.scripts.trigger.manual',
  'task.done': 'settings.scripts.trigger.taskDone',
  'task.failed': 'settings.scripts.trigger.taskFailed',
  'queue.idle': 'settings.scripts.trigger.queueIdle',
  'link.added': 'settings.scripts.trigger.linkAdded',
  'package.done': 'settings.scripts.trigger.packageDone',
  'extract.done': 'settings.scripts.trigger.extractDone',
  'checksum.failed': 'settings.scripts.trigger.checksumFailed',
  'reconnect.done': 'settings.scripts.trigger.reconnectDone',
  'account.expired': 'settings.scripts.trigger.accountExpired',
  'captcha.pending': 'settings.scripts.trigger.captchaPending',
};

/** useTriggerLabel returns a function giving a trigger's name in the reader's
 *  language, or its raw id when unknown. */
export function useTriggerLabel(): (trigger: string) => string {
  const { t } = useT();
  return useCallback(
    (trigger: string) => {
      const key = TRIGGER_LABEL_KEY[trigger];
      return key ? t(key) : trigger;
    },
    [t],
  );
}

/**
 * The triggers that fire again after a restart, because their state lives in
 * memory: queue.idle announces an idle box within two seconds of start,
 * captcha.pending sees every waiting captcha as new, and account.expired
 * crosses the edge again for every lapsed account. package.done seeds silently
 * on its first pass. Nothing suppresses these, since a grace period could
 * swallow a real event; the page warns instead.
 */
export const REPLAYS_AFTER_RESTART = new Set(['queue.idle', 'captcha.pending', 'account.expired']);

/** link.added fires once per link, so pasting two hundred links sends two
 *  hundred messages; the picker warns about it by name. */
export const FIRES_PER_LINK = 'link.added';
