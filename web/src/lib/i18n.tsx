import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';
import { en, type Dict, type TranslationKey } from './locales/en';
import { DICTS } from './locales';

export type { TranslationKey, Dict };

interface LanguageDef {
  code: string;
  label: string;
  /** ISO 3166-1 alpha-2 region code used by flag-icons (fi fi-XX). */
  flag: string;
  rtl?: boolean;
}

// The catalogue mirrors BombVault's, so the two apps offer the same choice.
// Only entries with a shipped dictionary are offered — a language in the menu
// always means a translated app, never an English fallback in disguise.
const CATALOGUE: LanguageDef[] = [
  { code: 'en', label: 'English', flag: 'gb' },
  { code: 'de', label: 'Deutsch', flag: 'de' },
  { code: 'fr', label: 'Français', flag: 'fr' },
  { code: 'es', label: 'Español', flag: 'es' },
  { code: 'it', label: 'Italiano', flag: 'it' },
  { code: 'pt', label: 'Português', flag: 'pt' },
  { code: 'nl', label: 'Nederlands', flag: 'nl' },
  { code: 'pl', label: 'Polski', flag: 'pl' },
  { code: 'ru', label: 'Русский', flag: 'ru' },
  { code: 'uk', label: 'Українська', flag: 'ua' },
  { code: 'cs', label: 'Čeština', flag: 'cz' },
  { code: 'sv', label: 'Svenska', flag: 'se' },
  { code: 'da', label: 'Dansk', flag: 'dk' },
  { code: 'fi', label: 'Suomi', flag: 'fi' },
  { code: 'no', label: 'Norsk', flag: 'no' },
  { code: 'tr', label: 'Türkçe', flag: 'tr' },
  { code: 'el', label: 'Ελληνικά', flag: 'gr' },
  { code: 'hu', label: 'Magyar', flag: 'hu' },
  { code: 'ro', label: 'Română', flag: 'ro' },
  { code: 'ja', label: '日本語', flag: 'jp' },
  { code: 'ko', label: '한국어', flag: 'kr' },
  { code: 'zh', label: '中文', flag: 'cn' },
  { code: 'ar', label: 'العربية', flag: 'sa', rtl: true },
  { code: 'he', label: 'עברית', flag: 'il', rtl: true },
  { code: 'th', label: 'ไทย', flag: 'th' },
  { code: 'vi', label: 'Tiếng Việt', flag: 'vn' },
];

export const LANGUAGES: LanguageDef[] = CATALOGUE.filter((l) => l.code in DICTS);

export type Lang = string;

const STORAGE_KEY = 'kl-lang';

function detect(): Lang {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored && stored in DICTS) return stored;
  const nav = navigator.language?.toLowerCase() ?? 'en';
  const exact = LANGUAGES.find((l) => nav === l.code || nav.startsWith(l.code + '-'));
  return exact?.code ?? 'en';
}

function applyDocumentLanguage(code: Lang): void {
  const def = CATALOGUE.find((l) => l.code === code);
  document.documentElement.setAttribute('lang', code);
  document.documentElement.setAttribute('dir', def?.rtl ? 'rtl' : 'ltr');
}

/** Applied at boot so <html lang>/<html dir> are right before first paint. */
export function applyStoredLanguage(): void {
  applyDocumentLanguage(detect());
}

interface I18nAPI {
  t: (key: TranslationKey, vars?: Record<string, string | number>) => string;
  lang: Lang;
  setLang: (l: Lang) => void;
  languages: LanguageDef[];
}

const Ctx = createContext<I18nAPI>({
  t: (k) => en[k],
  lang: 'en',
  setLang: () => {},
  languages: LANGUAGES,
});

export const useT = () => useContext(Ctx);

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(detect);

  useEffect(() => applyDocumentLanguage(lang), [lang]);

  const setLang = useCallback((l: Lang) => {
    localStorage.setItem(STORAGE_KEY, l);
    setLangState(l);
  }, []);

  const t = useCallback(
    (key: TranslationKey, vars?: Record<string, string | number>) => {
      let s: string = DICTS[lang]?.[key] ?? en[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [lang],
  );

  return <Ctx.Provider value={{ t, lang, setLang, languages: LANGUAGES }}>{children}</Ctx.Provider>;
}
