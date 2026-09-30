// Which card of the Downloads page a package is drawn in, and whether the two
// cards beside the download list are switched on. The rule has a twin in the
// app (packageCard in mobile/src/api/taskState.ts), and the two must agree.
//
// The switches are a module-level store like labelModes.ts: Layout seeds it from
// the settings, the settings page writes it the moment a toggle moves, and a
// copy in localStorage keeps the first paint after a reload from drawing the
// cards somebody switched off.
import { useSyncExternalStore } from 'react';
import type { Task } from './api';

export type ListCard = 'downloads' | 'finished' | 'torrents';

export interface CardSwitches {
  finished: boolean;
  torrents: boolean;
}

const unpackFailed = (t: Task): boolean => t.unpack === 'error' || t.unpack === 'password';

// Same signal as isTorrentTask in components/columns.tsx. A torrent a debrid
// service fetched comes down over HTTP, never seeds, and is not one.
const isTorrent = (t: Task): boolean => t.resolver === 'torrent';

/** A finished torrent that seeds or is about to, which Stop seeding ends. */
export const seedingOn = (t: Task): boolean => t.status === 'done' && isTorrent(t) && !t.seedingOver;

/** A finished torrent that seeds no more, which Start seeding takes up again. */
export const seedingOff = (t: Task): boolean => t.status === 'done' && isTorrent(t) && !!t.seedingOver;

/**
 * packageCard is the card a package belongs in, judged over all of its links.
 *
 * It is finished once every link it holds has downloaded and nothing in it is
 * left to unpack: no archive waiting for its turn or being unpacked (both are
 * status extracting) and none that failed to unpack, since that archive is the
 * thing left to act on. A failed link keeps the package in the download list for
 * the same reason. A finished package holding a torrent goes to Torrents, where
 * it stays whether it still seeds or not, since a torrent is often kept there
 * for a long time.
 *
 * A switched-off link that has not downloaded does not hold the package back,
 * since switching a link off is how somebody says they do not want it. One that
 * is still downloading or unpacking does, and a package switched off whole is
 * parked rather than finished, so it stays where it can be switched on again.
 */
export function packageCard(items: readonly Task[]): ListCard {
  if (items.every((x) => x.enabled === false)) return 'downloads';
  let done = 0;
  let torrent = false;
  for (const x of items) {
    if (x.status === 'done' && !unpackFailed(x)) {
      done++;
      if (isTorrent(x)) torrent = true;
      continue;
    }
    if (x.enabled === false && x.status !== 'running' && x.status !== 'extracting') continue;
    return 'downloads';
  }
  if (done === 0) return 'downloads';
  return torrent ? 'torrents' : 'finished';
}

// With the Torrents card off a torrent is finished like any other download.
function placed(card: ListCard, on: CardSwitches): ListCard {
  if (card === 'torrents' && !on.torrents) card = 'finished';
  if (card === 'finished' && !on.finished) return 'downloads';
  return card;
}

/**
 * splitByCard sorts the grouped download list into the cards that are switched
 * on, keeping the order of the groups. The loose links share the unnamed group
 * without belonging together, so each of them is placed on its own and a
 * finished file among them does not wait for an unrelated one. A card that is
 * off hands its packages on: Torrents to Finished, Finished to the download
 * list.
 *
 * Pass the whole list, not a filtered one: a package's card follows all of its
 * links, so a filter cannot move it.
 */
export function splitByCard(groups: [string, Task[]][], on: CardSwitches): Record<ListCard, [string, Task[]][]> {
  const out: Record<ListCard, [string, Task[]][]> = { downloads: [], finished: [], torrents: [] };
  for (const [name, items] of groups) {
    if (name !== '') {
      out[placed(packageCard(items), on)].push([name, items]);
      continue;
    }
    const loose: Record<ListCard, Task[]> = { downloads: [], finished: [], torrents: [] };
    for (const x of items) loose[placed(packageCard([x]), on)].push(x);
    for (const card of ['downloads', 'finished', 'torrents'] as const) {
      if (loose[card].length > 0) out[card].push(['', loose[card]]);
    }
  }
  return out;
}

const CACHE = 'kl-list-cards';

function readCache(): CardSwitches {
  try {
    const raw = localStorage.getItem(CACHE);
    const v = raw ? (JSON.parse(raw) as Partial<CardSwitches>) : {};
    return { finished: v.finished !== false, torrents: v.torrents !== false };
  } catch {
    return { finished: true, torrents: true };
  }
}

let switches: CardSwitches = readCache();
const listeners = new Set<() => void>();

/** setListCards stores the switches and wakes the readers: at boot with the
 *  saved values, and when a toggle on the settings page moves. */
export function setListCards(next: Partial<CardSwitches>): void {
  const merged = { ...switches, ...next };
  if (merged.finished === switches.finished && merged.torrents === switches.torrents) return;
  switches = merged;
  try {
    localStorage.setItem(CACHE, JSON.stringify(switches));
  } catch {
    // Private windows refuse storage; the next load waits for the settings.
  }
  for (const fn of listeners) fn();
}

function subscribe(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** The switches in force, re-rendering the caller when they change. */
export function useListCards(): CardSwitches {
  const read = () => switches;
  return useSyncExternalStore(subscribe, read, read);
}
