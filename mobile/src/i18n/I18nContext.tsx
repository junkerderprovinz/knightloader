import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';
import { getLocales } from 'expo-localization';
import { en, type Dict, type TranslationKey } from './en';
import { AVAILABLE, load, loaded } from './index';
import { isolate, setRightToLeft } from './bidi';
import { LANGUAGES } from './catalogue';
import { getLanguageOverride, setLanguageOverride } from '../storage/languagePreference';

export type { TranslationKey, Dict };

// detectDeviceLanguage reads the OS-level language list (expo-localization
// wraps the platform API) and picks the first one this app has a dictionary
// for: the same closest-match-else-English rule as the web UI's detect() in
// lib/i18n.tsx, off the device setting instead of navigator.language.
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
  // The chosen language's dictionary arrives asynchronously; until it does,
  // English stands in rather than the UI showing raw keys, as in the web UI's
  // I18nProvider.
  const [dict, setDict] = useState<Dict>(() => loaded(lang) ?? en);

  // A saved override from the language picker beats the device setting once it
  // is read back; the device-detected language renders in the meantime rather
  // than a loading flash.
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
      if (!current) return;
      // Before the dictionary, so the render it causes already isolates.
      setRightToLeft(LANGUAGES.some((l) => l.code === lang && l.rtl));
      setDict(d);
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
      // A text value is isolated so it keeps its own order in a right-to-left
      // sentence; a number is not, since digits already stay together and an
      // isolate would turn "{n}/{total}" into "5/1". The value goes in through
      // a function, since a replacement string would read "$&" in a name as a
      // pattern.
      if (vars) {
        for (const [k, v] of Object.entries(vars)) {
          const text = typeof v === 'number' ? String(v) : isolate(v);
          s = s.replaceAll(`{${k}}`, () => text);
        }
      }
      return s;
    },
    [dict]
  );

  return <Ctx.Provider value={{ t, lang, setLanguage }}>{children}</Ctx.Provider>;
}
