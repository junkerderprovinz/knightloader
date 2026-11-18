import { useEffect, useState } from 'react';

/**
 * useSettled is `value` once it has held still for `ms`, so something that
 * moves in many small steps, such as a count while a whole list is started,
 * is shown once at its end instead of at every step on the way.
 */
export function useSettled<T>(value: T, ms: number): T {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    if (Object.is(value, settled)) return;
    const timer = setTimeout(() => setSettled(value), ms);
    return () => clearTimeout(timer);
  }, [value, settled, ms]);
  return settled;
}
