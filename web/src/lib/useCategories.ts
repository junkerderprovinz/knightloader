import { useEffect, useState } from 'react';

import { apiBase, connectWS, fetchSettings, type Category } from './api';

/**
 * useCategories reads the categories a link can be filed under, and again
 * whenever the settings change. A peer's categories are not readable from
 * here, so the list stays empty for one.
 */
export function useCategories(base: string): Category[] {
  const [loaded, setLoaded] = useState<{ base: string; list: Category[] } | null>(null);

  useEffect(() => {
    if (base !== apiBase('')) return;
    let live = true;
    const load = () =>
      fetchSettings().then(
        (s) => {
          if (live) setLoaded({ base, list: s.categories ?? [] });
        },
        () => {
          // An empty list leaves the choice out.
        },
      );
    void load();
    const close = connectWS((type) => type === 'settings' && void load(), ['settings']);
    return () => {
      live = false;
      close();
    };
  }, [base]);

  return loaded?.base === base ? loaded.list : [];
}
