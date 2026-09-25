import { ApiError } from '../../lib/api';
import { useT, type TranslationKey } from '../../lib/i18n';
import { en } from '../../lib/locales/en';
import type { Feature } from './features';
import { moduleArgs } from './moduleArgs';

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

type Tx = (key: TranslationKey, vars?: Record<string, string | number>) => string;

/**
 * moduleDetail is a module's live line in the reader's language where the
 * server sent a code this build has words for, and the server's English
 * sentence otherwise, such as from a newer server.
 */
export function moduleDetail(tx: Tx, m: Feature): string | undefined {
  return moduleLine(tx, 'settings.modules.detail.', m.detailCode, m.detailArgs, m.detail);
}

/** moduleReason is moduleDetail for why a module has no switch or is not in this build. */
export function moduleReason(tx: Tx, m: Feature): string | undefined {
  return moduleLine(tx, 'settings.modules.reason.', m.reasonCode, m.reasonArgs, m.reason);
}

function moduleLine(
  tx: Tx,
  prefix: string,
  code: string | undefined,
  args: Record<string, string> | undefined,
  english: string | undefined,
): string | undefined {
  if (!code) return english;
  // A reconnect problem comes with the code the Reconnect page words already,
  // and with the method as it was typed, which an unknown one has to repeat.
  if (code.startsWith('reconnect.')) {
    const key = `settings.reconnect.reason.${code.slice('reconnect.'.length)}` as TranslationKey;
    return key in en ? tx(key, args) : english;
  }
  const key = (prefix + code) as TranslationKey;
  if (!(key in en)) return english;
  return tx(key, moduleArgs(args, (k) => (k in en ? tx(k as TranslationKey) : undefined)));
}

/**
 * refusalText words a save the server refused: by its code where this build has
 * words for it, and in the server's own sentence otherwise.
 */
export function refusalText(tx: Tx, e: unknown): string {
  if (e instanceof ApiError && e.code) {
    const key = `settings.${e.code}`.replace('settings.reconnect.', 'settings.reconnect.reason.') as TranslationKey;
    if (key in en) return tx(key, e.params);
  }
  return String(e).replace(/^(Error|ApiError):\s*/, '');
}

/**
 * switchRefusal words a module switch the server refused. A parked module with
 * nothing to bring back names the page it is set up on, in the rail's words.
 */
export function switchRefusal(tx: Tx, e: unknown): string {
  if (e instanceof ApiError && e.code === 'configureFirst') {
    return tx('settings.modules.configureFirst', { page: label(tx, 'settings.nav.', String(e.params?.page ?? '')) });
  }
  return String(e).replace(/^(Error|ApiError):\s*/, '');
}

export type ChoicePrefix =
  | 'settings.advanced.mirror.'
  | 'settings.advanced.offline.'
  | 'settings.advanced.reclaim.'
  | 'settings.resume.';

/** choices turns the ids of a server-sent menu into tabs, labelled as label() does. */
export function choices(tx: (key: TranslationKey) => string, prefix: ChoicePrefix, ids: string[]) {
  return ids.map((id) => ({ id, label: label(tx, prefix, id) }));
}
