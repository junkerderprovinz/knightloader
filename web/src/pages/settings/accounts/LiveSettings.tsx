import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { connectWS, fetchSettings, patchSettings, type Settings } from '../../../lib/api';
import { useT } from '../../../lib/i18n';
import { useToast } from '../../../lib/toast';
import { SettingsProvider, type FeatureAccess, type SettingsDraft } from '../context';
import { fetchFeatures, setFeature, type FeatureState } from '../features';

// Settings cards drawn on a page outside the settings shell, such as the
// captcha order on the Accounts page. There is no draft there to autosave, so
// every edit is sent on its own, the way the Free downloads switch is.

// Long enough to type "60" into a field the server clamps to 10 and up.
const SAVE_AFTER_MS = 600;

/**
 * LiveSettings gives the cards inside it the settings and the module registry
 * the settings shell would. An edit shows at once and is sent shortly after,
 * together with the edits made in between, and a change saved anywhere else
 * arrives with the settings broadcast. Nothing is drawn until both have loaded.
 */
export function LiveSettings({ children }: { children: ReactNode }) {
  const { t } = useT();
  const { toast } = useToast();
  const [cfg, setCfg] = useState<Settings | null>(null);
  const [features, setFeatures] = useState<FeatureState | null>(null);
  const pending = useRef<Partial<Settings>>({});
  const timer = useRef(0);
  // Numbers the saves, so only the newest one's reply is drawn: an older one
  // would put back what was edited after it.
  const saves = useRef(0);

  useEffect(() => {
    let live = true;
    const load = () =>
      fetchSettings().then(
        (s) => {
          // An edit on its way outranks the broadcast it will cause.
          if (live && Object.keys(pending.current).length === 0) setCfg(s);
        },
        () => {},
      );
    void load();
    fetchFeatures().then(
      (f) => live && setFeatures(f),
      () => {},
    );
    const close = connectWS((type) => type === 'settings' && void load(), ['settings']);
    return () => {
      live = false;
      close();
      window.clearTimeout(timer.current);
    };
  }, []);

  const save = useCallback(async () => {
    const fields = pending.current;
    pending.current = {};
    const save = ++saves.current;
    try {
      const saved = await patchSettings(fields);
      if (save === saves.current && Object.keys(pending.current).length === 0) setCfg(saved);
    } catch (e) {
      toast(t('list.failed', { error: e instanceof Error ? e.message : String(e) }), 'fail');
      const stored = await fetchSettings().catch(() => null);
      if (save === saves.current) setCfg(stored);
    }
  }, [t, toast]);

  const patch = useCallback(
    (fields: Partial<Settings>) => {
      setCfg((cur) => (cur ? { ...cur, ...fields } : cur));
      pending.current = { ...pending.current, ...fields };
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => void save(), SAVE_AFTER_MS);
    },
    [save],
  );

  if (!cfg || !features) return null;

  const draft: SettingsDraft = {
    cfg,
    saved: cfg,
    patch,
    replace: (next) => patch(next),
    dirty: false,
    patchNow: async (fields) => {
      patch(fields);
      window.clearTimeout(timer.current);
      await save();
    },
    reseed: () => {},
    fieldError: () => undefined,
    showRefusalsAt: () => () => {},
  };
  const access: FeatureAccess = {
    features,
    toggle: async (id, enabled) => setFeatures(await setFeature(id, enabled)),
  };
  return (
    <SettingsProvider draft={draft} features={access}>
      {children}
    </SettingsProvider>
  );
}
