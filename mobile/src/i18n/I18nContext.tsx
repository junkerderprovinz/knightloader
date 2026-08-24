import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';
import { getLocales } from 'expo-localization';
import { en, type Dict, type TranslationKey } from './en';
import { AVAILABLE, load, loaded } from './index';
import { getLanguageOverride, setLanguageOverride } from '../storage/languagePreference';

export type { TranslationKey, Dict };

// detectDeviceLanguage reads the OS-level language list (expo-localization
// wraps the platform API) and picks the first one this app actually has a
// dictionary for - the same "closest available match, else English" logic
// as the web UI's own detect() (lib/i18n.tsx), just off the device's own
// setting instead of navigator.language.
export function detectDeviceLanguage(): string {
  for (const locale of getLocales()) {
    const code = locale.languageCode?.toLowerCase();
    if (code && AVAILABLE.includes(code)) return code;
  }
  return 'en';
}

interface I18nAPI {
  t: (key: TranslationKey, vars?: Record<string, string | number>) => string;
  lang: string;
  /** null clears the override and goes back to following the device. */
  setLanguage: (code: string | null) => void;
}

const Ctx = createContext<I18nAPI>({
  t: (k) => en[k],
  lang: 'en',
  setLanguage: () => {},
});

export const useT = () => useContext(Ctx);

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState(detectDeviceLanguage);
  // The chosen language's dictionary arrives asynchronously (its chunk has
  // to load); until it does, English stands in rather than the UI showing
  // raw keys - same reasoning as the web UI's own I18nProvider.
  const [dict, setDict] = useState<Dict>(() => loaded(lang) ?? en);

  // A saved manual override (Settings' language picker) beats the device
  // setting once it's read back, but the device-detected language is what
  // renders in the meantime rather than a loading flash.
  useEffect(() => {
    let current = true;
    getLanguageOverride().then((override) => {
      if (current && override && AVAILABLE.includes(override)) setLangState(override);
    });
    return () => {
      current = false;
    };
  }, []);

  useEffect(() => {
    let current = true;
    load(lang).then((d) => {
      if (current) setDict(d);
    });
    return () => {
      current = false;
    };
  }, [lang]);

  const setLanguage = useCallback((code: string | null) => {
    setLanguageOverride(code);
    setLangState(code ?? detectDeviceLanguage());
  }, []);

  const t = useCallback(
    (key: TranslationKey, vars?: Record<string, string | number>) => {
      let s: string = dict[key] ?? en[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [dict]
  );

  return <Ctx.Provider value={{ t, lang, setLanguage }}>{children}</Ctx.Provider>;
}
