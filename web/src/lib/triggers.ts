// What each of internal/script's eleven triggers is CALLED, in one place.
//
// It lives here rather than in either page because two pages now offer the same
// vocabulary: the script editor binds one script to one trigger, and the event
// targets page ticks any number of them per target. A second, private map beside
// the first is the drift this codebase argues against everywhere - and it is not
// hypothetical, because the first one was already incomplete. Scripts.tsx's own
// KNOWN_TRIGGER_KEY had labels for FOUR of the eleven; the seven the event bus
// added rendered as their raw dotted ids ("checksum.failed") in a menu of
// sentences. script.AllTriggers' own doc comment makes the identical argument
// one layer down about the list itself.
//
// THE LIST IS STILL NOT HERE. Which triggers exist comes from
// GET /api/scripts/triggers, from the registry that actually fires them - see
// lib/scripts.ts's fetchScriptTriggers. This file only answers "and what is that
// one called", which is why an id it does not recognise falls back to itself
// rather than to a blank: a build newer than this frontend may fire a twelfth
// event, and rendering it as its own id is honest where rendering nothing is
// not.

import { useCallback } from 'react';
import { useT, type TranslationKey } from './i18n';

/**
 * Keyed by internal/script.Trigger's real values (script.go), in the order
 * AllTriggers returns them: the original four first, then the seven the bus
 * added. The order matters to nothing here - a Record has none - but keeping it
 * makes this file readable beside the Go one.
 *
 * The four settings.scripts.trigger.* keys are the ones the script editor
 * already shipped. They are reused rather than re-minted under an
 * eventTargets.* prefix: one English name per event, not two that can drift into
 * calling the same thing different things on two pages.
 */
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

/**
 * The trigger's name in the reader's language, or the raw id when this build
 * has never heard of it.
 *
 * A hook rather than a plain function taking `t`, so that a component adds one
 * line to use it instead of one line plus a `t` it may not already have. It
 * memoises on `t` alone, which changes only when the language does.
 */
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
 * Which trigger fires again after a restart, and is therefore the noise an
 * operator meets on the morning after a container update.
 *
 * Three of the eleven, and each for a reason that lives in memory rather than on
 * disk (see internal/app):
 *
 *   - queue.idle: watchQueueIdleForScripts is level-triggered and deliberately
 *     has NO everBusy gate, so an idle box announces itself idle within two
 *     seconds of every start.
 *   - captcha.pending: the captcha store is built fresh per process, so
 *     everything still waiting counts as newly added on the first sweep.
 *   - account.expired: the edge detection reads a previous value out of an
 *     in-memory table, so every already-lapsed account crosses the edge again.
 *
 * package.done is the one that is restart-safe, and its own poll loop says why:
 * the first pass seeds silently. There is deliberately no grace period anywhere
 * that suppresses these - one that swallowed a real captcha arriving eight
 * seconds after boot would be worse than the noise - so the page says it and
 * leaves the choice with the operator.
 */
export const REPLAYS_AFTER_RESTART = new Set(['queue.idle', 'captcha.pending', 'account.expired']);

/** link.added fires once per link (script.go), so pasting two hundred links is
 *  two hundred messages. The one trigger a target's own picker has to warn
 *  about by name. */
export const FIRES_PER_LINK = 'link.added';
