// What went wrong with a download or an unpacking and what to do about it, in
// the reader's language, from the code the server sends beside its own
// sentence (core.ErrorCode in internal/core/errorcode.go). The sentence stays
// available as the technical detail; it is never the message.

import type { TranslationKey } from './i18n';
import { rejectionReason } from './rejectionReason';

type Translate = (key: TranslationKey, vars?: Record<string, string | number>) => string;

interface Words {
  /** What went wrong, short enough for a row. */
  line: TranslationKey;
  /** The next step. */
  next: TranslationKey;
}

/**
 * Every code the server sends, one entry each. A code this build does not know
 * yet, from a newer server, reads as the general sentence, so adding one on the
 * server without words here costs a vague row and nothing worse.
 */
const WORDS: Record<string, Words> = {
  gone: { line: 'failure.gone.line', next: 'failure.gone.next' },
  accessDenied: { line: 'failure.accessDenied.line', next: 'failure.accessDenied.next' },
  premiumNeeded: { line: 'failure.premiumNeeded.line', next: 'failure.premiumNeeded.next' },
  limit: { line: 'failure.limit.line', next: 'failure.limit.next' },
  unavailable: { line: 'failure.unavailable.line', next: 'failure.unavailable.next' },
  unreachable: { line: 'failure.unreachable.line', next: 'failure.unreachable.next' },
  timeout: { line: 'failure.timeout.line', next: 'failure.timeout.next' },
  diskFull: { line: 'failure.diskFull.line', next: 'failure.diskFull.next' },
  noPermission: { line: 'failure.noPermission.line', next: 'failure.noPermission.next' },
  localFile: { line: 'failure.localFile.line', next: 'failure.localFile.next' },
  unsupported: { line: 'failure.unsupported.line', next: 'failure.unsupported.next' },
  hostExcluded: { line: 'failure.hostExcluded.line', next: 'failure.hostExcluded.next' },
  debridRefused: { line: 'failure.debridRefused.line', next: 'failure.debridRefused.next' },
  pinned: { line: 'failure.pinned.line', next: 'failure.pinned.next' },
  fileExists: { line: 'failure.fileExists.line', next: 'failure.fileExists.next' },
  captcha: { line: 'failure.captcha.line', next: 'failure.captcha.next' },
  cancelled: { line: 'failure.cancelled.line', next: 'failure.cancelled.next' },
  botCheck: { line: 'failure.botCheck.line', next: 'failure.botCheck.next' },
  membersOnly: { line: 'failure.membersOnly.line', next: 'failure.membersOnly.next' },
  geoBlocked: { line: 'failure.geoBlocked.line', next: 'failure.geoBlocked.next' },
  drm: { line: 'failure.drm.line', next: 'failure.drm.next' },
  extractorBroken: { line: 'failure.extractorBroken.line', next: 'failure.extractorBroken.next' },
  unsupportedPlayer: { line: 'failure.unsupportedPlayer.line', next: 'failure.unsupportedPlayer.next' },
  archiveDamaged: { line: 'failure.archiveDamaged.line', next: 'failure.archiveDamaged.next' },
  archivePassword: { line: 'failure.archivePassword.line', next: 'failure.archivePassword.next' },
  archivePartMissing: { line: 'failure.archivePartMissing.line', next: 'failure.archivePartMissing.next' },
  archiveUnsupported: { line: 'failure.archiveUnsupported.line', next: 'failure.archiveUnsupported.next' },
  archiveFolderExists: { line: 'failure.archiveFolderExists.line', next: 'failure.archiveFolderExists.next' },
};

const GENERAL: Words = { line: 'failure.unknown.line', next: 'failure.unknown.next' };

// The reasons whose code has another name, as core.Reason.Code maps them. A
// task stored before codes existed has only its reason.
const REASON_CODE: Record<string, string> = { auth: 'accessDenied', network: 'unreachable' };

// What to do about a link the queue rejected, by its reject code
// (lib/rejectionReason.ts words the rejection itself).
const REJECTION_NEXT: Record<string, TranslationKey> = {
  filterRule: 'failure.filterRule.next',
  filterRuleReason: 'failure.filterRule.next',
  bannedTracker: 'failure.bannedTracker.next',
};

/** A failure as the interface shows it. */
export interface Explained {
  line: string;
  next: string;
  /** The server's own sentence, for support. May be empty. */
  raw: string;
}

/** What a failure carries: its sentence, its code, and the task's own reason. */
export interface FailureSource {
  error?: string;
  errorCode?: string;
  errorParams?: Record<string, string>;
  /** Read when there is no code, as on a task stored before codes existed. */
  reason?: string;
  /** Set instead of a code when the link filter or the tracker ban refused
   *  to start the link. */
  rejectCode?: string;
  rejectParams?: Record<string, string>;
}

/**
 * explainFailure words a failure, or answers null when there is none.
 * `fallback` fills a value the wording needs and the server left out, such as
 * the archive's own name for a part it did not name.
 */
export function explainFailure(
  t: Translate,
  f: FailureSource,
  fallback: Record<string, string> = {},
): Explained | null {
  const raw = f.error ?? '';
  if (f.rejectCode) {
    return {
      line: rejectionReason(t, f.rejectCode, f.rejectParams, raw),
      next: t(REJECTION_NEXT[f.rejectCode] ?? GENERAL.next),
      raw,
    };
  }
  if (!raw && !f.errorCode) return null;
  const code = f.errorCode || (f.reason && (REASON_CODE[f.reason] ?? f.reason));
  const words = (code && WORDS[code]) || GENERAL;
  const vars = { ...fallback, ...f.errorParams };
  return { line: t(words.line, vars), next: t(words.next, vars), raw };
}
