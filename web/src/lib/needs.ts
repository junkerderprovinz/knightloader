// What waits for the person, and how the queue stands. The overview's head,
// its "Needs you" card and the count on the rail's Overview entry all read
// needsOf, so the three cannot disagree.

import { retryStateOf } from '../components/RetryCountdown';
import type {
  Account,
  CaptchaChallenge,
  CatalogueService,
  DiskVolume,
  HealthReport,
  HealthSubsystem,
  HosterLogin,
  Task,
  VolumeUsage,
} from './api';
import { goTimeMs } from './countdown';

/** How long before its end an account is worth a reminder. */
const EXPIRY_NOTICE_MS = 7 * 24 * 60 * 60 * 1000;

interface NeedBase {
  /** Stable while the thing stays as it is, so a row keeps its place. */
  key: string;
  /** What "Done" remembers; empty for a need nobody can set aside. */
  covers: string[];
  tone: 'fail' | 'warn';
}

export type Need = NeedBase &
  (
    | { kind: 'captcha'; challenge: CaptchaChallenge }
    /** Downloads of one package that failed and will not be tried again by themselves. */
    | { kind: 'failed'; tasks: Task[] }
    /** Finished downloads of one package whose archive did not unpack. */
    | { kind: 'unpack'; tasks: Task[]; password: boolean }
    | { kind: 'account'; name: string; detail: string }
    | { kind: 'login'; login: HosterLogin }
    | { kind: 'expiry'; name: string; expiry: string; renewUrl?: string }
    | { kind: 'disk'; volume: DiskVolume; stopped: boolean }
    | { kind: 'volumeCap'; usage: VolumeUsage }
    | { kind: 'health'; part: HealthSubsystem }
  );

export interface NeedSources {
  tasks: readonly Task[];
  captchas: readonly CaptchaChallenge[];
  accounts: readonly Account[];
  services: readonly CatalogueService[];
  logins: readonly HosterLogin[];
  health: HealthReport | null;
  volume: VolumeUsage | null;
  /** settings.diskLowSpace and settings.diskCriticalSpace, 0 for unset. */
  diskLow: number;
  diskStop: number;
  /** What the person marked as done, by the keys in Need.covers. */
  done: ReadonlySet<string>;
  now: number;
}

const taskDone = (t: Task) => `task:${t.id}`;
const unpackDone = (t: Task) => `unpack:${t.id}`;

/** failedForGood is a failed download no scheduled retry is coming for. */
function failedForGood(t: Task, now: number): boolean {
  if (t.status !== 'error' || t.enabled === false) return false;
  const retry = retryStateOf(t, now)?.kind;
  return retry !== 'waiting' && retry !== 'due';
}

const unpackFailed = (t: Task): boolean =>
  t.status === 'done' && t.enabled !== false && (t.unpack === 'error' || t.unpack === 'password');

/** byPackage groups tasks as the lists do; a loose link is a group of its own. */
function byPackage(tasks: Task[]): Task[][] {
  const groups = new Map<string, Task[]>();
  for (const t of tasks) {
    const key = t.package ? `p:${t.package}` : `t:${t.id}`;
    const group = groups.get(key);
    if (group) group.push(t);
    else groups.set(key, [t]);
  }
  return [...groups.values()];
}

/** accountName is the service's name, with the account's own label where it has one. */
function accountName(a: Account, services: readonly CatalogueService[]): string {
  const svc = services.find((s) => s.id === a.service);
  if (svc?.group === 'remoteServer') return a.account;
  const name = svc?.label ?? a.service;
  return a.label ? `${name} (${a.label})` : name;
}

function endsSoon(expiry: string, now: number): boolean {
  const at = goTimeMs(expiry);
  return at !== null && at > now && at - now <= EXPIRY_NOTICE_MS;
}

/**
 * needsOf lists everything that waits for the person: a captcha first, since
 * it runs out, then what has failed, then what only warns.
 */
export function needsOf(src: NeedSources): Need[] {
  const fails: Need[] = [];
  const warns: Need[] = [];
  const open = (keys: string[]) => keys.some((k) => !src.done.has(k));

  const captchas: Need[] = src.captchas
    // Nothing waits on a test captcha the instance drew itself.
    .filter((c) => !c.test)
    .map((c) => ({ kind: 'captcha', key: `captcha:${c.id}`, covers: [], tone: 'warn', challenge: c }));

  for (const v of diskNeeds(src)) (v.tone === 'fail' ? fails : warns).push(v);

  const failed = src.tasks.filter((t) => failedForGood(t, src.now) && !src.done.has(taskDone(t)));
  for (const tasks of byPackage(failed)) {
    fails.push({ kind: 'failed', key: `failed:${tasks[0].id}`, covers: tasks.map(taskDone), tone: 'fail', tasks });
  }

  const unpacked = src.tasks.filter((t) => unpackFailed(t) && !src.done.has(unpackDone(t)));
  for (const tasks of byPackage(unpacked)) {
    fails.push({
      kind: 'unpack',
      key: `unpack:${tasks[0].id}`,
      covers: tasks.map(unpackDone),
      tone: 'fail',
      tasks,
      password: tasks.some((t) => t.unpack === 'password'),
    });
  }

  let refused = 0;
  for (const a of src.accounts) {
    if (!a.enabled || !a.configured) continue;
    const name = accountName(a, src.services);
    // An empty detail is an account nobody has checked yet, not a broken one.
    if (a.detail && !a.ok) {
      refused++;
      const key = `account:${a.id}`;
      if (open([key])) fails.push({ kind: 'account', key, covers: [key], tone: 'fail', name, detail: a.detail });
    } else if (a.expiry && endsSoon(a.expiry, src.now)) {
      const key = `expiry:${a.id}:${a.expiry}`;
      const svc = src.services.find((s) => s.id === a.service);
      const renewUrl = svc && svc.group !== 'captchaSolver' && svc.whereUrl ? svc.whereUrl : undefined;
      if (open([key])) warns.push({ kind: 'expiry', key, covers: [key], tone: 'warn', name, expiry: a.expiry, renewUrl });
    }
  }
  for (const l of src.logins) {
    if (!l.enabled) continue;
    if (l.status === 'rejected') {
      refused++;
      const key = `login:${l.host}`;
      if (open([key])) fails.push({ kind: 'login', key, covers: [key], tone: 'fail', login: l });
    } else if (l.expiry && endsSoon(l.expiry, src.now)) {
      const key = `expiry:login:${l.host}:${l.expiry}`;
      if (open([key])) warns.push({ kind: 'expiry', key, covers: [key], tone: 'warn', name: l.host, expiry: l.expiry });
    }
  }

  const cap = src.volume;
  if (cap && cap.cap > 0 && cap.reached) {
    // The period is in the key, so "Done" lasts until the counter starts over.
    const key = `volumeCap:${cap.periodStart}`;
    if (open([key])) warns.push({ kind: 'volumeCap', key, covers: [key], tone: 'warn', usage: cap });
  }

  for (const part of src.health?.subsystems ?? []) {
    if ((part.state !== 'failed' && part.state !== 'degraded') || !part.remedy) continue;
    // The captcha list and the folders above say the same with the names in
    // it, and a refused account has its own row.
    if (part.id === 'captcha' || part.remedy === 'disk.low') continue;
    if (part.remedy === 'accounts.invalid' && refused > 0) continue;
    const key = `health:${part.id}:${part.remedy}`;
    if (!open([key])) continue;
    const tone = part.state === 'failed' ? 'fail' : 'warn';
    (tone === 'fail' ? fails : warns).push({ kind: 'health', key, covers: [key], tone, part });
  }

  return [...captchas, ...fails, ...warns];
}

/**
 * diskNeeds reads the folders against the two floors with the rule the
 * server's own check uses (diskSubsystem in app_healthreport.go). A folder
 * listed under two roles is one folder.
 */
function diskNeeds(src: NeedSources): Need[] {
  const out: Need[] = [];
  const seen = new Set<string>();
  for (const v of src.health?.volumes ?? []) {
    if (!v.known || seen.has(v.dir)) continue;
    seen.add(v.dir);
    const stopped = src.diskStop > 0 && v.free < src.diskStop;
    if (!stopped && !(src.diskLow > 0 && v.free < src.diskLow)) continue;
    const key = `disk:${stopped ? 'stop' : 'low'}:${v.dir}`;
    if (src.done.has(key)) continue;
    out.push({ kind: 'disk', key, covers: [key], tone: stopped ? 'fail' : 'warn', volume: v, stopped });
  }
  return out;
}

/** A kind's state on the ring, worst first. Green only once everything of the kind is loaded. */
export type KindTone = 'fail' | 'warn' | 'run' | 'neutral' | 'ok';
const TONE_RANK: readonly KindTone[] = ['fail', 'warn', 'run', 'neutral', 'ok'];

/** The waits a person can end; a full slot or a host's own limit pass by themselves. */
const HELD = new Set(['captcha', 'account', 'disk', 'volumeCap', 'module', 'premium']);

export interface KindSegment {
  /** The backend's id, as resolverLabel names it. */
  id: string;
  tone: KindTone;
  /** For a warning, the wait that holds the kind back (core.Waiting), if one does. */
  held?: string;
}

/** A backend's id without the account it runs under. */
export const kindOf = (t: Task): string => t.resolver.split('#')[0];

/**
 * kindSegments is the ring: one segment per backend that has a download in the
 * list, coloured by the worst state among them. A failure marked as done
 * counts as settled.
 */
export function kindSegments(tasks: readonly Task[], done: ReadonlySet<string>, now: number): KindSegment[] {
  const kinds = new Map<string, KindSegment>();
  for (const t of tasks) {
    const id = kindOf(t);
    if (!id || t.status === 'collected' || t.enabled === false) continue;
    let tone: KindTone = 'neutral';
    let held: string | undefined;
    const failed = failedForGood(t, now);
    if (failed || unpackFailed(t)) {
      tone = done.has(failed ? taskDone(t) : unpackDone(t)) ? 'ok' : 'fail';
    } else if (t.status === 'error') {
      tone = 'warn';
    } else if (t.status === 'queued' && t.waiting && HELD.has(t.waiting)) {
      tone = 'warn';
      held = t.waiting;
    } else if (t.status === 'running' || t.status === 'extracting' || t.repair) {
      tone = 'run';
    } else if (t.status === 'done') {
      tone = 'ok';
    }
    const cur = kinds.get(id);
    if (!cur) kinds.set(id, { id, tone, held });
    else if (TONE_RANK.indexOf(tone) < TONE_RANK.indexOf(cur.tone)) {
      cur.tone = tone;
      cur.held = held;
    }
  }
  return [...kinds.values()].sort((a, b) => (a.id < b.id ? -1 : 1));
}

export interface QueueFigures {
  /** Downloads in the list at all, the collector's links left out. */
  listed: number;
  running: number;
  /** Queued, paused or waiting for a scheduled retry. */
  waiting: number;
  speed: number;
  /** Seconds until the open queue is through at this speed, or null. */
  eta: number | null;
  /** How far the packages still being loaded have come, 0 to 100, or null without a known size. */
  percent: number | null;
  /** The master switch holds a queued download back. */
  halted: boolean;
}

/** queueFigures counts the download list for the overview's head. */
export function queueFigures(tasks: readonly Task[], now: number): QueueFigures {
  const f: QueueFigures = { listed: 0, running: 0, waiting: 0, speed: 0, eta: null, percent: null, halted: false };
  const open = new Set<string>();
  let remaining = 0;
  const packageOf = (t: Task) => (t.package ? `p:${t.package}` : `t:${t.id}`);
  for (const t of tasks) {
    if (t.status === 'collected') continue;
    f.listed++;
    if (t.enabled === false) continue;
    const moving = t.status === 'running';
    if (moving) {
      f.running++;
      f.speed += t.speed;
    } else if (t.status === 'queued' || t.status === 'paused' || (t.status === 'error' && !failedForGood(t, now))) {
      f.waiting++;
    }
    if (moving || t.status === 'queued') {
      open.add(packageOf(t));
      if (t.size > t.loaded) remaining += t.size - t.loaded;
      if (t.waiting === 'halted') f.halted = true;
    }
  }
  if (f.running > 0 && f.speed > 0 && remaining > 0) f.eta = Math.round(remaining / f.speed);

  let total = 0;
  let loaded = 0;
  for (const t of tasks) {
    if (t.status === 'collected' || t.enabled === false || t.size <= 0 || !open.has(packageOf(t))) continue;
    total += t.size;
    loaded += Math.min(t.loaded, t.size);
  }
  if (total > 0) f.percent = Math.min(100, Math.floor((loaded / total) * 100));
  return f;
}

/** The head's fixed states. A wait is never rounded up to "everything is loaded". */
export type HeadState = 'needs' | 'running' | 'waiting' | 'empty' | 'done';

export function headState(needs: number, f: QueueFigures): HeadState {
  if (needs > 0) return 'needs';
  if (f.running > 0) return 'running';
  if (f.waiting > 0) return 'waiting';
  return f.listed === 0 ? 'empty' : 'done';
}
