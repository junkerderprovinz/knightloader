import { ApiError } from './api';

/**
 * The server takes any password up to 72 bytes, which is what bcrypt hashes.
 * Below twelve characters, counted by code point so every alphabet is held to
 * the same length, the page says the password is easy to guess and sets it all
 * the same.
 */
export const SHORT_PASSWORD_CHARS = 12;
export const MAX_PASSWORD_BYTES = 72;

/** isShort says whether password is typed but easy to guess for its length. */
export function isShort(password: string): boolean {
  const chars = [...password].length;
  return chars > 0 && chars < SHORT_PASSWORD_CHARS;
}

/** tooLong says whether the server would refuse password for its length. */
export function tooLong(password: string): boolean {
  return new TextEncoder().encode(password).length > MAX_PASSWORD_BYTES;
}

/** refusedTooLong reads the server's refusal of a password for its length. */
export function refusedTooLong(e: unknown): boolean {
  return e instanceof ApiError && e.code === 'passwordTooLong';
}
