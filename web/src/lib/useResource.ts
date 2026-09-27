import { useCallback, useEffect, useState } from 'react';

// useResource loads data once and exposes the three states a page must be able
// to render: loading, failed (with a retry), and loaded. Pages that skip the
// failure case show an empty shell forever when the server is unreachable.
//
// A change of `key` loads again, for a load that reads something the caller
// picks. What the last load brought stays on show until the next one answers.
export function useResource<T>(load: () => Promise<T>, key?: string) {
  const [data, setData] = useState<T | null>(null);
  const [failed, setFailed] = useState(false);
  const [nonce, setNonce] = useState(0);

  useEffect(() => {
    let alive = true;
    setFailed(false);
    load()
      .then((d) => alive && setData(d))
      .catch(() => alive && setFailed(true));
    return () => {
      alive = false;
    };
    // `load` is expected to be stable per page; reload() and `key` re-run it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nonce, key]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);
  return { data, failed, loading: data === null && !failed, reload, setData };
}
