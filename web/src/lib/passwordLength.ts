import { ApiError } from './api';
import type { TranslationKey } from './i18n';

/**
 * The lengths the server takes for a password: at least eight characters,
 * counted as it counts them, by code point, and at most 72 bytes, which is
 * what bcrypt hashes.
 */
export const MIN_PASSWORD_CHARS = 8;
export const MAX_PASSWORD_BYTES = 72;

export type LengthProblem = 'short' | 'long';

export const LENGTH_PROBLEM: Record<LengthProblem, TranslationKey> = {
  short: 'auth.passwordShort',
  long: 'auth.passwordLong',
};

/** lengthProblem says what is wrong with the length of password, if anything. */
export function lengthProblem(password: string): LengthProblem | undefined {
  if ([...password].length < MIN_PASSWORD_CHARS) return 'short';
  if (new TextEncoder().encode(password).length > MAX_PASSWORD_BYTES) return 'long';
  return undefined;
}

/** refusedLength reads the server's refusal of a password for its length. */
export function refusedLength(e: unknown): LengthProblem | undefined {
  if (!(e instanceof ApiError)) return undefined;
  if (e.code === 'passwordTooShort') return 'short';
  if (e.code === 'passwordTooLong') return 'long';
  return undefined;
}
