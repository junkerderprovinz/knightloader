import { useCallback, useEffect, useState } from 'react';
import { fetchFeatures, setFeature, type Feature } from '../pages/settings/features';

/**
 * The Click'n'Load listener's live state, for the one control that shows it
 * outside the settings (jdp, 2026-09-07: "Da könnten wir auch zwei
 * schaltflächen für die beiden optionen im Linksammler einfügen").
 *
 * Its own small fetch rather than the settings shell's FeatureCtx: that
 * context is provided by the settings shell alone, so a component on the
 * collector page asking for it throws by design. The registry answers with the
 * whole table on every switch, which is why this keeps the one row it cares
 * about and drops the rest.
 *
 * `row` is null while the fetch is in flight and stays null where the server
 * has no such module at all - a desktop build, or an older instance a browser
 * tab is still pointed at. Callers render nothing in that case rather than a
 * switch over a listener that is not there.
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
    void fetchFeatures()
      .then((f) => {
        if (alive) setRow(f.modules.find((m) => m.id === 'cnl') ?? null);
      })
      // Silent: this powers one optional badge, and an instance that cannot
      // answer /api/features has a louder problem being reported elsewhere.
      .catch(() => {});
    return () => {
      alive = false;
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
