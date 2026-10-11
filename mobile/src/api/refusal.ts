import type { TranslationKey } from '../i18n/en';

export class ApiError extends Error {
  status: number;
  /** Names the reason where the server sent one, so it can be translated. */
  code?: string;

  constructor(message: string, status: number, code?: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

// refusal reads a refused call. A refusal the interface can translate comes as
// {error, code}; the message is then the sentence, not the envelope.
function refusal(body: string, status: number): ApiError {
  try {
    const p = JSON.parse(body) as { error?: unknown; code?: unknown };
    if (p && typeof p.error === 'string') {
      return new ApiError(p.error, status, typeof p.code === 'string' ? p.code : undefined);
    }
  } catch {
    // Plain text.
  }
  return new ApiError(body, status);
}

/**
 * refusedCall is the error for a call the instance answered with status. A
 * direct connection signs in with an API token, and the instance drops every
 * token when its password is changed or reset. This app has nowhere to enter
 * a new one, so the connection is made again from the phrase. A peer's 401
 * comes through another base and keeps its own words.
 */
export function refusedCall(direct: boolean, base: string, status: number, text: string): ApiError {
  if (status === 401 && direct && base === '/api') {
    return new ApiError(text, status, 'connectionEnded');
  }
  // An instance that was reset answers the old words with a plain 410
  // "reset" (internal/api/retired_group.go).
  if (status === 410 && text === 'reset') {
    return new ApiError(text, status, 'groupReset');
  }
  return refusal(text, status);
}

// The refusals this app words itself. The rest show the server's sentence.
// The member refusals are the ones an instance answers when it will not take
// this phone's key (internal/federation/members.go); check-member-refusals.mjs
// holds them to the server's codes.
const REFUSALS: Partial<Record<string, TranslationKey>> = {
  federationOff: 'error.federationOff',
  addAgain: 'error.addAgain',
  connectionEnded: 'error.connectionEnded',
  groupReset: 'error.groupReset',
  ownLoginNeeded: 'error.ownLoginNeeded',
  memberPending: 'error.memberPending',
  memberKeyChanged: 'error.memberKeyChanged',
  memberUnsigned: 'error.memberUnsigned',
  memberBusy: 'error.memberBusy',
  groupFull: 'error.groupFull',
  groupJoinsBusy: 'error.groupJoinsBusy',
  memberNotPinned: 'error.memberNotPinned',
};

/** errorText is what a failed call shows: translated where the code is known. */
export function errorText(t: (key: TranslationKey) => string, e: unknown): string {
  const key = e instanceof ApiError && e.code && Object.hasOwn(REFUSALS, e.code) ? REFUSALS[e.code] : undefined;
  if (key) return t(key);
  return e instanceof Error ? e.message : String(e);
}
