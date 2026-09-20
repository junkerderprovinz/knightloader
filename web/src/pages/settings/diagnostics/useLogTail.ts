import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchLogTail, type LogLine } from '../../../lib/api';

/**
 * useLogTail follows the log by sequence number, asking only for the lines after
 * the cursor. It keeps a running count of lines the server's buffer dropped
 * between polls so the card can show the gap, and it polls only while following.
 */

/** How often a following view asks for what is new. */
const INTERVAL_MS = 2000;

export interface LogTail {
  lines: LogLine[];
  /** Running total of lines the server's buffer threw away between polls. */
  dropped: number;
  /** How many lines the server keeps at most, for the card's own wording. */
  capacity: number;
  /** The source buckets the server offers, in its own order. */
  sources: string[];
  loading: boolean;
  failed: boolean;
  /** Start again from the beginning, which also clears the gap count. */
  reload: () => void;
}

export function useLogTail(follow: boolean): LogTail {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [dropped, setDropped] = useState(0);
  const [capacity, setCapacity] = useState(0);
  const [sources, setSources] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);

  // A ref, because the interval closes over it once.
  const cursor = useRef(0);
  const live = useRef(true);
  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
    };
  }, []);

  const take = useCallback((since: number) => {
    return fetchLogTail(since).then(
      (tail) => {
        if (!live.current) return;
        cursor.current = tail.newest;
        setCapacity(tail.capacity);
        setSources(tail.sources);
        if (tail.dropped > 0) setDropped((n) => n + tail.dropped);
        setLines((old) => {
          const next = since === 0 ? tail.entries : [...old, ...tail.entries];
          // Trimmed to the server's own capacity so a tab left open does not grow without end.
          return tail.capacity > 0 && next.length > tail.capacity ? next.slice(next.length - tail.capacity) : next;
        });
        setFailed(false);
        setLoading(false);
      },
      () => {
        if (!live.current) return;
        // A failed poll keeps the lines on screen; only the first fetch reports failure.
        setLoading(false);
        if (since === 0) setFailed(true);
      },
    );
  }, []);

  const reload = useCallback(() => {
    cursor.current = 0;
    setDropped(0);
    setLoading(true);
    void take(0);
  }, [take]);

  useEffect(() => {
    void take(0);
  }, [take]);

  useEffect(() => {
    if (!follow) return;
    const id = window.setInterval(() => void take(cursor.current), INTERVAL_MS);
    return () => window.clearInterval(id);
  }, [follow, take]);

  return { lines, dropped, capacity, sources, loading, failed, reload };
}
