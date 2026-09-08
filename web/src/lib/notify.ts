// Where each kind of event lands: a bubble in the app, a notification the
// operating system raises, or nothing at all.
//
// This is the per-event grid lib/toast.tsx's own module comment defers ("the
// per-event settings grid it also describes is deferred to the later sweep").
// It adds no event vocabulary of its own: NotificationKind is already the real
// taxonomy over there, KIND_BY_TONE already gives every untyped call site a
// kind, and toast() is already the one funnel every notification in this app
// passes through. All that was missing was the routing table in front of it.
//
// Three things decide the shape of this file:
//
//  1. THE SYSTEM CHANNEL IS SIMPLY ABSENT ON THE ORDINARY INSTALL. Notification
//     is secure-context only, and main.tsx already writes down what this
//     deployment usually is: "the container's default deployment is plain HTTP
//     on a LAN address, where most browsers do not expose
//     navigator.serviceWorker at all (it needs a secure context)". The same
//     sentence applies word for word here. So SYSTEM_SUPPORTED is a
//     feature-detect read once at module scope, exactly like
//     clipboardWatch.ts's WATCH_SUPPORTED, and the card dims the segment and
//     says why rather than offering a control that stands over nothing.
//
//  2. THE PERMISSION PROMPT IS ASKED FOR ONCE, FROM A PRESS. Never on mount,
//     never from an arrow key: Chrome penalises an origin that asks on load,
//     Safari refuses outside a gesture, and a refusal is sticky for good. So
//     the ask lives in choose(), which only ever runs from a real click (the
//     card passes activateOnFocus={false} so keyboard navigation through the
//     strip cannot reach it), and requestPermission's THIRD outcome is handled:
//     'default' means Chrome's quieter messaging suppressed the prompt without
//     asking anybody anything, and storing 'system' on that would leave a row
//     that looks set and delivers nothing, forever, with no error anywhere.
//
//  3. ONE FIELD HOLDING A MAP, NEVER A FIELD PER EVENT. Same reasoning as
//     dialogmute.ts's own: a field per event is how this file would have to be
//     edited for the eighth row, and the ninth. The bucket is shared between
//     browsers on purpose (uistate.ts: "Two browsers using it is deliberate"),
//     and notification permission is per browser and per origin, so a stored
//     'system' is READ as 'app' where this browser cannot honour it and is
//     never written back clamped - a phone on plain HTTP opening the settings
//     must not destroy the desktop's choice.
import { useCallback, useEffect, useState } from 'react';
import type { NotificationKind } from './toast';
import type { TranslationKey } from './i18n';
import { peekUIState, useUIState } from './uistate';

/**
 * Where one event lands.
 *
 * 'silent' is an instruction and not an absent preference, which is why it is
 * labelled "Show nothing" rather than "off": a row set to silent has been
 * decided, and the person who decided it wants to be able to read that back.
 */
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
 * The seven rows, and the reason it is exactly these seven.
 *
 * A row is offered only where the event has a source that is mounted on every
 * page: download completion (Layout.tsx's useCompletionToasts), extraction
 * (lib/useExtractionToasts.ts, written for this matrix) and the three captcha
 * outcomes (CaptchaModal, mounted once in Layout). A control over an event that
 * never arrives is worse than no control, so:
 *
 *  - account-benched / account-restored are NOT here. They exist in the kind
 *    union and in toast.tsx's CRITICAL table with nothing that can ever fire
 *    them: account health is a cached field on the account object, polled only
 *    while the Accounts page is open, and no hub broadcast announces the bench
 *    edge. The server already knows the moment (internal/accounts/health.go
 *    returns started=true on exactly that transition); it simply never says so.
 *    The day a Hub.Broadcast("accountHealth", ...) exists, two entries land
 *    here and nothing else in this file changes.
 *  - action-done / action-failed / info are NOT here either, and that is a
 *    different reason: they are the derived-from-tone catch-alls behind roughly
 *    fifty call sites, every one of them a synchronous answer to a press
 *    somebody just made. They keep today's bubble and are not configurable,
 *    which is why channelFor() answers 'app' for any kind not in this list.
 *
 * The defaults are chosen so that upgrading into this feature changes nothing
 * anybody already sees. Two of them are 'silent' on purpose: extraction-done
 * has no source in the shipped build at all, so starting it at 'app' would be
 * this release inventing new noise, and captcha-needs-answer arrives together
 * with the captcha window itself, which is already the loudest thing on screen.
 * Both rows exist so somebody can promote them, which is the whole point.
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

/**
 * A stable empty default, declared once at module scope.
 *
 * useUIState leaves its fallback out of the effect's dependencies on purpose
 * (uistate.ts says so outright), so an inline {} would be a fresh reference on
 * every render and would resubscribe the effect forever. dialogmute.ts's own
 * NONE is the same constant for the same reason.
 */
export const NO_CHANNELS: Readonly<Record<string, NotifyChannel>> = {};

/**
 * Whether this browser can raise an OS notification at all. Read once at module
 * scope, exactly like clipboardWatch.ts's WATCH_SUPPORTED: it is a property of
 * the deployment (secure context or not) and cannot change without a reload.
 *
 * Feature-detected rather than derived from window.isSecureContext, because the
 * secure context is the usual reason the constructor is missing and not the only
 * one: a browser that never shipped the API, or one where it is disabled by
 * policy, must land on the same answer.
 */
export const SYSTEM_SUPPORTED =
  typeof window !== 'undefined' &&
  'Notification' in window &&
  typeof Notification.requestPermission === 'function';

export type NotifyPermission = 'granted' | 'denied' | 'default' | 'unsupported';

/**
 * The permission as it stands right now.
 *
 * Notification.permission is a snapshot with no event behind it, which is the
 * whole reason watchPermission() below exists - a reading taken once at mount
 * goes stale the moment somebody unblocks the site in the browser's own
 * settings, and nothing tells the page.
 */
export function permissionNow(): NotifyPermission {
  if (!SYSTEM_SUPPORTED) return 'unsupported';
  return Notification.permission as NotifyPermission;
}

/**
 * The icon every system notification carries.
 *
 * Deliberately the manifest's own file and NOT the document's <link rel="icon">:
 * lib/tabIndicator.ts rewrites that favicon to a freshly drawn canvas data URL
 * whenever the queue owes work, so an icon read off the document would carry
 * whatever percent ring happened to be painted at that instant.
 */
const ICON = '/icons/icon-192.png';

/**
 * Raises one OS notification. A no-op unless the permission is actually granted,
 * so no caller has to check first.
 *
 * `tag` is not decoration. Two hundred finished files is two hundred queued,
 * bulk-undismissable Windows toasts; same-origin tag replacement collapses each
 * kind down to its most recent one. It also answers the second copy of the same
 * problem: two KnightLoader tabs open on one machine each hold their own socket
 * and each raise their own notification for the same event.
 *
 * The try/catch is not defensive padding. Android Chrome throws a TypeError from
 * this constructor outright (it serves notifications only through a service
 * worker registration, and public/sw.js deliberately handles nothing), and a
 * throw here would take the toast() call that made it down with it.
 */
export function showSystem(title: string, body: string, tag: string): void {
  if (!SYSTEM_SUPPORTED || Notification.permission !== 'granted') return;
  try {
    new Notification(title, { body, tag, icon: ICON });
  } catch {
    // Nothing to report and nothing to fall back to: the bubble was already
    // skipped by the time this ran, and a browser that refuses the constructor
    // refuses it every time.
  }
}

/**
 * Asks the browser, once, from whatever press called this.
 *
 * Never call it from an effect or on mount. That is the pattern browsers now
 * punish and people refuse, and a refusal here is sticky for the life of the
 * origin - there is no second chance to ask well.
 */
export async function requestSystemPermission(): Promise<NotifyPermission> {
  if (!SYSTEM_SUPPORTED) return 'unsupported';
  if (Notification.permission === 'granted') return 'granted';
  try {
    return (await Notification.requestPermission()) as NotifyPermission;
  } catch {
    // Older Safari hands back a callback-style API with no promise at all.
    // Whatever it did, the reading below is the truth about the outcome.
    return permissionNow();
  }
}

/**
 * Subscribes to the permission changing underneath the page, and returns the
 * unsubscribe.
 *
 * The Permissions API is the only real signal; visibilitychange is the fallback
 * for the browsers that have no such permission name. Both are needed: somebody
 * who unblocks the site in the browser's own settings while this page is open
 * would otherwise leave the card insisting they are blocked until a reload,
 * while every row set to 'system' quietly keeps not firing.
 */
export function watchPermission(onChange: (p: NotifyPermission) => void): () => void {
  if (!SYSTEM_SUPPORTED || typeof document === 'undefined') return () => {};
  let live = true;
  let dropStatus = () => {};
  const read = () => onChange(permissionNow());
  document.addEventListener('visibilitychange', read);
  try {
    // Safari does not know this permission name and throws, synchronously in
    // some versions and as a rejection in others - hence both a try and a catch
    // on the promise.
    void navigator.permissions
      ?.query({ name: 'notifications' })
      .then((status) => {
        if (!live) return;
        status.addEventListener('change', read);
        dropStatus = () => status.removeEventListener('change', read);
      })
      .catch(() => {});
  } catch {
    // The visibilitychange fallback above is already bound and is what this
    // browser gets.
  }
  return () => {
    live = false;
    document.removeEventListener('visibilitychange', read);
    dropStatus();
  };
}

/**
 * Where one kind lands right now. Synchronous and hook-free on purpose.
 *
 * toast() is memoised with an empty dependency list for a documented reason
 * (toast.tsx: flipping quiet mode must not "resubscribe every effect holding
 * onto the toast() identity it returned"), so the matrix is READ at call time
 * out of the uistate bucket rather than closed over from a hook. Putting
 * useNotifyChannels()'s result in toast()'s dependencies would reopen
 * useCompletionToasts's WebSocket every time somebody touched this card.
 *
 * The clamp is the second half of the shared-bucket rule at the top of this
 * file: a stored 'system' that this browser cannot honour is read as 'app', so
 * the event still reaches the person in front of it, and the stored value is
 * left exactly as the browser that wrote it left it.
 */
export function channelFor(kind: NotificationKind): NotifyChannel {
  const ev = NOTIFY_EVENTS.find((e) => e.kind === kind);
  // Not a configurable row: the tone-derived catch-alls keep the bubble they
  // have always had.
  if (!ev) return 'app';
  const stored = peekUIState<Record<string, NotifyChannel>>(NOTIFY_FIELD, NO_CHANNELS);
  const chosen = stored[kind] ?? ev.fallback;
  if (chosen === 'system' && permissionNow() !== 'granted') return 'app';
  return chosen;
}

/**
 * The matrix as the settings card edits it, plus the live permission reading.
 *
 * `channels` holds ONLY the rows somebody moved off their default. Writing every
 * row would put seven entries in a 256 KiB document shared with column widths
 * and package folds for no gain, and would freeze today's defaults into
 * everybody's stored state the first time they open this card.
 */
export function useNotifyChannels(): {
  channels: Record<string, NotifyChannel>;
  permission: NotifyPermission;
  /** Asks only when moving a row TO 'system', and only from a real press. */
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
      // A row put back on its default drops out of the document rather than
      // being written as the default: the stored map is the set of decisions,
      // and a default that was never a decision must not become one.
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
        // The segment is dimmed and the card already says why, so the press
        // lands on the honest value instead of storing an intention this
        // browser can never carry out.
        write(kind, 'app');
        return 'unsupported';
      }
      if (Notification.permission === 'granted') {
        write(kind, 'system');
        setPermission('granted');
        return 'granted';
      }
      const outcome = await ask();
      // 'denied' still stores 'system', and that is deliberate: the choice
      // survives, so allowing notifications again in the browser's own site
      // settings makes the row work without anybody having to come back here
      // and set it a second time. channelFor() reads it as 'app' meanwhile.
      // 'default' must NOT store 'system' - nothing was asked and nothing was
      // granted, so a stored 'system' would be a row that looks set and
      // delivers nothing at all, with no error anywhere to explain it.
      write(kind, outcome === 'default' ? 'app' : 'system');
      return outcome;
    },
    [ask, write],
  );

  return { channels, permission, choose, ask };
}

// --- The captcha arrival dedupe -------------------------------------------
//
// MODULE SCOPE, not a component ref, and that is load-bearing. main.tsx wraps
// the app in React.StrictMode, which double-invokes effects in development: a
// useRef<Set> is rebuilt on the second mount, the same captcha notifies twice,
// and the only place anybody would ever see it is the one place they would
// conclude the dedupe does not work and "fix" it wrongly.
//
// It has to exist at all because the event cannot be trusted on its own.
// internal/app/app_captcha.go broadcasts "captcha" for newly added AND merely
// changed challenges on a two-second poll, CaptchaModal refetches the whole
// pending list on every 'snapshot', and 'snapshot' arrives on every reconnect
// while connectWS retries every 1500 ms. A restarted server therefore replays
// every pending challenge once per reconnect.
const notifiedCaptchas = new Set<string>();

// Whether the pending list has been read at least once. Until it has, an
// arriving challenge is NOT new - it is far more likely to be one that was
// already waiting when the page opened, and a page load with a captcha already
// on screen has to stay quiet (Layout.tsx states the same rule for downloads:
// "transition only, so the initial snapshot is quiet").
let captchaBaseline = false;

/**
 * The pending list as it was found, marked without notifying.
 *
 * Called for the first fetch AND for every refetch a reconnect triggers. The set
 * is never cleared, so a challenge that was announced once stays announced
 * across as many reconnects as the server restarts cause.
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
