import { fetchFileOwner, type FileOwnerIdentity } from './api';
import type { TranslationKey } from './i18n';
import { useResource } from './useResource';

/** RunsAs is the account this instance runs as. */
export interface RunsAs {
  uid: number;
  gid: number;
}

type Translate = (k: TranslationKey, vars?: Record<string, string | number>) => string;

// One request per page load: the account is fixed when the process starts. A
// failed request is not kept, so the next hint asks again.
let pending: Promise<RunsAs | undefined> | undefined;

/** accountOf is the account a readout names, or undefined for Windows, which
 *  has no unix owners, and for root, which the sentence would call "not root". */
export function accountOf(id: FileOwnerIdentity): RunsAs | undefined {
  return id.known && id.uid !== 0 ? { uid: id.uid, gid: id.gid } : undefined;
}

function load(): Promise<RunsAs | undefined> {
  pending ??= fetchFileOwner().then(accountOf, (err: unknown) => {
    pending = undefined;
    throw err;
  });
  return pending;
}

/**
 * useRunsAs is the account for the hints that say which owner a folder or a
 * program needs. It is undefined until /api/fileowner answers, and where there
 * is no account to name.
 */
export function useRunsAs(): RunsAs | undefined {
  return useResource(load).data ?? undefined;
}

/** withRunsAs adds the sentence naming the account to a hint, or leaves the
 *  hint as it is when there is none to name. */
export function withRunsAs(t: Translate, hint: string, who: RunsAs | undefined): string {
  if (!who) return hint;
  return `${hint} ${t('settings.owner.runsAs', { uid: who.uid, gid: who.gid })}`;
}
