import AsyncStorage from '@react-native-async-storage/async-storage';

// Which notifications this phone wants, and whether it has asked Android for
// the permission once already. Preferences rather than secrets, so
// AsyncStorage.
const KEY = 'knightloader-notifications';

export interface NotifyPrefs {
  captcha: boolean;
  finished: boolean;
  failed: boolean;
  /** Keep the connection open all the time rather than only while something
   *  runs, so a download started elsewhere is noticed too. */
  stay: boolean;
  /** Set once the app has put Android's permission question up by itself. A
   *  second time would be a prompt somebody already answered. */
  asked: boolean;
  /** Set once the app has offered the way to battery optimisation. */
  batteryAsked: boolean;
}

const DEFAULTS: NotifyPrefs = {
  captcha: true,
  finished: true,
  failed: true,
  stay: true,
  asked: false,
  batteryAsked: false,
};

export async function loadNotifyPrefs(): Promise<NotifyPrefs> {
  try {
    const raw = await AsyncStorage.getItem(KEY);
    return raw ? { ...DEFAULTS, ...(JSON.parse(raw) as Partial<NotifyPrefs>) } : DEFAULTS;
  } catch {
    return DEFAULTS;
  }
}

let saving: Promise<unknown> = Promise.resolve();

/**
 * saveNotifyPrefs stores `change` over what is stored, one save at a time, so
 * a writer holding an older copy cannot put back a field another one set.
 */
export function saveNotifyPrefs(change: Partial<NotifyPrefs>): Promise<void> {
  const saved = saving.then(async () => {
    await AsyncStorage.setItem(KEY, JSON.stringify({ ...(await loadNotifyPrefs()), ...change }));
  });
  saving = saved.catch(() => {});
  return saved;
}

export const anyKind = (p: NotifyPrefs): boolean => p.captcha || p.finished || p.failed;
