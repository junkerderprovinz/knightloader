import { useT, type TranslationKey } from '../../lib/i18n';
import { en } from '../../lib/locales/en';

/**
 * useTx is `t` under the name the settings tree already calls.
 *
 * It used to be `t` for keys the catalogue did not have yet, backed by an
 * English fallback table in strings.ts. Those keys are now in en.ts and in all
 * 41 other locales, so the table is gone and `tx` is plain `t` — and not one of
 * the dozen call sites had to change, which is why the strings went through a
 * destination key instead of being written as literals.
 *
 * The alias stays rather than the pages switching to `useT`: the two names cost
 * nothing, and a sub-page that still says `tx` is not wrong about anything.
 */
export function useTx(): {
  t: (key: TranslationKey, vars?: Record<string, string | number>) => string;
  tx: (key: TranslationKey, vars?: Record<string, string | number>) => string;
} {
  const { t } = useT();
  return { t, tx: t };
}

/**
 * label is a name for something the server identified by id — a module, a page.
 * An id with no string falls back to the id itself, because a module a later
 * wave adds must show up unlabelled rather than as a blank row that reads as a
 * rendering fault.
 *
 * The membership test is against `en`, not against the active dictionary: every
 * locale is typed as Dict and therefore has exactly the same keys, and English
 * is the one that is loaded synchronously — asking the fetched dictionary would
 * turn a label into the raw id for as long as its chunk is in flight.
 */
export function label(
  tx: (key: TranslationKey) => string,
  prefix: 'settings.module.' | 'settings.nav.',
  id: string,
): string {
  const key = (prefix + id) as TranslationKey;
  return key in en ? tx(key) : id;
}
