import { useT, type TranslationKey } from '../../lib/i18n';
import { en } from '../../lib/locales/en';

/** useTx returns `t` under the name `tx` that the settings pages use. */
export function useTx(): {
  t: (key: TranslationKey, vars?: Record<string, string | number>) => string;
  tx: (key: TranslationKey, vars?: Record<string, string | number>) => string;
} {
  const { t } = useT();
  return { t, tx: t };
}

/**
 * label names something the server identified by id, such as a module or a
 * page, and falls back to the id so an unknown one still shows up. The key is
 * looked up in `en` because it is loaded synchronously and every locale has the
 * same keys.
 */
export function label(
  tx: (key: TranslationKey) => string,
  prefix: 'settings.module.' | 'settings.nav.' | ChoicePrefix,
  id: string,
): string {
  const key = (prefix + id) as TranslationKey;
  return key in en ? tx(key) : id;
}

type ChoicePrefix = 'settings.advanced.mirror.' | 'settings.advanced.offline.' | 'settings.advanced.reclaim.';

/** choices turns the ids of a server-sent menu into tabs, labelled as label() does. */
export function choices(tx: (key: TranslationKey) => string, prefix: ChoicePrefix, ids: string[]) {
  return ids.map((id) => ({ id, label: label(tx, prefix, id) }));
}
