// The global download limit in force, which the speed graphs draw as a line.
//
// The queue state carries the limit the timetable and quiet mode put in force.
// The volume cap's throttle is folded in here, as app_volumecap.go folds it
// into the budget on the server, because it arrives on a push of its own. Both
// pushes come from this instance only, so a peer scope reads its queue state on
// a timer and has no volume cap folded in: the volume routes are not forwarded.
//
// One source per scope, shared by the Overview curve and the shell meter, so
// the two graphs open one socket between them rather than one each.

import { useCallback, useSyncExternalStore } from 'react';
import { apiBase, connectWS, fetchQueue, fetchVolumeUsage, type QueueState, type VolumeUsage } from './api';

// Often enough that a peer's limit window shows within a minute of opening.
const PEER_POLL_MS = 20_000;

/**
 * foldVolumeCap is the limit the meters are held to once the volume cap's
 * throttle applies: the smaller of the two, where 0 means none.
 */
export function foldVolumeCap(limit: number, volume: VolumeUsage | null): number {
  if (!volume || !volume.reached || volume.action !== 'throttle' || volume.throttle <= 0) return limit;
  return limit <= 0 || volume.throttle < limit ? volume.throttle : limit;
}

interface Source {
  queue: number;
  volume: VolumeUsage | null;
  listeners: Set<() => void>;
  stop: (() => void) | undefined;
}

const sources = new Map<string, Source>();

function sourceFor(instance: string): Source {
  let s = sources.get(instance);
  if (!s) {
    s = { queue: 0, volume: null, listeners: new Set(), stop: undefined };
    sources.set(instance, s);
  }
  return s;
}

function emit(s: Source): void {
  for (const l of s.listeners) l();
}

function start(instance: string, s: Source): () => void {
  let live = true;
  const takeQueue = (q: QueueState) => {
    if (!live) return;
    s.queue = q.limit ?? 0;
    emit(s);
  };
  const readQueue = () =>
    void fetchQueue(apiBase(instance)).then(takeQueue, () => {
      // The last known limit stays drawn.
    });
  readQueue();

  if (instance !== '') {
    const timer = setInterval(readQueue, PEER_POLL_MS);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }

  void fetchVolumeUsage().then(
    (u) => {
      if (!live) return;
      s.volume = u;
      emit(s);
    },
    () => {
      // Without a reading the cap is not folded in.
    },
  );
  const close = connectWS(
    (type, data) => {
      if (type === 'queue') takeQueue(data as QueueState);
      else if (type === 'volume' && live) {
        s.volume = data as VolumeUsage;
        emit(s);
      }
    },
    ['queue', 'volume'],
  );
  return () => {
    live = false;
    close();
  };
}

/**
 * useLimitInForce is the global download limit in force for an instance scope
 * ('' = this instance) in bytes/s, or 0 while none applies.
 */
export function useLimitInForce(instance: string): number {
  const subscribe = useCallback(
    (onChange: () => void) => {
      const s = sourceFor(instance);
      s.listeners.add(onChange);
      if (!s.stop) s.stop = start(instance, s);
      return () => {
        s.listeners.delete(onChange);
        if (s.listeners.size === 0) {
          s.stop?.();
          s.stop = undefined;
        }
      };
    },
    [instance],
  );
  return useSyncExternalStore(
    subscribe,
    () => {
      const s = sourceFor(instance);
      return foldVolumeCap(s.queue, s.volume);
    },
    () => 0,
  );
}
