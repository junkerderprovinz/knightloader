import { useCallback } from 'react';
import { useT, type TranslationKey } from '../../lib/i18n';
import { PENDING, type PendingKey } from './strings';

/**
 * useTx is `t` for a key that may not be in the catalogue yet.
 *
 * The settings tree needs about ninety strings the locale files do not have,
 * and the locale files are one writer's lane per wave. Rather than shipping
 * English literals scattered through a dozen components — which is a hunt when
 * the translation wave arrives — every one of them goes through here against
 * the key it is destined for.
 *
 * The lookup asks the real catalogue first, so nothing in this directory has to
 * be touched when those keys land: the day `settings.nav.general` exists in
 * en.ts, this stops falling back and starts translating.
 */
export function useTx(): {
  t: (key: TranslationKey) => string;
  tx: (key: PendingKey, vars?: Record<string, string | number>) => string;
} {
  const { t } = useT();
  const tx = useCallback(
    (key: PendingKey, vars?: Record<string, string | number>) => {
      // The cast is the whole point: the key is not in the union yet. It is
      // narrow — only keys that exist in PENDING can be passed — and it goes
      // away with the fallback table.
      const translated = t(key as unknown as TranslationKey) as string | undefined;
      let s: string = translated ?? PENDING[key];
      if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
      return s;
    },
    [t],
  );
  return { t, tx };
}

/**
 * label is a name for something the server identified by id — a module, a page.
 * An id with no string falls back to the id itself, because a module a later
 * wave adds must show up unlabelled rather than as a blank row that reads as a
 * rendering fault.
 */
export function label(
  tx: (key: PendingKey) => string,
  prefix: 'settings.module.' | 'settings.nav.',
  id: string,
): string {
  const key = (prefix + id) as PendingKey;
  return key in PENDING ? tx(key) : id;
}
