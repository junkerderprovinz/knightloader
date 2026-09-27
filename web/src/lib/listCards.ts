// Which card of the Downloads page a package is drawn in, and whether the two
// cards beside the download list are switched on. The rule has a twin in the
// app (packageCard in mobile/src/api/taskState.ts), and the two must agree.
//
// The switches are a module-level store like navLabels.ts: Layout seeds it from
// the settings, the settings page writes it the moment a toggle moves, and a
// copy in localStorage keeps the first paint after a reload from drawing the
// cards somebody switched off.
import { useSyncExternalStore } from 'react';
import type { Task } from './api';

export type ListCard = 'downloads' | 'seeding' | 'finished';

export interface CardSwitches {
  seeding: boolean;
  finished: boolean;
}

const unpackFailed = (t: Task): boolean => t.unpack === 'error' || t.unpack === 'password';

/**
 * packageCard is the card a package belongs in, judged over all of its links.
 *
 * It is finished once every link it holds has downloaded and nothing in it is
 * left to unpack: no archive waiting for its turn or being unpacked (both are
 * status extracting) and none that failed to unpack, since that archive is the
 * thing left to act on. A failed link keeps the package in the download list for
 * the same reason. It is seeding when it would be finished but for a torrent
 * that is still uploading.
 *
 * A switched-off link that has not downloaded does not hold the package back,
 * since switching a link off is how somebody says they do not want it. One that
 * is still downloading or unpacking does, and at least one link has to have
 * downloaded, so a package switched off whole stays where it is.
 */
export function packageCard(items: readonly Task[]): ListCard {
  let done = 0;
  let seeding = false;
  for (const x of items) {
    if (x.status === 'done' && !unpackFailed(x)) {
      done++;
      if (x.seeding) seeding = true;
      continue;
    }
    if (x.enabled === false && x.status !== 'running' && x.status !== 'extracting') continue;
    return 'downloads';
  }
  if (done === 0) return 'downloads';
  return seeding ? 'seeding' : 'finished';
}

function placed(card: ListCard, on: CardSwitches): ListCard {
  if (card === 'seeding' && !on.seeding) return 'downloads';
  if (card === 'finished' && !on.finished) return 'downloads';
  return card;
}

/**
 * splitByCard sorts the grouped download list into the cards that are switched
 * on, keeping the order of the groups. The loose links share the unnamed group
 * without belonging together, so each of them is placed on its own and a
 * finished file among them does not wait for an unrelated one. A card that is
 * off leaves its packages in the download list.
 *
 * Pass the whole list, not a filtered one: a package's card follows all of its
 * links, so a filter cannot move it.
 */
export function splitByCard(groups: [string, Task[]][], on: CardSwitches): Record<ListCard, [string, Task[]][]> {
  const out: Record<ListCard, [string, Task[]][]> = { downloads: [], seeding: [], finished: [] };
  for (const [name, items] of groups) {
    if (name !== '') {
      out[placed(packageCard(items), on)].push([name, items]);
      continue;
    }
    const loose: Record<ListCard, Task[]> = { downloads: [], seeding: [], finished: [] };
    for (const x of items) loose[placed(packageCard([x]), on)].push(x);
    for (const card of ['downloads', 'seeding', 'finished'] as const) {
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
    return { seeding: v.seeding !== false, finished: v.finished !== false };
  } catch {
    return { seeding: true, finished: true };
  }
}

let switches: CardSwitches = readCache();
const listeners = new Set<() => void>();

/** setListCards stores the switches and wakes the readers: at boot with the
 *  saved values, and when a toggle on the settings page moves. */
export function setListCards(next: Partial<CardSwitches>): void {
  const merged = { ...switches, ...next };
  if (merged.seeding === switches.seeding && merged.finished === switches.finished) return;
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
