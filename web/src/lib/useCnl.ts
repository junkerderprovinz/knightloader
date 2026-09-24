import { useCallback, useEffect, useState } from 'react';
import { fetchFeatures, setFeature, type Feature } from '../pages/settings/features';
import { connectWS } from './api';

/**
 * useCnl is the Click'n'Load listener's state for the collector's switch.
 * It fetches on its own, because FeatureCtx only exists inside the settings
 * shell. `row` is null while loading and where the server has no such module
 * (the desktop build, an older instance); callers then render nothing.
 */
export function useCnl(): {
  row: Feature | null;
  busy: boolean;
  set: (on: boolean) => Promise<void>;
} {
  const [row, setRow] = useState<Feature | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let alive = true;
    const load = () =>
      void fetchFeatures()
        .then((f) => {
          if (alive) setRow(f.modules.find((m) => m.id === 'cnl') ?? null);
        })
        // Silent: an instance that cannot answer /api/features reports it elsewhere.
        .catch(() => {});
    load();
    // Again whenever the switch moves elsewhere, such as on the Modules page.
    const close = connectWS((type) => type === 'settings' && load(), ['settings']);
    return () => {
      alive = false;
      close();
    };
  }, []);

  const set = useCallback(async (on: boolean) => {
    setBusy(true);
    try {
      const f = await setFeature('cnl', on);
      setRow(f.modules.find((m) => m.id === 'cnl') ?? null);
    } finally {
      setBusy(false);
    }
  }, []);

  return { row, busy, set };
}
