// Where each kind of event lands: a bubble in the app, a notification from the
// operating system, or nothing. toast() consults this for every notification.
//
// The system channel needs a secure context, which the usual plain-HTTP LAN
// install is not, so SYSTEM_SUPPORTED is detected once and the card explains a
// missing option. The permission prompt is only ever triggered by a click,
// since browsers penalise prompts on load and a refusal sticks.
//
// The choices live in one uistate field holding a map. That bucket is shared
// between browsers while permission is per browser, so a stored 'system' that
// this browser cannot honour is read as 'app' but never written back.
import { useCallback, useEffect, useState } from 'react';
import type { NotificationKind } from './toast';
import type { TranslationKey } from './i18n';
import { peekUIState, useUIState } from './uistate';

/** Where one event lands. 'silent' is a decision and reads "Show nothing". */
export type NotifyChannel = 'app' | 'system' | 'silent';

/** One configurable row. Only kinds with a real, always-mounted source appear. */
export interface NotifyEvent {
  kind: NotificationKind;
  label: TranslationKey;
  hint: TranslationKey;
  /** What this build did before the matrix existed, so an upgrade changes nothing. */
  fallback: NotifyChannel;
}

/**
 * The configurable rows: only events whose source is mounted on every page
 * (download completion, extraction, the captcha outcomes).
 *
 * account-benched and account-restored are missing because nothing broadcasts
 * the bench edge yet. action-done, action-failed and info are the tone-derived
 * catch-alls answering a click and always use the bubble.
 *
 * extraction-done defaults to silent because nothing in the shipped build
 * fires it, and captcha-needs-answer because the captcha window is already on
 * screen. Both rows exist so somebody can promote them.
 */
export const NOTIFY_EVENTS: readonly NotifyEvent[] = [
  {
    kind: 'download-done',
    label: 'notifications.event.downloadDone',
    hint: 'notifications.event.downloadDoneHint',
    fallback: 'app',
  },
  {
    kind: 'download-failed',
    label: 'notifications.event.downloadFailed',
    hint: 'notifications.event.downloadFailedHint',
    fallback: 'app',
  },
  {
    kind: 'extraction-done',
    label: 'notifications.event.extractionDone',
    hint: 'notifications.event.extractionDoneHint',
    fallback: 'silent',
  },
  {
    kind: 'extraction-failed',
    label: 'notifications.event.extractionFailed',
    hint: 'notifications.event.extractionFailedHint',
    fallback: 'app',
  },
  {
    kind: 'captcha-needs-answer',
    label: 'notifications.event.captchaWaiting',
    hint: 'notifications.event.captchaWaitingHint',
    fallback: 'silent',
  },
  {
    kind: 'captcha-failed',
    label: 'notifications.event.captchaFailed',
    hint: 'notifications.event.captchaFailedHint',
    fallback: 'app',
  },
  {
    kind: 'captcha-resolved',
    label: 'notifications.event.captchaResolved',
    hint: 'notifications.event.captchaResolvedHint',
    fallback: 'app',
  },
];

/** The one uistate field. A map, never a field per event (see lib/dialogmute.ts). */
export const NOTIFY_FIELD = 'notifications.channels';

/** A stable empty default. useUIState leaves its fallback out of the effect's
 *  dependencies, so an inline {} would resubscribe on every render. */
export const NO_CHANNELS: Readonly<Record<string, NotifyChannel>> = {};

/**
 * Whether this browser can raise an OS notification at all. Detected from the
 * API rather than isSecureContext, since a browser may lack or disable it for
 * other reasons. It cannot change without a reload.
 */
export const SYSTEM_SUPPORTED =
  typeof window !== 'undefined' &&
  'Notification' in window &&
  typeof Notification.requestPermission === 'function';

export type NotifyPermission = 'granted' | 'denied' | 'default' | 'unsupported';

/** permissionNow reads the permission now; nothing announces a change, see
 *  watchPermission. */
export function permissionNow(): NotifyPermission {
  if (!SYSTEM_SUPPORTED) return 'unsupported';
  return Notification.permission as NotifyPermission;
}

// The manifest's icon rather than the favicon, which tabIndicator.ts redraws
// with a progress ring.
const ICON = '/icons/icon-192.png';

/**
 * showSystem raises one OS notification if permission is granted. The tag
 * makes a burst of the same kind replace itself instead of stacking, also
 * across two open tabs. Android Chrome throws from the constructor because it
 * only supports notifications through a service worker, hence the catch.
 */
export function showSystem(title: string, body: string, tag: string): void {
  if (!SYSTEM_SUPPORTED || Notification.permission !== 'granted') return;
  try {
    new Notification(title, { body, tag, icon: ICON });
  } catch {
    // A browser that refuses the constructor refuses it every time.
  }
}

/**
 * requestSystemPermission asks the browser. Call it only from a click, never
 * from an effect or on mount: a refusal sticks for the origin.
 */
export async function requestSystemPermission(): Promise<NotifyPermission> {
  if (!SYSTEM_SUPPORTED) return 'unsupported';
  if (Notification.permission === 'granted') return 'granted';
  try {
    return (await Notification.requestPermission()) as NotifyPermission;
  } catch {
    // Older Safari has a callback API without a promise; read the outcome directly.
    return permissionNow();
  }
}

/**
 * watchPermission calls onChange when the permission may have changed, for
 * example after the site was unblocked in the browser's settings, and returns
 * the unsubscribe. The Permissions API is the real signal; visibilitychange
 * covers browsers without it.
 */
export function watchPermission(onChange: (p: NotifyPermission) => void): () => void {
  if (!SYSTEM_SUPPORTED || typeof document === 'undefined') return () => {};
  let live = true;
  let dropStatus = () => {};
  const read = () => onChange(permissionNow());
  document.addEventListener('visibilitychange', read);
  try {
    // Safari throws on this permission name, synchronously in some versions
    // and as a rejection in others.
    void navigator.permissions
      ?.query({ name: 'notifications' })
      .then((status) => {
        if (!live) return;
        status.addEventListener('change', read);
        dropStatus = () => status.removeEventListener('change', read);
      })
      .catch(() => {});
  } catch {
    // The visibilitychange listener above is the fallback.
  }
  return () => {
    live = false;
    document.removeEventListener('visibilitychange', read);
    dropStatus();
  };
}

/**
 * channelFor is where one kind lands right now. It reads the uistate bucket
 * at call time instead of using a hook, so toast() keeps its stable identity.
 * A stored 'system' without permission is read as 'app'.
 */
export function channelFor(kind: NotificationKind): NotifyChannel {
  const ev = NOTIFY_EVENTS.find((e) => e.kind === kind);
  if (!ev) return 'app';
  const stored = peekUIState<Record<string, NotifyChannel>>(NOTIFY_FIELD, NO_CHANNELS);
  const chosen = stored[kind] ?? ev.fallback;
  if (chosen === 'system' && permissionNow() !== 'granted') return 'app';
  return chosen;
}

/**
 * useNotifyChannels is the matrix as the settings card edits it, plus the
 * live permission. `channels` holds only rows moved off their default, so the
 * defaults are not frozen into everyone's stored state.
 */
export function useNotifyChannels(): {
  channels: Record<string, NotifyChannel>;
  permission: NotifyPermission;
  /** Asks only when moving a row to 'system', and only from a real press. */
  choose: (kind: NotificationKind, next: NotifyChannel) => Promise<NotifyPermission>;
  /** The same ask on its own, for the card's test button. */
  ask: () => Promise<NotifyPermission>;
} {
  const [channels, setChannels] = useUIState<Record<string, NotifyChannel>>(NOTIFY_FIELD, NO_CHANNELS);
  const [permission, setPermission] = useState<NotifyPermission>(permissionNow);

  useEffect(() => watchPermission(setPermission), []);

  const write = useCallback(
    (kind: NotificationKind, ch: NotifyChannel) => {
      const ev = NOTIFY_EVENTS.find((e) => e.kind === kind);
      const next = { ...channels };
      // A row set back to its default is removed rather than stored.
      if (!ev || ch === ev.fallback) delete next[kind];
      else next[kind] = ch;
      setChannels(next);
    },
    [channels, setChannels],
  );

  const ask = useCallback(async () => {
    const outcome = await requestSystemPermission();
    setPermission(outcome);
    return outcome;
  }, []);

  const choose = useCallback(
    async (kind: NotificationKind, next: NotifyChannel): Promise<NotifyPermission> => {
      if (next !== 'system') {
        write(kind, next);
        return permissionNow();
      }
      if (!SYSTEM_SUPPORTED) {
        // The segment is dimmed with an explanation; store what will actually happen.
        write(kind, 'app');
        return 'unsupported';
      }
      if (Notification.permission === 'granted') {
        write(kind, 'system');
        setPermission('granted');
        return 'granted';
      }
      const outcome = await ask();
      // 'denied' still stores 'system', so allowing notifications later in the
      // browser makes the row work; channelFor reads it as 'app' meanwhile.
      // 'default' means the browser suppressed the prompt, and storing
      // 'system' then would give a row that looks set and never fires.
      write(kind, outcome === 'default' ? 'app' : 'system');
      return outcome;
    },
    [ask, write],
  );

  return { channels, permission, choose, ask };
}

// Captchas already announced. Module scope rather than a ref, because
// StrictMode's double mount would rebuild a ref and notify twice. The dedupe
// is needed because the server re-broadcasts changed challenges and every
// reconnect's snapshot refetches the whole pending list.
const notifiedCaptchas = new Set<string>();

// Until the pending list has been read once, an arriving challenge was most
// likely already waiting when the page opened and stays quiet.
let captchaBaseline = false;

/**
 * seedCaptchasSeen marks the pending list as announced without notifying.
 * Called for the first fetch and every refetch after a reconnect; the set is
 * never cleared, so a challenge is announced once.
 */
export function seedCaptchasSeen(ids: string[]): void {
  for (const id of ids) notifiedCaptchas.add(id);
  captchaBaseline = true;
}

/** True exactly once per challenge, and never before the baseline is in. */
export function captchaIsNew(id: string): boolean {
  if (!captchaBaseline || notifiedCaptchas.has(id)) return false;
  notifiedCaptchas.add(id);
  return true;
}

/** Pruned when the challenge settles, so the set cannot grow without bound. */
export function forgetCaptcha(id: string): void {
  notifiedCaptchas.delete(id);
}
