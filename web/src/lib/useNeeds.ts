// What needsOf reads beside the task list, kept once for every reader. The
// rail's count is mounted on every page and the overview joins it, so one
// socket and one set of polls serve both.

import { useCallback, useEffect, useMemo, useSyncExternalStore } from 'react';
import {
  connectWS,
  fetchAccountCatalogue,
  fetchAccounts,
  fetchCaptchas,
  fetchHealthReport,
  fetchHosterLogins,
  fetchSettings,
  fetchVolumeUsage,
  type Account,
  type CaptchaChallenge,
  type CaptchaResolution,
  type CatalogueService,
  type HealthReport,
  type HosterLogin,
  type Task,
  type VolumeUsage,
} from './api';
import { needsOf, type Need, type NeedSources } from './needs';
import { peekUIState, useUIState } from './uistate';

// The server shares one health reading for half a minute, so this only keeps
// the page from lagging a second interval behind it.
const HEALTH_POLL_MS = 10_000;
// The accounts page's own interval: expiry and a refused key change in hours.
const ACCOUNT_POLL_MS = 30_000;
const FRESH_MS = 5_000;

interface Ambient {
  captchas: CaptchaChallenge[];
  /** Null until the first answer, which is when "Done" may forget an account. */
  accounts: Account[] | null;
  services: CatalogueService[];
  logins: HosterLogin[] | null;
  health: HealthReport | null;
  volume: VolumeUsage | null;
  floors: { low: number; stop: number } | null;
}

let ambient: Ambient = {
  captchas: [],
  accounts: null,
  services: [],
  logins: null,
  health: null,
  volume: null,
  floors: null,
};
const listeners = new Set<() => void>();
let stop: (() => void) | undefined;
let refresh: (() => void) | undefined;
let readAt = 0;

function put(patch: Partial<Ambient>): void {
  ambient = { ...ambient, ...patch };
  for (const l of listeners) l();
}

function start(): () => void {
  let live = true;
  // A failed read keeps the last reading and raises nothing: the pages these
  // figures come from report their own failures.
  const take = <T>(load: Promise<T>, apply: (value: T) => void) =>
    void load.then(
      (value) => {
        if (live) apply(value);
      },
      () => {},
    );
  const readCaptchas = () => take(fetchCaptchas(), (captchas) => put({ captchas }));
  const readHealth = () => take(fetchHealthReport(), (health) => put({ health }));
  const readFloors = () =>
    take(fetchSettings(), (s) => put({ floors: { low: s.diskLowSpace, stop: s.diskCriticalSpace } }));
  const readAccounts = () => {
    take(fetchAccounts(), (accounts) => put({ accounts }));
    take(fetchHosterLogins(), (logins) => put({ logins }));
  };
  refresh = () => {
    readAt = Date.now();
    readHealth();
    readAccounts();
  };

  readCaptchas();
  readFloors();
  refresh();
  take(fetchAccountCatalogue(), (services) => put({ services }));
  take(fetchVolumeUsage(), (volume) => put({ volume }));
  const healthTimer = setInterval(readHealth, HEALTH_POLL_MS);
  const accountTimer = setInterval(readAccounts, ACCOUNT_POLL_MS);

  const close = connectWS(
    (type, data) => {
      // The snapshot comes with every reconnect, and a challenge may have
      // been settled while the socket was down.
      if (type === 'snapshot') readCaptchas();
      else if (type === 'captcha') {
        const c = data as CaptchaChallenge;
        put({ captchas: [...ambient.captchas.filter((x) => x.id !== c.id), c] });
      } else if (type === 'captchaResolved') {
        const { id } = data as CaptchaResolution;
        put({ captchas: ambient.captchas.filter((x) => x.id !== id) });
      } else if (type === 'volume') put({ volume: data as VolumeUsage });
      else if (type === 'settings') readFloors();
    },
    ['captcha', 'captchaResolved', 'volume', 'settings'],
  );

  return () => {
    live = false;
    refresh = undefined;
    clearInterval(healthTimer);
    clearInterval(accountTimer);
    close();
  };
}

function subscribe(onChange: () => void): () => void {
  listeners.add(onChange);
  if (!stop) stop = start();
  return () => {
    listeners.delete(onChange);
    if (listeners.size === 0) {
      stop?.();
      stop = undefined;
    }
  };
}

/**
 * refreshNeeds reads the polled sources now, for a page somebody just opened.
 * A reading a few seconds old is fresh enough, which also keeps the page and
 * the rail from asking twice when both mount.
 */
export function refreshNeeds(): void {
  if (Date.now() - readAt > FRESH_MS) refresh?.();
}

const DONE_FIELD = 'overview.needsDone';
const NONE: string[] = [];
const NOTHING_DONE: ReadonlySet<string> = new Set();

/** Whether the source a "Done" key belongs to has answered, so its absence means it is over. */
function answered(key: string, tasks: readonly Task[], a: Ambient): boolean {
  switch (key.slice(0, key.indexOf(':'))) {
    case 'task':
    case 'unpack':
      return tasks.length > 0;
    case 'account':
    case 'login':
    case 'expiry':
      return a.accounts !== null && a.logins !== null;
    case 'disk':
      return a.health !== null && a.floors !== null;
    case 'health':
      return a.health !== null;
    case 'volumeCap':
      return a.volume !== null;
  }
  return true;
}

export interface NeedsFeed {
  needs: Need[];
  /** What was marked as done, for the ring. */
  done: ReadonlySet<string>;
  /** markDone sets a need aside until the thing behind it changes. */
  markDone: (need: Need) => void;
  /** restore brings back what markDone set aside. */
  restore: (keys: readonly string[]) => void;
}

/**
 * useNeeds is the list of what waits for the person on this instance. Every
 * reader gets the same list for the same tasks.
 */
export function useNeeds(tasks: Record<string, Task>): NeedsFeed {
  const a = useSyncExternalStore(
    subscribe,
    () => ambient,
    () => ambient,
  );
  const [stored, setStored] = useUIState<string[]>(DONE_FIELD, NONE);
  const list = useMemo(() => Object.values(tasks), [tasks]);

  const [needs, done, lapsed] = useMemo(() => {
    const src: NeedSources = {
      tasks: list,
      captchas: a.captchas,
      accounts: a.accounts ?? [],
      services: a.services,
      logins: a.logins ?? [],
      health: a.health,
      volume: a.volume,
      diskLow: a.floors?.low ?? 0,
      diskStop: a.floors?.stop ?? 0,
      done: NOTHING_DONE,
      now: Date.now(),
    };
    const done: ReadonlySet<string> = new Set(Array.isArray(stored) ? stored : NONE);
    if (done.size === 0) return [needsOf(src), done, false] as const;
    // A key nothing answers to any more is dropped, so the same download
    // failing again after a retry shows again.
    const standing = new Set(needsOf(src).flatMap((n) => n.covers));
    const kept = new Set([...done].filter((k) => standing.has(k) || !answered(k, list, a)));
    return [needsOf({ ...src, done: kept }), kept, kept.size !== done.size] as const;
  }, [list, a, stored]);

  useEffect(() => {
    if (lapsed) setStored([...done]);
  }, [lapsed, done, setStored]);

  const markDone = useCallback(
    (need: Need) => setStored([...new Set([...peekUIState<string[]>(DONE_FIELD, NONE), ...need.covers])]),
    [setStored],
  );
  const restore = useCallback(
    (keys: readonly string[]) => setStored(peekUIState<string[]>(DONE_FIELD, NONE).filter((k) => !keys.includes(k))),
    [setStored],
  );

  return { needs, done, markDone, restore };
}
