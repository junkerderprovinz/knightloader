import { useEffect, useRef } from 'react';
import { AppState, PermissionsAndroid, Platform } from 'react-native';
import { KnightWatch, type OpenRequest } from '../../modules/watch';
import { ApiError, fetchCaptchasUnwatched, fetchTasks } from '../api/client';
import { explainFailure } from '../api/taskError';
import type { CaptchaChallenge, ServerConnection, Task } from '../api/types';
import { AVAILABLE, load } from '../i18n';
import { detectDeviceLanguage, translate } from '../i18n/I18nContext';
import type { TranslationKey } from '../i18n/en';
import { listConnections } from '../storage/connections';
import { getLanguageOverride } from '../storage/languagePreference';
import { anyKind, loadNotifyPrefs, saveNotifyPrefs, type NotifyPrefs } from './prefs';
import { FAST_MS, compare, isBusy, keepRunning, lookOf, nextDelay, noticeId, type Look, type News } from './rules';

// The watch behind the app's notifications. Android's foreground service in
// modules/watch runs one pass of it at a time as a headless task, and the app
// runs the same pass while it is in front to find out whether the service is
// needed. Both share this module's memory, so a look taken in front is the
// "before" of the first look in the background.

/** The headless task's name, WatchService.TASK on the native side. */
export const WATCH_TASK = 'KnightLoaderWatch';

/** How often the app in front looks whether the service should start. */
const CHECK_MS = 15_000;

interface Watched {
  look: Look | null;
  lastBusy: number;
  ok: boolean;
  captchaShown: boolean;
}

type Translate = (key: TranslationKey, vars?: Record<string, string | number>) => string;

const watched = new Map<string, Watched>();
let delay = FAST_MS;
let startedAt = 0;
let onScreen: string | null = null;
let passing: Promise<boolean> | null = null;

async function translator(): Promise<Translate> {
  const override = await getLanguageOverride().catch(() => null);
  const dict = await load(override && AVAILABLE.includes(override) ? override : detectDeviceLanguage());
  return (key, vars) => translate(dict, key, vars);
}

function nameChannels(t: Translate): void {
  KnightWatch?.channels(
    t('notify.channelCaptcha'),
    t('notify.channelFinished'),
    t('notify.channelFailed'),
    t('notify.channelWatch'),
  );
}

/**
 * One look at every saved instance, posting whatever is news. Answers whether
 * the service should keep running. Calls that overlap share one pass, since the
 * service and the app in front can ask at the same moment.
 */
export function watchPass(): Promise<boolean> {
  passing ??= runPass().finally(() => {
    passing = null;
  });
  return passing;
}

async function runPass(): Promise<boolean> {
  if (!KnightWatch) return false;
  const prefs = await loadNotifyPrefs();
  if (!anyKind(prefs) || !KnightWatch.enabled()) return false;
  const t = await translator();
  nameChannels(t);

  const conns = await listConnections();
  for (const id of watched.keys()) {
    if (!conns.some((c) => c.id === id)) watched.delete(id);
  }
  const front = AppState.currentState === 'active';
  let changed = false;
  let waiting = false;

  await Promise.all(
    conns.map(async (conn) => {
      let w = watched.get(conn.id);
      if (!w) {
        w = { look: null, lastBusy: 0, ok: true, captchaShown: false };
        watched.set(conn.id, w);
      }
      let tasks: Task[];
      let captchas: CaptchaChallenge[];
      try {
        [tasks, captchas] = await Promise.all([
          fetchTasks(conn),
          // An instance too old for the captcha routes refuses them; its
          // downloads are still worth watching.
          fetchCaptchasUnwatched(conn).catch((e) => {
            if (e instanceof ApiError) return [];
            throw e;
          }),
        ]);
      } catch {
        w.ok = false;
        return;
      }
      const news = compare(w.look, tasks, captchas);
      w.look = lookOf(tasks, captchas);
      w.ok = true;
      if (isBusy(tasks, captchas)) w.lastBusy = Date.now();
      if (news.changed) changed = true;
      if (captchas.length > 0) waiting = true;
      announce(conn, w, news, captchas, prefs, t, front && conn.id === onScreen);
    }),
  );

  delay = nextDelay(delay, changed, waiting);
  return keepRunning([...watched.values()], startedAt, Date.now());
}

// The instance open on screen while the app is in front says everything there,
// and its captcha banner is the heads-up, so it gets no notifications.
function announce(
  conn: ServerConnection,
  w: Watched,
  news: News,
  captchas: CaptchaChallenge[],
  prefs: NotifyPrefs,
  t: Translate,
  onScreenNow: boolean,
): void {
  if (!KnightWatch) return;
  const captchaId = noticeId('captcha', conn.id);
  if (captchas.length === 0 || onScreenNow) {
    if (w.captchaShown) KnightWatch.cancel(captchaId);
    w.captchaShown = false;
  }
  if (onScreenNow) return;

  if (prefs.captcha && news.arrived.length > 0) {
    KnightWatch.notify({
      id: captchaId,
      channel: 'captcha',
      title: t('notify.captchaTitle'),
      text:
        captchas.length === 1
          ? t('captcha.waiting', { host: captchas[0].host || '?' })
          : t('notify.captchaMany', { n: captchas.length }),
      sub: conn.name,
      open: 'captcha',
      connection: conn.id,
    });
    w.captchaShown = true;
  }

  if (prefs.finished) {
    for (const p of news.finished) {
      const one = p.total === 1;
      KnightWatch.notify({
        id: noticeId('finished', conn.id, p.key),
        channel: 'finished',
        title: one ? t('notify.finishedTitle') : t('notify.packageFinishedTitle'),
        text: one
          ? p.tasks[0].name || p.tasks[0].url
          : p.failed > 0
            ? t('notify.packagePartly', { name: p.name, done: p.done, total: p.total, failed: p.failed })
            : t('notify.packageDone', { name: p.name, n: p.total }),
        sub: conn.name,
        open: 'downloads',
        connection: conn.id,
      });
    }
  }

  if (prefs.failed) {
    for (const p of news.failed) {
      const task = p.tasks[0];
      const name = task.name || task.url;
      const one = p.tasks.length === 1;
      const why = one
        ? explainFailure(t, task, { part: name, file: name, path: task.dir || name, service: task.resolver })?.line
        : undefined;
      KnightWatch.notify({
        id: noticeId('failed', conn.id, p.key),
        channel: 'failed',
        title: t('notify.failedTitle'),
        text: one ? name : t('notify.failedMany', { n: p.tasks.length, name: p.name }),
        detail: why ? `${name}\n${why}` : undefined,
        sub: conn.name,
        open: 'downloads',
        connection: conn.id,
      });
    }
  }
}

/** The headless task: one pass, then the next tick or the end of the service. */
export async function watchTask(): Promise<void> {
  if (!KnightWatch) return;
  // A pass that fails outright stops the service rather than keeping the phone
  // awake for a watch that cannot see anything.
  const keep = await watchPass().catch(() => false);
  if (keep) KnightWatch.next(delay);
  else KnightWatch.stop();
}

/**
 * Whether the app may post notifications, asking Android once on its own. The
 * question comes up the first time the service is about to start, which is
 * when somebody can see what it is for.
 */
async function mayNotify(prefs: NotifyPrefs): Promise<boolean> {
  if (!KnightWatch || Platform.OS !== 'android') return false;
  if (Platform.Version >= 33 && !(await PermissionsAndroid.check(PermissionsAndroid.PERMISSIONS.POST_NOTIFICATIONS))) {
    if (prefs.asked) return false;
    await saveNotifyPrefs({ ...prefs, asked: true });
    const answer = await PermissionsAndroid.request(PermissionsAndroid.PERMISSIONS.POST_NOTIFICATIONS);
    if (answer !== PermissionsAndroid.RESULTS.GRANTED) return false;
  }
  return KnightWatch.enabled();
}

/** askNotifications puts Android's question up again, for the settings card. */
export async function askNotifications(): Promise<boolean> {
  if (!KnightWatch || Platform.OS !== 'android') return false;
  if (Platform.Version >= 33) {
    const answer = await PermissionsAndroid.request(PermissionsAndroid.PERMISSIONS.POST_NOTIFICATIONS);
    if (answer !== PermissionsAndroid.RESULTS.GRANTED) return false;
  }
  return KnightWatch.enabled();
}

/** Whether Android shows this app's notifications, false where there are none. */
export async function notificationsAllowed(): Promise<boolean> {
  if (!KnightWatch || Platform.OS !== 'android') return false;
  if (Platform.Version >= 33 && !(await PermissionsAndroid.check(PermissionsAndroid.PERMISSIONS.POST_NOTIFICATIONS))) {
    return false;
  }
  return KnightWatch.enabled();
}

/**
 * Starts the service. Only the app in front can, since Android refuses a
 * foreground service to an app in the background; that is why opening the app
 * and adding a download are what start it.
 */
export async function startWatch(): Promise<void> {
  if (!KnightWatch || KnightWatch.running() || AppState.currentState !== 'active') return;
  const prefs = await loadNotifyPrefs();
  if (!anyKind(prefs) || !(await mayNotify(prefs))) return;
  const t = await translator();
  nameChannels(t);
  startedAt = Date.now();
  delay = FAST_MS;
  KnightWatch.next(delay);
  KnightWatch.start(t('notify.watchTitle'), t('notify.watchText'));
}

/** Stops the service, for the settings card when every kind is switched off. */
export function stopWatch(): void {
  if (KnightWatch?.running()) KnightWatch.stop();
}

/**
 * Keeps the watch going from the app in front: it tells the watch which
 * instance is on screen, and while the service is not running it looks every
 * CHECK_MS whether something is on the go and starts it if so.
 */
export function useWatch(connectionOnScreen: string | null): void {
  useEffect(() => {
    onScreen = connectionOnScreen;
  }, [connectionOnScreen]);

  useEffect(() => {
    if (!KnightWatch) return;
    let checking = false;
    const check = async () => {
      if (checking || AppState.currentState !== 'active' || KnightWatch?.running()) return;
      checking = true;
      try {
        if (await watchPass().catch(() => false)) await startWatch();
      } finally {
        checking = false;
      }
    };
    void check();
    const timer = setInterval(check, CHECK_MS);
    const sub = AppState.addEventListener('change', (s) => {
      if (s === 'active') void check();
    });
    return () => {
      clearInterval(timer);
      sub.remove();
    };
  }, []);
}

/**
 * Hands every tapped notification to `onOpen`: the one that started the app,
 * and any that arrive while it runs.
 */
export function useOpenRequests(onOpen: (request: OpenRequest) => void): void {
  const handler = useRef(onOpen);
  handler.current = onOpen;
  useEffect(() => {
    if (!KnightWatch) return;
    const take = () => {
      const request = KnightWatch?.takeOpen();
      if (request) handler.current(request);
    };
    take();
    const sub = KnightWatch.addListener('onOpen', take);
    const app = AppState.addEventListener('change', (s) => {
      if (s === 'active') take();
    });
    return () => {
      sub.remove();
      app.remove();
    };
  }, []);
}
