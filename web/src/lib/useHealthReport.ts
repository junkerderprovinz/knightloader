import { useEffect, useRef, useState } from 'react';
import { fetchHealthReport, type HealthReport, type HealthState } from './api';
import { type TranslationKey, useT } from './i18n';
import { en } from './locales/en';

// The detailed health readout's poll and the helpers its cards use, built like
// useDiskSpace. It takes no base: the route describes this machine, and a
// peer answers 403.

// Just outside the server's cache, and long enough that a probe of a hung
// sidecar is not repeated while the last one still waits. A poll rather than a
// hub subscription, because disks, sidecars and relays change without any task
// event.
const POLL_MS = 10000;

type Translate = ReturnType<typeof useT>['t'];

export interface HealthFeed {
  /** The last report that arrived, or null before the first one does. */
  report: HealthReport | null;
  /** Whether every attempt so far has failed. Once a report has arrived it
   *  stays on screen and this stays false. */
  failed: boolean;
}

/**
 * useHealthReport keeps the latest reading. A failed poll keeps the last good
 * report and raises no toast; only a page that has never received one reports
 * `failed`.
 */
export function useHealthReport(): HealthFeed {
  const [report, setReport] = useState<HealthReport | null>(null);
  const [failed, setFailed] = useState(false);
  // A ref, because the effect runs once and its closure would see `report` as
  // null forever.
  const everArrived = useRef(false);

  useEffect(() => {
    let alive = true;
    const load = () =>
      void fetchHealthReport()
        .then((r) => {
          if (!alive) return;
          everArrived.current = true;
          setReport(r);
          setFailed(false);
        })
        .catch(() => {
          if (alive && !everArrived.current) setFailed(true);
        });
    load();
    const iv = setInterval(load, POLL_MS);
    return () => {
      alive = false;
      clearInterval(iv);
    };
  }, []);

  return { report, failed };
}

/**
 * healthLabel translates a server id, or returns the id itself when this build
 * has no word for it, as roleLabel in useDiskSpace.ts does. It checks `en`,
 * the dictionary that is always loaded, so labels do not flicker to ids while
 * another locale loads.
 */
export function healthLabel(t: Translate, prefix: 'health.state.' | 'health.part.' | 'health.remedy.', id: string): string {
  const key = `${prefix}${id}` as TranslationKey;
  return key in en ? t(key) : id;
}

/** A task-list breakdown id (core.Waiting / core.Reason), same rule. */
export function breakdownLabel(t: Translate, prefix: 'task.waiting.' | 'task.reason.', id: string): string {
  const key = `${prefix}${id}` as TranslationKey;
  return key in en ? t(key) : id;
}

/**
 * healthTone is the tone a state is drawn in. "unused", "unknown" and any
 * state this build does not know are neutral, so the fault colour keeps its
 * meaning.
 */
export function healthTone(state: HealthState | string): 'ok' | 'fail' | undefined {
  if (state === 'ok') return 'ok';
  if (state === 'failed' || state === 'degraded') return 'fail';
  return undefined;
}

/**
 * sampleAge is how old the reading is in seconds, or null. The probed rows
 * are shared for thirty seconds, so the page states the age rather than
 * presenting a live gauge.
 */
export function sampleAge(report: HealthReport | null): number | null {
  if (!report) return null;
  const taken = Date.parse(report.sampledAt);
  if (Number.isNaN(taken)) return null;
  return Math.max(0, Math.round((Date.now() - taken) / 1000));
}
