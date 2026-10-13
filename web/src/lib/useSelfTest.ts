import { useCallback, useEffect, useState } from 'react';
import { type SelfTestResult, type SelfTestRun, type SelfTestStatus, fetchRequestView, fetchSelfTest, startSelfTest } from './api';
import { happened } from './countdown';
import { clockSkewMs, fmtSkew, skewStatus } from './selftest';

// The results are polled rather than pushed, because the WebSocket is one of
// the things under test. The browser finishes the clock row itself, timing the
// request echo so a slow round trip does not read as drift (clockSkewMs).
const POLL_MS = 1000;

/**
 * clockRow folds the browser's half into the server's clock result. It says
 * how far apart the clocks are, not which one is wrong, since neither is
 * authoritative.
 */
function clockRow(res: SelfTestResult, skewMs: number | null): SelfTestResult {
  if (skewMs === null) return res;
  const verdict = skewStatus(skewMs);
  if (verdict === 'pass') return res;
  return {
    ...res,
    // The worse status wins; the sentence reports the drift as the more urgent.
    status: verdict === 'fail' ? 'fail' : res.status === 'fail' ? 'fail' : 'warn',
    code: 'clock.skew',
    params: { ...(res.params ?? {}), skew: fmtSkew(skewMs) },
  };
}

export interface SelfTest {
  /** The last run, or null until the instance has answered. An instance that was never swept answers with an empty id. */
  run: SelfTestRun | null;
  /** The run on screen is still going. */
  running: boolean;
  /** A run is being started or is still going. */
  busy: boolean;
  /** Why the last start failed, as the server said it. */
  error: string;
  /** Whether the run on screen was started here, whose answer it then is. */
  mine: boolean;
  /** One check's result with the browser's half of the clock row folded in, or nothing while it is awaited. */
  resultOf: (id: string) => SelfTestResult | undefined;
  /** The statuses of every result so far. */
  statuses: SelfTestStatus[];
  start: () => Promise<boolean>;
}

/** useSelfTest reads the instance's last self-test, starts a new one and follows it while it runs. */
export function useSelfTest(): SelfTest {
  const [run, setRun] = useState<SelfTestRun | null>(null);
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState('');
  const [skewMs, setSkewMs] = useState<number | null>(null);
  const [mine, setMine] = useState(false);

  const refresh = useCallback(() => {
    void fetchSelfTest().then(setRun, () => undefined);
  }, []);

  useEffect(refresh, [refresh]);

  const running = run !== null && run.id !== '' && !happened(run.finishedAt);
  useEffect(() => {
    if (!running) return;
    const id = window.setInterval(refresh, POLL_MS);
    return () => window.clearInterval(id);
  }, [running, refresh]);

  async function start(): Promise<boolean> {
    setError('');
    setStarting(true);
    try {
      setRun(await startSelfTest());
      setMine(true);
      // Started once the sweep runs, so the two calls do not queue on a
      // single-connection proxy. A failure leaves the server's clock row as is.
      const sentAt = Date.now();
      try {
        const view = await fetchRequestView();
        setSkewMs(clockSkewMs(view, sentAt, Date.now()));
      } catch {
        setSkewMs(null);
      }
      return true;
    } catch (e) {
      setError(String(e).replace(/^Error:\s*/, ''));
      return false;
    } finally {
      setStarting(false);
    }
  }

  const results = (run?.results ?? []).map((r) => (r.id === 'clock' ? clockRow(r, skewMs) : r));
  return {
    run,
    running,
    busy: starting || running,
    error,
    mine,
    resultOf: (id) => results.find((r) => r.id === id),
    statuses: results.map((r) => r.status),
    start,
  };
}
