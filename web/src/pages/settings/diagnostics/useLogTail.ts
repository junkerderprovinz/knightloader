import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchLogTail, type LogLine } from '../../../lib/api';

/**
 * The log as a live tail: what has been logged, and a way to keep asking for
 * what is new without re-fetching everything.
 *
 * WHY A CURSOR AND NOT A REFRESH. The old card fetched the whole diagnostics
 * bundle and re-rendered five hundred lines. Following that way means asking
 * for all five hundred every two seconds, comparing them client-side, and
 * hoping: two identical lines cannot be told apart, and a burst that pushed the
 * whole buffer over between two polls looks exactly like a quiet period. The
 * server hands out a sequence number per line instead, so this asks for
 * "everything after 412" and gets only that.
 *
 * WHY `dropped` IS KEPT AND NOT SWALLOWED. A busy instance can log more than
 * the buffer holds in the two seconds between polls. The server counts what
 * fell out between the cursor and its own oldest surviving line, and this keeps
 * the running total so the card can SAY there is a hole. Joining the two halves
 * silently would be a log that reads as continuous and is not, which is the one
 * failure a diagnostic view must never have.
 *
 * WHY THE POLL ONLY EXISTS WHILE FOLLOWING. A settings tab nobody is looking at
 * must not keep a laptop warm - the maintenance card one folder over makes the
 * same argument for its own interval. Switching following off leaves the lines
 * exactly where they are, which is the whole point of the switch: you can read
 * something without it sliding away.
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

  // The cursor lives in a ref and not in state: the interval closes over it
  // once, and a cursor in state would leave every tick after the first asking
  // with the value the effect was created with - re-sending the same lines for
  // ever while looking like it worked.
  const cursor = useRef(0);
  // Guards every setState against a response that arrives after the card is
  // gone, which React would otherwise report as an update on an unmounted
  // component and which would, worse, advance a cursor nobody is reading.
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
          // Trimmed to what the server itself keeps, so the card's "the last N
          // lines" stays true and a tab left following overnight does not grow
          // an array without end.
          return tail.capacity > 0 && next.length > tail.capacity ? next.slice(next.length - tail.capacity) : next;
        });
        setFailed(false);
        setLoading(false);
      },
      () => {
        if (!live.current) return;
        // A poll that failed is not an empty log. The lines already on screen
        // stay exactly where they are, and only the first fetch of all - the
        // one with nothing to show yet - reports a failure the card can draw.
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

  // The first fetch, once.
  useEffect(() => {
    void take(0);
  }, [take]);

  // The poll, created only while following and cleared by the effect's own
  // return, so a card sitting with the switch off costs nothing at all.
  useEffect(() => {
    if (!follow) return;
    const id = window.setInterval(() => void take(cursor.current), INTERVAL_MS);
    return () => window.clearInterval(id);
  }, [follow, take]);

  return { lines, dropped, capacity, sources, loading, failed, reload };
}
