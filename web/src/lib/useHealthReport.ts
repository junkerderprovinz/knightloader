import { useEffect, useRef, useState } from 'react';
import { fetchHealthReport, type HealthReport, type HealthState } from './api';
import { type TranslationKey, useT } from './i18n';
import { en } from './locales/en';

/**
 * The detailed health readout's one poll, and the rules every card reads it
 * through.
 *
 * Modelled line for line on useDiskSpace, which settled the same questions for
 * the same kind of readout - and the two are deliberately alike so that a
 * reader who has understood one has understood the other.
 *
 * NOTHING HERE TAKES A `base`. GET /api/health/detail is on neither the
 * federation forwarder's list nor the relay allowlist, so a peer answers 403 -
 * and that refusal is the point rather than an oversight. This describes THIS
 * machine's sidecar, queue and disks, and a row of them drawn under a peer's
 * name would be a confident answer about a different box.
 */

// Ten seconds, matching useDiskSpace's own poll and for the same two reasons:
// it is just outside the server's own cache, so a tab is rarely handed the same
// sample twice, and it is far enough apart that a probe against a sidecar that
// has stopped answering is not asked again while the last one still hangs.
//
// Deliberately a poll and NOT a hub subscription. The hub broadcasts on task
// and queue events, and almost everything on this page moves while nothing at
// all is happening: a disk fills, a sidecar dies, a relay drops. A subscription
// would leave the page frozen at exactly the moment it is worth looking at.
const POLL_MS = 10000;

type Translate = ReturnType<typeof useT>['t'];

export interface HealthFeed {
  /** The last report that arrived, or null before the first one does. */
  report: HealthReport | null;
  /**
   * Whether every attempt so far has failed. It is NOT "the last poll failed":
   * once a report has landed it stays on screen and this stays false, because a
   * readout that empties itself on one dropped request is a readout people stop
   * looking at (QueueBar settled that, and useDiskSpace repeats it).
   */
  failed: boolean;
}

/**
 * useHealthReport keeps the latest reading for as long as the caller wants one.
 *
 * A failed poll keeps the last good report instead of blanking it, and there is
 * no toast: nobody asked for this number, so failing to refresh it is not an
 * event. The one thing worth saying out loud is that NOTHING has ever arrived,
 * which is a page that cannot show anything at all - hence `failed` rather than
 * a silent empty page.
 */
export function useHealthReport(): HealthFeed {
  const [report, setReport] = useState<HealthReport | null>(null);
  const [failed, setFailed] = useState(false);
  // A ref and not the `report` state, because the effect below runs once and
  // captures whatever `report` was at that moment - which is null forever. Read
  // out of the closure it would make every dropped poll look like a page that
  // has never loaded, blanking a readout that is standing there working, which
  // is the exact behaviour the doc comment above promises not to have.
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
          // The last reading stands. `failed` is only ever set while there is
          // nothing to stand: see HealthFeed.failed.
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
 * A server id turned into a word, or left as the id.
 *
 * Every lookup in this feature goes through here, for the reason roleLabel
 * (useDiskSpace.ts) carries in its own doc comment: the server sends stable ids
 * because it cannot know which of the 42 locales this browser is showing, and a
 * state or a part a newer server has learnt before this build has a word for it
 * must render as its raw id rather than as a blank cell. A blank reads as a
 * broken row; an id is at least something a person can search for.
 *
 * Tested against `en`, the one dictionary that is always loaded synchronously -
 * asking the fetched one would turn every label into a raw id for as long as
 * its chunk is in flight.
 *
 * And nothing here `as`-casts the server's string into the union: that would
 * freeze an assumption a newer server breaks silently.
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
 * The tone a state is drawn in.
 *
 * Three tones for five states, and the collapse is the point: "not set up here"
 * and "cannot be checked here" are not faults, and drawing them in the same
 * colour as a real failure trains somebody to ignore the colour. They read as
 * neutral, exactly as the summary treats them.
 *
 * A state this build has never heard of is neutral too. It is the only honest
 * choice - a colour is a claim about severity, and this build has none to make
 * about a word it does not know.
 */
export function healthTone(state: HealthState | string): 'ok' | 'fail' | undefined {
  if (state === 'ok') return 'ok';
  if (state === 'failed' || state === 'degraded') return 'fail';
  return undefined;
}

/**
 * How old the reading is, in seconds, or null when there is nothing to say.
 *
 * It carries the age rather than animating the figures, because the reading is
 * stale by design: the probing rows are shared for thirty seconds. Between a
 * sidecar coming back and the next probe this can show it as down while the
 * downloads behind it are already moving, and a gauge that ticked like a live
 * one would be claiming that contradiction is impossible.
 */
export function sampleAge(report: HealthReport | null): number | null {
  if (!report) return null;
  const taken = Date.parse(report.sampledAt);
  if (Number.isNaN(taken)) return null;
  return Math.max(0, Math.round((Date.now() - taken) / 1000));
}
