import { AVAILABLE } from './index';

export interface LanguageDef {
  code: string;
  label: string;
  /**
   * ISO 3166-1 alpha-2 region, or an ISO 3166-2 subdivision like "es-ct" -
   * the exact same field the web UI's own lib/i18n.tsx carries, kept
   * identical on purpose so the two language lists can be diffed against
   * each other rather than drifting into two slightly different opinions
   * about which flag belongs to which language.
   */
  flag: string;
}

// Mirrors the web UI's own lib/i18n.tsx CATALOGUE - same languages, same
// native labels, same order, same flag regions. Filtered to AVAILABLE the
// same way the web UI filters its own, so a language listed here always
// means a real shipped translation, never a fallback to English in disguise.
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
  { code: 'ar', label: 'العربية', flag: 'sa' },
  { code: 'he', label: 'עברית', flag: 'il' },
  { code: 'th', label: 'ไทย', flag: 'th' },
  { code: 'vi', label: 'Tiếng Việt', flag: 'vn' },
  { code: 'bg', label: 'Български', flag: 'bg' },
  { code: 'sk', label: 'Slovenčina', flag: 'sk' },
  { code: 'sl', label: 'Slovenščina', flag: 'si' },
  { code: 'hr', label: 'Hrvatski', flag: 'hr' },
  { code: 'sr', label: 'Српски', flag: 'rs' },
  { code: 'lt', label: 'Lietuvių', flag: 'lt' },
  { code: 'lv', label: 'Latviešu', flag: 'lv' },
  { code: 'et', label: 'Eesti', flag: 'ee' },
  { code: 'is', label: 'Íslenska', flag: 'is' },
  // The three languages of Spain get their own regional flags on the web,
  // where flag-icons ships real es-ct/es-ga/es-pv artwork. Emoji has no
  // renderable equivalent (see flagEmoji below), so here they fall back to
  // the Spanish flag and the native label alone tells them apart.
  { code: 'ca', label: 'Català', flag: 'es-ct' },
  { code: 'gl', label: 'Galego', flag: 'es-ga' },
  { code: 'eu', label: 'Euskara', flag: 'es-pv' },
  { code: 'id', label: 'Bahasa Indonesia', flag: 'id' },
  { code: 'ms', label: 'Bahasa Melayu', flag: 'my' },
  { code: 'hi', label: 'हिन्दी', flag: 'in' },
  { code: 'fa', label: 'فارسی', flag: 'ir' },
];

export const LANGUAGES: LanguageDef[] = CATALOGUE.filter((l) => AVAILABLE.includes(l.code));

/**
 * flagEmoji turns a catalogue `flag` region into the emoji flag for it.
 *
 * Emoji rather than images because the web UI's flag-icons CSS has no React
 * Native equivalent, and 42 bundled PNGs would be 42 assets to keep in sync
 * with a list that already lives in one place. A country flag is a pair of
 * regional-indicator codepoints derived arithmetically from the letters, so
 * this needs no per-language table at all and cannot fall out of step with
 * the catalogue above.
 *
 * Subdivisions (es-ct Catalonia, es-ga Galicia, es-pv Basque Country) fall
 * back to their parent country's flag: Unicode only has tag-sequence flags
 * for England, Scotland and Wales in its recommended set, so a Catalan tag
 * sequence would render as blank or tofu on essentially every device rather
 * than as the flag it names. The parent flag is the honest degradation - the
 * native label next to it is what actually distinguishes those three.
 *
 * Rendering relies on the platform emoji font (Noto Color Emoji on Android
 * 7+, which is this app's minSdk, and Apple Color Emoji on iOS). A small
 * number of Android ROMs ship a font with the flag block removed; there the
 * label still reads correctly and only the glyph is missing.
 */
export function flagEmoji(flag: string): string {
  const country = flag.slice(0, 2).toUpperCase();
  if (!/^[A-Z]{2}$/.test(country)) return '';
  // 0x1F1E6 is REGIONAL INDICATOR SYMBOL LETTER A, in the same order as A-Z.
  return String.fromCodePoint(...[...country].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65));
}
