// How tall a row of the download and collector lists is. Stored per browser
// like the motion level, since a screen and its distance from the eyes decide
// it, and kept in a module-level store because the lists render outside the
// settings page that changes it.
import { useSyncExternalStore } from 'react';

export type RowHeight = 'compact' | 'medium' | 'comfortable';

export const ROW_HEIGHTS: RowHeight[] = ['compact', 'medium', 'comfortable'];

export const DEFAULT_ROW_HEIGHT: RowHeight = 'compact';

/**
 * RowMetrics is what one step sets: the height of every control in a row
 * (badges, switches, dropdowns), which becomes --btn-h inside the list table,
 * and the padding above and below it. Compact is JDownloader's density,
 * Comfortable the size of a button elsewhere in the app with room around it,
 * and Medium halfway between.
 */
export interface RowMetrics {
  control: number;
  pad: number;
}

export const ROW_METRICS: Record<RowHeight, RowMetrics> = {
  compact: { control: 24, pad: 2 },
  medium: { control: 28, pad: 5 },
  comfortable: { control: 32, pad: 8 },
};

/**
 * ROW_ESTIMATES is the height the list's window assumes for a row it has not
 * measured, per step: the control and its padding, and for a package header
 * the 1px rule above it. Built once, so a step always hands the window the
 * same object.
 */
export const ROW_ESTIMATES: Record<RowHeight, { package: number; task: number }> = Object.fromEntries(
  ROW_HEIGHTS.map((h) => {
    const row = ROW_METRICS[h].control + 2 * ROW_METRICS[h].pad;
    return [h, { package: row + 1, task: row }];
  }),
) as Record<RowHeight, { package: number; task: number }>;

const CACHE = 'kl-row-height';

function read(): RowHeight {
  try {
    const raw = localStorage.getItem(CACHE);
    return ROW_HEIGHTS.includes(raw as RowHeight) ? (raw as RowHeight) : DEFAULT_ROW_HEIGHT;
  } catch {
    return DEFAULT_ROW_HEIGHT;
  }
}

let current: RowHeight | undefined;
const listeners = new Set<() => void>();

function subscribe(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

function snapshot(): RowHeight {
  current ??= read();
  return current;
}

/** setRowHeight stores the step in this browser and redraws every list. */
export function setRowHeight(next: RowHeight): void {
  current = next;
  try {
    localStorage.setItem(CACHE, next);
  } catch {
    // Storage switched off: the step holds until the page is reloaded.
  }
  for (const fn of listeners) fn();
}

/** The step in force, re-rendering the caller when it changes. */
export function useRowHeight(): RowHeight {
  return useSyncExternalStore(subscribe, snapshot, snapshot);
}
